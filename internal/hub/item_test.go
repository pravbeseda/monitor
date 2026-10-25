package hub_test

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// logbook is the threshold store and the event log an item's page reads beside the state.
type logbook struct {
	thresholds map[string]storage.Threshold
	events     []storage.Transition
	anomalies  []storage.Anomaly
	// err fails reading a threshold; logErr fails reading the log, and only that.
	err    error
	logErr error
}

func (l logbook) ThresholdOf(_ context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error) {
	th, found := l.thresholds[heldKey(ref)]
	return th, found, l.err
}

func (l logbook) EventsBetween(context.Context, time.Time, time.Time) ([]storage.Transition, error) {
	return nil, l.err
}

func (l logbook) RecentEvents(_ context.Context, nodes []string, limit int) ([]storage.Transition, error) {
	var out []storage.Transition
	for _, one := range l.events {
		if slices.Contains(nodes, one.Node) && len(out) < limit {
			out = append(out, one)
		}
	}
	return out, l.logErr
}

func (l logbook) AnomaliesSince(context.Context, time.Time) ([]storage.Anomaly, error) {
	return nil, l.err
}

func (l logbook) RecentAnomalies(_ context.Context, nodes []string, limit int) ([]storage.Anomaly, error) {
	var out []storage.Anomaly
	for _, one := range l.anomalies {
		if slices.Contains(nodes, one.Node) && len(out) < limit {
			out = append(out, one)
		}
	}
	return out, l.logErr
}

func untargeted(string) (evaluate.Target, bool) { return evaluate.Target{}, false }

func showItem(t *testing.T, current state.State, source held, book logbook, method, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	read := func(context.Context) (state.State, error) { return current, nil }
	hub.Item(read, historyOf(source), book, layout(), untargeted).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Code, rec.Body.String()
}

func itemPage(t *testing.T, current state.State, source held, book logbook, target string) string {
	t.Helper()
	code, body := showItem(t, current, source, book, http.MethodGet, target)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, code, body)
	}
	return body
}

var (
	familyPattern  = regexp.MustCompile(`<h2 class="family-heading">([^<]*)</h2>`)
	chartPattern   = regexp.MustCompile(`(?s)<figure class="chart" data-series="([^"]*)">.*?</figure>`)
	numberPattern  = regexp.MustCompile(`(?s)<li class="number" data-series="([^"]*)">.*?</li>`)
	windowPattern  = regexp.MustCompile(`<strong class="window">([^<]*)</strong>`)
	levelLineY     = regexp.MustCompile(`<line class="threshold level-(\w+)" x1="[^"]*" y1="([^"]*)"`)
	plotTopPattern = regexp.MustCompile(`<line class="axis" x1="[^"]*" y1="([^"]*)"`)
)

func charts(body string) []string {
	var out []string
	for _, found := range chartPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, found[1])
	}
	return out
}

func chartOf(t *testing.T, body, name string) string {
	t.Helper()
	for _, found := range chartPattern.FindAllStringSubmatch(body, -1) {
		if found[1] == name {
			return found[0]
		}
	}
	t.Fatalf("no chart %q in %v", name, charts(body))
	return ""
}

func families(body string) []string {
	var out []string
	for _, found := range familyPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, found[1])
	}
	return out
}

func marked(body string) string {
	if found := windowPattern.FindStringSubmatch(body); found != nil {
		return found[1]
	}
	return ""
}

func ext4(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}
}

