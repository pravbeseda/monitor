package hub_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

// usualFree is a week in which a volume's free space sat between 30e9 and 50e9, 40e9 its
// norm: the 40e9 pair reports is ordinary, and 5e9 is unusual.
func usualFree() []storage.Point {
	out := make([]storage.Point, 101)
	for i := range out {
		out[i] = storage.Point{TS: lastSeen.Add(-8 * 24 * time.Hour).Add(time.Duration(i) * time.Hour), Value: 30e9 + float64(i)*0.2e9}
	}
	return out
}

func showDebug(t *testing.T, store stored, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	hub.Debug(hub.ReadState(store, judging(), anomaly.NewNorms(store), func() time.Time { return lastSeen })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// unusualRoot is laptop-a with / at 5e9 free, far below its usual 40e9, and /data at its
// usual 40e9.
func unusualRoot() stored {
	values := append(pair(mounted("/"), lastSeen), pair(mounted("/data"), lastSeen)...)
	values[0].Value = 5e9
	return stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}},
		points: map[string][]storage.Point{"disk.free_bytes": usualFree()},
	}
}

// spec: state.md#page — a series with an anomaly rank is marked unusual beside its usual
// value; one within its band is not.
func TestDebugMarksAnUnusualSeries(t *testing.T) {
	body := showDebug(t, unusualRoot(), "/debug")
	if row := rowOf(t, body, "disk.free_bytes", "/"); !strings.Contains(row, "unusual, usually 40.0 GB") {
		t.Errorf("row of / = %s, want it marked unusual beside its usual 40.0 GB", row)
	}
	if row := rowOf(t, body, "disk.free_bytes", "/data"); strings.Contains(row, "unusual") {
		t.Errorf("row of /data = %s, want no mark", row)
	}
}

// spec: state.md#page — the mark in Russian.
func TestDebugMarksAnUnusualSeriesInRussian(t *testing.T) {
	body := showDebug(t, unusualRoot(), "/debug?lang=ru")
	if row := rowOf(t, body, "disk.free_bytes", "/"); !strings.Contains(row, "необычно, обычно 40") {
		t.Errorf("row of / = %s, want the Russian mark", row)
	}
}

// spec: state.md#page — /debug carries the tabs, mission control among them, keeping the
// language.
func TestDebugCarriesTheTabs(t *testing.T) {
	for target, want := range map[string]string{
		"/debug":         "/board",
		"/debug?lang=ru": "/board?lang=ru",
	} {
		if tabs := tabsOf(t, showDebug(t, unusualRoot(), target)); len(tabs) == 0 || tabs[0].href != want {
			t.Errorf("GET %s tabs = %v, want mission control's first at %s", target, tabs, want)
		}
	}
}

// spec: mission-control.md#page — the table of every series lives at /debug.
func TestTheTableLivesAtDebug(t *testing.T) {
	rec := getState(t, unusualRoot(), "/debug", func() time.Time { return lastSeen })
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<td>disk.free_bytes</td>") {
		t.Fatalf("GET /debug = %d, want the table: %s", rec.Code, rec.Body)
	}
}

// spec: history.md#page — any chart page carries the tabs, the table among them, keeping
// the language.
func TestHistoryPageLinksToTheTable(t *testing.T) {
	for target, want := range map[string]string{
		oneVolume:              `href="/debug"`,
		oneVolume + "&lang=ru": `href="/debug?lang=ru"`,
	} {
		if _, body := page(t, served{series: []seriesPoints{volume()}}, target); !strings.Contains(body, want) {
			t.Errorf("GET %s carries no %s", target, want)
		}
	}
}
