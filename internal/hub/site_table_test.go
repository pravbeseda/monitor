package hub_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

var (
	headerPattern   = regexp.MustCompile(`(?s)<th scope="col" data-column="([^"]*)"(?: aria-sort="([^"]*)")?>(.*?)</th>`)
	siteCellPattern = regexp.MustCompile(`(?s)<td class="cell([^"]*)" data-column="([^"]*)"(?: title="([^"]*)")?>(.*?)</td>`)
	dayPattern      = regexp.MustCompile(`(?s)<td class="day">(.*?)</td>`)
	alsoPattern     = regexp.MustCompile(`(?s)<td class="also">(.*?)</td>`)
	alsoItem        = regexp.MustCompile(`<span class="also-item">([^<]*) <span class="value([^"]*)"[^>]*>([^<]*)</span>`)
	linkPattern     = regexp.MustCompile(`href="([^"]*)"`)
)

// header is a column header as a reader sees it: its text, where its link goes, and the
// direction it marks.
type header struct{ column, text, link, sort string }

func headersOf(body string) []header {
	var out []header
	for _, found := range headerPattern.FindAllStringSubmatch(body, -1) {
		one := header{column: found[1], sort: found[2], text: strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(found[3], ""))}
		if link := linkPattern.FindStringSubmatch(found[3]); link != nil {
			one.link = strings.ReplaceAll(link[1], "&amp;", "&")
		}
		out = append(out, one)
	}
	return out
}

func headerOf(t *testing.T, body, column string) header {
	t.Helper()
	for _, one := range headersOf(body) {
		if one.column == column {
			return one
		}
	}
	t.Fatalf("no header %q", column)
	return header{}
}

// cell is a value cell of a site's row as a reader sees it.
type cell struct{ class, title, body string }

func (c cell) values() []string { return line{body: c.body}.values() }

func (c cell) why() whySaid { return line{body: c.body}.whySaid() }

func (c cell) dash() bool { return strings.TrimSpace(c.body) == "—" }

func siteCell(t *testing.T, row, column string) cell {
	t.Helper()
	for _, found := range siteCellPattern.FindAllStringSubmatch(row, -1) {
		if found[2] == column {
			return cell{class: found[1], title: found[3], body: found[4]}
		}
	}
	t.Fatalf("no cell %q in %s", column, row)
	return cell{}
}

// also is what a site's row lists under "Also": "name value" or "name value (class)".
func alsoOf(t *testing.T, row string) (items []string, rest string) {
	t.Helper()
	found := alsoPattern.FindStringSubmatch(row)
	if found == nil {
		t.Fatalf("no also cell in %s", row)
	}
	for _, item := range alsoItem.FindAllStringSubmatch(found[1], -1) {
		text := item[1] + " " + item[3]
		if class := strings.TrimSpace(item[2]); class != "" {
			text += " (" + class + ")"
		}
		items = append(items, text)
	}
	return items, alsoItem.ReplaceAllString(found[1], "")
}

// sites are blog-a and shop-c as the sites table's rows assume them.
func sites() []state.Subject {
	return []state.Subject{
		siteOf("blog-a", "site.requests_per_min", 12.6), siteOf("blog-a", "site.pageviews_24h", 88),
		siteOf("blog-a", "site.server_error_pct", 0.1), siteOf("blog-a", "site.response_p95_seconds", 0.4),
		siteOf("shop-c", "site.requests_per_min", 3), siteOf("shop-c", "site.server_error_pct", 0),
	}
}

func siteRef(site, metric string) storage.SeriesRef {
	return storage.SeriesRef{Node: "sites", Metric: metric, Labels: map[string]string{"site": site}}
}

// aDay are two points of the last day, enough for a line.
var aDay = []storage.Point{{TS: lastSeen.Add(-2 * time.Hour), Value: 10}, {TS: lastSeen.Add(-time.Hour), Value: 12}}

