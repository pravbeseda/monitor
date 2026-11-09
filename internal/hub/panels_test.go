package hub_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/state"
)

var (
	panelPattern     = regexp.MustCompile(`(?s)<article class="group state-([\w-]+)" data-group="([^"]*)">(.*?)</article>`)
	columnPattern    = regexp.MustCompile(`<th class="group-column" scope="col" title="([^"]*)">([^<]*)</th>`)
	panelRowPattern  = regexp.MustCompile(`(?s)<tr class="group-row">(.*?)</tr>`)
	rowNamePattern   = regexp.MustCompile(`(?s)<th scope="row"[^>]*>.*?</th>`)
	panelCellPattern = regexp.MustCompile(`(?s)<td[^>]*>.*?</td>`)
)

// grouped is current with the backup sensor's names and a group label per family.
func grouped(current state.State, groups map[string]string) state.State {
	current.Names = names.Book{
		Metrics: map[string]names.Text{
			"backup.failed":                        {i18n.English: "Failed {backup} {job}", i18n.Russian: "Сбой {backup} {job}"},
			"backup.success_age_seconds":           {i18n.English: "Since success {backup} {job}", i18n.Russian: "С успеха {backup} {job}"},
			"backup.archives":                      {i18n.English: "Archives {backup} {job}", i18n.Russian: "Архивов {backup} {job}"},
			"backup.latest_archive_age_seconds":    {i18n.English: "Newest archive {backup} {job}", i18n.Russian: "Свежий архив {backup} {job}"},
			"backup.stored_bytes":                  {i18n.English: "Stored {backup} {job}", i18n.Russian: "Занято {backup} {job}"},
			"backup.latest_archive_original_bytes": {i18n.English: "Newest archive data {backup} {job}", i18n.Russian: "Данные архива {backup} {job}"},
		},
		Families: map[string]names.Text{"backup": {i18n.English: "Backups", i18n.Russian: "Бэкапы"}},
		Groups:   groups,
	}
	return current
}

func job(node, backup, job, metric string, value float64) state.Subject {
	return reading(node, "backup."+metric, map[string]string{"backup": backup, "job": job}, value)
}

// mirror is server-b's backup series: web and vpn, each a copy and a maintain job.
func mirror() []state.Subject {
	var out []state.Subject
	for _, backup := range []string{"web", "vpn"} {
		out = append(out,
			job("server-b", backup, "copy", "failed", 0),
			job("server-b", backup, "copy", "success_age_seconds", 4*3600),
			job("server-b", backup, "copy", "latest_archive_age_seconds", 7*3600),
			job("server-b", backup, "copy", "archives", 10),
			job("server-b", backup, "copy", "stored_bytes", 5.1e9),
			job("server-b", backup, "maintain", "failed", 0),
			job("server-b", backup, "maintain", "success_age_seconds", 4*3600),
			job("server-b", backup, "maintain", "archives", 8),
		)
	}
	return out
}

var byBackup = map[string]string{"backup": "backup"}

type panel struct{ class, title, body string }

func panelsOf(body string) []panel {
	var out []panel
	for _, found := range panelPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, panel{class: found[1], title: html.UnescapeString(found[2]), body: found[3]})
	}
	return out
}

func panelOf(t *testing.T, body, title string) panel {
	t.Helper()
	for _, g := range panelsOf(body) {
		if g.title == title {
			return g
		}
	}
	t.Fatalf("no panel %q in %v", title, titles(body))
	return panel{}
}

func titles(body string) []string {
	var out []string
	for _, g := range panelsOf(body) {
		out = append(out, g.title)
	}
	return out
}

// text is markup as a reader reads it: tags dropped, entities decoded, spaces closed up.
func text(markup string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(markup, " "))), " ")
}

// visible is markup as a sighted reader sees it: a mark's word, kept for a screen reader,
// left out.
func visible(markup string) string {
	return text(markPattern.ReplaceAllString(markup, ""))
}

// columns are a panel's column headings and the metric id each shows on hover.
func columns(g panel) (labels, metrics []string) {
	for _, found := range columnPattern.FindAllStringSubmatch(g.body, -1) {
		labels, metrics = append(labels, html.UnescapeString(found[2])), append(metrics, found[1])
	}
	return labels, metrics
}

