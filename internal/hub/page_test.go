package hub_test

import (
	"context"
	"errors"
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
	states []storage.NodeState
	err    error
}

func (s stored) SaveIngest(context.Context, storage.Ingest) error { return nil }
func (s stored) Close() error                                     { return nil }

func (s stored) States(context.Context) ([]storage.NodeState, error) {
	return s.states, s.err
}

func (s stored) Series(context.Context, storage.Selection) ([]storage.SeriesRef, error) {
	return nil, s.err
}

func (s stored) Points(context.Context, storage.Selection, time.Time) ([]storage.SeriesPoints, error) {
	return nil, s.err
}

var laptop = storage.NodeState{
	Node:     "laptop-a",
	LastSeen: lastSeen,
	Values: []storage.Value{
		{
			Metric: "disk.free_bytes",
			Labels: map[string]string{"mount": "/", "fs": "apfs", "removable": "false"},
			Value:  1.5e9,
			TS:     lastSeen,
		},
		{
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/", "fs": "apfs", "removable": "false"},
			Value:  34.24,
			TS:     lastSeen,
		},
	},
}

func show(t *testing.T, store storage.Storage, target, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	rec := httptest.NewRecorder()
	hub.Page(store, configured(time.Minute, time.Hour), func() time.Time { return lastSeen }).ServeHTTP(rec, req)
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
// otherwise.
func TestPageLeavesOutOrMarksSeriesThatStoppedArriving(t *testing.T) {
	const marker = "no fresh data"
	bound := 3 * time.Minute
	series := func(metric, mount, removable string, age time.Duration) storage.Value {
		return diskValue(metric, mount, removable, 1, age)
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
		{"a series whose node resolves no interval", series("coffee.level", "/Volumes/stick-a", "true", 24*time.Hour), true, false},
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
		{
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"},
			Value:  1,
			TS:     anHourAgo,
		},
		{
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/Volumes/data-a", "fs": "apfs", "removable": "false"},
			Value:  1,
			TS:     anHourAgo,
		},
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
	fixed := storage.Value{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/Volumes/data-a", "fs": "apfs", "removable": "false"},
		Value:  1,
		TS:     lastSeen.Add(-time.Hour),
	}
	state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{fixed}}

	body := show(t, stored{states: []storage.NodeState{state}}, "/?lang=ru", "").Body.String()

	if !strings.Contains(body, "нет свежих данных") {
		t.Errorf("page = %q, want the mark in Russian", body)
	}
}

// spec: history.md#page — the hub ages a series by the interval its node's configuration
// resolves, and a node the configuration no longer names resolves none.
func TestRootAgesSeriesByTheConfiguredInterval(t *testing.T) {
	old := storage.Value{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/Volumes/data-a", "fs": "apfs", "removable": "false"},
		Value:  1,
		TS:     lastSeen.Add(-30 * 24 * time.Hour),
	}
	configured := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{old}}
	forgotten := storage.NodeState{Node: "server-c", LastSeen: lastSeen, Values: []storage.Value{old}}

	for _, tc := range []struct {
		state      storage.NodeState
		wantMarked bool
	}{{configured, true}, {forgotten, false}} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		routesWith(t, stored{states: []storage.NodeState{tc.state}}, time.Now).ServeHTTP(rec, req)

		if marked := strings.Contains(rec.Body.String(), "no fresh data"); marked != tc.wantMarked {
			t.Errorf("%s: marked = %v, want %v", tc.state.Node, marked, tc.wantMarked)
		}
	}
}

// spec: history.md#page — a node whose every series was left out says it has nothing
// current, not that it never measured anything.
func TestPageSaysANodeWhoseSeriesAllVanishedHasNothingCurrent(t *testing.T) {
	stick := storage.Value{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"},
		Value:  1,
		TS:     lastSeen.Add(-time.Hour),
	}
	state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{stick}}

	body := show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String()

	if !strings.Contains(body, "No current measurements") || strings.Contains(body, "No measurements yet") {
		t.Errorf("page = %q, want it to say nothing is current rather than nothing was measured", body)
	}
}

// spec: history.md#page — a node silent past its silence_after has its rows left out or
// marked in that same moment evaluation freezes them, before they are three intervals old.
func TestPageFreezesTheRowsOfASilentNode(t *testing.T) {
	aMinuteAgo := lastSeen.Add(-time.Minute)
	values := []storage.Value{
		{
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"},
			Value:  1,
			TS:     aMinuteAgo,
		},
		{
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/Volumes/data-a", "fs": "apfs", "removable": "false"},
			Value:  1,
			TS:     aMinuteAgo,
		},
	}
	state := storage.NodeState{Node: "server-b", LastSeen: aMinuteAgo, Values: values}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	page := hub.Page(stored{states: []storage.NodeState{state}}, configured(time.Hour, 30*time.Second), func() time.Time { return lastSeen })
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
		Labels: map[string]string{"mount": mount, "fs": "apfs", "removable": removable},
		Value:  value,
		TS:     lastSeen.Add(-age),
	}
}

