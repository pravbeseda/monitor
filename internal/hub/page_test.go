package hub_test

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/version"
)

var lastSeen = time.Date(2026, 8, 28, 10, 5, 0, 0, time.UTC)

// stored is a storage that answers with whatever the test put in it.
type stored struct {
	states     []storage.NodeState
	levels     []storage.State
	thresholds []storage.Threshold
	err        error
}

func (s stored) SaveIngest(context.Context, storage.Ingest) error { return nil }
func (s stored) Close() error                                     { return nil }

func (s stored) Snapshot(context.Context, []string) (storage.Snapshot, error) {
	return storage.Snapshot{Nodes: s.states, States: s.levels, Thresholds: s.thresholds}, s.err
}

func (s stored) Series(context.Context, storage.Selection) ([]storage.SeriesNewest, error) {
	return nil, s.err
}

func (s stored) Newest(context.Context, storage.Selection, time.Time) ([]storage.SeriesNewest, error) {
	return nil, s.err
}

func (s stored) Points(context.Context, storage.SeriesRef, time.Time, time.Time) iter.Seq2[storage.Point, error] {
	return func(func(storage.Point, error) bool) {}
}

// The threshold store: nothing is stored unless a test says otherwise. A store that keeps
// what it is given lives in thresholds_test.go.
func (s stored) ThresholdOf(context.Context, storage.SeriesRef) (storage.Threshold, bool, error) {
	return storage.Threshold{}, false, s.err
}

func (s stored) SaveThreshold(context.Context, storage.Threshold) error   { return s.err }
func (s stored) DeleteThreshold(context.Context, storage.SeriesRef) error { return s.err }

var laptop = storage.NodeState{
	Node:     "laptop-a",
	LastSeen: lastSeen,
	Values: []storage.Value{
		{
			Metric: "disk.free_bytes",
			Sensor: "disk",
			Labels: map[string]string{"mount": "/", "fs": "apfs", "removable": "false"},
			Value:  1.5e9,
			TS:     lastSeen,
		},
		{
			Metric: "disk.free_pct",
			Sensor: "disk",
			Labels: map[string]string{"mount": "/", "fs": "apfs", "removable": "false"},
			Value:  34.24,
			TS:     lastSeen,
		},
	},
}

func show(t *testing.T, store hub.Snapshots, target, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	rec := httptest.NewRecorder()
	hub.Page(hub.ReadState(store, configured(time.Minute, time.Hour), func() time.Time { return lastSeen })).ServeHTTP(rec, req)
	return rec
}

// configured is a configuration naming every node: each runs the disk sensor every interval
// and falls silent after silenceAfter.
func configured(interval, silenceAfter time.Duration) func(node string) (evaluate.Target, bool) {
	return func(node string) (evaluate.Target, bool) {
		return evaluate.Target{
			Node:         node,
			SilenceAfter: silenceAfter,
			Intervals:    map[string]time.Duration{"disk": interval},
		}, true
	}
}

func TestPageShowsEveryNodeWithItsLatestValues(t *testing.T) {
	rec := show(t, stored{states: []storage.NodeState{laptop}}, "/", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"laptop-a", "1.5 GB", "34.2%", "2026-08-28 10:05 UTC", "/", "apfs"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not show %q", want)
		}
	}
}

func TestPageShowsTheHubVersionBesideTheTitle(t *testing.T) {
	rec := show(t, stored{states: []storage.NodeState{laptop}}, "/", "")

	want := "<h1>Monitor <small>" + version.Current + "</small></h1>"
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("page = %q, want the heading %q", rec.Body.String(), want)
	}
}