// columnHeads are a panel's column headings.
func columnHeads(g panel) []string {
	labels, _ := columns(g)
	return labels
}

// tableRow is one row of a panel's table: its name's element and each cell's.
type tableRow struct {
	name  string
	cells []string
}

func tableRows(g panel) []tableRow {
	var out []tableRow
	for _, found := range panelRowPattern.FindAllStringSubmatch(g.body, -1) {
		row := tableRow{name: rowNamePattern.FindString(found[1]), cells: panelCellPattern.FindAllString(found[1], -1)}
		out = append(out, row)
	}
	return out
}

// panelRows are a panel's rows as a sighted reader reads them: the name, then each cell.
func panelRows(g panel) []string {
	var out []string
	for _, row := range tableRows(g) {
		parts := []string{visible(row.name)}
		for _, cell := range row.cells {
			parts = append(parts, visible(cell))
		}
		out = append(out, strings.TrimSpace(strings.Join(parts, " ")))
	}
	return out
}

// webPanel is the panel web on server-b's page of these backup series.
func webPanel(t *testing.T, subjects ...state.Subject) panel {
	t.Helper()
	return panelOf(t, serverB(t, grouped(stateOf(subjects...), byBackup), ""), "web")
}

func serverB(t *testing.T, current state.State, query string) string {
	t.Helper()
	return itemPage(t, current, held{}, logbook{}, "/item?node=server-b"+query)
}

// service is server-b's series on the page of the service gdrive-a, which charts them.
func service(t *testing.T, current state.State) string {
	t.Helper()
	return itemPage(t, onService(current, "server-b"), held{}, logbook{}, "/item?node=gdrive-a")
}

// spec: overview.md#panels
func TestPanelsReplaceChartsOfAGroupedFamily(t *testing.T) {
	body := serverB(t, grouped(stateOf(append(mirror(), freePct("server-b", "/", 40))...), byBackup), "")
	equal(t, "panels", titles(body), []string{"vpn", "web"})
	equal(t, "tiles", tileNames(body), []string{"/ · ext4"})
	equal(t, "families", families(body), []string{"Backups"})
	// spec: overview.md#item-page — every panel, then the window links, then every chart.
	inOrder(t, service(t, grouped(stateOf(reading("server-b", "q.depth", map[string]string{"queue": "a"}, 1), freePct("server-b", "/", 40)),
		map[string]string{"q": "queue"})), `data-group="a"`, `<nav class="windows">`, `<figure class="chart" data-series="disk.free_pct / · ext4"`)
	inOrder(t, service(t, grouped(stateOf(mirror()...), byBackup)), `data-group="web"`, `<nav class="windows">`)
	inOrder(t, service(t, stateOf(freePct("server-b", "/", 40))), `<nav class="windows">`, `<figure class="chart"`)
	// The page refreshes by fetching its own address, its panels with it (web.md#live).
	containsAll(t, "page", body, `name="monitor-live"`)
	// A lone panel is as wide as its table, not stretched across the row.
	containsAll(t, "page", body, "article.group { flex: 0 1 auto; max-width: 100%;")

	t.Run("no group label declared", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(mirror()...), nil), "")
		if len(titles(body)) != 0 || len(tileNames(body)) != len(mirror()) {
			t.Errorf("panels %v, %d tiles: want a tile of each series", titles(body), len(tileNames(body)))
		}
	})

	t.Run("a label none of the series carry", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(mirror()...), map[string]string{"backup": "repository"}), "")
		if len(titles(body)) != 0 || len(tileNames(body)) != len(mirror()) {
			t.Errorf("panels %v, %d tiles: want a tile of each series", titles(body), len(tileNames(body)))
		}
	})

	t.Run("panels first, then the window links, then the series without the label", func(t *testing.T) {
		body := service(t, grouped(stateOf(
			reading("server-b", "q.depth", map[string]string{"queue": "b"}, 1),
			reading("server-b", "q.depth", map[string]string{"queue": "a"}, 1),
			plain("server-b", "q.total", 2),
		), map[string]string{"q": "queue"}))
		equal(t, "panels", titles(body), []string{"a", "b"})
		equal(t, "charts", charts(body), []string{"q.total"})
		equal(t, "families", families(body), []string{"q", "q"})
		inOrder(t, body, `data-group="b"`, `<nav class="windows">`, `<figure class="chart" data-series="q.total"`)
	})

	t.Run("titles", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(
			job("server-b", "web", "copy", "failed", 0), job("server-b", "", "copy", "failed", 0),
			job("server-b", "my repo", "copy", "failed", 0), job("server-b", "<b>x</b>", "<b>y</b>", "failed", 0),
		), byBackup), "")
		equal(t, "panels", titles(body), []string{`""`, "<b>x</b>", `"my repo"`, "web"})
		if strings.Contains(body, "<b>x</b>") || strings.Contains(body, "<b>y</b>") {
			t.Errorf("a label value was not escaped")
		}
		equal(t, "rows", panelRows(panelOf(t, body, "<b>x</b>")), []string{"<b>y</b>"})
		// spec: history.md#page — a figure of a series with an empty label still links to it.
		containsAll(t, "link", panelOf(t, body, `""`).body, `href="/history?label.backup=&amp;label.job=copy&amp;metric=backup.failed&amp;node=server-b&amp;window=24h"`)
	})

	t.Run("a group the page leaves out", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(staled(removable(freePct("server-b", "/usb", 3))), freePct("server-b", "/", 40)),
			map[string]string{"disk": "mount"}), "")
		equal(t, "panels", titles(body), []string{"/"})
	})

	t.Run("a group label belongs to the family", func(t *testing.T) {
		current := grouped(stateOf(job("server-b", "web", "copy", "failed", 0), job("server-c", "web", "create", "failed", 0)), byBackup)
		current.Nodes = append(current.Nodes, state.Node{Node: "server-c", Configured: true, LastSeen: lastSeen})
		body := itemPage(t, current, held{}, logbook{}, "/item?node=server-c")
		equal(t, "panels", titles(body), []string{"web"})
	})
}