var traffic = held{heldKey(siteRef("blog-a", "site.requests_per_min")): aDay}

func siteRow(t *testing.T, subjects []state.Subject, source held, stored limits, site string) string {
	t.Helper()
	return showCard(t, stateOf(subjects...), source, stored, site)
}

func sitesAt(t *testing.T, target string, subjects ...state.Subject) string {
	t.Helper()
	return showOverview(t, stateOf(subjects...), held{}, target)
}

func siteOrder(body string) []string {
	for _, section := range sections(body) {
		if strings.HasPrefix(section[0], "Sites") {
			return section[1:]
		}
	}
	return nil
}

// spec: overview.md#site-table
func TestSitesTable(t *testing.T) {
	t.Run("any overview with sites", func(t *testing.T) {
		body := sitesAt(t, "/overview", sites()...)
		var texts []string
		for _, one := range headersOf(body) {
			texts = append(texts, one.text)
		}
		equal(t, "headers", texts, []string{"Site", "Requests/min", "Last 24 h", "Views/day", "Server errors", "Response p95", "Also"})
		sitesSection := sectionPattern.FindAllString(body, -1)
		if len(sitesSection) != 1 || cardPattern.MatchString(sitesSection[0]) || len(sitePattern.FindAllString(sitesSection[0], -1)) != 2 {
			t.Errorf("the sites are not two rows of a table and no card: %v", sitesSection)
		}
	})
	t.Run("the sites above", func(t *testing.T) {
		body := showOverview(t, stateOf(sites()...), traffic, "/overview")
		equal(t, "rows", siteOrder(body), []string{"blog-a", "shop-c"})
		row := card(t, body, "blog-a")
		var values []string
		for _, column := range []string{"requests", "views", "errors", "latency"} {
			values = append(values, strings.Join(siteCell(t, row, column).values(), " "))
		}
		equal(t, "values", values, []string{"12.60", "88.00", "0.1%", "400 ms"})
		if day := dayPattern.FindStringSubmatch(row); day == nil || !strings.Contains(day[1], `<svg class="spark"`) || strings.Contains(day[1], `class="threshold`) {
			t.Errorf("no line of the day, or one with a threshold: %s", row)
		}
		if items, rest := alsoOf(t, row); len(items) != 0 || strings.TrimSpace(rest) != "" {
			t.Errorf("something under Also: %q %q", items, rest)
		}
	})
	t.Run("no views, no response time", func(t *testing.T) {
		row := card(t, sitesAt(t, "/overview", sites()...), "shop-c")
		if !siteCell(t, row, "views").dash() || !siteCell(t, row, "latency").dash() {
			t.Errorf("no dash in the missing cells: %s", row)
		}
	})
	t.Run("no point in the last day", func(t *testing.T) {
		row := card(t, sitesAt(t, "/overview", sites()...), "blog-a")
		if day := dayPattern.FindStringSubmatch(row); day == nil || strings.TrimSpace(day[1]) != "" {
			t.Errorf("the day cell is not empty: %s", row)
		}
		equal(t, "values", siteCell(t, row, "requests").values(), []string{"12.60"})
	})
	t.Run("a site's row", func(t *testing.T) {
		row := card(t, sitesAt(t, "/overview?lang=ru", sites()...), "blog-a")
		containsAll(t, "row", row, `href="/item?label.site=blog-a&amp;lang=ru&amp;node=sites"`, `<span class="dot" title="на связи, без порогов" aria-hidden="true"></span>`,
			`<span class="site-state row-mark-word">на связи, без порогов</span>`)
		if sorted := card(t, sitesAt(t, "/overview?sort=-views", sites()...), "blog-a"); strings.Contains(sorted, "sort=") {
			t.Errorf("a site's link carries the sort: %s", sorted)
		}
	})
	t.Run("requests unusual", func(t *testing.T) {
		row := siteRow(t, replaced(sites(), banded(siteOf("blog-a", "site.requests_per_min", 34.2), 8, 9.5, 11, 1)), traffic, limits{}, "blog-a")
		requests := siteCell(t, row, "requests")
		equal(t, "values", requests.values(), []string{"34.20 (level-unusual marked)"})
		if requests.why().text != "usually 9.50" {
			t.Errorf("why %+v", requests.why())
		}
		if items, _ := alsoOf(t, row); len(items) != 0 {
			t.Errorf("under Also: %q", items)
		}
	})
	t.Run("requests at warning under a readable threshold", func(t *testing.T) {
		stored := limits{heldKey(siteRef("blog-a", "site.requests_per_min")): {Direction: storage.Above, Warning: f(30)}}
		row := siteRow(t, replaced(sites(), judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Warning)), traffic, stored, "blog-a")
		requests := siteCell(t, row, "requests")
		equal(t, "values", requests.values(), []string{"34.20 (level-warning marked)"})
		if requests.why().text != "threshold 30.00" || strings.Contains(row, `class="threshold`) || !strings.Contains(row, `<svg class="spark"`) {
			t.Errorf("why %+v: %s", requests.why(), row)
		}
	})
	t.Run("requests at warning, the threshold unreadable", func(t *testing.T) {
		row := siteRow(t, replaced(sites(), judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Warning)), traffic, limits{}, "blog-a")
		if why := siteCell(t, row, "requests").why(); why.text != "warning" {
			t.Errorf("why %+v", why)
		}
	})
	t.Run("requests at critical and stale", func(t *testing.T) {
		row := siteRow(t, replaced(sites(), staled(judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Critical))), traffic, limits{}, "blog-a")
		requests := siteCell(t, row, "requests")
		equal(t, "values", requests.values(), []string{"34.20 (level-stale)"})
		if !strings.Contains(requests.class, "faded") || requests.why() != (whySaid{}) {
			t.Errorf("cell %q, why %+v", requests.class, requests.why())
		}
	})
	t.Run("server errors stale and unwatched", func(t *testing.T) {
		errors := siteCell(t, card(t, sitesAt(t, "/overview", replaced(sites(), staled(siteOf("blog-a", "site.server_error_pct", 0.1)))...), "blog-a"), "errors")
		if !strings.Contains(errors.class, "faded") || errors.why() != (whySaid{}) {
			t.Errorf("cell %q, why %+v", errors.class, errors.why())
		}
	})
	t.Run("any site's row", func(t *testing.T) {
		row := card(t, showOverview(t, stateOf(replaced(sites(), judgedAt(siteOf("blog-a", "site.server_error_pct", 0.1), evaluate.OK))...), traffic, "/overview"), "blog-a")
		if strings.Contains(row, "card-reason") || strings.Contains(row, "All within thresholds") {
			t.Errorf("a reason in a site's row: %s", row)
		}
	})
	t.Run("a six-hour gap in the day", func(t *testing.T) {
		gap := held{heldKey(siteRef("blog-a", "site.requests_per_min")): {
			{TS: lastSeen.Add(-20 * time.Hour), Value: 4}, {TS: lastSeen.Add(-19*time.Hour - 55*time.Minute), Value: 5},
			{TS: lastSeen.Add(-10 * time.Hour), Value: 6}, {TS: lastSeen.Add(-9*time.Hour - 55*time.Minute), Value: 7},
		}}
		row := card(t, showOverview(t, stateOf(sites()...), gap, "/overview"), "blog-a")
		if day := dayPattern.FindStringSubmatch(row); day == nil || strings.Count(day[1], "<polyline points=") != 2 {
			t.Errorf("the line is not broken across the gap: %s", row)
		}
	})
	t.Run("requests stale fades the line of the day", func(t *testing.T) {
		row := siteRow(t, replaced(sites(), staled(siteOf("blog-a", "site.requests_per_min", 12.6))), traffic, limits{}, "blog-a")
		if !strings.Contains(siteCell(t, row, "requests").class, "faded") {
			t.Errorf("the requests cell is not faded")
		}
		containsAll(t, "stylesheet", sitesAt(t, "/overview", sites()...), "td.cell.faded + td.day svg")
	})
	t.Run("no requests", func(t *testing.T) {
		row := card(t, sitesAt(t, "/overview", without(sites(), "site.requests_per_min")...), "blog-a")
		if day := dayPattern.FindStringSubmatch(row); !siteCell(t, row, "requests").dash() || day == nil || strings.TrimSpace(day[1]) != "" {
			t.Errorf("no dash and an empty day: %s", row)
		}
	})
	t.Run("server errors at critical", func(t *testing.T) {
		body := sitesAt(t, "/overview", replaced(sites(), judgedAt(siteOf("shop-c", "site.server_error_pct", 9), evaluate.Critical))...)
		row := card(t, body, "shop-c")
		equal(t, "values", siteCell(t, row, "errors").values(), []string{"9.0% (level-critical marked)"})
		if !strings.HasPrefix(row, `<tr class="site state-critical"`) {
			t.Errorf("the row is not red: %.80s", row)
		}
		equal(t, "rows", siteOrder(body), []string{"shop-c", "blog-a"})
	})
	t.Run("a series no column shows", func(t *testing.T) {
		row := siteRow(t, append(sites(), judgedAt(siteOf("blog-a", "site.client_error_pct", 4), evaluate.Warning)), traffic, limits{}, "blog-a")
		items, _ := alsoOf(t, row)
		equal(t, "also", items, []string{"site.client_error_pct 4.0% (level-warning marked)"})
	})
	t.Run("in the order attention lists them", func(t *testing.T) {
		row := siteRow(t, append(sites(), ranked(siteOf("blog-a", "uptime.x", 9), 1), judgedAt(siteOf("blog-a", "site.client_error_pct", 4), evaluate.Warning)), traffic, limits{}, "blog-a")
		items, _ := alsoOf(t, row)
		equal(t, "also", items, []string{"site.client_error_pct 4.0% (level-warning marked)", "uptime.x 9.00 (level-unusual marked)"})
	})
	t.Run("at most two, then more", func(t *testing.T) {
		row := siteRow(t, append(sites(), judgedAt(siteOf("blog-a", "queue.a", 1), evaluate.Warning),
			judgedAt(siteOf("blog-a", "queue.b", 1), evaluate.Warning), judgedAt(siteOf("blog-a", "queue.c", 1), evaluate.Warning)), traffic, limits{}, "blog-a")
		items, rest := alsoOf(t, row)
		equal(t, "also", items, []string{"queue.a 1.00 (level-warning marked)", "queue.b 1.00 (level-warning marked)"})
		containsAll(t, "also", rest, `<span class="card-more">1 more</span>`)
	})
	t.Run("a silent site", func(t *testing.T) {
		row := siteRow(t, append(sites(), silenceOf("sites", evaluate.Critical)), traffic, limits{}, "blog-a")
		if _, rest := alsoOf(t, row); !strings.Contains(rest, `<span class="card-seen late">last report `) {
			t.Errorf("no last report under Also: %s", rest)
		}
	})
	t.Run("a host silent and a series needing attention", func(t *testing.T) {
		subjects := append(sites(), judgedAt(siteOf("shop-c", "site.client_error_pct", 4), evaluate.Warning), silenceOf("server-b", evaluate.Critical), freePct("server-b", "/", 40))
		row := card(t, sitesAt(t, "/overview", subjects...), "shop-c")
		items, _ := alsoOf(t, row)
		equal(t, "also", items, []string{"site.client_error_pct 4.0% (level-warning marked)"})
		if entry, seen := strings.Index(row, `class="also-item"`), strings.Index(row, `<span class="card-seen late">last report `); entry < 0 || seen < entry {
			t.Errorf("no last report after the entries: %s", row)
		}
	})
	t.Run("none of the columns' series, nothing needing attention", func(t *testing.T) {
		row := card(t, sitesAt(t, "/overview", siteOf("blog-a", "uptime.x", 9)), "blog-a")
		for _, column := range []string{"requests", "views", "errors", "latency"} {
			if !siteCell(t, row, column).dash() {
				t.Errorf("%s is not a dash", column)
			}
		}
		if items, rest := alsoOf(t, row); len(items) != 0 || strings.TrimSpace(rest) != "" {
			t.Errorf("under Also: %q %q", items, rest)
		}
	})
	t.Run("the dot of no fresh data", func(t *testing.T) {
		containsAll(t, "stylesheet", sitesAt(t, "/overview", sites()...), "tr.site.state-no-data { --tone: var(--rule); }")
	})
	t.Run("a site with no fresh data", func(t *testing.T) {
		var asleep []state.Subject
		for _, s := range sites() {
			if s.Labels["site"] == "blog-a" {
				s = staled(s)
			}
			asleep = append(asleep, s)
		}
		if _, rest := alsoOf(t, siteRow(t, asleep, traffic, limits{}, "blog-a")); !strings.Contains(rest, "last report ") {
			t.Errorf("no last report under Also: %s", rest)
		}
	})
	t.Run("a site reporting", func(t *testing.T) {
		if row := card(t, sitesAt(t, "/overview", sites()...), "blog-a"); strings.Contains(row, "last report") {
			t.Errorf("a last report on a reporting site: %s", row)
		}
	})
	t.Run("a value cell on hover", func(t *testing.T) {
		if views := siteCell(t, card(t, sitesAt(t, "/overview", sites()...), "blog-a"), "views"); views.title != "site.pageviews_24h" {
			t.Errorf("hover %q", views.title)
		}
	})
}

