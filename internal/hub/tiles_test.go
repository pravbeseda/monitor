package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

var (
	tilePattern = regexp.MustCompile(`(?s)<li class="tile state-([^"]*)" data-tile="([^"]*)">(.*?)</li>`)
	tickPattern = regexp.MustCompile(`<line class="bar-tick level-(\w+)" x1="([^"]*)"`)
	fillPattern = regexp.MustCompile(`<rect class="bar-fill([^"]*)" width="([^"]*)"`)
	tileWindow  = regexp.MustCompile(`href="[^"]*window=(\w+)"`)
)

type tile struct{ name, state, body string }

func tilesOf(body string) []tile {
	var out []tile
	for _, found := range tilePattern.FindAllStringSubmatch(body, -1) {
		out = append(out, tile{name: found[2], state: found[1], body: found[3]})
	}
	return out
}

func tileNames(body string) []string {
	var out []string
	for _, one := range tilesOf(body) {
		out = append(out, one.name)
	}
	return out
}

func tileOf(t *testing.T, body, name string) tile {
	t.Helper()
	for _, one := range tilesOf(body) {
		if one.name == name {
			return one
		}
	}
	t.Fatalf("no tile %q in %v", name, tileNames(body))
	return tile{}
}

// computer is server-b's page, named as its sensors name its families and metrics.
func computer(t *testing.T, subjects []state.Subject, points held, book logbook, query string) string {
	t.Helper()
	current := stateOf(subjects...)
	current.Names = names.Book{
		Metrics: map[string]names.Text{
			"load.avg_1m":          {i18n.English: "Load 1 min", i18n.Russian: "Нагрузка 1 мин"},
			"load.avg_5m":          {i18n.English: "Load 5 min", i18n.Russian: "Нагрузка 5 мин"},
			"load.avg_15m":         {i18n.English: "Load 15 min", i18n.Russian: "Нагрузка 15 мин"},
			"uptime.boot_seconds":  {i18n.English: "Uptime", i18n.Russian: "Аптайм"},
			"systemd.failed_units": {i18n.English: "Failed units", i18n.Russian: "Упавшие юниты"},
		},
		Families: map[string]names.Text{
			"disk":   {i18n.English: "Disk", i18n.Russian: "Диск"},
			"load":   {i18n.English: "Load", i18n.Russian: "Нагрузка"},
			"memory": {i18n.English: "Memory", i18n.Russian: "Память"},
		},
	}
	return itemPage(t, current, points, book, "/item?node=server-b"+query)
}

func volumeOf(mount string, bytes, pct float64) []state.Subject {
	return []state.Subject{free("server-b", mount, bytes), freePct("server-b", mount, pct)}
}

func memoryOf(bytes, pct float64) []state.Subject {
	return []state.Subject{plain("server-b", "memory.available_bytes", bytes), plain("server-b", "memory.available_pct", pct)}
}

func loadOf(one, five, fifteen float64) []state.Subject {
	return []state.Subject{plain("server-b", "load.avg_1m", one), plain("server-b", "load.avg_5m", five), plain("server-b", "load.avg_15m", fifteen)}
}

func pctRef(mount string) storage.SeriesRef {
	return storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: ext4(mount)}
}

func bareRef(metric string) storage.SeriesRef {
	return storage.SeriesRef{Node: "server-b", Metric: metric, Labels: map[string]string{}}
}

// recent are two points of a series an hour ago, so its line of a day is drawn.
func recent(refs ...storage.SeriesRef) held {
	out := held{}
	for _, ref := range refs {
		out[heldKey(ref)] = []storage.Point{{TS: lastSeen.Add(-time.Hour), Value: 1}, {TS: lastSeen.Add(-50 * time.Minute), Value: 2}}
	}
	return out
}

func belowOf(ref storage.SeriesRef, warning, critical float64) logbook {
	return logbook{thresholds: map[string]storage.Threshold{heldKey(ref): below(f(warning), f(critical))}}
}

func ticksOf(body string) [][2]string {
	var out [][2]string
	for _, found := range tickPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, [2]string{found[1], found[2]})
	}
	return out
}

