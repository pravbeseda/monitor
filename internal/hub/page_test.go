package hub_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/history"
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
	hub.Page(store, diskEvery(time.Minute)).ServeHTTP(rec, req)
	return rec
}

// diskEvery is a configuration that expects the disk metrics every interval and says
// nothing of any other metric.
func diskEvery(interval time.Duration) history.Interval {
	return func(_, metric string) time.Duration {
		if strings.HasPrefix(metric, "disk.") {
			return interval
		}
		return 0
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

// spec: history.md#page — a series that stopped arriving while its node reports is hidden
// when removable and marked otherwise, aged against the node's last-seen time.
func TestPageLeavesOutOrMarksSeriesThatStoppedArriving(t *testing.T) {
	const marker = "no fresh data"
	bound := 3 * time.Minute
	series := func(metric, mount, removable string, age time.Duration) storage.Value {
		return storage.Value{
			Metric: metric,
			Labels: map[string]string{"mount": mount, "fs": "apfs", "removable": removable},
			Value:  1,
			TS:     lastSeen.Add(-age),
		}
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

// spec: history.md#page — a node that stopped reporting keeps its rows as they were, and a
// node still reporting with no measurements ages them like any other.
func TestPageAgesSeriesByTheirNodesLastReport(t *testing.T) {
	stick := storage.Value{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"},
		Value:  1,
		TS:     lastSeen,
	}
	silent := storage.NodeState{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{stick}}
	heartbeatOnly := storage.NodeState{Node: "server-b", LastSeen: lastSeen.Add(time.Hour), Values: []storage.Value{stick}}

	body := show(t, stored{states: []storage.NodeState{silent, heartbeatOnly}}, "/", "").Body.String()

	if got := strings.Count(body, "/Volumes/stick-a"); got != 1 {
		t.Errorf("the stick is shown %d times, want once: under the silent node only; page = %q", got, body)
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
