package hub_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

// pickedDebug reads /debug as a reader does, with no filter added for them.
func pickedDebug(t *testing.T, store stored, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	hub.Debug(hub.ReadState(store, judging(), anomaly.NewNorms(store), func() time.Time { return lastSeen })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// fleet is laptop-a with load and a volume, and server-b with load and a volume; nothing
// judged.
func fleet() stored {
	node := func(name string) storage.NodeState {
		return storage.NodeState{Node: name, LastSeen: lastSeen, Values: append(pair(mounted("/"), lastSeen),
			storage.Value{Metric: "load.avg_5m", Sensor: "load", Value: 2.5, TS: lastSeen})}
	}
	return stored{states: []storage.NodeState{node("laptop-a"), node("server-b")}}
}

// troubled is fleet with server-b's volume critical.
func troubled() stored {
	store := fleet()
	store.thresholds = []storage.Threshold{watched("server-b", "disk.free_bytes", mounted("/"))}
	store.levels = []storage.State{level("server-b", "disk.free_bytes", mounted("/"), "critical")}
	return store
}

var (
	treePane  = regexp.MustCompile(`(?s)<nav class="tree".*?</nav>`)
	treeEntry = regexp.MustCompile(`(?s)<a href="([^"]*)"[^>]*>(.*?)</a>`)
	tagged    = regexp.MustCompile(`<[^>]*>`)
)

// treeLinks are the links of the left pane, the text of each by what it reads.
func treeLinks(t *testing.T, body string) map[string]string {
	t.Helper()
	pane := treePane.FindString(body)
	if pane == "" {
		t.Fatalf("no left pane: %s", body)
	}
	out := map[string]string{}
	for _, found := range treeEntry.FindAllStringSubmatch(pane, -1) {
		text := strings.Join(strings.Fields(tagged.ReplaceAllString(found[2], " ")), " ")
		out[text] = strings.ReplaceAll(found[1], "&amp;", "&")
	}
	return out
}

// shows reports which of a node's tables the right pane draws.
func showsVolume(body string) bool  { return strings.Contains(body, `<table class="matrix">`) }
func showsGeneral(body string) bool { return strings.Contains(body, `<table class="own">`) }

// spec: history.md#table — with no group named and a problem somewhere, the first group
// holding one, its entry marked; the rest of the hub is on the left only.
func TestDebugOpensTheFirstGroupWithAProblem(t *testing.T) {
	body := pickedDebug(t, troubled(), "/debug")
	if !showsVolume(body) || showsGeneral(body) {
		t.Errorf("want the matrix holding the problem alone: %s", body)
	}
	containsAll(t, "page", body, "<h2>server-b", `aria-current="true"`)
	if strings.Contains(body, "<h2>laptop-a") {
		t.Error("another node's line is drawn")
	}
}

// spec: history.md#table — nothing wrong: the first group of the first node, "General"
// standing first.
func TestDebugOpensTheFirstGroupWhenNothingIsWrong(t *testing.T) {
	body := pickedDebug(t, fleet(), "/debug")
	if !showsGeneral(body) || showsVolume(body) || !strings.Contains(body, "<h2>laptop-a") {
		t.Errorf("want laptop-a's General: %s", body)
	}
}

// spec: history.md#table — an entry opens its group and drops a filter; its address works
// in the other language; a group the hub does not show reads as none named.
func TestDebugOpensTheGroupAnEntryNames(t *testing.T) {
	links := treeLinks(t, pickedDebug(t, fleet(), "/debug?show=problems&q=x"))
	link, ok := links["disk"]
	if !ok {
		t.Fatalf("no disk entry in %v", links)
	}
	if strings.Contains(link, "show=") || strings.Contains(link, "q=") {
		t.Errorf("entry %s keeps the filter", link)
	}
	if body := pickedDebug(t, fleet(), link); !showsVolume(body) || showsGeneral(body) {
		t.Errorf("%s drew another group: %s", link, body)
	}
	if body := pickedDebug(t, fleet(), link+"&lang=ru"); !showsVolume(body) {
		t.Errorf("%s in Russian drew another group: %s", link, body)
	}
	for _, target := range []string{"/debug?node=nowhere", "/debug?node=laptop-a&group=gone"} {
		if body := pickedDebug(t, troubled(), target); !strings.Contains(body, "<h2>server-b") {
			t.Errorf("%s = %s, want what /debug opens", target, body)
		}
	}
}

// spec: history.md#table — a node named alone: its first group holding a problem, else its
// first group.
func TestDebugOpensANodeNamedAlone(t *testing.T) {
	if body := pickedDebug(t, troubled(), "/debug?node=server-b"); !showsVolume(body) {
		t.Errorf("want its problem: %s", body)
	}
	body := pickedDebug(t, troubled(), "/debug?node=laptop-a")
	if !showsGeneral(body) || !strings.Contains(body, "<h2>laptop-a") {
		t.Errorf("want its first group: %s", body)
	}
}

// spec: history.md#table — a node showing no series: its name links to its reason; every
// node so: the first one's reason; no node at all: the line saying so, the filters at zero.
func TestDebugShowsAnEmptyNodesReason(t *testing.T) {
	store := fleet()
	store.states = append(store.states, storage.NodeState{Node: "server-c", LastSeen: lastSeen})
	link, ok := treeLinks(t, pickedDebug(t, store, "/debug"))["server-c"]
	if !ok {
		t.Fatalf("no link to the empty node")
	}
	reason := pickedDebug(t, store, link)
	containsAll(t, "reason", reason, "<h2>server-c", "No measurements yet")
	containsAll(t, "its entry", treePane.FindString(reason), `aria-current="true">server-c`)

	empty := stored{states: []storage.NodeState{{Node: "server-c", LastSeen: lastSeen}}}
	containsAll(t, "only reason", pickedDebug(t, empty, "/debug"), "<h2>server-c", "No measurements yet")

	nobody := pickedDebug(t, stored{}, "/debug")
	containsAll(t, "empty hub", nobody, "No node has reported yet", "All<b>0</b>")
	if strings.Contains(treePane.FindString(nobody), "<a ") {
		t.Errorf("left pane = %s, want it empty", treePane.FindString(nobody))
	}
}

// spec: history.md#table — an entry counts its problems under the worst mark, and its
// unusual series; an entry with neither says nothing.
func TestDebugEntriesCountTheirTrouble(t *testing.T) {
	pane := treePane.FindString(pickedDebug(t, troubled(), "/debug"))
	if !regexp.MustCompile(`■</i>\s*1`).MatchString(pane) {
		t.Errorf("left pane = %s, want one critical counted", pane)
	}
	unusual := unusualRoot()
	pane = treePane.FindString(pickedDebug(t, unusual, "/debug"))
	if !regexp.MustCompile(`◆</i>\s*1`).MatchString(pane) {
		t.Errorf("left pane = %s, want one unusual counted", pane)
	}
	if clean := treePane.FindString(pickedDebug(t, fleet(), "/debug")); strings.Contains(clean, "■") || strings.Contains(clean, "◆") {
		t.Errorf("left pane = %s, want no counts", clean)
	}
}

// spec: history.md#table — the worst mark speaks for an entry: critical, then warning, then
// no fresh data; the count is of every problem in the group.
func TestDebugEntriesShowTheWorstMark(t *testing.T) {
	th := func(mount string) storage.Threshold { return watched("server-b", "disk.free_bytes", mounted(mount)) }
	values := append(pair(mounted("/"), lastSeen), pair(mounted("/data"), lastSeen)...)
	store := stored{
		states:     []storage.NodeState{{Node: "server-b", LastSeen: lastSeen, Values: values}},
		thresholds: []storage.Threshold{th("/"), th("/data")},
		levels:     []storage.State{level("server-b", "disk.free_bytes", mounted("/"), "warning"), level("server-b", "disk.free_bytes", mounted("/data"), "critical")},
	}
	if pane := treePane.FindString(pickedDebug(t, store, "/debug")); !regexp.MustCompile(`■</i>\s*2`).MatchString(pane) || strings.Contains(pane, "▲") {
		t.Errorf("left pane = %s, want two problems under the critical mark", pane)
	}
	store.levels = store.levels[:1]
	if pane := treePane.FindString(pickedDebug(t, store, "/debug")); !strings.Contains(pane, "▲") {
		t.Errorf("left pane = %s, want the warning mark", pane)
	}
	store.levels = nil
	store.states[0].Values[0].TS = lastSeen.Add(-24 * time.Hour)
	if pane := treePane.FindString(pickedDebug(t, store, "/debug")); !strings.Contains(pane, "◌") {
		t.Errorf("left pane = %s, want the no-fresh-data mark", pane)
	}
}

// spec: history.md#table — "Without a threshold" counts a node the file no longer names.
func TestDebugFiltersCountADetachedNode(t *testing.T) {
	store := fleet()
	store.states = append(store.states, storage.NodeState{Node: "server-z", LastSeen: lastSeen, Values: []storage.Value{
		{Metric: "load.avg_5m", Sensor: "load", Value: 1, TS: lastSeen},
	}})
	rec := httptest.NewRecorder()
	routesWith(t, store, func() time.Time { return lastSeen }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug", nil))
	containsAll(t, "page", rec.Body.String(), "Without a threshold<b>7</b>")
}

// spec: history.md#table — a search looks at one field at a time, and a label set is not one
// field.
func TestDebugSearchLooksAtEachFieldAlone(t *testing.T) {
	if body := pickedDebug(t, fleet(), "/debug?q=laptop-a+load"); !strings.Contains(body, "Nothing matches") {
		t.Errorf("a search matched across fields: %s", body)
	}
}

// spec: history.md#table — "Problems" with a search narrows a silent node's line too.
func TestDebugSearchNarrowsASilentNode(t *testing.T) {
	store := fleet()
	store.states[0].LastSeen = lastSeen.Add(-24 * time.Hour)
	store.levels = []storage.State{silenceLevel("laptop-a", "critical")}
	containsAll(t, "page", pickedDebug(t, store, "/debug?show=problems&q=nothing-here"), "Nothing matches")
}

// spec: history.md#table — two series whose labels would read alike unescaped stay two, and
// a label key holding a comma stays its own group.
func TestDebugKeepsAwkwardLabelsApart(t *testing.T) {
	values := []storage.Value{
		{Metric: "job.lag", Sensor: "disk", Labels: map[string]string{"a": "b,c=d"}, Value: 1, TS: lastSeen},
		{Metric: "job.lag", Sensor: "disk", Labels: map[string]string{"a": "b", "c": "d"}, Value: 2, TS: lastSeen},
		{Metric: "job.lag", Sensor: "disk", Labels: map[string]string{"k,l": "m"}, Value: 3, TS: lastSeen},
		{Metric: "job.lag", Sensor: "disk", Labels: map[string]string{"k": "m", "l": "n"}, Value: 4, TS: lastSeen},
	}
	store := stored{states: []storage.NodeState{{Node: "server-b", LastSeen: lastSeen, Values: values}}}
	store.thresholds = []storage.Threshold{watched("server-b", "job.lag", map[string]string{"a": "b,c=d"})}
	store.levels = []storage.State{level("server-b", "job.lag", map[string]string{"a": "b,c=d"}, "critical")}
	containsAll(t, "page", pickedDebug(t, store, "/debug"), "Problems<b>1</b>")

	links := treeLinks(t, pickedDebug(t, store, "/debug"))
	first, second := links["job (k,l)"], links["job (k, l)"]
	if first == "" || second == "" || first == second {
		t.Errorf("entries = %v, want the two key sets apart", links)
	}
}

// spec: state.md#page — a matrix cell says on hover what it is judged by, or that what is
// stored cannot be read.
func TestDebugCellSaysWhatJudgesIt(t *testing.T) {
	warning := 20e9
	store := fleet()
	series := storage.SeriesRef{Node: "laptop-a", Metric: "disk.free_bytes", Labels: mounted("/")}
	store.thresholds = []storage.Threshold{{Series: series, Direction: storage.Below, Warning: &warning}}
	cell := func() string { return cellOf(t, showDebug(t, store, "/debug?q=laptop-a"), "disk.free_bytes", "/") }
	containsAll(t, "set", cell(), "&lt; 20.0 GB")
	store.thresholds[0].Direction = "sideways"
	containsAll(t, "unreadable", cell(), "cannot be read")
}

// spec: history.md#table — a group is told apart by what it holds: two matrices of one
// family each name their keys and open their own; a metric with no dot names its group.
func TestDebugTellsGroupsApart(t *testing.T) {
	store := stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{
		{Metric: "job.runs", Sensor: "job", Labels: map[string]string{"job": "a"}, Value: 1, TS: lastSeen},
		{Metric: "job.lag", Sensor: "job", Labels: map[string]string{"queue": "b"}, Value: 3, TS: lastSeen},
		{Metric: "general", Sensor: "x", Labels: map[string]string{"k": "v"}, Value: 4, TS: lastSeen},
		{Metric: "load.avg_5m", Sensor: "load", Value: 2, TS: lastSeen},
	}}}}
	links := treeLinks(t, pickedDebug(t, store, "/debug"))
	for _, want := range []string{"General", "general", "job (job)", "job (queue)"} {
		if _, ok := links[want]; !ok {
			t.Errorf("no entry %q in %v", want, links)
		}
	}
	if body := pickedDebug(t, store, links["job (queue)"]); !strings.Contains(body, ">lag<") || strings.Contains(body, ">runs<") {
		t.Errorf("job (queue) drew another matrix: %s", body)
	}
	if body := pickedDebug(t, store, links["general"]); showsGeneral(body) {
		t.Errorf("the family general opened General: %s", body)
	}
}