// spec: overview.md#computer-page
func TestComputerVolumeTile(t *testing.T) {
	body := computer(t, volumeOf("/srv", 180e9, 9), held{}, logbook{}, "")
	equal(t, "tiles", tileNames(body), []string{"/srv · ext4"})
	srv := tileOf(t, body, "/srv · ext4").body
	inOrder(t, srv, ">180.0 GB<", "of 2.0 TB", ">9.0%<")
	containsAll(t, "name", srv, `<span class="tile-name">/srv · ext4</span>`)

	t.Run("in Russian", func(t *testing.T) {
		srv := tileOf(t, computer(t, volumeOf("/srv", 180e9, 9), held{}, logbook{}, "&lang=ru"), "/srv · ext4").body
		inOrder(t, srv, ">180,0 ГБ<", "из 2,0 ТБ", ">9,0 %<")
		containsAll(t, "link", srv, "lang=ru&amp;metric=disk.free_bytes&amp;node=server-b&amp;window=10d")
	})

	t.Run("names", func(t *testing.T) {
		bare := []state.Subject{reading("server-b", "disk.free_pct", map[string]string{}, 40)}
		equal(t, "no labels", tileNames(computer(t, bare, held{}, logbook{}, "")), []string{"Disk"})
		usb := []state.Subject{reading("server-b", "disk.free_pct", map[string]string{"mount": "/usb", "fs": "exfat", "removable": "true"}, 40)}
		equal(t, "removable", tileNames(computer(t, usb, held{}, logbook{}, "")), []string{"/usb · exfat · removable"})
		gone := append(volumeOf("/", 1e9, 40), staled(removable(freePct("server-b", "/usb", 3))))
		equal(t, "a stale removable volume", tileNames(computer(t, gone, held{}, logbook{}, "")), []string{"/ · ext4"})
	})

	t.Run("a series missing", func(t *testing.T) {
		bytes := tileOf(t, computer(t, volumeOf("/srv", 180e9, 9)[:1], held{}, logbook{}, ""), "/srv · ext4").body
		containsAll(t, "bytes alone", bytes, ">180.0 GB<", "window=10d")
		for _, absent := range []string{"of ", `class="bar"`, `class="spark"`} {
			if strings.Contains(bytes, absent) {
				t.Errorf("bytes alone carry %q", absent)
			}
		}
		share := tileOf(t, computer(t, volumeOf("/srv", 180e9, 9)[1:], recent(pctRef("/srv")), logbook{}, ""), "/srv · ext4").body
		containsAll(t, "share alone", share, `<span class="tile-values"><a class="figure"`, ">9.0%<", `class="bar"`, `class="spark"`)
		if strings.Contains(share, "tile-share") || strings.Contains(share, "of ") {
			t.Errorf("the share alone is muted or has a total: %s", share)
		}
	})

	t.Run("the total", func(t *testing.T) {
		for pct, shown := range map[float64]bool{0.5: false, 0: false, 1: true, 100: true, 105: false} {
			got := strings.Contains(tileOf(t, computer(t, volumeOf("/", 1e9, pct), held{}, logbook{}, ""), "/ · ext4").body, "tile-of")
			if got != shown {
				t.Errorf("at %v%% the total shown %v, want %v", pct, got, shown)
			}
		}
	})

	t.Run("the bar", func(t *testing.T) {
		at24 := tileOf(t, computer(t, volumeOf("/", 1e9, 24), held{}, belowOf(pctRef("/"), 10, 5), ""), "/ · ext4").body
		fill := fillPattern.FindStringSubmatch(at24)
		if fill == nil || fill[2] != "76" || fill[1] != "" {
			t.Fatalf("fill = %v", fill)
		}
		if got := ticksOf(at24); len(got) != 2 || got[0] != [2]string{"warning", "90"} || got[1] != [2]string{"critical", "95"} {
			t.Errorf("ticks = %v", got)
		}
		containsAll(t, "bar", at24, `aria-label="used 76.0%"`)
		ru := tileOf(t, computer(t, volumeOf("/", 1e9, 24), held{}, logbook{}, "&lang=ru"), "/ · ext4").body
		containsAll(t, "bar", ru, `aria-label="занято 76,0 %"`)

		bytesRef := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: ext4("/")}
		for name, book := range map[string]logbook{
			"on the bytes": belowOf(bytesRef, 10, 5),
			"unwatched":    {},
			"unreadable":   {thresholds: map[string]storage.Threshold{heldKey(pctRef("/")): {Series: pctRef("/"), Direction: "sideways", Warning: f(10)}}},
			"out of range": {thresholds: map[string]storage.Threshold{heldKey(pctRef("/")): {Series: pctRef("/"), Direction: storage.Below, Warning: f(120)}}},
		} {
			if got := ticksOf(tileOf(t, computer(t, volumeOf("/", 1e9, 24), held{}, book, ""), "/ · ext4").body); len(got) != 0 {
				t.Errorf("%s: ticks %v", name, got)
			}
		}
		if strings.Contains(tileOf(t, computer(t, volumeOf("/", 1e9, 105), held{}, logbook{}, ""), "/ · ext4").body, `class="bar"`) {
			t.Errorf("a bar at 105%% free")
		}
	})

	t.Run("marks", func(t *testing.T) {
		warn := []state.Subject{free("server-b", "/", 1e9), judgedAt(freePct("server-b", "/", 9), evaluate.Warning)}
		one := tileOf(t, computer(t, warn, held{}, logbook{}, ""), "/ · ext4")
		if one.state != "warning" {
			t.Errorf("edge %q", one.state)
		}
		containsAll(t, "warning", one.body, `class="figure level-warning"`, `<rect class="bar-fill level-warning"`, `<span class="tile-state">warning</span>`)

		stale := []state.Subject{judgedAt(free("server-b", "/", 1e9), evaluate.OK), staled(judgedAt(freePct("server-b", "/", 9), evaluate.Critical))}
		one = tileOf(t, computer(t, stale, held{}, logbook{}, ""), "/ · ext4")
		if one.state != "ok" {
			t.Errorf("a stale level raised the edge to %q", one.state)
		}
		containsAll(t, "stale", one.body, `class="figure level-stale"`, `<rect class="bar-fill" `)

		odd := []state.Subject{free("server-b", "/", 1e9), ranked(freePct("server-b", "/", 9), 1)}
		containsAll(t, "unusual", tileOf(t, computer(t, odd, held{}, logbook{}, ""), "/ · ext4").body, `<rect class="bar-fill" `)
	})

	t.Run("the line over ten days", func(t *testing.T) {
		old := held{heldKey(pctRef("/")): {{TS: lastSeen.Add(-11 * 24 * time.Hour), Value: 30}}}
		if strings.Contains(tileOf(t, computer(t, volumeOf("/", 1e9, 24), old, logbook{}, ""), "/ · ext4").body, `class="spark"`) {
			t.Errorf("a point eleven days old drawn")
		}
		within := held{heldKey(pctRef("/")): {{TS: lastSeen.Add(-11 * 24 * time.Hour), Value: 30}, {TS: lastSeen.Add(-9 * 24 * time.Hour), Value: 28}, {TS: lastSeen.Add(-time.Hour), Value: 24}}}
		one := tileOf(t, computer(t, volumeOf("/", 1e9, 24), within, logbook{}, ""), "/ · ext4").body
		containsAll(t, "line", one, `<svg class="spark"`, `aria-hidden="true"`)
		// The points stand days apart, each its own: the two inside the window, not the third.
		if dots := strings.Count(one, `<polyline class="point"`); dots != 2 {
			t.Errorf("points drawn = %d, want 2", dots)
		}
		equal(t, "windows", captured(tileWindow, one), []string{"10d", "10d"})
		containsAll(t, "link", one, `href="/history?label.fs=ext4&amp;label.mount=%2F&amp;label.removable=false&amp;metric=disk.free_pct&amp;node=server-b&amp;window=10d"`)
		if strings.Contains(one, "<a class=\"spark") || strings.Contains(one, "<a href") {
			t.Errorf("the line is a link")
		}
	})
}

