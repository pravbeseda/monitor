package hub_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

// spec: state.md#page — a series with an anomaly rank is marked unusual in its cell beside
// its usual value; one within its band is not.
func TestDebugMarksAnUnusualSeries(t *testing.T) {
	body := showDebug(t, unusualRoot(), "/debug")
	if cell := cellOf(t, body, "disk.free_bytes", "/"); !strings.Contains(cell, "unusual, usually 40.0 GB") {
		t.Errorf("cell of / = %s, want it marked unusual beside its usual 40.0 GB", cell)
	}
	if cell := cellOf(t, body, "disk.free_bytes", "/data"); strings.Contains(cell, "unusual") {
		t.Errorf("cell of /data = %s, want no mark", cell)
	}
}

// spec: state.md#page — the mark in Russian.
func TestDebugMarksAnUnusualSeriesInRussian(t *testing.T) {
	body := showDebug(t, unusualRoot(), "/debug?lang=ru")
	if cell := cellOf(t, body, "disk.free_bytes", "/"); !strings.Contains(cell, "необычно, обычно 40") {
		t.Errorf("cell of / = %s, want the Russian mark", cell)
	}
}

// spec: state.md#page — /debug carries the tabs, the timeline among them, keeping the
// language.
func TestDebugCarriesTheTabs(t *testing.T) {
	for target, want := range map[string]string{
		"/debug":         "/timeline",
		"/debug?lang=ru": "/timeline?lang=ru",
	} {
		if tabs := tabsOf(t, showDebug(t, unusualRoot(), target)); len(tabs) == 0 || tabs[0].href != want {
			t.Errorf("GET %s tabs = %v, want the timeline's first at %s", target, tabs, want)
		}
	}
}

// spec: state.md#page — /debug?lang=ru keeps the language on every link.
func TestDebugKeepsTheLanguageOnEveryLink(t *testing.T) {
	body := showDebug(t, unusualRoot(), "/debug?lang=ru")
	links := hrefPattern.FindAllStringSubmatch(body, -1)
	if len(links) == 0 {
		t.Fatal("no link on /debug")
	}
	for _, found := range links {
		if !strings.HasPrefix(found[1], "/") {
			continue // the shell's icon, not a link
		}
		if target, _ := url.Parse(strings.ReplaceAll(found[1], "&amp;", "&")); target.Query().Get("lang") != "ru" {
			t.Errorf("a link drops the language: %s", found[1])
		}
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