// spec: overview.md#panels
func TestPanelState(t *testing.T) {
	word := func(t *testing.T, subjects []state.Subject, title string) string {
		t.Helper()
		return panelOf(t, serverB(t, grouped(stateOf(subjects...), byBackup), ""), title).class
	}
	with := func(changed ...state.Subject) []state.Subject {
		out := mirror()
		for _, c := range changed {
			for i, s := range out {
				if s.Metric == c.Metric && s.Labels["backup"] == c.Labels["backup"] && s.Labels["job"] == c.Labels["job"] {
					out[i] = c
				}
			}
		}
		return out
	}
	age := job("server-b", "web", "copy", "success_age_seconds", 4*3600)
	for _, c := range []struct {
		name     string
		subjects []state.Subject
		want     string
	}{
		{"none watched", mirror(), "reporting"},
		{"critical", with(judgedAt(age, evaluate.Critical)), "critical"},
		{"critical but stale", with(staled(judgedAt(age, evaluate.Critical)), judgedAt(job("server-b", "web", "maintain", "failed", 0), evaluate.OK)), "ok"},
		{"warning beside unusual", with(judgedAt(age, evaluate.Warning), ranked(job("server-b", "web", "copy", "archives", 10), 1)), "warning"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := word(t, c.subjects, "web"); got != c.want {
				t.Errorf("web = %s, want %s", got, c.want)
			}
		})
	}
	t.Run("the other panel by its own series", func(t *testing.T) {
		if got := word(t, with(judgedAt(age, evaluate.Critical)), "vpn"); got != "reporting" {
			t.Errorf("vpn = %s, want reporting", got)
		}
	})
	var stale []state.Subject
	for _, s := range mirror() {
		stale = append(stale, staled(s))
	}
	t.Run("every series stale", func(t *testing.T) {
		if got := word(t, stale, "web"); got != "no-data" {
			t.Errorf("web = %s, want no fresh data", got)
		}
	})
	t.Run("the node silent", func(t *testing.T) {
		current := grouped(stateOf(stale...), byBackup)
		current.Subjects = append(current.Subjects, judgedAt(plain("server-b", evaluate.SilenceMetric, 0), evaluate.Critical))
		for _, g := range panelsOf(serverB(t, current, "")) {
			if g.class != "no-data" {
				t.Errorf("%s = %s, want no fresh data, never silent", g.title, g.class)
			}
		}
	})
}