// spec: overview.md#computer-page
func TestComputerOtherTiles(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		one := tileOf(t, computer(t, memoryOf(5.2e9, 32.5), recent(bareRef("memory.available_pct")), logbook{}, ""), "Memory").body
		inOrder(t, one, ">5.2 GB<", "of 16.0 GB", ">32.5%<")
		if fill := fillPattern.FindStringSubmatch(one); fill == nil || fill[2] != "67.5" {
			t.Errorf("fill = %v", fill)
		}
		containsAll(t, "line", one, `<svg class="spark"`)
		equal(t, "windows", captured(tileWindow, one), []string{"24h", "24h"})
		older := held{heldKey(bareRef("memory.available_pct")): {{TS: lastSeen.Add(-48 * time.Hour), Value: 30}}}
		if strings.Contains(tileOf(t, computer(t, memoryOf(5.2e9, 32.5), older, logbook{}, ""), "Memory").body, `class="spark"`) {
			t.Errorf("a point two days old drawn on the memory's line of a day")
		}
	})

	t.Run("load", func(t *testing.T) {
		fifteen := bareRef("load.avg_15m")
		book := logbook{thresholds: map[string]storage.Threshold{heldKey(fifteen): {Direction: storage.Above, Warning: f(4)}}}
		one := tileOf(t, computer(t, loadOf(0.42, 0.38, 0.30), recent(fifteen), book, ""), "Load").body
		containsAll(t, "load", one, `<span class="tile-aside">1 · 5 · 15 min</span>`, `<line class="threshold level-warning"`)
		inOrder(t, one, ">0.42<", " · ", ">0.38<", " · ", ">0.30<")
		containsAll(t, "hover", one, `title="Load 5 min"`)

		two := tileOf(t, computer(t, []state.Subject{loadOf(0.42, 0, 0)[0], loadOf(0, 0, 0.30)[2]}, held{}, logbook{}, ""), "Load").body
		containsAll(t, "two", two, `<span class="tile-aside">1 · 15 min</span>`)
		inOrder(t, two, ">0.42<", " · ", ">0.30<")
		lone := tileOf(t, computer(t, loadOf(0.42, 0, 0)[:1], recent(bareRef("load.avg_1m")), logbook{}, ""), "Load").body
		containsAll(t, "lone", lone, `<span class="tile-aside">1 min</span>`, ">0.42<")
		if strings.Contains(lone, `class="spark"`) {
			t.Errorf("a load tile without the 15 min average drew a line")
		}
	})

	t.Run("uptime and failed units", func(t *testing.T) {
		body := computer(t, []state.Subject{plain("server-b", "uptime.boot_seconds", 3600), plain("server-b", "systemd.failed_units", 2)},
			recent(bareRef("uptime.boot_seconds"), bareRef("systemd.failed_units")), logbook{}, "")
		up := tileOf(t, body, "Uptime").body
		containsAll(t, "uptime", up, ">1.0 h<")
		equal(t, "windows", captured(tileWindow, up), []string{"24h"})
		failed := tileOf(t, body, "Failed units").body
		containsAll(t, "failed", failed, ">2<")
		for _, one := range []string{up, failed} {
			if strings.Contains(one, `class="spark"`) {
				t.Errorf("a line on %s", one)
			}
		}
	})

	t.Run("generic", func(t *testing.T) {
		zoned := reading("server-b", "memory.available_pct", map[string]string{"zone": "a"}, 40)
		destination := reading("server-b", "timemachine.backup_age_seconds", map[string]string{"destination": "nas"}, 7200)
		dest := storage.SeriesRef{Node: "server-b", Metric: "timemachine.backup_age_seconds", Labels: destination.Labels}
		body := computer(t, append(memoryOf(5.2e9, 32.5), zoned, destination), recent(dest), logbook{}, "")
		equal(t, "tiles", tileNames(body), []string{"Memory", "memory.available_pct zone=a", "timemachine.backup_age_seconds destination=nas"})
		one := tileOf(t, body, "timemachine.backup_age_seconds destination=nas").body
		containsAll(t, "generic", one, ">2.0 h<", `<svg class="spark"`)
	})

	t.Run("usual values", func(t *testing.T) {
		odd := ranked(plain("server-b", "memory.available_pct", 32.5), 1)
		odd.Anomaly.Norm = 50
		containsAll(t, "unusual", tileOf(t, computer(t, []state.Subject{odd}, held{}, logbook{}, ""), "Memory").body, "usually 50.0%")
		if strings.Contains(tileOf(t, computer(t, []state.Subject{staled(odd)}, held{}, logbook{}, ""), "Memory").body, "usually") {
			t.Errorf("a stale series says its usual value")
		}
		first, last := ranked(plain("server-b", "load.avg_1m", 3), 1), ranked(plain("server-b", "load.avg_15m", 2), 1)
		first.Anomaly.Norm, last.Anomaly.Norm = 0.5, 0.25
		one := tileOf(t, computer(t, []state.Subject{last, first}, held{}, logbook{}, ""), "Load").body
		containsAll(t, "first", one, "usually 0.50")
	})

	t.Run("states", func(t *testing.T) {
		one := tileOf(t, computer(t, []state.Subject{staled(plain("server-b", "memory.available_pct", 30))}, held{}, logbook{}, ""), "Memory")
		if one.state != "no-data" || !strings.Contains(one.body, `<span class="tile-state">no fresh data</span>`) {
			t.Errorf("all stale: %q %s", one.state, one.body)
		}
		one = tileOf(t, computer(t, memoryOf(5.2e9, 32.5), held{}, logbook{}, ""), "Memory")
		if one.state != "reporting" || !strings.Contains(one.body, `<span class="tile-state">reporting, nothing watched</span>`) {
			t.Errorf("unwatched: %q %s", one.state, one.body)
		}
		current := stateOf(append(memoryOf(5.2e9, 32.5), silenceOf("server-b", evaluate.Critical))...)
		body := itemPage(t, current, held{}, logbook{}, "/item?node=server-b")
		containsAll(t, "silent", body, "silent")
		for _, one := range tilesOf(body) {
			if one.state == "silent" {
				t.Errorf("a silent tile %q", one.name)
			}
		}
		stale := tileOf(t, computer(t, []state.Subject{staled(plain("server-b", "memory.available_pct", 30))}, held{}, logbook{}, ""), "Memory").body
		if strings.Contains(stale, "chart-time") || strings.Contains(stale, "09:") || strings.Contains(stale, "/thresholds?") {
			t.Errorf("a stale tile shows its own time or the thresholds' form: %s", stale)
		}
	})
}