// spec: overview.md#item-page
func TestItemPage(t *testing.T) {
	current := stateOf(judgedAt(freePct("server-b", "/", 9), evaluate.Warning), plain("server-b", "load.avg_15m", 1))
	body := itemPage(t, current, held{}, logbook{}, "/item?node=server-b")
	containsAll(t, "page", body, `<h1 class="item-name">server-b</h1>`, "Computer", "warning", "last report")
	if !strings.Contains(body, `data-skin="overview"`) || strings.Contains(body, `" aria-current="page">`) {
		t.Errorf("the tabs are missing or one is marked")
	}
	var numbers []string
	for _, found := range numberPattern.FindAllStringSubmatch(body, -1) {
		numbers = append(numbers, found[1])
	}
	equal(t, "numbers", numbers, []string{"disk.free_pct /", "load.avg_15m"})
	if strings.Contains(body, `class="spark"`) {
		t.Errorf("the numbers carry lines")
	}

	t.Run("a site", func(t *testing.T) {
		body := itemPage(t, stateOf(siteOf("blog-a", "site.requests_per_min", 4)), held{}, logbook{}, "/item?node=sites&label.site=blog-a")
		containsAll(t, "page", body, `<h1 class="item-name">blog-a</h1>`, "Site")
		equal(t, "charts", charts(body), []string{"site.requests_per_min"})
	})

	t.Run("a service that stored nothing", func(t *testing.T) {
		current := neverReported(stateOf(freePct("server-b", "/", 40)), "gdrive-a")
		body := itemPage(t, current, held{}, logbook{}, "/item?node=gdrive-a")
		containsAll(t, "page", body, "Service", "no fresh data", "last report", "No series yet")
	})

	t.Run("windows", func(t *testing.T) {
		for target, want := range map[string]string{
			"/item?node=server-b": "24h", "/item?node=server-b&window=7d": "7d",
			"/item?node=server-b&window=168h": "7d", "/item?node=server-b&window=12h": "",
		} {
			if got := marked(itemPage(t, current, held{}, logbook{}, target)); got != want {
				t.Errorf("%s marks %q, want %q", target, got, want)
			}
		}
		body := itemPage(t, current, held{}, logbook{}, "/item?node=server-b&lang=ru")
		containsAll(t, "links", body, `href="/item?lang=ru&amp;node=server-b&amp;window=30d"`)
	})

	t.Run("families and their order", func(t *testing.T) {
		body := itemPage(t, stateOf(
			plain("server-b", "uptime.boot_seconds", 9), plain("server-b", "memory.available_pct", 50),
			plain("server-b", "load.avg_5m", 1), plain("server-b", "load.avg_15m", 1), plain("server-b", "load.avg_1m", 1),
			free("server-b", "/b", 9e9), freePct("server-b", "/b", 9), free("server-b", "/a", 9e9), freePct("server-b", "/a", 9),
		), held{}, logbook{}, "/item?node=server-b")
		equal(t, "families", families(body), []string{"disk", "load", "memory", "uptime"})
		got := charts(body)
		equal(t, "charts", got[:7], []string{
			"disk.free_bytes · /a · ext4", "disk.free_pct · /a · ext4", "disk.free_bytes · /b · ext4", "disk.free_pct · /b · ext4",
			"load.avg_15m", "load.avg_1m", "load.avg_5m",
		})
	})

	t.Run("sixty series", func(t *testing.T) {
		var subjects []state.Subject
		for i := range 60 {
			subjects = append(subjects, reading("box-x", "queue.depth", map[string]string{"queue": "q" + strconv.Itoa(100+i)}, 1))
		}
		if got := charts(itemPage(t, stateOf(subjects...), held{}, logbook{}, "/item?node=box-x")); len(got) != 60 {
			t.Errorf("charts = %d, want 60", len(got))
		}
	})

	t.Run("a stale removable volume", func(t *testing.T) {
		body := itemPage(t, stateOf(staled(removable(freePct("server-b", "/usb", 3))), freePct("server-b", "/", 40)), held{}, logbook{}, "/item?node=server-b")
		equal(t, "charts", charts(body), []string{"disk.free_pct · / · ext4"})
	})
}