// spec: history.md#table — four filters, each counting the whole hub whatever is chosen.
func TestDebugFiltersCountTheWholeHub(t *testing.T) {
	for _, target := range []string{"/debug", "/debug?show=problems&q=nothing-matches"} {
		containsAll(t, target, pickedDebug(t, troubled(), target),
			"All<b>6</b>", "Problems<b>1</b>", "Unusual<b>0</b>", "Without a threshold<b>5</b>")
	}
}

// spec: history.md#table — "All": every group of every node, nothing marked on the left.
func TestDebugShowsEverything(t *testing.T) {
	body := pickedDebug(t, troubled(), "/debug?show=all")
	containsAll(t, "page", body, "<h2>laptop-a", "<h2>server-b")
	if !showsGeneral(body) || !showsVolume(body) || strings.Contains(treePane.FindString(body), `aria-current`) {
		t.Errorf("want everything and no entry marked: %s", body)
	}
	if !strings.Contains(body, `aria-current="true">All`) {
		t.Errorf("want All marked as chosen: %s", body)
	}
}

// spec: history.md#table — "Problems": what the timeline would list, on every node; a matrix
// row holding one stays whole, its other cells dimmed; a named group gives way.
func TestDebugFiltersProblems(t *testing.T) {
	body := pickedDebug(t, troubled(), "/debug?show=problems&node=laptop-a&group=x")
	if strings.Contains(body, "<h2>laptop-a") || !strings.Contains(body, "<h2>server-b") || showsGeneral(body) {
		t.Errorf("want server-b's problem alone: %s", body)
	}
	cell := matrixRowOf(t, body, "/").cells["disk.free_pct"]
	if !strings.Contains(cell, "dim") {
		t.Errorf("the row's other cell = %s, want it dimmed", cell)
	}
	if strings.Contains(matrixRowOf(t, body, "/").cells["disk.free_bytes"], "dim") {
		t.Error("the problem itself is dimmed")
	}
}