// spec: overview.md#computer-page
func TestComputerPage(t *testing.T) {
	all := append(append(append(volumeOf("/b", 1e9, 40), volumeOf("/a", 1e9, 40)...), loadOf(1, 1, 1)...), memoryOf(5e9, 50)...)
	all = append(all, plain("server-b", "uptime.boot_seconds", 9))
	body := computer(t, all, held{}, logbook{}, "")
	equal(t, "tiles", tileNames(body), []string{"/a · ext4", "/b · ext4", "Load", "Memory", "Uptime"})
	if len(families(body)) != 0 {
		t.Errorf("tiles under a family heading")
	}
	for _, absent := range []string{`<ul class="numbers">`, `<figure class="chart"`, `<nav class="windows">`, `/thresholds?`, `<a class="tile-name`} {
		if strings.Contains(body, absent) {
			t.Errorf("a computer's page carries %q", absent)
		}
	}
	containsAll(t, "page", body, `name="monitor-live"`, "@media (max-width: 40rem) { ul.tiles { grid-template-columns: minmax(0, 1fr); } }")

	t.Run("panels after the tiles", func(t *testing.T) {
		body := itemPage(t, grouped(stateOf(append(mirror(), freePct("server-b", "/", 40))...), byBackup), held{}, logbook{}, "/item?node=server-b&window=7d")
		equal(t, "tiles", tileNames(body), []string{"/ · ext4"})
		equal(t, "panels", titles(body), []string{"vpn", "web"})
		inOrder(t, body, `data-tile="/ · ext4"`, `<h2 class="family-heading">Backups</h2>`, `data-group="vpn"`)
		containsAll(t, "panel links", panelOf(t, body, "web").body, "window=7d")
		if strings.Contains(tileOf(t, body, "/ · ext4").body, "window=7d") {
			t.Errorf("the window reached a tile")
		}
	})

	t.Run("no series", func(t *testing.T) {
		body := itemPage(t, stateOf(staled(removable(freePct("server-b", "/usb", 3)))), held{}, logbook{}, "/item?node=server-b")
		containsAll(t, "page", body, "No series yet")
		if strings.Contains(body, `<nav class="windows">`) {
			t.Errorf("window links without series")
		}
	})

	t.Run("in Russian", func(t *testing.T) {
		body := computer(t, append(memoryOf(5.2e9, 32.5), loadOf(1, 1, 1)...), held{}, logbook{}, "&lang=ru")
		equal(t, "tiles", tileNames(body), []string{"Нагрузка", "Память"})
		containsAll(t, "words", body, "из 16,0 ГБ", "1 · 5 · 15 мин")
	})

	t.Run("a failure reads as /debug's", func(t *testing.T) {
		rec := httptest.NewRecorder()
		read := func(context.Context) (state.State, error) { return stateOf(volumeOf("/", 1e9, 40)...), nil }
		hub.Item(read, historyOf(broken{recent(pctRef("/"))}), logbook{}, layout(), untargeted).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/item?node=server-b", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("failing points answer %d", rec.Code)
		}
		code, _ := showItem(t, stateOf(volumeOf("/", 1e9, 40)...), held{}, logbook{err: errFailed}, http.MethodGet, "/item?node=server-b")
		if code != http.StatusInternalServerError {
			t.Errorf("failing thresholds answer %d", code)
		}
	})
}

