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
	panelPattern = regexp.MustCompile(`(?s)<article class="group state-([\w-]+)" data-group="([^"]*)">(.*?)</article>`)
	linePattern  = regexp.MustCompile(`(?s)<li class="group-line">(.*?)</li>`)
)

// grouped is current with the backup sensor's names and a group label per family.
func grouped(current state.State, groups map[string]string) state.State {
	current.Names = names.Book{
		Metrics: map[string]names.Text{
			"backup.failed":                        {i18n.English: "Failed {backup} {job}"},
			"backup.success_age_seconds":           {i18n.English: "Since success {backup} {job}"},
			"backup.archives":                      {i18n.English: "Archives {backup} {job}"},
			"backup.latest_archive_age_seconds":    {i18n.English: "Newest archive {backup} {job}"},
			"backup.stored_bytes":                  {i18n.English: "Stored {backup} {job}"},
			"backup.latest_archive_original_bytes": {i18n.English: "Newest archive data {backup} {job}"},
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

func lines(g panel) []string {
	var out []string
	for _, found := range linePattern.FindAllStringSubmatch(g.body, -1) {
		out = append(out, text(found[1]))
	}
	return out
}

func serverB(t *testing.T, current state.State, query string) string {
	t.Helper()
	return itemPage(t, current, held{}, logbook{}, "/item?node=server-b"+query)
}

// spec: overview.md#panels
func TestPanelsReplaceChartsOfAGroupedFamily(t *testing.T) {
	body := serverB(t, grouped(stateOf(append(mirror(), freePct("server-b", "/", 40))...), byBackup), "")
	equal(t, "panels", titles(body), []string{"vpn", "web"})
	equal(t, "charts", charts(body), []string{"disk.free_pct / · ext4"})
	equal(t, "families", families(body), []string{"Backups", "disk"})
	// The page refreshes by fetching its own address, its panels with it (web.md#live).
	containsAll(t, "page", body, `name="monitor-live"`)

	t.Run("no group label declared", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(mirror()...), nil), "")
		if len(titles(body)) != 0 || len(charts(body)) != len(mirror()) {
			t.Errorf("panels %v, %d charts: want a chart of each series", titles(body), len(charts(body)))
		}
	})

	t.Run("a label none of the series carry", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(mirror()...), map[string]string{"backup": "repository"}), "")
		if len(titles(body)) != 0 || len(charts(body)) != len(mirror()) {
			t.Errorf("panels %v, %d charts: want a chart of each series", titles(body), len(charts(body)))
		}
	})

	t.Run("panels first, then the series without the label", func(t *testing.T) {
		body := serverB(t, grouped(stateOf(
			reading("server-b", "q.depth", map[string]string{"queue": "b"}, 1),
			reading("server-b", "q.depth", map[string]string{"queue": "a"}, 1),
			plain("server-b", "q.total", 2),
		), map[string]string{"q": "queue"}), "")
		equal(t, "panels", titles(body), []string{"a", "b"})
		equal(t, "charts", charts(body), []string{"q.total"})
		if panel, chart := strings.Index(body, `data-group="b"`), strings.Index(body, `<figure class="chart" data-series="q.total"`); panel > chart {
			t.Errorf("the chart comes before the panels")
		}
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
		equal(t, "lines", lines(panelOf(t, body, "<b>x</b>")), []string{"<b>y</b> · last run ok"})
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

// spec: overview.md#panels, metric-names.md#where — a figure of a metric the panel does not
// know, and a line that is not a job's, are named as the matrix names its columns and rows.
func TestBackupPanel(t *testing.T) {
	body := serverB(t, grouped(stateOf(mirror()...), byBackup), "")
	web := panelOf(t, body, "web")
	equal(t, "lines", lines(web), []string{
		"copy · last run ok · succeeded 4.0 h ago · newest archive 7.0 h ago · archives: 10 · 5.1 GB stored",
		"maintain · last run ok · succeeded 4.0 h ago · archives: 8",
	})

	t.Run("in Russian", func(t *testing.T) {
		web := panelOf(t, serverB(t, grouped(stateOf(mirror()...), byBackup), "&lang=ru"), "web")
		equal(t, "lines", lines(web)[:1], []string{"copy · запуск ок · успех 4,0 ч назад · свежий архив 7,0 ч назад · архивов: 10 · занято 5,1 ГБ"})
	})

	t.Run("every figure", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(
			job("server-b", "web", "copy", "stored_bytes", 5.1e9),
			job("server-b", "web", "copy", "latest_archive_original_bytes", 2.3e9),
			job("server-b", "web", "copy", "archives", 10.4),
		), byBackup), ""), "web")
		equal(t, "lines", lines(g), []string{"copy · archives: 10 · 5.1 GB stored · 2.3 GB of data"})
	})

	t.Run("a failure", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(job("server-b", "web", "copy", "failed", 1)), byBackup), ""), "web")
		equal(t, "lines", lines(g), []string{"copy · last run failed"})
		if strings.Contains(g.body, "level-") {
			t.Errorf("an unwatched failure is marked: %s", g.body)
		}
		g = panelOf(t, serverB(t, grouped(stateOf(job("server-b", "web", "copy", "failed", 2)), byBackup), ""), "web")
		equal(t, "lines", lines(g), []string{"copy · last run 2.00"})
	})

	t.Run("marks", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(
			judgedAt(job("server-b", "web", "copy", "success_age_seconds", 3600), evaluate.Critical),
			staled(job("server-b", "web", "maintain", "success_age_seconds", 3600)),
			ranked(job("server-b", "web", "verify", "archives", 3), 1),
		), byBackup), ""), "web")
		equal(t, "lines", lines(g), []string{
			"copy · succeeded 1.0 h ago critical", "maintain · succeeded 1.0 h ago no fresh data", "verify · archives: 3 unusual",
		})
	})

	t.Run("links", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(job("server-b", "web", "copy", "archives", 3)), byBackup), "&window=7d&lang=ru"), "web")
		containsAll(t, "link", g.body,
			`href="/history?label.backup=web&amp;label.job=copy&amp;lang=ru&amp;metric=backup.archives&amp;node=server-b&amp;window=7d"`)
	})

	t.Run("a metric the panel does not know", func(t *testing.T) {
		current := grouped(stateOf(job("server-b", "web", "copy", "archives", 3), job("server-b", "web", "copy", "queue_depth", 2)), byBackup)
		current.Names.Metrics["backup.queue_depth"] = names.Text{i18n.English: "Queue {backup} {job}"}
		equal(t, "lines", lines(panelOf(t, serverB(t, current, ""), "web")), []string{"copy · archives: 3 · Queue 2.00"})
		current.Names.Metrics["backup.queue_depth"] = names.Text{i18n.English: "{backup} {job}"}
		equal(t, "lines", lines(panelOf(t, serverB(t, current, ""), "web")), []string{"copy · archives: 3 · queue_depth 2.00"})
	})

	t.Run("lines that are not a job's", func(t *testing.T) {
		g := panelOf(t, serverB(t, grouped(stateOf(
			reading("server-b", "backup.archives", map[string]string{"backup": "web", "job": "copy", "host": "h1"}, 3),
			reading("server-b", "backup.archives", map[string]string{"backup": "web"}, 4),
			job("server-b", "web", "copy", "archives", 5),
		), byBackup), ""), "web")
		equal(t, "lines", lines(g), []string{"copy · archives: 5", "archives: 4", "host=h1 job=copy · archives: 3"})
	})
}