// spec: overview.md#item-page
func TestItemCharts(t *testing.T) {
	root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: ext4("/")}
	load := storage.SeriesRef{Node: "server-b", Metric: "load.avg_15m", Labels: map[string]string{}}
	ten, five, four := 10e9, 5e9, 4.0
	points := held{
		heldKey(root): {{TS: lastSeen.Add(-time.Hour), Value: 20e9}, {TS: lastSeen.Add(-55 * time.Minute), Value: 19e9}},
		heldKey(load): {{TS: lastSeen.Add(-time.Hour), Value: 0.5}, {TS: lastSeen.Add(-55 * time.Minute), Value: 0.6}},
	}
	book := logbook{thresholds: map[string]storage.Threshold{
		heldKey(root): {Series: root, Direction: storage.Below, Warning: &ten, Critical: &five},
		heldKey(load): {Series: load, Direction: storage.Above, Warning: &four},
	}}
	current := stateOf(judgedAt(free("server-b", "/", 19e9), evaluate.OK), judgedAt(plain("server-b", "load.avg_15m", 0.6), evaluate.OK))
	body := itemPage(t, current, points, book, "/item?node=server-b")

	disk := chartOf(t, body, "disk.free_bytes · / · ext4")
	lines := levelLineY.FindAllStringSubmatch(disk, -1)
	if len(lines) != 2 || lines[0][1] != "warning" || lines[1][1] != "critical" {
		t.Fatalf("threshold lines = %v", lines)
	}
	if plot := atof(t, plotTopPattern.FindStringSubmatch(disk)[1]); atof(t, lines[0][2]) >= atof(t, lines[1][2]) || atof(t, lines[0][2]) < plot {
		t.Errorf("10 GB at y %s should sit above 5 GB at y %s, inside the plot from %v", lines[0][2], lines[1][2], plot)
	}
	containsAll(t, "disk chart", disk, ">warning<", ">critical<",
		`href="/history?label.fs=ext4&amp;label.mount=%2F&amp;label.removable=false&amp;metric=disk.free_bytes&amp;node=server-b&amp;window=24h"`,
		`href="/thresholds?`)

	cpu := chartOf(t, body, "load.avg_15m")
	found := levelLineY.FindAllStringSubmatch(cpu, -1)
	top := plotTopPattern.FindStringSubmatch(cpu)
	if len(found) != 1 || top == nil {
		t.Fatalf("load chart lines = %v, top %v", found, top)
	}
	if y, plot := atof(t, found[0][2]), atof(t, top[1]); y < plot {
		t.Errorf("the warning line at %v is above the plot's top %v", y, plot)
	}

	t.Run("an unwatched series and an unreadable threshold", func(t *testing.T) {
		odd := logbook{thresholds: map[string]storage.Threshold{heldKey(load): {Series: load, Direction: "sideways", Warning: &four}}}
		body := itemPage(t, stateOf(free("server-b", "/", 19e9), plain("server-b", "load.avg_15m", 0.6)), points, odd, "/item?node=server-b")
		if strings.Contains(body, `class="threshold`) {
			t.Errorf("a threshold line without a readable threshold")
		}
	})

	t.Run("unusual, stale and empty", func(t *testing.T) {
		body := itemPage(t, stateOf(ranked(free("server-b", "/", 19e9), 1), staled(plain("server-b", "load.avg_15m", 0.6)), plain("server-b", "memory.available_pct", 50)),
			points, logbook{}, "/item?node=server-b")
		containsAll(t, "unusual", chartOf(t, body, "disk.free_bytes · / · ext4"), "usually 40.0 GB")
		containsAll(t, "stale", chartOf(t, body, "load.avg_15m"), "no fresh data")
		containsAll(t, "empty", chartOf(t, body, "memory.available_pct"), "No data for this window")
	})

	t.Run("in Russian", func(t *testing.T) {
		body := itemPage(t, current, points, book, "/item?node=server-b&lang=ru")
		containsAll(t, "page", body, "Компьютер", "последний отчёт")
	})

	t.Run("the state cannot be read", func(t *testing.T) {
		rec := httptest.NewRecorder()
		read := func(context.Context) (state.State, error) { return state.State{}, errFailed }
		hub.Item(read, historyOf(held{}), logbook{}, layout(), untargeted).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/item?node=server-b", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d", rec.Code)
		}
		code, _ := showItem(t, current, points, logbook{err: errFailed}, http.MethodGet, "/item?node=server-b")
		if code != http.StatusInternalServerError {
			t.Errorf("a failing log answers %d", code)
		}
	})
}

func atof(t *testing.T, text string) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func transition(node, metric string, labels map[string]string, from, to string, at time.Time) storage.Transition {
	return storage.Transition{Subject: storage.Subject{Node: node, Metric: metric, Labels: labels}, From: from, To: to, At: at}
}