// spec: history.md#table — a series stale on a node reporting others is a problem only if
// watched, and a sleeping node holds none: "Problems" is the timeline's own.
func TestDebugProblemsAreTheTimelines(t *testing.T) {
	asleep := fleet()
	for i := range asleep.states[0].Values {
		asleep.states[0].Values[i].TS = lastSeen.Add(-24 * time.Hour)
	}
	asleep.states[0].LastSeen = lastSeen.Add(-30 * time.Minute)
	containsAll(t, "asleep", pickedDebug(t, asleep, "/debug"), "Problems<b>0</b>")

	gone := fleet()
	gone.states[1].Values[0].TS = lastSeen.Add(-24 * time.Hour)
	unwatched := pickedDebug(t, gone, "/debug")
	containsAll(t, "unwatched", unwatched, "Problems<b>0</b>")
	gone.thresholds = []storage.Threshold{watched("server-b", "disk.free_bytes", mounted("/"))}
	containsAll(t, "watched", pickedDebug(t, gone, "/debug"), "Problems<b>1</b>", "<h2>server-b")
}

// spec: history.md#table — "Problems" keeps a silent node's line.
func TestDebugProblemsKeepASilentNode(t *testing.T) {
	store := fleet()
	store.states[0].LastSeen = lastSeen.Add(-24 * time.Hour)
	store.levels = []storage.State{silenceLevel("laptop-a", "critical")}
	containsAll(t, "page", pickedDebug(t, store, "/debug?show=problems"), "<h2>laptop-a", "silent")
}