// spec: overview.md#panels, metric-names.md#where — a column of a metric the panel does not
// know, and a row that is not a job's, are named as the matrix names its columns and rows.
func TestBackupPanel(t *testing.T) {
	body := serverB(t, grouped(stateOf(mirror()...), byBackup), "")
	web := panelOf(t, body, "web")
	equal(t, "columns", columnHeads(web), []string{"Since success", "Newest archive", "Archives", "Stored"})
	equal(t, "rows", panelRows(web), []string{
		"copy 4.0 h 7.0 h 10 5.1 GB",
		"maintain 4.0 h — 8 —",
	})
	_, metrics := columns(web)
	equal(t, "column hover", metrics, []string{
		"backup.success_age_seconds", "backup.latest_archive_age_seconds", "backup.archives", "backup.stored_bytes",
	})

	t.Run("in Russian", func(t *testing.T) {
		web := panelOf(t, serverB(t, grouped(stateOf(mirror()...), byBackup), "&lang=ru"), "web")
		equal(t, "columns", columnHeads(web), []string{"С успеха", "Свежий архив", "Архивов", "Занято"})
		equal(t, "rows", panelRows(web)[:1], []string{"copy 4,0 ч 7,0 ч 10 5,1 ГБ"})
	})

	t.Run("every known figure", func(t *testing.T) {
		current := grouped(stateOf(
			job("server-b", "web", "copy", "queue_depth", 2),
			job("server-b", "web", "copy", "latest_archive_original_bytes", 2.3e9),
			job("server-b", "web", "copy", "stored_bytes", 5.1e9),
			job("server-b", "web", "copy", "archives", 10),
			job("server-b", "web", "copy", "latest_archive_age_seconds", 7*3600),
			job("server-b", "web", "copy", "success_age_seconds", 4*3600),
		), byBackup)
		current.Names.Metrics["backup.queue_depth"] = names.Text{i18n.English: "Queue {backup} {job}"}
		g := panelOf(t, serverB(t, current, ""), "web")
		equal(t, "columns", columnHeads(g), []string{"Since success", "Newest archive", "Archives", "Stored", "Newest archive data", "Queue"})
		equal(t, "rows", panelRows(g), []string{"copy 4.0 h 7.0 h 10 5.1 GB 2.3 GB 2"})
	})

	t.Run("archives rounded, not grouped", func(t *testing.T) {
		for value, want := range map[float64]string{10.4: "copy 10", 10.5: "copy 11", 1500: "copy 1500"} {
			g := webPanel(t, job("server-b", "web", "copy", "archives", value))
			equal(t, "rows", panelRows(g), []string{want})
		}
	})

	flag := func(t *testing.T, s state.Subject) tableRow {
		t.Helper()
		return tableRows(webPanel(t, s))[0]
	}
	failed := func(value float64) state.Subject { return job("server-b", "web", "copy", "failed", value) }

	t.Run("the flag alone", func(t *testing.T) {
		g := webPanel(t, failed(1))
		equal(t, "columns", columnHeads(g), nil)
		equal(t, "rows", panelRows(g), []string{"copy"})
	})

	t.Run("the flag unwatched", func(t *testing.T) {
		for value, word := range map[float64]string{0: "last run ok", 1: "last run failed", 2: "last run 2"} {
			name := flag(t, failed(value)).name
			containsAll(t, word, name, `<th scope="row" title="`+word+`">`, `<span class="row-mark-word"> `+word+`</span>`)
			if strings.Contains(name, "level-") {
				t.Errorf("an unwatched flag colours its row: %s", name)
			}
		}
	})

	t.Run("the flag watched", func(t *testing.T) {
		name := flag(t, judgedAt(failed(1), evaluate.Critical)).name
		containsAll(t, "critical", name, `level-critical`, `<th scope="row" title="last run failed · critical">`, `<span class="row-mark-word"> last run failed · critical</span>`)
		if got := visible(name); got != "copy" {
			t.Errorf("shown %q, want copy", got)
		}
		containsAll(t, "stale", flag(t, staled(judgedAt(failed(0), evaluate.Critical))).name, `level-stale`)
	})

	t.Run("a row's name", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(
			failed(0),
			judgedAt(job("server-b", "web", "maintain", "archives", 3), evaluate.Critical),
		), byBackup), "&window=7d&lang=ru"), "web")
		named := tableRows(g)
		containsAll(t, "copy", named[0].name,
			`href="/history?label.backup=web&amp;label.job=copy&amp;lang=ru&amp;metric=backup.failed&amp;node=server-b&amp;window=7d"`)
		if name := named[1].name; name != `<th scope="row">maintain</th>` {
			t.Errorf("a row without a failure flag is more than its name: %s", name)
		}
	})

	t.Run("quoted job names", func(t *testing.T) {
		g := webPanel(t, job("server-b", "web", "my repo", "archives", 1), job("server-b", "web", "", "archives", 1))
		equal(t, "rows", panelRows(g), []string{`"" 1`, `"my repo" 1`})
	})

	t.Run("marks", func(t *testing.T) {
		g := webPanel(t,
			judgedAt(job("server-b", "web", "copy", "success_age_seconds", 3600), evaluate.Critical),
			staled(job("server-b", "web", "maintain", "success_age_seconds", 3600)),
			ranked(job("server-b", "web", "verify", "success_age_seconds", 3600), 1),
		)
		equal(t, "rows", panelRows(g), []string{"copy 1.0 h", "maintain 1.0 h", "verify 1.0 h"})
		for i, want := range []struct{ class, word string }{
			{"level-critical", "critical"}, {"level-stale", "no fresh data"}, {"level-unusual", "unusual"},
		} {
			containsAll(t, want.word, tableRows(g)[i].cells[0], want.class, `<td title="`+want.word+`">`, `<span class="row-mark-word"> `+want.word+`</span>`)
		}
	})

	t.Run("links", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(job("server-b", "web", "copy", "archives", 3)), byBackup), "&window=7d&lang=ru"), "web")
		containsAll(t, "link", g.body,
			`href="/history?label.backup=web&amp;label.job=copy&amp;lang=ru&amp;metric=backup.archives&amp;node=server-b&amp;window=7d"`)
	})

	t.Run("metrics the panel does not know", func(t *testing.T) {
		current := grouped(stateOf(job("server-b", "web", "copy", "archives", 3), job("server-b", "web", "copy", "queue_depth", 2)), byBackup)
		current.Names.Metrics["backup.queue_depth"] = names.Text{i18n.English: "Queue {backup} {job}"}
		g := panelOf(t, serverB(t, current, ""), "web")
		equal(t, "columns", columnHeads(g), []string{"Archives", "Queue"})
		equal(t, "rows", panelRows(g), []string{"copy 3 2"})
		current.Names.Metrics["backup.queue_depth"] = names.Text{i18n.English: "{backup} {job}"}
		equal(t, "columns", columnHeads(panelOf(t, serverB(t, current, ""), "web")), []string{"Archives", "queue_depth"})

		current = grouped(stateOf(job("server-b", "web", "copy", "b_depth", 1), job("server-b", "web", "copy", "a_depth", 1),
			job("server-b", "web", "copy", "verify_age_seconds", 3600)), byBackup)
		current.Names.Metrics["backup.b_depth"] = names.Text{i18n.English: "Alpha {backup} {job}"}
		current.Names.Metrics["backup.a_depth"] = names.Text{i18n.English: "Zeta {backup} {job}"}
		g = panelOf(t, serverB(t, current, ""), "web")
		equal(t, "columns", columnHeads(g), []string{"Zeta", "Alpha", "verify_age_seconds"})
		equal(t, "rows", panelRows(g), []string{"copy 1 1 1.0 h"})
	})

	t.Run("rows that are not a job's", func(t *testing.T) {
		g := webPanel(t,
			reading("server-b", "backup.archives", map[string]string{"backup": "web", "job": "copy", "host": "h1"}, 3),
			reading("server-b", "backup.archives", map[string]string{"backup": "web"}, 4),
			job("server-b", "web", "copy", "archives", 5),
		)
		equal(t, "rows", panelRows(g), []string{"copy 5", "4", "host=h1 job=copy 3"})

		g = webPanel(t, reading("server-b", "backup.failed", map[string]string{"backup": "web"}, 1))
		equal(t, "rows", panelRows(g), []string{"last run failed"})
		containsAll(t, "link", tableRows(g)[0].name, `metric=backup.failed`)
	})
}