// spec: overview.md#changes
func TestItemChanges(t *testing.T) {
	srv := ext4("/srv")
	back := 1.0
	book := logbook{
		events: []storage.Transition{
			transition("server-c", "disk.free_pct", srv, "ok", "warning", lastSeen.Add(-time.Hour)),
			transition("server-b", "disk.free_pct", srv, "ok", "warning", lastSeen.Add(-time.Hour)),
			transition("server-c", evaluate.SilenceMetric, map[string]string{}, "critical", "ok", lastSeen.Add(-2*time.Hour)),
			transition("server-c", evaluate.SilenceMetric, map[string]string{}, "ok", "critical", lastSeen.Add(-3*time.Hour)),
		},
		anomalies: []storage.Anomaly{{
			Subject: storage.Subject{Node: "server-c", Metric: "load.avg_15m", Labels: map[string]string{}},
			Began:   lastSeen.Add(-5 * time.Hour), Ended: lastSeen.Add(-4 * time.Hour), Value: 3, Back: &back,
		}},
	}
	body := itemPage(t, stateOf(freePct("server-c", "/srv", 9), freePct("server-b", "/", 9)), held{}, book, "/item?node=server-c")
	got := changePattern.FindAllString(body, -1)
	if len(got) != 5 {
		t.Fatalf("changes = %d, want 5: %v", len(got), got)
	}
	containsAll(t, "changes", strings.Join(got, ""), "ok → warning", "reporting again", "fell silent", "unusual", "usual again")
	containsAll(t, "page", body, "Today")
	if strings.Contains(strings.Join(got, ""), ">server-b<") {
		t.Errorf("another node's change is listed")
	}

	t.Run("a site's own changes and its hosts' silences", func(t *testing.T) {
		book := logbook{events: []storage.Transition{
			transition("sites", "site.server_error_pct", map[string]string{"site": "shop-c"}, "ok", "critical", lastSeen.Add(-time.Hour)),
			transition("sites", "site.server_error_pct", map[string]string{"site": "blog-a"}, "ok", "warning", lastSeen.Add(-time.Hour)),
			transition("sites", evaluate.SilenceMetric, map[string]string{}, "ok", "critical", lastSeen.Add(-2*time.Hour)),
			transition("server-b", evaluate.SilenceMetric, map[string]string{}, "ok", "critical", lastSeen.Add(-3*time.Hour)),
			transition("server-b", "disk.free_pct", srv, "ok", "warning", lastSeen.Add(-4*time.Hour)),
		}}
		current := stateOf(siteOf("shop-c", "site.server_error_pct", 9), siteOf("blog-a", "site.server_error_pct", 9))
		shop := changePattern.FindAllString(itemPage(t, current, held{}, book, "/item?node=sites&label.site=shop-c"), -1)
		blog := changePattern.FindAllString(itemPage(t, current, held{}, book, "/item?node=sites&label.site=blog-a"), -1)
		if len(shop) != 3 || len(blog) != 2 {
			t.Errorf("shop-c changes = %d, want 3; blog-a = %d, want 2", len(shop), len(blog))
		}
	})

	t.Run("the newest 20", func(t *testing.T) {
		var events []storage.Transition
		for i := range 5 {
			events = append(events, transition("server-c", evaluate.SilenceMetric, map[string]string{}, "ok", "critical", lastSeen.Add(-time.Duration(i)*time.Minute)))
		}
		for i := range 25 {
			events = append(events, transition("server-c", "disk.free_pct", srv, "ok", "warning", lastSeen.Add(-time.Duration(10+i)*time.Minute)))
		}
		got := changePattern.FindAllString(itemPage(t, stateOf(freePct("server-c", "/srv", 9)), held{}, logbook{events: events}, "/item?node=server-c"), -1)
		if len(got) != 20 || strings.Count(strings.Join(got, ""), "fell silent") != 5 {
			t.Errorf("changes = %d, want the newest 20, the five silences among them", len(got))
		}
	})

	t.Run("none", func(t *testing.T) {
		containsAll(t, "page", itemPage(t, stateOf(freePct("server-c", "/srv", 9)), held{}, logbook{}, "/item?node=server-c"), "Nothing has changed yet")
	})
}

