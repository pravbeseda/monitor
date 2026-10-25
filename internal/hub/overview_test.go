package hub_test

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The overview reads the state, like the list of what needs attention, so its rows are
// tested against states written by hand; the lines of a card read the points a source
// holds per series.

var classes = map[string]string{
	"server-a": "server", "server-b": "server", "server-c": "server", "server-d": "server", "server-e": "server",
	"laptop-a": "laptop", "box-x": "box", "sites": "sites", "gdrive-a": "service",
}

var hosts = map[string]string{"blog-a": "server-a", "shop-c": "server-b"}

func layout() hub.Layout {
	return hub.Layout{
		Class: func(node string) (string, bool) { class, known := classes[node]; return class, known },
		Host:  func(site string) (string, bool) { host, listed := hosts[site]; return host, listed },
	}
}

// held is a history source keeping points per series, whatever else a test asks of it.
type held map[string][]storage.Point

func heldKey(ref storage.SeriesRef) string {
	return ref.Node + "|" + ref.Metric + "|" + storage.LabelKey(ref.Labels)
}

func (h held) refs() []storage.SeriesRef {
	var out []storage.SeriesRef
	for key := range h {
		parts := strings.SplitN(key, "|", 3)
		labels := map[string]string{}
		for _, pair := range strings.Split(parts[2], ",") {
			if name, value, ok := strings.Cut(pair, "="); ok {
				labels[name] = value
			}
		}
		out = append(out, storage.SeriesRef{Node: parts[0], Metric: parts[1], Labels: labels})
	}
	return out
}

func (h held) Series(context.Context, storage.Selection) ([]storage.SeriesNewest, error) {
	return nil, nil
}

func (h held) Newest(_ context.Context, sel storage.Selection, from time.Time) ([]storage.SeriesNewest, error) {
	var out []storage.SeriesNewest
	for _, ref := range h.refs() {
		if ref.Node != sel.Node || ref.Metric != sel.Metric {
			continue
		}
		for _, point := range h[heldKey(ref)] {
			if !point.TS.Before(from) {
				out = append(out, storage.SeriesNewest{SeriesRef: ref, Newest: point.TS, Sensor: "disk"})
				break
			}
		}
	}
	return out, nil
}

func (h held) Points(_ context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error] {
	return func(yield func(storage.Point, error) bool) {
		for _, point := range h[heldKey(ref)] {
			if !point.TS.Before(from) && !point.TS.After(to) && !yield(point, nil) {
				return
			}
		}
	}
}

func historyOf(source history.Source) history.Reader {
	return history.Reader{
		Source:   source,
		Interval: func(string, string) time.Duration { return 5 * time.Minute },
		Now:      func() time.Time { return lastSeen },
	}
}

