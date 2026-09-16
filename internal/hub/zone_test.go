package hub_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// The hub host's own clock is pinned away from UTC, so the case that matters — a browser
// naming that clock instead of the reader's zone — fails on a hub that accepts it. A CI
// runner in UTC would otherwise pass that test on broken code (spec: web.md#zone).
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("HOST", 7*3600)
	os.Exit(m.Run())
}

// getFrom renders a page for a browser presenting a stored zone.
func getFrom(t *testing.T, store storage.Storage, target, zone string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if zone != "" {
		req.AddCookie(&http.Cookie{Name: "tz", Value: zone})
	}
	rec := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(rec, req)
	return rec
}

func indexFrom(t *testing.T, zone string) *httptest.ResponseRecorder {
	t.Helper()
	return getFrom(t, stored{states: []storage.NodeState{laptop}}, "/", zone)
}

// spec: web.md#zone — a browser that has reported its zone reads every time in it.
func TestPageReadsTimesInTheReportedZone(t *testing.T) {
	body := indexFrom(t, "Europe/Moscow").Body.String()

	if want := "2026-08-28 13:05 MSK"; !strings.Contains(body, want) {
		t.Errorf("page does not show %q", want)
	}
	if strings.Contains(body, "10:05 UTC") {
		t.Error("page still shows the UTC reading")
	}
}

// spec: web.md#zone — a zone the hub will not accept is UTC, as a page and not an error.
// "Local" is the one that matters: a zone database answers to it with the hub host's own
// clock, which is neither the reader's zone nor UTC.
func TestPageFallsBackToUTCForAZoneItWillNotAccept(t *testing.T) {
	tests := map[string]string{
		"no cookie at all":        "",
		"the host's own clock":    "Local",
		"a flat name":             "EST5EDT",
		"a path":                  "../../etc/passwd",
		"an absolute path":        "/etc/localtime",
		"a region nobody has":     "Mars/Phobos",
		"a name beyond the bound": "Europe/" + strings.Repeat("a", 80),
		"a name with a space":     "Europe/Moscow Time",
	}

	for name, zone := range tests {
		t.Run(name, func(t *testing.T) {
			rec := indexFrom(t, zone)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if want := "2026-08-28 10:05 UTC"; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("page does not show %q", want)
			}
		})
	}
}

// spec: web.md#zone — a browser already in UTC reads UTC. It gets there through the refusal
// of its flat name rather than past it, which is the same page either way.
func TestPageReadsUTCForABrowserInUTC(t *testing.T) {
	if want := "2026-08-28 10:05 UTC"; !strings.Contains(indexFrom(t, "UTC").Body.String(), want) {
		t.Errorf("page does not show %q", want)
	}
}

// spec: web.md#zone — the API answers the same browser in UTC, whatever it reports.
func TestAPIStaysInUTCForAReaderWithAZone(t *testing.T) {
	store := served{series: []storage.SeriesPoints{volume()}}

	body := getFrom(t, store, "/api/v1/history?metric=disk.free_pct&node=server-b", "Europe/Moscow").Body.String()

	if !strings.Contains(body, "Z\"") || strings.Contains(body, "MSK") {
		t.Errorf("the API answered %q, want RFC 3339 UTC", body)
	}
}

// spec: web.md#shell — every page carries the same shell and the same cache headers; a page
// carrying its own would be the one that is silently UTC, or the one a cache keeps.
func TestEveryPageCarriesTheShell(t *testing.T) {
	store := served{series: []storage.SeriesPoints{volume()}}
	pages := map[string]storage.Storage{"/": stored{states: []storage.NodeState{laptop}}, oneVolume: store}

	for target, page := range pages {
		t.Run(target, func(t *testing.T) {
			rec := getFrom(t, page, target, "")
			for _, want := range []string{
				"Intl.DateTimeFormat().resolvedOptions().timeZone",
				// The page is fetched again only after the cookie reads back, so a
				// browser that stores nothing keeps the UTC page (spec: web.md#zone).
				"if (!zone || holds(zone)) return;",
				"if (holds(zone)) location.reload();",
			} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("page does not carry %q", want)
				}
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := rec.Header().Get("Vary"); got != "Cookie, Accept-Language" {
				t.Errorf("Vary = %q, want the reader's zone and language", got)
			}
		})
	}
}

// spec: web.md#shell — a refusal is a page too, so a first visit that fails still leaves
// the reader in their own zone for the next one.
func TestARefusedPageCarriesTheShell(t *testing.T) {
	body := getFrom(t, served{}, "/history?metric=disk.free_pct&nonsense=1", "").Body.String()

	if !strings.Contains(body, "Intl.DateTimeFormat().resolvedOptions().timeZone") {
		t.Error("a refused page does not carry the shell")
	}
}

// spec: web.md#zone, history.md#page — a chart names the zone its axis is read in, once:
// the per-tick labels have no room for a marker.
func TestChartNamesItsZone(t *testing.T) {
	store := served{series: []storage.SeriesPoints{volume()}}

	body := getFrom(t, store, oneVolume, "Europe/Moscow").Body.String()

	if got := strings.Count(body, ">MSK<"); got != 1 {
		t.Errorf("the chart names its zone %d times, want once", got)
	}
	if !strings.Contains(body, "14:30 MSK") {
		t.Error("the newest value is not read in the reader's zone")
	}
}