// spec: overview.md#addresses
func TestItemAddresses(t *testing.T) {
	current := stateOf(freePct("server-b", "/", 40), siteOf("blog-a", "site.requests_per_min", 4), siteOf("gone-z", "site.requests_per_min", 1))
	for target, want := range map[string]int{
		"/item":                                 http.StatusBadRequest,
		"/item?node=sites":                      http.StatusBadRequest,
		"/item?node=server-b&label.site=blog-a": http.StatusBadRequest,
		"/item?node=sites&label.site=a&label.site=b": http.StatusBadRequest,
		"/item?node=sites&label.site=":               http.StatusBadRequest,
		"/item?node=server-b&label.mount=/":          http.StatusBadRequest,
		"/item?node=server-b&x=1":                    http.StatusBadRequest,
		"/item?node=server-b&window=2w":              http.StatusBadRequest,
		"/item?node=server-z":                        http.StatusNotFound,
		"/item?node=sites&label.site=gone-z":         http.StatusNotFound,
		"/item?node=sites&label.site=shop-c":         http.StatusNotFound,
		"/item?node=server-b&window=7d&lang=ru":      http.StatusOK,
		"/item?node=sites&label.site=blog-a&lang=en": http.StatusOK,
	} {
		code, body := showItem(t, current, held{}, logbook{}, http.MethodGet, target)
		if code != want {
			t.Errorf("GET %s = %d, want %d", target, code, want)
		}
		if code != http.StatusOK && !strings.Contains(body, "<html") {
			t.Errorf("GET %s refuses with no translated page", target)
		}
	}

	t.Run("a page gone is not refreshed into its refusal", func(t *testing.T) {
		_, body := showItem(t, current, held{}, logbook{}, http.MethodGet, "/item?node=server-z")
		if strings.Contains(body, `<meta name="monitor-live"`) {
			t.Errorf("the 404 page refreshes itself")
		}
	})

	t.Run("HEAD", func(t *testing.T) {
		code, _ := showItem(t, current, held{}, logbook{}, http.MethodHead, "/item?node=server-b")
		if code != http.StatusOK {
			t.Errorf("HEAD = %d", code)
		}
	})
}

// broken is a history source whose every read of points fails.
type broken struct{ held }

func (b broken) Points(context.Context, storage.SeriesRef, time.Time, time.Time) iter.Seq2[storage.Point, error] {
	return func(yield func(storage.Point, error) bool) { yield(storage.Point{}, errFailed) }
}