func TestPageShowsEachNodesAgentVersionBesideItsName(t *testing.T) {
	upgraded := laptop
	upgraded.AgentVersion = "0.2.0"
	unknown := storage.NodeState{Node: "server-b", LastSeen: lastSeen}

	body := show(t, stored{states: []storage.NodeState{upgraded, unknown}}, "/", "").Body.String()

	if want := "<h2>laptop-a <small>0.2.0</small></h2>"; !strings.Contains(body, want) {
		t.Errorf("page = %q, want the heading %q", body, want)
	}
	if want := "<h2>server-b</h2>"; !strings.Contains(body, want) {
		t.Errorf("page = %q, want a node with no reported version headed %q", body, want)
	}
}

func TestPageFollowsTheRequestedLanguage(t *testing.T) {
	tests := []struct {
		name           string
		target         string
		acceptLanguage string
		want           string
	}{
		{"the query asks for Russian", "/?lang=ru", "", "1,5 ГБ"},
		{"the browser asks for Russian", "/", "ru-RU,ru;q=0.9", "1,5 ГБ"},
		{"the query overrules the browser", "/?lang=en", "ru-RU", "1.5 GB"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := show(t, stored{states: []storage.NodeState{laptop}}, tc.target, tc.acceptLanguage)

			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("page does not show %q", tc.want)
			}
		})
	}
}

func TestPageShowsAMetricWithNoUnitAsAPlainNumber(t *testing.T) {
	state := storage.NodeState{
		Node:     "server-b",
		LastSeen: lastSeen,
		Values:   []storage.Value{{Metric: "coffee.level", Labels: map[string]string{}, Value: 7.5, TS: lastSeen}},
	}

	rec := show(t, stored{states: []storage.NodeState{state}}, "/", "")

	if !strings.Contains(rec.Body.String(), "7.50") {
		t.Errorf("page = %q, want the raw value shown", rec.Body.String())
	}
}

func TestPageSaysSoWhenNoNodeHasReported(t *testing.T) {
	rec := show(t, stored{}, "/", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No node has reported yet") {
		t.Errorf("page = %q, want it to say the panel is empty", rec.Body.String())
	}
}

func TestPageFailsLoudlyWhenStorageDoes(t *testing.T) {
	rec := show(t, stored{err: errors.New("database is locked")}, "/", "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "database is locked") {
		t.Errorf("page = %q, want it to keep the internal error to the log", rec.Body.String())
	}
}

func TestRootIsMountedOnTheRoutes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	routes(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the page mounted on /", rec.Code)
	}
}

// spec: history.md#page — a series that stopped arriving is hidden when removable and marked
// otherwise; one that names no sensor ages by its node's longest interval.
func TestPageLeavesOutOrMarksSeriesThatStoppedArriving(t *testing.T) {
	const marker = "no fresh data"
	bound := evaluate.StaleFactor * time.Minute
	series := func(metric, mount, removable string, age time.Duration) storage.Value {
		return diskValue(metric, mount, removable, 1, age)
	}
	loose := func(mount string, age time.Duration) storage.Value {
		removable := "false"
		if strings.Contains(mount, "stick") {
			removable = "true"
		}
		value := diskValue("coffee.level", mount, removable, 1, age)
		value.Sensor = ""
		return value
	}
	unrun := func(mount, removable string) storage.Value {
		value := diskValue("coffee.level", mount, removable, 1, 0)
		value.Sensor = "coffee"
		return value
	}
	tests := []struct {
		name       string
		value      storage.Value
		wantShown  bool
		wantMarked bool
	}{
		{"a fresh series, or one reporting again", series("disk.free_pct", "/Volumes/stick-a", "true", 0), true, false},
		{"a series exactly at the bound", series("disk.free_pct", "/Volumes/stick-a", "true", bound), true, false},
		{"a removable series past the bound", series("disk.free_pct", "/Volumes/stick-a", "true", bound+time.Second), false, false},
		{"a fixed series past the bound", series("disk.free_pct", "/Volumes/data-a", "false", bound+time.Second), true, true},
		{"a series naming no sensor, inside its node's longest interval", loose("/Volumes/data-a", bound), true, false},
		{"a removable series naming no sensor, past that bound", loose("/Volumes/stick-a", bound+time.Second), false, false},
		{"a fixed series naming no sensor, past that bound", loose("/Volumes/data-a", bound+time.Second), true, true},
		{"a removable series whose node runs no such sensor", unrun("/Volumes/stick-a", "true"), false, false},
		{"a fixed series whose node runs no such sensor", unrun("/Volumes/data-a", "false"), true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{tc.value}}

			body := show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String()

			if shown := strings.Contains(body, tc.value.Labels["mount"]); shown != tc.wantShown {
				t.Errorf("shown = %v, want %v; page = %q", shown, tc.wantShown, body)
			}
			if marked := strings.Contains(body, marker); marked != tc.wantMarked {
				t.Errorf("marked = %v, want %v; page = %q", marked, tc.wantMarked, body)
			}
		})
	}
}