// spec: metric-names.md#where — a tile says the declared names: a volume by its labels, the
// memory and the load by their family, the rest by the series' name, a value by its series'.
func TestComputerTileNames(t *testing.T) {
	declared := stateOf(append(append(volumeOf("/srv", 1e9, 40), memoryOf(5e9, 50)...),
		plain("server-b", "load.avg_15m", 1), plain("server-b", "uptime.boot_seconds", 9), plain("server-b", "systemd.failed_units", 0),
		reading("server-b", "timemachine.backup_age_seconds", map[string]string{"destination": "nas"}, 9))...)
	declared.Names = names.Book{
		Metrics: map[string]names.Text{
			"disk.free_pct":                  {i18n.English: "Free on {mount}", i18n.Russian: "Свободно {mount}"},
			"load.avg_15m":                   {i18n.English: "Load 15 min", i18n.Russian: "Нагрузка 15 мин"},
			"uptime.boot_seconds":            {i18n.English: "Uptime", i18n.Russian: "Аптайм"},
			"systemd.failed_units":           {i18n.English: "Failed units", i18n.Russian: "Упавшие юниты"},
			"timemachine.backup_age_seconds": {i18n.English: "Backup {destination}", i18n.Russian: "Бэкап {destination}"},
		},
		Families: map[string]names.Text{
			"memory": {i18n.English: "Memory", i18n.Russian: "Память"},
			"load":   {i18n.English: "Load", i18n.Russian: "Нагрузка"},
		},
	}
	body := itemPage(t, declared, held{}, logbook{}, "/item?node=server-b&lang=ru")
	equal(t, "tiles", tileNames(body), []string{"/srv · ext4", "Нагрузка", "Память", "Упавшие юниты", "Бэкап nas", "Аптайм"})
	containsAll(t, "hover", tileOf(t, body, "/srv · ext4").body, `title="Свободно /srv"`)
}

// spec: history.md#page — a tile's value of a series with an empty label still links to it.
func TestComputerTileEmptyLabel(t *testing.T) {
	body := computer(t, []state.Subject{reading("server-b", "queue.depth", map[string]string{"queue": ""}, 1)}, held{}, logbook{}, "&lang=ru")
	containsAll(t, "link", body, `href="/history?label.queue=&amp;lang=ru&amp;metric=queue.depth&amp;node=server-b&amp;window=24h"`)
}