// spec: overview.md#item-page
func TestItemPageMore(t *testing.T) {
	root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: ext4("/")}
	old := held{heldKey(root): {{TS: lastSeen.Add(-72 * time.Hour), Value: 20e9}, {TS: lastSeen.Add(-72*time.Hour + 5*time.Minute), Value: 19e9}}}
	current := stateOf(staled(judgedAt(free("server-b", "/", 19e9), evaluate.OK)))

	t.Run("the window reaches the charts", func(t *testing.T) {
		for target, drawn := range map[string]bool{
			"/item?node=server-b&window=7d": true, "/item?node=server-b": false, "/item?node=server-b&window=12h": false,
		} {
			chart := chartOf(t, itemPage(t, current, old, logbook{}, target), "disk.free_bytes · / · ext4")
			if strings.Contains(chart, "<svg") != drawn || strings.Contains(chart, "No data for this window") == drawn {
				t.Errorf("%s: chart drawn %v, want %v", target, !drawn, drawn)
			}
		}
	})

	t.Run("a stale series is still drawn, its links carry the language", func(t *testing.T) {
		chart := chartOf(t, itemPage(t, current, old, logbook{}, "/item?node=server-b&window=7d&lang=ru"), "disk.free_bytes · / · ext4")
		containsAll(t, "chart", chart, "<svg", "нет свежих данных", "lang=ru&amp;metric=disk.free_bytes", "window=7d", "/thresholds?")
	})

	t.Run("the page keeps itself current; a refusal does not", func(t *testing.T) {
		live := `<meta name="monitor-live"`
		containsAll(t, "page", itemPage(t, current, old, logbook{}, "/item?node=server-b"), live)
		for _, target := range []string{"/item?node=server-b&x=1", "/item?node=server-z"} {
			if _, body := showItem(t, current, old, logbook{}, http.MethodGet, target); strings.Contains(body, live) {
				t.Errorf("%s refreshes itself", target)
			}
		}
	})

	t.Run("a site's page when the sites node is gone from the file", func(t *testing.T) {
		if code, _ := showItem(t, current, old, logbook{}, http.MethodGet, "/item?node=nosites&label.site=blog-a"); code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", code)
		}
	})

	t.Run("an item whose only series are unplugged volumes", func(t *testing.T) {
		only := stateOf(staled(removable(freePct("server-b", "/usb", 3))))
		containsAll(t, "page", itemPage(t, only, held{}, logbook{}, "/item?node=server-b"), "No series yet")
		containsAll(t, "card", card(t, showOverview(t, only, held{}, "/overview"), "server-b"), "No series yet")
	})

	t.Run("a family without a dot is its own", func(t *testing.T) {
		body := itemPage(t, stateOf(plain("server-b", "disk", 1), freePct("server-b", "/", 40)), held{}, logbook{}, "/item?node=server-b")
		equal(t, "families", families(body), []string{"disk", "disk"})
	})

	t.Run("a site's last report is its newest point", func(t *testing.T) {
		newest := siteOf("blog-a", "site.requests_per_min", 4)
		newest.TS = lastSeen.Add(-7 * time.Minute)
		containsAll(t, "page", itemPage(t, stateOf(siteOf("blog-a", "site.server_error_pct", 0), newest), held{}, logbook{}, "/item?node=sites&label.site=blog-a"),
			"last report 2026-08-28 09:58")
	})

	t.Run("the points or the log cannot be read", func(t *testing.T) {
		if code, _ := showItem(t, current, held{}, logbook{logErr: errFailed}, http.MethodGet, "/item?node=server-b"); code != http.StatusInternalServerError {
			t.Errorf("a failing log answers %d", code)
		}
		recent := broken{held{heldKey(root): {{TS: lastSeen.Add(-time.Hour), Value: 20e9}}}}
		rec := httptest.NewRecorder()
		read := func(context.Context) (state.State, error) { return current, nil }
		hub.Item(read, historyOf(recent), logbook{}, layout(), untargeted).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/item?node=server-b", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("failing points answer %d on the item's page", rec.Code)
		}
		rec = httptest.NewRecorder()
		hub.Overview(read, historyOf(recent), layout()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("failing points answer %d on the overview", rec.Code)
		}
	})
}

// spec: overview.md#addresses — both pages are mounted, and HEAD answers as GET does.
func TestOverviewRoutes(t *testing.T) {
	routes := routesWith(t, stored{states: []storage.NodeState{laptop}}, func() time.Time { return lastSeen })
	for _, target := range []string{"/overview", "/item?node=laptop-a"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
			if rec.Code != http.StatusOK {
				t.Errorf("%s %s = %d: %s", method, target, rec.Code, rec.Body)
			}
		}
	}
}

// spec: overview.md#item-page — a value below zero has no place on an axis from zero.
func TestItemNegativeThreshold(t *testing.T) {
	temp := storage.SeriesRef{Node: "box-x", Metric: "temp.celsius", Labels: map[string]string{}}
	minus := -5.0
	points := held{heldKey(temp): {{TS: lastSeen.Add(-time.Hour), Value: 3}, {TS: lastSeen.Add(-55 * time.Minute), Value: 4}}}
	book := logbook{thresholds: map[string]storage.Threshold{heldKey(temp): {Series: temp, Direction: storage.Below, Warning: &minus}}}
	body := itemPage(t, stateOf(plain("box-x", "temp.celsius", 4)), points, book, "/item?node=box-x")
	if strings.Contains(chartOf(t, body, "temp.celsius"), `class="threshold`) {
		t.Errorf("a line drawn for a value below zero")
	}
}