// spec: history.md#page — series age by the hub's clock, the age evaluation freezes by: a
// node that stopped reporting and one reporting with no measurements age alike.
func TestPageAgesSeriesByTheHubsClock(t *testing.T) {
	anHourAgo := lastSeen.Add(-time.Hour)
	values := []storage.Value{
		diskValue("disk.free_pct", "/Volumes/stick-a", "true", 1, time.Hour),
		diskValue("disk.free_pct", "/Volumes/data-a", "false", 1, time.Hour),
	}
	silent := storage.NodeState{Node: "laptop-a", LastSeen: anHourAgo, Values: values}
	heartbeatOnly := storage.NodeState{Node: "server-b", LastSeen: lastSeen, Values: values}

	body := show(t, stored{states: []storage.NodeState{silent, heartbeatOnly}}, "/", "").Body.String()

	if strings.Contains(body, "/Volumes/stick-a") {
		t.Errorf("page = %q, want the stick left out under both nodes", body)
	}
	if got := strings.Count(body, "no fresh data"); got != 2 {
		t.Errorf("%d rows marked, want the fixed volume marked under both nodes; page = %q", got, body)
	}
}

// spec: history.md#page — the mark is in the reader's language.
func TestPageMarksAStaleSeriesInTheReadersLanguage(t *testing.T) {
	fixed := diskValue("disk.free_pct", "/Volumes/data-a", "false", 1, time.Hour)
	state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{fixed}}

	body := show(t, stored{states: []storage.NodeState{state}}, "/?lang=ru", "").Body.String()

	if !strings.Contains(body, "нет свежих данных") {
		t.Errorf("page = %q, want the mark in Russian", body)
	}
}

// spec: state.md#staleness — the hub ages a series by the interval its node's configuration
// resolves, and a node the file no longer names resolves none, so nothing will refresh it.
func TestRootAgesSeriesByTheConfiguredInterval(t *testing.T) {
	// laptop-a is in the test configuration, in the class whose disk interval is an hour;
	// server-c is not named there at all.
	for _, tc := range []struct {
		node       string
		age        time.Duration
		wantMarked bool
	}{
		{"laptop-a", time.Minute, false},
		{"laptop-a", 30 * 24 * time.Hour, true},
		{"server-c", time.Minute, true},
	} {
		value := diskValue("disk.free_pct", "/Volumes/data-a", "false", 1, tc.age)
		state := storage.NodeState{Node: tc.node, LastSeen: lastSeen, Values: []storage.Value{value}}
		rec := httptest.NewRecorder()
		routesWith(t, stored{states: []storage.NodeState{state}}, func() time.Time { return lastSeen }).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if marked := strings.Contains(rec.Body.String(), "no fresh data"); marked != tc.wantMarked {
			t.Errorf("%s at %v: marked = %v, want %v", tc.node, tc.age, marked, tc.wantMarked)
		}
	}
}