// spec: history.md#page — a volume is one row: the sensor, the volume, and its free space in
// bytes then in percent, each value its own link.
func TestPageShowsAVolumeAsOneRow(t *testing.T) {
	body := show(t, stored{states: []storage.NodeState{laptop}}, "/", "").Body.String()

	got := rows(body)
	if len(got) != 1 {
		t.Fatalf("%d rows, want one for the volume; page = %q", len(got), body)
	}
	row := got[0]
	if !strings.Contains(row, "<td>disk</td>") {
		t.Errorf("row = %q, want it named by its sensor", row)
	}
	if cells := strings.Count(row, "<td>"); cells != 4 {
		t.Errorf("row = %q, %d cells, want both values in one", row, cells)
	}
	bytes, pct := strings.Index(row, "1.5 GB"), strings.Index(row, "34.2%")
	if bytes < 0 || pct < 0 || bytes > pct {
		t.Errorf("row = %q, want the bytes then the percent", row)
	}
	for _, metric := range []string{"disk.free_bytes", "disk.free_pct"} {
		if !strings.Contains(row, "metric="+metric) {
			t.Errorf("row = %q, want a link to the history of %s", row, metric)
		}
	}
}

// spec: history.md#page — a metric no rule declares keeps a row of its own, named by its id;
// a volume with one stored series shows that value alone; rows follow metric, then volume,
// even when the volume that sorts first stored only its second series.
func TestPageRowsFollowMetricThenVolume(t *testing.T) {
	state := storage.NodeState{
		Node:     "laptop-a",
		LastSeen: lastSeen,
		Values: []storage.Value{
			{Metric: "coffee.level", Labels: map[string]string{}, Value: 7.5, TS: lastSeen},
			diskValue("disk.free_bytes", "/Volumes/data-a", "false", 2e9, 0),
			diskValue("disk.free_pct", "/", "false", 34.24, 0),
			diskValue("disk.free_pct", "/Volumes/data-a", "false", 50, 0),
		},
	}

	got := rows(show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String())

	if len(got) != 3 {
		t.Fatalf("%d rows, want coffee, / and /Volumes/data-a; rows = %q", len(got), got)
	}
	for i, want := range [][]string{
		{"<td>coffee.level</td>", "7.50"},
		{"<td>disk</td>", "<td>/ · apfs</td>", "34.2%"},
		{"<td>disk</td>", "/Volumes/data-a", "2.0 GB", "50.0%"},
	} {
		for _, w := range want {
			if !strings.Contains(got[i], w) {
				t.Errorf("row %d = %q, want %q in it", i, got[i], w)
			}
		}
	}
	if strings.Count(got[1], "<a ") != 1 {
		t.Errorf("row = %q, want the one stored value alone", got[1])
	}
}

// spec: history.md#page — a row is dated and aged by its older series, as evaluation freezes
// the volume.
func TestPageAgesAVolumeByItsOlderSeries(t *testing.T) {
	tests := []struct {
		name       string
		removable  string
		wantShown  bool
		wantMarked bool
	}{
		{"a removable volume with one series stale", "true", false, false},
		{"a fixed volume with one series stale", "false", true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{
				diskValue("disk.free_bytes", "/Volumes/drive-a", tc.removable, 1.5e9, 0),
				diskValue("disk.free_pct", "/Volumes/drive-a", tc.removable, 34.24, time.Hour),
			}}

			body := show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String()

			if shown := strings.Contains(body, "/Volumes/drive-a"); shown != tc.wantShown {
				t.Errorf("shown = %v, want %v; page = %q", shown, tc.wantShown, body)
			}
			if marked := strings.Contains(body, "no fresh data"); marked != tc.wantMarked {
				t.Errorf("marked = %v, want %v; page = %q", marked, tc.wantMarked, body)
			}
			if tc.wantShown && !strings.Contains(body, "2026-08-28 09:05 UTC") {
				t.Errorf("page = %q, want the row dated by its older series", body)
			}
		})
	}
}

// spec: history.md#page — a metric no rule declares keeps a row of its own even when its id
// is a sensor's name and its labels a volume's.
func TestPageKeepsAnUndeclaredMetricOutOfASensorsRow(t *testing.T) {
	namesake := diskValue("disk", "/", "false", 7.5, 0)
	state := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: append([]storage.Value{namesake}, laptop.Values...)}

	got := rows(show(t, stored{states: []storage.NodeState{state}}, "/", "").Body.String())

	if len(got) != 2 {
		t.Errorf("%d rows, want the volume and the undeclared metric apart; rows = %q", len(got), got)
	}
}