var (
	columnPattern   = regexp.MustCompile(`<th class="group-column"[^>]*>([^<]*)</th>`)
	panelRowPattern = regexp.MustCompile(`(?s)<tr class="group-row">(.*?)</tr>`)
)

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
	rows := func(g panel) []string {
		var out []string
		for _, found := range panelRowPattern.FindAllStringSubmatch(g.body, -1) {
			out = append(out, text(found[1]))
		}
		return out
	}
	columns := func(g panel) []string {
		var out []string
		for _, found := range columnPattern.FindAllStringSubmatch(g.body, -1) {
			out = append(out, found[1])
		}
		return out
	}

	g := page(t, queue("q.depth", map[string]string{"queue": "a", "shard": "2"}), queue("q.depth", map[string]string{"queue": "a", "shard": "1"}))
	equal(t, "columns", columns(g), []string{"Depth"})
	equal(t, "rows", rows(g), []string{"shard=1 1.00", "shard=2 1.00"})

	t.Run("no other label", func(t *testing.T) {
		equal(t, "rows", rows(page(t, queue("q.depth", map[string]string{"queue": "a"}))), []string{"1.00"})
	})
	t.Run("mixed label sets", func(t *testing.T) {
		g := page(t, queue("q.depth", map[string]string{"queue": "a", "shard": "1"}), queue("q.depth", map[string]string{"queue": "a"}))
		equal(t, "rows", rows(g), []string{"1.00", "shard=1 1.00"})
	})
	t.Run("a missing cell", func(t *testing.T) {
		g := page(t, queue("q.age_seconds", map[string]string{"queue": "a", "shard": "1"}),
			queue("q.depth", map[string]string{"queue": "a", "shard": "1"}), queue("q.depth", map[string]string{"queue": "a", "shard": "2"}))
		equal(t, "columns", columns(g), []string{"age_seconds", "Depth"})
		equal(t, "rows", rows(g), []string{"shard=1 1.0 s 1.00", "shard=2 — 1.00"})
	})
	t.Run("on a site's page", func(t *testing.T) {
		current := grouped(stateOf(reading("sites", "site.hits", map[string]string{"site": "blog-a", "path": "/x", "page": "p1"}, 1)),
			map[string]string{"site": "page"})
		g := panelOf(t, itemPage(t, current, held{}, logbook{}, "/item?node=sites&label.site=blog-a"), "p1")
		equal(t, "rows", rows(g), []string{"path=/x 1.00"})
	})
	t.Run("a family of sites grouped by site", func(t *testing.T) {
		current := grouped(stateOf(siteOf("blog-a", "site.requests_per_min", 4)), map[string]string{"site": "site"})
		equal(t, "panels", titles(itemPage(t, current, held{}, logbook{}, "/item?node=sites&label.site=blog-a")), []string{"blog-a"})
	})
}