// spec: history.md#page — a node whose every series was left out says it has nothing
// current, not that it never measured anything.
func TestPageSaysANodeWhoseSeriesAllVanishedHasNothingCurrent(t *testing.T) {
	stick := diskValue("disk.free_pct", "/Volumes/stick-a", "true", 1, time.Hour)
	state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{stick}}

	body := show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String()

	if !strings.Contains(body, "No current measurements") || strings.Contains(body, "No measurements yet") {
		t.Errorf("page = %q, want it to say nothing is current rather than nothing was measured", body)
	}
}

// spec: history.md#page — a node silent past its silence_after has its rows left out or
// marked in that same moment evaluation freezes them, before they are three intervals old.
func TestPageFreezesTheRowsOfASilentNode(t *testing.T) {
	values := []storage.Value{
		diskValue("disk.free_pct", "/Volumes/stick-a", "true", 1, time.Minute),
		diskValue("disk.free_pct", "/Volumes/data-a", "false", 1, time.Minute),
	}
	state := storage.NodeState{Node: "server-b", LastSeen: lastSeen.Add(-time.Minute), Values: values}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	page := hub.Page(hub.ReadState(stored{states: []storage.NodeState{state}}, configured(time.Hour, 30*time.Second), func() time.Time { return lastSeen }))
	page.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "/Volumes/stick-a") || !strings.Contains(body, "no fresh data") {
		t.Errorf("page = %q, want the stick left out and the fixed volume marked", body)
	}
}

// rows returns the body rows of every table on the page, one string each.
func rows(body string) []string {
	var out []string
	for _, tbody := range strings.Split(body, "<tbody>")[1:] {
		tbody, _, _ = strings.Cut(tbody, "</tbody>")
		out = append(out, strings.Split(tbody, "<tr>")[1:]...)
	}
	return out
}

// diskValue is a disk series of one volume, collected age before the page is read.
func diskValue(metric, mount, removable string, value float64, age time.Duration) storage.Value {
	return storage.Value{
		Metric: metric,
		Sensor: "disk",
		Labels: map[string]string{"mount": mount, "fs": "apfs", "removable": removable},
		Value:  value,
		TS:     lastSeen.Add(-age),
	}
}

// spec: history.md#page — a volume is two rows, one per series, since each is judged on its
// own, and each row links to its history and to what judges it.
func TestPageShowsAVolumeAsTwoRows(t *testing.T) {
	reversed := laptop
	reversed.Values = []storage.Value{laptop.Values[1], laptop.Values[0]}

	body := show(t, stored{states: []storage.NodeState{reversed}}, "/", "").Body.String()

	got := rows(body)
	if len(got) != 2 {
		t.Fatalf("%d rows, want one per series of the volume; page = %q", len(got), body)
	}
	for i, want := range [][]string{
		{"<td>disk.free_bytes</td>", "<td>/ · apfs</td>", "1.5 GB"},
		{"<td>disk.free_pct</td>", "<td>/ · apfs</td>", "34.2%"},
	} {
		for _, one := range want {
			if !strings.Contains(got[i], one) {
				t.Errorf("row %d = %q, want %q in it", i, got[i], one)
			}
		}
	}
	for i, metric := range []string{"disk.free_bytes", "disk.free_pct"} {
		if !strings.Contains(got[i], "/history?") || !strings.Contains(got[i], "metric="+metric) {
			t.Errorf("row %d = %q, want a link to the history of %s", i, got[i], metric)
		}
		if !strings.Contains(got[i], "/thresholds?") {
			t.Errorf("row %d = %q, want a link to what judges the series", i, got[i])
		}
	}
}