// spec: history.md#table — "Unusual" and "Without a threshold".
func TestDebugFiltersUnusualAndUnwatched(t *testing.T) {
	body := pickedDebug(t, unusualRoot(), "/debug?show=unusual")
	if matrixRowOf(t, body, "/").name == "" || strings.Contains(body, ">/data ") {
		t.Errorf("want / alone: %s", body)
	}
	body = pickedDebug(t, troubled(), "/debug?show=unwatched&q=server-b")
	if !strings.Contains(matrixRowOf(t, body, "/").cells["disk.free_bytes"], "dim") {
		t.Errorf("want the watched series dimmed beside the unwatched: %s", body)
	}
}

// spec: history.md#table — a search looks in the node, the name, the id and the label
// values, whatever the case; it combines with a filter; the field holds the text.
func TestDebugSearches(t *testing.T) {
	for query, want := range map[string]string{"SERVER-B": "<h2>server-b", "avg_5": "<table class=\"own\">", "%2Fdata": ">/data"} {
		store := unusualRoot()
		store.states = append(store.states, fleet().states[1])
		if body := pickedDebug(t, store, "/debug?q="+query); !strings.Contains(body, want) {
			t.Errorf("q=%s lacks %s: %s", query, want, body)
		}
	}
	body := pickedDebug(t, troubled(), "/debug?q=server-b&show=problems")
	if showsGeneral(body) || !showsVolume(body) {
		t.Errorf("want the search and the filter at once: %s", body)
	}
	containsAll(t, "field", body, `value="server-b"`, `name="show" value="problems"`)
	if strings.Contains(pickedDebug(t, troubled(), "/debug?q=server-b"), `aria-current="true">All`) {
		t.Error("a search alone marks a filter as chosen")
	}
	if !strings.Contains(pickedDebug(t, troubled(), "/debug?q=server-b&show=all"), `aria-current="true">All`) {
		t.Error("All with a search is not marked")
	}
	containsAll(t, "a filter keeps the search", body, `q=server-b&amp;show=unusual`)
}