// spec: overview.md#panels, metric-names.md#where — a generic panel names its columns and
// rows as a /debug matrix does.
func TestGenericPanel(t *testing.T) {
	queue := func(metric string, labels map[string]string) state.Subject {
		return reading("server-b", metric, labels, 1)
	}
	page := func(t *testing.T, subjects ...state.Subject) panel {
		t.Helper()
		current := grouped(stateOf(subjects...), map[string]string{"q": "queue"})
		current.Names.Metrics["q.depth"] = names.Text{i18n.English: "Depth {queue}"}
		return panelOf(t, serverB(t, current, ""), "a")
	}
	g := page(t, queue("q.depth", map[string]string{"queue": "a", "shard": "2"}), queue("q.depth", map[string]string{"queue": "a", "shard": "1"}))
	equal(t, "columns", columnHeads(g), []string{"Depth"})
	equal(t, "rows", panelRows(g), []string{"shard=1 1", "shard=2 1"})

	t.Run("no other label", func(t *testing.T) {
		equal(t, "rows", panelRows(page(t, queue("q.depth", map[string]string{"queue": "a"}))), []string{"1"})
	})
	t.Run("mixed label sets", func(t *testing.T) {
		g := page(t, queue("q.depth", map[string]string{"queue": "a", "shard": "1"}), queue("q.depth", map[string]string{"queue": "a"}))
		equal(t, "rows", panelRows(g), []string{"1", "shard=1 1"})
	})
	t.Run("a missing cell", func(t *testing.T) {
		g := page(t, queue("q.age_seconds", map[string]string{"queue": "a", "shard": "1"}),
			queue("q.depth", map[string]string{"queue": "a", "shard": "1"}), queue("q.depth", map[string]string{"queue": "a", "shard": "2"}))
		equal(t, "columns", columnHeads(g), []string{"age_seconds", "Depth"})
		equal(t, "rows", panelRows(g), []string{"shard=1 1.0 s 1", "shard=2 — 1"})
		_, metrics := columns(g)
		equal(t, "column hover", metrics, []string{"q.age_seconds", "q.depth"})
	})
	t.Run("a cell at critical", func(t *testing.T) {
		g := page(t, judgedAt(queue("q.depth", map[string]string{"queue": "a"}), evaluate.Critical))
		equal(t, "rows", panelRows(g), []string{"1"})
		containsAll(t, "cell", tableRows(g)[0].cells[0], `level-critical`, `<td title="critical">`, `<span class="row-mark-word"> critical</span>`)
	})
	t.Run("on a site's page", func(t *testing.T) {
		current := grouped(stateOf(reading("sites", "site.hits", map[string]string{"site": "blog-a", "path": "/x", "page": "p1"}, 1)),
			map[string]string{"site": "page"})
		g := panelOf(t, itemPage(t, current, held{}, logbook{}, "/item?node=sites&label.site=blog-a"), "p1")
		equal(t, "rows", panelRows(g), []string{"path=/x 1"})
	})
	t.Run("a family of sites grouped by site", func(t *testing.T) {
		current := grouped(stateOf(siteOf("blog-a", "site.requests_per_min", 4)), map[string]string{"site": "site"})
		equal(t, "panels", titles(itemPage(t, current, held{}, logbook{}, "/item?node=sites&label.site=blog-a")), []string{"blog-a"})
	})
}