// spec: history.md#page — a node's rows are grouped so that the series of one volume sit
// together, and ordered by metric inside the group.
func TestPageGroupsAVolumesRowsAndOrdersThemByMetric(t *testing.T) {
	state := storage.NodeState{
		Node:     "laptop-a",
		LastSeen: lastSeen,
		Values: []storage.Value{
			diskValue("disk.free_pct", "/Volumes/data-a", "false", 50, 0),
			diskValue("disk.free_pct", "/", "false", 34.24, 0),
			diskValue("disk.free_bytes", "/Volumes/data-a", "false", 2e9, 0),
			diskValue("disk.free_bytes", "/", "false", 1.5e9, 0),
		},
	}

	got := rows(show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String())

	if len(got) != 4 {
		t.Fatalf("%d rows, want one per series; rows = %q", len(got), got)
	}
	for i, want := range [][]string{
		{"<td>disk.free_bytes</td>", "<td>/ · apfs</td>"},
		{"<td>disk.free_pct</td>", "<td>/ · apfs</td>"},
		{"<td>disk.free_bytes</td>", "/Volumes/data-a"},
		{"<td>disk.free_pct</td>", "/Volumes/data-a"},
	} {
		for _, one := range want {
			if !strings.Contains(got[i], one) {
				t.Errorf("row %d = %q, want %q in it", i, got[i], one)
			}
		}
	}
}

// spec: history.md#page — each series of a volume is left out or marked by its own age, and
// dated by its own collection: nothing ages a series by another one.
func TestPageAgesEachSeriesOfAVolumeOnItsOwn(t *testing.T) {
	tests := []struct {
		name      string
		removable string
		wantRows  int
	}{
		{"a removable volume with one series stale", "true", 1},
		{"a fixed volume with one series stale", "false", 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{
				diskValue("disk.free_bytes", "/Volumes/drive-a", tc.removable, 1.5e9, 0),
				diskValue("disk.free_pct", "/Volumes/drive-a", tc.removable, 34.24, time.Hour),
			}}

			body := show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String()

			if got := len(rows(body)); got != tc.wantRows {
				t.Fatalf("%d rows, want %d; page = %q", got, tc.wantRows, body)
			}
			if got := strings.Count(body, "no fresh data"); got != tc.wantRows-1 {
				t.Errorf("%d rows marked, want only the stale series; page = %q", got, body)
			}
			if !strings.Contains(body, "2026-08-28 10:05 UTC") {
				t.Errorf("page = %q, want the fresh series dated by its own collection", body)
			}
		})
	}
}

// spec: state.md#page — a hub where nothing is watched says so above the tables, in the
// reader's language.
func TestPageSaysWhenNothingIsWatched(t *testing.T) {
	const notice = "Nothing here is being judged yet"
	store := stored{states: []storage.NodeState{laptop}}

	if body := show(t, store, "/", "").Body.String(); !strings.Contains(body, notice) {
		t.Errorf("page = %q, want %q above the tables", body, notice)
	}
	if body := show(t, store, "/?lang=ru", "").Body.String(); !strings.Contains(body, "Здесь пока ничего не оценивается") {
		t.Errorf("page = %q, want the notice in Russian", body)
	}

	watched := store
	watched.thresholds = []storage.Threshold{{Series: storage.SeriesRef{
		Node: "laptop-a", Metric: "disk.free_bytes", Labels: laptop.Values[0].Labels,
	}, Direction: storage.Below}}
	if body := show(t, watched, "/", "").Body.String(); strings.Contains(body, notice) {
		t.Errorf("page = %q, want no such line once a series is watched", body)
	}
}

// spec: state.md#page — a node some of whose series are unwatched says how many, beside its
// name.
func TestPageCountsANodesUnwatchedSeries(t *testing.T) {
	store := stored{
		states: []storage.NodeState{laptop},
		thresholds: []storage.Threshold{{Series: storage.SeriesRef{
			Node: "laptop-a", Metric: "disk.free_bytes", Labels: laptop.Values[0].Labels,
		}, Direction: storage.Below}},
	}

	body := show(t, store, "/", "").Body.String()

	if !strings.Contains(body, "1 series without a threshold") {
		t.Errorf("page = %q, want the count of unwatched series beside the node", body)
	}
	if strings.Contains(body, "2 series without a threshold") {
		t.Errorf("page = %q, want the watched series left out of the count", body)
	}
}