// spec: history.md#table — nothing matches: a line and a way back; a filter it does not know
// and an empty search read as neither.
func TestDebugFilterEdges(t *testing.T) {
	body := pickedDebug(t, fleet(), "/debug?q=nothing-here")
	containsAll(t, "page", body, "Nothing matches", `<a href="/debug">`)
	for _, target := range []string{"/debug?show=bogus", "/debug?q=%20%20", "/debug?q="} {
		if body := pickedDebug(t, troubled(), target); !strings.Contains(body, `aria-current="true"`) {
			t.Errorf("%s read as a filter: %s", target, body)
		}
	}
	if body := pickedDebug(t, troubled(), "/debug?show=bogus&q=server-b"); !strings.Contains(body, `value="server-b"`) || strings.Contains(body, "<h2>laptop-a") {
		t.Errorf("an unknown filter cost the search: %s", body)
	}
}

// spec: history.md#table — the node's line carries its last report once, and no row a time.
func TestDebugGivesATimeOncePerNode(t *testing.T) {
	body := pickedDebug(t, fleet(), "/debug?show=all&node=laptop-a")
	if !strings.Contains(body, "Last seen") {
		t.Errorf("no last report: %s", body)
	}
	if strings.Contains(body, ">Collected<") {
		t.Errorf("a column of times is drawn: %s", body)
	}
}

// spec: state.md#page — a mark stands before its value, a level's and an unusual one's in
// that order; the level on hover.
func TestDebugMarksStandBeforeTheValue(t *testing.T) {
	store := unusualRoot()
	store.thresholds = []storage.Threshold{watched("laptop-a", "disk.free_bytes", mounted("/"))}
	store.levels = []storage.State{level("laptop-a", "disk.free_bytes", mounted("/"), "warning")}
	cell := cellOf(t, showDebug(t, store, "/debug"), "disk.free_bytes", "/")
	level, unusual, value := strings.Index(cell, "▲"), strings.Index(cell, "◆"), strings.Index(cell, "5.0 GB")
	if level < 0 || unusual < level || value < unusual {
		t.Errorf("cell = %s, want the level's mark, then unusual, then the value", cell)
	}
	containsAll(t, "cell", cell, `title="warning`, "level-warning")
}