func showOverview(t *testing.T, current state.State, source history.Source, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	read := func(context.Context) (state.State, error) { return current, nil }
	hub.Overview(read, historyOf(source), layout()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func overview(t *testing.T, subjects ...state.Subject) string {
	t.Helper()
	return showOverview(t, stateOf(subjects...), held{}, "/overview")
}

var (
	sectionPattern = regexp.MustCompile(`(?s)<section class="section"[^>]*>.*?</section>`)
	headingPattern = regexp.MustCompile(`(?s)<h2 class="section-heading">(.*?)</h2>`)
	cardPattern    = regexp.MustCompile(`(?s)<a class="card [^"]*" href="[^"]*" data-item="([^"]*)">.*?</a>`)
	cardState      = regexp.MustCompile(`<span class="card-state">([^<]*)</span>`)
	rowPattern     = regexp.MustCompile(`(?s)<li class="row" data-series="([^"]*)">.*?</li>`)
	markPattern    = regexp.MustCompile(`<span class="row-mark[^"]*">([^<]*)</span>`)
)

// sections are each section's heading and the names of its cards, in page order.
func sections(body string) [][]string {
	var out [][]string
	for _, section := range sectionPattern.FindAllString(body, -1) {
		one := []string{strings.TrimSpace(headingPattern.FindStringSubmatch(section)[1])}
		for _, card := range cardPattern.FindAllStringSubmatch(section, -1) {
			one = append(one, card[1])
		}
		out = append(out, one)
	}
	return out
}

func card(t *testing.T, body, name string) string {
	t.Helper()
	for _, found := range cardPattern.FindAllStringSubmatch(body, -1) {
		if found[1] == name {
			return found[0]
		}
	}
	t.Fatalf("no card %q in %s", name, body)
	return ""
}

func stateWord(t *testing.T, body, name string) string {
	t.Helper()
	found := cardState.FindStringSubmatch(card(t, body, name))
	if found == nil {
		t.Fatalf("card %q has no state", name)
	}
	return found[1]
}

func cardRows(t *testing.T, body, name string) []string {
	t.Helper()
	var out []string
	for _, row := range rowPattern.FindAllStringSubmatch(card(t, body, name), -1) {
		out = append(out, row[1])
	}
	return out
}

func rowMark(t *testing.T, body, name, series string) string {
	t.Helper()
	for _, row := range rowPattern.FindAllStringSubmatch(card(t, body, name), -1) {
		if row[1] == series {
			if found := markPattern.FindStringSubmatch(row[0]); found != nil {
				return found[1]
			}
			return ""
		}
	}
	t.Fatalf("card %q has no row %q", name, series)
	return ""
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

func siteOf(name, metric string, value float64) state.Subject {
	return reading("sites", metric, map[string]string{"site": name}, value)
}

func plain(node, metric string, value float64) state.Subject {
	return reading(node, metric, map[string]string{}, value)
}

func removable(s state.Subject) state.Subject {
	labels := map[string]string{}
	for k, v := range s.Labels {
		labels[k] = v
	}
	labels["removable"] = "true"
	s.Labels = labels
	return s
}

func neverReported(current state.State, node string) state.State {
	current.Nodes = append(current.Nodes, state.Node{Node: node, Configured: true, LastSeen: lastSeen})
	return current
}

// spec: overview.md#items
func TestOverviewItems(t *testing.T) {
	body := overview(t,
		freePct("server-b", "/", 40), freePct("laptop-a", "/", 30), freePct("box-x", "/", 20),
		siteOf("blog-a", "site.requests_per_min", 4), siteOf("shop-c", "site.requests_per_min", 9),
		siteOf("gone-z", "site.requests_per_min", 1),
	)
	equal(t, "sections", flatten(sections(body)), []string{
		"Computers · 3", "box-x", "laptop-a", "server-b",
		"Sites · 2", "blog-a", "shop-c",
	})
	if strings.Contains(body, `data-item="sites"`) || strings.Contains(body, "gone-z") {
		t.Errorf("the sites node or an unlisted site has a card: %s", body)
	}

	t.Run("a service node recorded with no series", func(t *testing.T) {
		current := neverReported(stateOf(freePct("server-b", "/", 40)), "gdrive-a")
		body := showOverview(t, current, held{}, "/overview")
		got := card(t, body, "gdrive-a")
		containsAll(t, "card", got, "No series yet")
		if word := stateWord(t, body, "gdrive-a"); word != "no fresh data" {
			t.Errorf("state = %q, want no fresh data", word)
		}
		equal(t, "sections", flatten(sections(body)), []string{"Computers · 1", "server-b", "Services · 1 · 1 no fresh data", "gdrive-a"})
	})

	t.Run("a node the file lists that never reported", func(t *testing.T) {
		body := overview(t, freePct("server-b", "/", 40))
		if strings.Contains(body, `data-item="server-c"`) {
			t.Errorf("a node that never reported has a card")
		}
	})

	t.Run("a node the configuration no longer names", func(t *testing.T) {
		current := stateOf(freePct("server-b", "/", 40), freePct("server-z", "/", 1))
		current.Nodes[1].Configured = false
		body := showOverview(t, current, held{}, "/overview")
		if strings.Contains(body, "server-z") {
			t.Errorf("a configured-out node has a card")
		}
	})

	t.Run("no item at all", func(t *testing.T) {
		body := showOverview(t, state.State{At: lastSeen}, held{}, "/overview")
		containsAll(t, "page", body, "No node has reported yet")
		if len(sections(body)) != 0 {
			t.Errorf("sections on an empty hub")
		}
	})
}

func flatten(groups [][]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// spec: overview.md#sections
func TestOverviewSections(t *testing.T) {
	body := overview(t,
		siteOf("blog-a", "site.requests_per_min", 4),
		judgedAt(reading("gdrive-a", "gdrive.free_pct", map[string]string{}, 70), evaluate.OK),
		judgedAt(freePct("server-a", "/", 40), evaluate.Warning),
		freePct("server-b", "/", 40), silenceOf("server-b", evaluate.Critical),
		staled(freePct("server-c", "/", 40)),
		judgedAt(freePct("server-d", "/", 40), evaluate.OK),
	)
	equal(t, "headings", headings(body), []string{
		"Computers · 4 · 1 silent · 1 warning · 1 no fresh data", "Sites · 1", "Services · 1",
	})

	t.Run("ok and reporting are not counted", func(t *testing.T) {
		body := overview(t, judgedAt(freePct("server-a", "/", 40), evaluate.OK), freePct("server-b", "/", 40))
		equal(t, "headings", headings(body), []string{"Computers · 2"})
	})
}

func headings(body string) []string {
	var out []string
	for _, section := range sections(body) {
		out = append(out, section[0])
	}
	return out
}

// spec: overview.md#state
func TestOverviewState(t *testing.T) {
	cases := []struct {
		name     string
		subjects []state.Subject
		item     string
		want     string
	}{
		{"silent beats a level", []state.Subject{judgedAt(freePct("server-b", "/", 1), evaluate.Critical), silenceOf("server-b", evaluate.Critical)}, "server-b", "silent"},
		{"every series stale", []state.Subject{staled(freePct("server-b", "/", 40)), silenceOf("server-b", evaluate.OK)}, "server-b", "no fresh data"},
		{"critical", []state.Subject{judgedAt(freePct("server-b", "/", 1), evaluate.Critical)}, "server-b", "critical"},
		{"a stale level does not raise the item", []state.Subject{staled(judgedAt(freePct("server-b", "/a", 1), evaluate.Critical)), judgedAt(freePct("server-b", "/b", 40), evaluate.OK)}, "server-b", "ok"},
		{"warning beats unusual", []state.Subject{judgedAt(freePct("server-b", "/", 9), evaluate.Warning), ranked(plain("server-b", "load.avg_15m", 3), 1)}, "server-b", "warning"},
		{"unusual beats ok", []state.Subject{judgedAt(freePct("server-b", "/", 40), evaluate.OK), ranked(plain("server-b", "load.avg_15m", 3), 1)}, "server-b", "unusual"},
		{"nothing watched", []state.Subject{freePct("server-b", "/", 40)}, "server-b", "reporting, nothing watched"},
		{"a threshold no tick judged yet", []state.Subject{watchedOnly(freePct("server-b", "/", 40))}, "server-b", "reporting, nothing watched"},
		{"a site by its own series", []state.Subject{judgedAt(siteOf("shop-c", "site.server_error_pct", 9), evaluate.Critical), judgedAt(siteOf("blog-a", "site.server_error_pct", 0), evaluate.OK)}, "blog-a", "ok"},
		{"the other site", []state.Subject{judgedAt(siteOf("shop-c", "site.server_error_pct", 9), evaluate.Critical), judgedAt(siteOf("blog-a", "site.server_error_pct", 0), evaluate.OK)}, "shop-c", "critical"},
		{"the sites node silent", []state.Subject{siteOf("blog-a", "site.requests_per_min", 4), silenceOf("sites", evaluate.Critical)}, "blog-a", "silent"},
		{"a site's host silent", []state.Subject{siteOf("shop-c", "site.requests_per_min", 4), silenceOf("server-b", evaluate.Critical), freePct("server-b", "/", 40)}, "shop-c", "silent"},
		{"another host silent", []state.Subject{siteOf("blog-a", "site.requests_per_min", 4), silenceOf("server-b", evaluate.Critical), freePct("server-b", "/", 40)}, "blog-a", "reporting, nothing watched"},
		{"a site's series stale", []state.Subject{staled(siteOf("blog-a", "site.requests_per_min", 4)), siteOf("shop-c", "site.requests_per_min", 9)}, "blog-a", "no fresh data"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stateWord(t, overview(t, c.subjects...), c.item); got != c.want {
				t.Errorf("state = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("a card with no fresh data says when it last reported", func(t *testing.T) {
		got := card(t, overview(t, staled(freePct("laptop-a", "/", 40))), "laptop-a")
		containsAll(t, "card", got, "last report")
	})
}

// spec: overview.md#order
func TestOverviewOrder(t *testing.T) {
	body := overview(t,
		judgedAt(freePct("server-a", "/", 40), evaluate.OK),
		judgedAt(freePct("server-b", "/", 1), evaluate.Critical),
		freePct("server-c", "/", 40), silenceOf("server-c", evaluate.Critical),
		staled(freePct("laptop-a", "/", 40)),
		freePct("server-d", "/", 40),
		judgedAt(freePct("server-e", "/", 40), evaluate.OK),
	)
	equal(t, "cards", sections(body)[0][1:], []string{"server-c", "server-b", "server-a", "server-e", "server-d", "laptop-a"})
}

func tmAge(node, destination string, value float64) state.Subject {
	return reading(node, "timemachine.backup_age_seconds", map[string]string{"destination": destination}, value)
}

// spec: overview.md#rows
func TestOverviewRows(t *testing.T) {
	linux := []state.Subject{
		freePct("server-b", "/", 40), freePct("server-b", "/srv", 60),
		plain("server-b", "memory.available_pct", 50), plain("server-b", "load.avg_15m", 0.4),
		plain("server-b", "systemd.failed_units", 0), plain("server-b", "uptime.boot_seconds", 9),
	}
	cases := []struct {
		name     string
		subjects []state.Subject
		item     string
		want     []string
	}{
		{"the defaults", linux, "server-b", []string{"disk.free_pct /", "memory.available_pct", "load.avg_15m", "systemd.failed_units"}},
		{"what needs attention first", append([]state.Subject{ranked(plain("server-b", "load.avg_15m", 3), 1)}, linux[:3]...), "server-b",
			[]string{"load.avg_15m", "disk.free_pct /", "memory.available_pct"}},
		{"a byte series at warning", []state.Subject{judgedAt(free("server-b", "/", 9e9), evaluate.Warning), freePct("server-b", "/", 40), freePct("server-b", "/srv", 12)}, "server-b",
			[]string{"disk.free_bytes /", "disk.free_pct /srv"}},
		{"a removable volume is never a default", []state.Subject{removable(freePct("server-b", "/usb", 3)), freePct("server-b", "/", 40)}, "server-b", []string{"disk.free_pct /"}},
		{"every volume removable", []state.Subject{removable(freePct("server-b", "/usb", 3)), plain("server-b", "load.avg_15m", 1)}, "server-b", []string{"load.avg_15m"}},
		{"a tie goes to the first", []state.Subject{freePct("server-b", "/b", 12), freePct("server-b", "/a", 12)}, "server-b", []string{"disk.free_pct /a"}},
		{"a stale volume is not the least free while one is fresh", []state.Subject{staled(freePct("server-b", "/a", 1)), freePct("server-b", "/b", 40), plain("server-b", "load.avg_15m", 1)}, "server-b",
			[]string{"disk.free_pct /b", "load.avg_15m"}},
		{"the oldest backup", []state.Subject{tmAge("laptop-a", "a", 2*86400), tmAge("laptop-a", "b", 9*86400)}, "laptop-a", []string{"timemachine.backup_age_seconds destination=b"}},
		{"the oldest fresh backup", []state.Subject{tmAge("laptop-a", "a", 2*86400), staled(tmAge("laptop-a", "b", 9*86400)), plain("laptop-a", "load.avg_15m", 1)}, "laptop-a",
			[]string{"load.avg_15m", "timemachine.backup_age_seconds destination=a"}},
		{"a site", []state.Subject{siteOf("blog-a", "site.pageviews_24h", 9), siteOf("blog-a", "site.response_p95_seconds", 0.3), siteOf("blog-a", "site.server_error_pct", 0), siteOf("blog-a", "site.requests_per_min", 4)}, "blog-a",
			[]string{"site.requests_per_min", "site.server_error_pct", "site.response_p95_seconds"}},
		{"a service", []state.Subject{reading("gdrive-a", "gdrive.free_pct", map[string]string{}, 75), reading("gdrive-a", "gdrive.free_bytes", map[string]string{}, 8e10)}, "gdrive-a", []string{"gdrive.free_pct"}},
		{"the first two the page charts", []state.Subject{plain("box-x", "uptime.boot_seconds", 9), plain("box-x", "queue.depth", 2), plain("box-x", "net.rx_bytes", 1)}, "box-x",
			[]string{"net.rx_bytes", "queue.depth"}},
		{"a series needing attention that is a default", []state.Subject{judgedAt(plain("server-b", "load.avg_15m", 9), evaluate.Warning), freePct("server-b", "/", 40)}, "server-b",
			[]string{"load.avg_15m", "disk.free_pct /"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			equal(t, "rows", cardRows(t, overview(t, c.subjects...), c.item), c.want)
		})
	}

	t.Run("an item whose only anomaly is sixth on the hub", func(t *testing.T) {
		subjects := []state.Subject{plain("server-a", "memory.available_pct", 50), ranked(plain("server-a", "load.avg_1m", 9), 6)}
		for i, node := range []string{"server-b", "server-c", "server-d", "server-e", "laptop-a"} {
			subjects = append(subjects, ranked(plain(node, "load.avg_1m", 9), i+1))
		}
		equal(t, "rows", cardRows(t, overview(t, subjects...), "server-a")[:1], []string{"load.avg_1m"})
	})

	t.Run("six rows at most", func(t *testing.T) {
		six := []state.Subject{}
		for _, mount := range []string{"/a", "/b", "/c", "/d", "/e", "/f"} {
			six = append(six, judgedAt(freePct("server-b", mount, 9), evaluate.Warning))
		}
		body := overview(t, six...)
		if got := cardRows(t, body, "server-b"); len(got) != 6 {
			t.Errorf("rows = %d, want 6", len(got))
		}
		if strings.Contains(card(t, body, "server-b"), "more") {
			t.Errorf("a more line with six qualifying")
		}

		eight := []state.Subject{
			judgedAt(freePct("server-b", "/a", 9), evaluate.Warning), judgedAt(freePct("server-b", "/b", 9), evaluate.Warning),
			judgedAt(freePct("server-b", "/c", 9), evaluate.Warning), judgedAt(freePct("server-b", "/d", 9), evaluate.Warning),
			freePct("server-b", "/e", 5), plain("server-b", "memory.available_pct", 50),
			plain("server-b", "load.avg_15m", 1), plain("server-b", "systemd.failed_units", 0),
		}
		body = overview(t, eight...)
		containsAll(t, "card", card(t, body, "server-b"), "2 more")
	})

	t.Run("marks", func(t *testing.T) {
		body := overview(t,
			judgedAt(freePct("server-b", "/a", 1), evaluate.Critical),
			ranked(judgedAt(freePct("server-b", "/b", 1), evaluate.Critical), 1),
			staled(judgedAt(freePct("server-b", "/c", 1), evaluate.Critical)),
			freePct("server-b", "/d", 50),
		)
		for series, want := range map[string]string{
			"disk.free_pct /a": "critical", "disk.free_pct /b": "critical", "disk.free_pct /c": "no fresh data",
		} {
			if got := rowMark(t, body, "server-b", series); got != want {
				t.Errorf("%s marked %q, want %q", series, got, want)
			}
		}
	})

	t.Run("names", func(t *testing.T) {
		body := overview(t, reading("box-x", "queue.depth", map[string]string{"queue": "payments"}, 2), siteOf("blog-a", "site.requests_per_min", 4))
		equal(t, "rows", cardRows(t, body, "box-x"), []string{"queue.depth queue=payments"})
		equal(t, "rows", cardRows(t, body, "blog-a"), []string{"site.requests_per_min"})
		equal(t, "rows", cardRows(t, overview(t, freePct("server-b", "/srv", 40)), "server-b"), []string{"disk.free_pct /srv"})
	})

	t.Run("a value in its unit", func(t *testing.T) {
		containsAll(t, "card", card(t, overview(t, freePct("server-b", "/", 40)), "server-b"), "40.0%")
	})

	t.Run("a card links to its item", func(t *testing.T) {
		body := showOverview(t, stateOf(freePct("server-b", "/", 40), siteOf("blog-a", "site.requests_per_min", 4)), held{}, "/overview?lang=ru")
		containsAll(t, "cards", body, `href="/item?lang=ru&amp;node=server-b"`, `href="/item?label.site=blog-a&amp;lang=ru&amp;node=sites"`)
	})
}

// spec: overview.md#rows
func TestOverviewLines(t *testing.T) {
	ref := func(mount string) storage.SeriesRef {
		return storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}}
	}
	points := held{
		heldKey(ref("/a")): {{TS: lastSeen.Add(-3 * time.Hour), Value: 10}, {TS: lastSeen.Add(-3*time.Hour + 5*time.Minute), Value: 11}},
		heldKey(ref("/b")): {{TS: lastSeen.Add(-20 * time.Hour), Value: 10}, {TS: lastSeen.Add(-20*time.Hour + 5*time.Minute), Value: 11},
			{TS: lastSeen.Add(-time.Hour), Value: 12}, {TS: lastSeen.Add(-time.Hour + 5*time.Minute), Value: 12}},
		heldKey(ref("/c")): {{TS: lastSeen.Add(-time.Hour), Value: 10}},
	}
	current := stateOf(
		judgedAt(freePct("server-b", "/a", 9), evaluate.Warning), judgedAt(freePct("server-b", "/b", 9), evaluate.Warning),
		judgedAt(freePct("server-b", "/c", 9), evaluate.Warning),
	)
	body := card(t, showOverview(t, current, points, "/overview"), "server-b")
	found := rowPattern.FindAllStringSubmatch(body, -1)
	if len(found) != 3 {
		t.Fatalf("rows = %d", len(found))
	}
	if !strings.Contains(found[0][0], `<svg class="spark"`) || strings.Count(found[0][0], "<polyline") != 1 {
		t.Errorf("first row has no line: %s", found[0][0])
	}
	if strings.Count(found[1][0], "<polyline") != 2 {
		t.Errorf("a gap does not break the second row's line: %s", found[1][0])
	}
	if strings.Contains(found[2][0], "<svg") {
		t.Errorf("the third row has a line")
	}

	t.Run("no point in the last day", func(t *testing.T) {
		body := card(t, showOverview(t, stateOf(freePct("server-b", "/", 9)), held{}, "/overview"), "server-b")
		if strings.Contains(body, "<svg") {
			t.Errorf("a line with no points")
		}
	})
	t.Run("a single point", func(t *testing.T) {
		body := card(t, showOverview(t, stateOf(judgedAt(freePct("server-b", "/c", 9), evaluate.Warning)), points, "/overview"), "server-b")
		if !strings.Contains(body, `<polyline class="point"`) {
			t.Errorf("a lone point is not drawn: %s", body)
		}
	})
}

// spec: overview.md#overview-page
func TestOverviewPage(t *testing.T) {
	body := overview(t, freePct("server-b", "/", 40))
	containsAll(t, "page", body, `data-skin="overview" aria-current="page"`, "monitor-live")

	ru := showOverview(t, stateOf(freePct("server-b", "/", 40)), held{}, "/overview?lang=ru")
	containsAll(t, "page", ru, "Компьютеры", "на связи, без порогов")

	other := showOverview(t, stateOf(freePct("server-b", "/", 40)), held{}, "/overview?x=1")
	containsAll(t, "page", other, `data-item="server-b"`)

	t.Run("the state cannot be read", func(t *testing.T) {
		rec := httptest.NewRecorder()
		read := func(context.Context) (state.State, error) { return state.State{}, errFailed }
		hub.Overview(read, historyOf(held{}), layout()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// spec: overview.md#rows — a line reads exactly its series: an empty label or a family of
// sixty siblings costs the card nothing.
func TestOverviewLineReadsItsOwnSeries(t *testing.T) {
	odd := reading("box-x", "queue.depth", map[string]string{"queue": ""}, 2)
	points := held{heldKey(storage.SeriesRef{Node: "box-x", Metric: "queue.depth", Labels: map[string]string{"queue": ""}}): {
		{TS: lastSeen.Add(-time.Hour), Value: 2}, {TS: lastSeen.Add(-55 * time.Minute), Value: 3},
	}}
	subjects := []state.Subject{odd}
	for i := range 60 {
		labels := map[string]string{"queue": "", "shard": strings.Repeat("s", i+1)}
		subjects = append(subjects, reading("box-x", "queue.depth", labels, 1))
		points[heldKey(storage.SeriesRef{Node: "box-x", Metric: "queue.depth", Labels: labels})] = []storage.Point{{TS: lastSeen.Add(-time.Hour), Value: 1}}
	}
	body := card(t, showOverview(t, stateOf(subjects...), points, "/overview"), "box-x")
	if !strings.Contains(body, "<polyline") {
		t.Errorf("the empty-labelled series has no line: %s", body)
	}
}