// spec: overview.md#site-table — sorting.
func TestSitesTableSort(t *testing.T) {
	more := append(sites(), siteOf("shop-c", "site.pageviews_24h", 0))
	t.Run("no sort chosen", func(t *testing.T) {
		body := sitesAt(t, "/overview", sites()...)
		equal(t, "rows", siteOrder(body), []string{"blog-a", "shop-c"})
		for _, one := range headersOf(body) {
			if one.sort != "" {
				t.Errorf("header %q marked %q", one.column, one.sort)
			}
		}
		if views := headerOf(t, body, "views"); views.link != "/overview?sort=-views" {
			t.Errorf("views links to %q", views.link)
		}
		if site := headerOf(t, body, "site"); site.link != "/overview?sort=site" {
			t.Errorf("site links to %q", site.link)
		}
		for _, column := range []string{"day", "also"} {
			if one := headerOf(t, body, column); one.link != "" {
				t.Errorf("%s sorts: %q", column, one.link)
			}
		}
	})
	t.Run("from the most views down", func(t *testing.T) {
		body := sitesAt(t, "/overview?sort=-views", more...)
		equal(t, "rows", siteOrder(body), []string{"blog-a", "shop-c"})
		views := headerOf(t, body, "views")
		if views.sort != "descending" || !strings.Contains(views.text, "▼") || views.link != "/overview?sort=views" {
			t.Errorf("views header %+v", views)
		}
	})
	t.Run("from the fewest up", func(t *testing.T) {
		body := sitesAt(t, "/overview?sort=views", more...)
		equal(t, "rows", siteOrder(body), []string{"shop-c", "blog-a"})
		views := headerOf(t, body, "views")
		if views.sort != "ascending" || !strings.Contains(views.text, "▲") || views.link != "/overview?sort=-views" {
			t.Errorf("views header %+v", views)
		}
	})
	t.Run("by name, no red run, and reversed", func(t *testing.T) {
		body := sitesAt(t, "/overview?sort=site", replaced(sites(), judgedAt(siteOf("shop-c", "site.server_error_pct", 9), evaluate.Critical))...)
		equal(t, "rows", siteOrder(body), []string{"blog-a", "shop-c"})
		if site := headerOf(t, body, "site"); site.sort != "ascending" || !strings.Contains(site.text, "▲") || site.link != "/overview?sort=-site" {
			t.Errorf("site header %+v", site)
		}
		if views := headerOf(t, body, "views"); views.link != "/overview?sort=-views" || views.sort != "" {
			t.Errorf("views header %+v", views)
		}
		reversed := sitesAt(t, "/overview?sort=-site", sites()...)
		equal(t, "rows", siteOrder(reversed), []string{"shop-c", "blog-a"})
		if site := headerOf(t, reversed, "site"); site.sort != "descending" || !strings.Contains(site.text, "▼") {
			t.Errorf("site header %+v", site)
		}
	})
	t.Run("a stale value by its value", func(t *testing.T) {
		stale := replaced(sites(), staled(siteOf("blog-a", "site.requests_per_min", 12.6)))
		equal(t, "rows", siteOrder(sitesAt(t, "/overview?sort=-requests", stale...)), []string{"blog-a", "shop-c"})
	})
	t.Run("the sort is the sites' alone", func(t *testing.T) {
		body := sitesAt(t, "/overview?sort=-views", append(sites(), judgedAt(freePct("server-b", "/", 40), evaluate.OK), judgedAt(freePct("server-c", "/", 1), evaluate.Critical))...)
		equal(t, "computers", sections(body)[0][1:], []string{"server-c", "server-b"})
	})
	t.Run("no value stands last", func(t *testing.T) {
		for _, target := range []string{"/overview?sort=-views", "/overview?sort=views"} {
			equal(t, target, siteOrder(sitesAt(t, target, sites()...)), []string{"blog-a", "shop-c"})
		}
	})
	t.Run("equal values by name", func(t *testing.T) {
		same := replaced(sites(), siteOf("shop-c", "site.requests_per_min", 12.6))
		for _, target := range []string{"/overview?sort=-requests", "/overview?sort=requests"} {
			equal(t, target, siteOrder(sitesAt(t, target, same...)), []string{"blog-a", "shop-c"})
		}
	})
	t.Run("the links keep the language", func(t *testing.T) {
		if views := headerOf(t, sitesAt(t, "/overview?lang=ru&sort=-views", more...), "views"); views.link != "/overview?lang=ru&sort=views" {
			t.Errorf("views links to %q", views.link)
		}
	})
	t.Run("a sort it does not know", func(t *testing.T) {
		for _, sort := range []string{"nonsense", "day", "also", "", "--views", "Views"} {
			body := sitesAt(t, "/overview?sort="+sort, replaced(sites(), judgedAt(siteOf("shop-c", "site.server_error_pct", 9), evaluate.Critical))...)
			equal(t, sort, siteOrder(body), []string{"shop-c", "blog-a"})
			for _, one := range headersOf(body) {
				if one.sort != "" {
					t.Errorf("sort=%s marks %q", sort, one.column)
				}
			}
		}
	})
	t.Run("a narrow screen", func(t *testing.T) {
		containsAll(t, "stylesheet", sitesAt(t, "/overview", sites()...),
			".sites-scroll { overflow-x: auto;", "table.sites .site-name { position: sticky; left: 0;", `table.sites th[data-column="site"] { position: sticky; left: 0;`)
	})
	t.Run("the refresh keeps the sort", func(t *testing.T) {
		// The page refreshes by fetching its own address, so the sort in it is all it needs.
		body := sitesAt(t, "/overview?sort=views", more...)
		containsAll(t, "page", body, `<meta name="monitor-live"`)
		equal(t, "rows", siteOrder(body), []string{"shop-c", "blog-a"})
	})
}