// spec: state.md#page — a stale series is greyed under its own mark whatever level it stored,
// the level kept on hover; it is no problem by that level.
func TestDebugAStaleSeriesKeepsItsLevelOnHover(t *testing.T) {
	store := troubled()
	store.states[1].Values[0].TS = lastSeen.Add(-24 * time.Hour)
	cell := cellOf(t, showDebug(t, store, "/debug?q=server-b"), "disk.free_bytes", "/")
	containsAll(t, "cell", cell, "◌", "level-stale", `title="critical`, "no fresh data")
	if strings.Contains(cell, "■") {
		t.Errorf("cell = %s, want no critical mark on a stale series", cell)
	}
}

// spec: history.md#page — a row of its own says what the series is judged by: a sign and its
// values, "none", or that it cannot be read.
func TestDebugRowsSayWhatJudgesThem(t *testing.T) {
	warning, critical := 4.0, 8.0
	load := storage.SeriesRef{Node: "laptop-a", Metric: "load.avg_5m"}
	for name, tc := range map[string]struct {
		th   []storage.Threshold
		want string
	}{
		"none":       {nil, ">none<"},
		"set":        {[]storage.Threshold{{Series: load, Direction: storage.Above, Warning: &warning, Critical: &critical}}, "&gt; 4 / 8"},
		"unreadable": {[]storage.Threshold{{Series: load, Direction: "sideways", Warning: &warning}}, ">cannot be read<"},
	} {
		store := fleet()
		store.thresholds = tc.th
		if body := pickedDebug(t, store, "/debug?show=all"); !strings.Contains(body, tc.want) {
			t.Errorf("%s: page lacks %q: %s", name, tc.want, body)
		}
	}
}

// spec: history.md#page — a whole row or cell is one link, to the settings of its series.
func TestDebugLinksToSettings(t *testing.T) {
	body := showDebug(t, fleet(), "/debug")
	cell := cellOf(t, body, "disk.free_pct", "/")
	containsAll(t, "cell", cell, `class="fill"`, `href="/thresholds?`, "metric=disk.free_pct", "label.mount=%2F")
	if strings.Contains(cell, "/history?") {
		t.Errorf("cell = %s, want no link to the chart", cell)
	}
	containsAll(t, "row", ownRows(t, body)[0], `class="fill"`, `href="/thresholds?`, "metric=load.avg_5m")
}

// spec: history.md#page — a matrix row is named by its values; an empty one or one holding a
// comma in quotes; the names on hover.
func TestDebugNamesARowByItsValues(t *testing.T) {
	values := []storage.Value{
		{Metric: "backup.failed", Sensor: "backup", Labels: map[string]string{"backup": "vpn", "job": "copy"}, Value: 0, TS: lastSeen},
		{Metric: "backup.failed", Sensor: "backup", Labels: map[string]string{"backup": "a, b", "job": ""}, Value: 0, TS: lastSeen},
	}
	body := showDebug(t, stored{states: []storage.NodeState{{Node: "server-b", LastSeen: lastSeen, Values: values}}}, "/debug")
	rows := matrices(t, body)[0].rows
	if rows[0].name != "&#34;a, b&#34;, &#34;&#34;" || rows[1].name != "vpn, copy" {
		t.Errorf("rows = %q and %q", rows[0].name, rows[1].name)
	}
	containsAll(t, "page", body, `title="backup=vpn job=copy"`)
	if !strings.Contains(rows[1].cells["backup.failed"], ">0<") {
		t.Errorf("cell = %s, want a whole number with no decimals", rows[1].cells["backup.failed"])
	}
}

// spec: attention.md#items, attention.md#order, timeline.md — what points at /debug says
// where.
func TestAttentionLinksIntoTheTable(t *testing.T) {
	store := fleet()
	store.states[0].LastSeen = lastSeen.Add(-24 * time.Hour)
	store.levels = []storage.State{silenceLevel("laptop-a", "critical")}
	containsAll(t, "timeline", showTimeline(t, logged{stored: store}, "/timeline"), `href="/debug?node=laptop-a"`)
}
