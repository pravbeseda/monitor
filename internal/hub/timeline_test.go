package hub_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

// logged is a store that also keeps an event log.
type logged struct {
	stored
	events []storage.Transition
	// asked records the nodes the page asked recent events of.
	asked *[]string
}

func (l logged) EventsBetween(_ context.Context, from, to time.Time) ([]storage.Transition, error) {
	var out []storage.Transition
	for _, event := range l.events {
		if event.At.After(from) && !event.At.After(to) {
			out = append(out, event)
		}
	}
	return out, l.err
}

func (l logged) RecentEvents(_ context.Context, nodes []string, limit int) ([]storage.Transition, error) {
	if l.asked != nil {
		*l.asked = append([]string(nil), nodes...)
	}
	named := map[string]bool{}
	for _, node := range nodes {
		named[node] = true
	}
	var out []storage.Transition
	for i := len(l.events) - 1; i >= 0; i-- {
		if named[l.events[i].Node] && len(out) < limit {
			out = append(out, l.events[i])
		}
	}
	return out, l.err
}

func showTimeline(t *testing.T, store hub.Store, target string) string {
	t.Helper()
	rec := getState(t, store, target, func() time.Time { return lastSeen })
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// reportingNode is a node whose disk series sent a point every five minutes for 30 hours.
func reportingNode(node string) storage.NodeState {
	state := laptop
	state.Node = node
	return state
}

func everyFiveMinutes() []storage.Point {
	var out []storage.Point
	for ts := lastSeen.Add(-30 * time.Hour); !ts.After(lastSeen); ts = ts.Add(5 * time.Minute) {
		out = append(out, storage.Point{TS: ts, Value: 40})
	}
	return out
}

func timelineStore(nodes ...storage.NodeState) logged {
	points := everyFiveMinutes()
	return logged{stored: stored{states: nodes, points: map[string][]storage.Point{"disk.free_pct": points, "disk.free_bytes": points}}}
}

var (
	lanePattern = regexp.MustCompile(`(?s)<div class="lane"><span class="node">([^<]*)</span><div class="cells">(.*?)</div></div>`)
	cellPattern = regexp.MustCompile(`<i class="cell cell-([a-z-]+)" title="([^"]*)"></i>`)
)

// lanesOf maps each lane's node to its cells, each as class and hover title.
func lanesOf(body string) (order []string, cells map[string][][2]string) {
	cells = map[string][][2]string{}
	for _, lane := range lanePattern.FindAllStringSubmatch(body, -1) {
		order = append(order, lane[1])
		for _, cell := range cellPattern.FindAllStringSubmatch(lane[2], -1) {
			cells[lane[1]] = append(cells[lane[1]], [2]string{cell[1], cell[2]})
		}
	}
	return order, cells
}

// attentionPattern is the headline and the items, up to the end of the panel or the page.
var attentionPattern = regexp.MustCompile(`(?s)(<h1 class="headline.*?)\s*(?:</section>|</body>)`)

// spec: timeline.md#now — now is mission control's headline, notice and items.
func TestTheTimelineShowsWhatMissionControlShows(t *testing.T) {
	root := mounted("/")
	for _, tc := range []struct {
		name       string
		thresholds []storage.Threshold
		levels     []storage.State
		headline   string
	}{
		{"a node silent", nil, []storage.State{silenceLevel("laptop-a", "critical")}, "laptop-a"},
		{"a volume critical", []storage.Threshold{watched("laptop-a", "disk.free_bytes", root)},
			[]storage.State{silenceLevel("laptop-a", "ok"), level("laptop-a", "disk.free_bytes", root, "critical")}, "disk.free_bytes"},
		{"everything ok", []storage.Threshold{watched("laptop-a", "disk.free_bytes", root)},
			[]storage.State{silenceLevel("laptop-a", "ok"), level("laptop-a", "disk.free_bytes", root, "ok")}, "All is well"},
		{"nothing watched", nil, nil, "Nothing here is being judged yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := timelineStore(reportingNode("laptop-a"))
			store.thresholds, store.levels = tc.thresholds, tc.levels
			board := getState(t, store, "/board", func() time.Time { return lastSeen }).Body.String()
			want := attentionPattern.FindStringSubmatch(board)
			if want == nil || !strings.Contains(want[1], tc.headline) {
				t.Fatalf("the board does not show %q: %v", tc.headline, want)
			}
			if got := attentionPattern.FindStringSubmatch(showTimeline(t, store, "/timeline")); got == nil || got[1] != want[1] {
				t.Errorf("now = %v\nwant mission control's %s", got, want[1])
			}
		})
	}
}

// spec: timeline.md#lanes — one lane per node that takes part, in the order of their names,
// named, 24 cells each.
func TestALaneForEveryNodeThatTakesPart(t *testing.T) {
	store := timelineStore(reportingNode("server-b"), reportingNode("server-z"), reportingNode("laptop-a"))
	store.levels = []storage.State{level("server-b", "disk.free_pct", mounted("/"), "critical")}
	order, cells := lanesOf(showTimeline(t, store, "/timeline"))
	if strings.Join(order, ",") != "laptop-a,server-b" {
		t.Fatalf("lanes = %v, want the two configured nodes in name order, the critical one second", order)
	}
	for node, lane := range cells {
		if len(lane) != 24 {
			t.Errorf("%s has %d cells", node, len(lane))
		}
	}
	for _, cell := range cells["laptop-a"] {
		if cell[0] != "reporting" {
			t.Errorf("laptop-a shows %s with nothing watched", cell[0])
			break
		}
	}

	if order, _ := lanesOf(showTimeline(t, timelineStore(reportingNode("laptop-a")), "/timeline")); strings.Join(order, ",") != "laptop-a" {
		t.Errorf("lanes = %v, want none for server-b, which never reported", order)
	}
}

// spec: timeline.md#lanes — the hour of every fourth cell under the lanes, and each cell's
// hour and word on hovering.
func TestTheLanesNameTheirHours(t *testing.T) {
	body := showTimeline(t, timelineStore(reportingNode("laptop-a")), "/timeline")
	hours := regexp.MustCompile(`<span class="hour">([^<]*)</span>`).FindAllStringSubmatch(body, -1)
	var labels []string
	for _, hour := range hours {
		if hour[1] != "" {
			labels = append(labels, hour[1])
		}
	}
	if got := strings.Join(labels, " "); len(hours) != 24 || got != "11:00 15:00 19:00 23:00 03:00 07:00" {
		t.Errorf("hours = %q over %d cells, want every fourth from 11:00 yesterday", got, len(hours))
	}
	_, cells := lanesOf(body)
	if lane := cells["laptop-a"]; len(lane) != 24 || lane[23][1] != "10:00 reporting, nothing watched" {
		t.Errorf("last cell = %v, want its hour and its word", lane[len(lane)-1])
	}
}

// spec: timeline.md#lanes — the legend names every state with its colour.
func TestTheLegendNamesEveryState(t *testing.T) {
	body := showTimeline(t, timelineStore(reportingNode("laptop-a")), "/timeline")
	legend := regexp.MustCompile(`<li><i class="cell cell-([a-z-]+)"></i>([^<]*)</li>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, entry := range legend {
		got = append(got, entry[1]+"="+entry[2])
	}
	want := "silent=silent no-data=no fresh data critical=critical warning=warning ok=ok reporting=reporting, nothing watched"
	if strings.Join(got, " ") != want {
		t.Errorf("legend = %q, want %q", strings.Join(got, " "), want)
	}
}

// spec: timeline.md#lanes — a level the log and the stored levels record colours the hours
// it was held in, and a silence wins where it held.
func TestTheLanesReadTheLogAndTheLevels(t *testing.T) {
	root := map[string]string{"mount": "/", "fs": "apfs", "removable": "false"}
	store := timelineStore(reportingNode("laptop-a"), reportingNode("server-b"))
	store.levels = []storage.State{
		{Subject: storage.Subject{Node: "laptop-a", Metric: "disk.free_pct", Labels: root}, Level: "critical", Since: lastSeen.Add(-90 * time.Minute)},
		{Subject: storage.Subject{Node: "server-b", Metric: "silence"}, Level: "critical", Since: lastSeen.Add(-30 * time.Minute)},
	}
	store.events = []storage.Transition{
		{Subject: storage.Subject{Node: "laptop-a", Metric: "disk.free_pct", Labels: root}, At: lastSeen.Add(-90 * time.Minute), From: "ok", To: "critical", FromSince: lastSeen.Add(-30 * time.Hour)},
		{Subject: storage.Subject{Node: "server-b", Metric: "silence"}, At: lastSeen.Add(-30 * time.Minute), From: "ok", To: "critical", FromSince: lastSeen.Add(-30 * time.Hour)},
	}
	_, cells := lanesOf(showTimeline(t, store, "/timeline"))

	var laptop, server []string
	for i := range 24 {
		laptop = append(laptop, cells["laptop-a"][i][0])
		server = append(server, cells["server-b"][i][0])
	}
	// 08:35 critical: the 08:00 cell on; ok before it, since the level was watched.
	if want := strings.Repeat("ok ", 21) + "critical critical critical"; strings.Join(laptop, " ") != want {
		t.Errorf("laptop-a = %v", laptop)
	}
	// 09:35 silent: the 09:00 cell on.
	if want := strings.Repeat("reporting ", 22) + "silent silent"; strings.Join(server, " ") != want {
		t.Errorf("server-b = %v", server)
	}
}

var (
	changePattern = regexp.MustCompile(`(?s)<li class="change">(.*?)</li>`)
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
)

// changesOf is each change as its raw markup and as the text a reader sees.
func changesOf(body string) (raw, text []string) {
	for _, found := range changePattern.FindAllStringSubmatch(body, -1) {
		raw = append(raw, found[1])
		text = append(text, strings.Join(strings.Fields(tagPattern.ReplaceAllString(found[1], " ")), " "))
	}
	return raw, text
}

func dataChange(at time.Duration, from, to string, reading float64) storage.Transition {
	return storage.Transition{
		Subject: storage.Subject{Node: "server-b", Metric: "disk.free_pct", Labels: dataVolume},
		At:      lastSeen.Add(at), From: from, To: to, FromSince: lastSeen.Add(at - time.Hour),
		Readings: map[string]float64{"disk.free_pct": reading},
	}
}

func silenceChange(at time.Duration, from, to string) storage.Transition {
	return storage.Transition{Subject: storage.Subject{Node: "laptop-a", Metric: "silence"}, At: lastSeen.Add(at), From: from, To: to, FromSince: lastSeen.Add(at - time.Hour)}
}

// spec: timeline.md#changes — each change with its time, a dot in the colour of the level it
// entered, its node, series, levels and value, the newest first, under the day it happened
// on; a silence names its window and links to the table, a series to its chart.
func TestTheChangesListWhatTheLogRecorded(t *testing.T) {
	store := timelineStore(reportingNode("laptop-a"), reportingNode("server-b"))
	firstCritical := dataChange(-26*time.Hour, "ok", "critical", 3)
	firstCritical.FromSince = firstCritical.At
	store.events = []storage.Transition{
		firstCritical,
		dataChange(-25*time.Hour, "critical", "warning", 12),
		silenceChange(-3*time.Hour, "ok", "critical"),
		dataChange(-63*time.Minute, "warning", "critical", 4),
		dataChange(-62*time.Minute, "critical", "ok", 30),
		silenceChange(-time.Hour, "critical", "ok"),
	}
	body := showTimeline(t, store, "/timeline?lang=ru")
	raw, got := changesOf(body)

	want := []string{
		"09:05 laptop-a · снова на связи",
		"09:03 server-b · disk.free_pct · /data критично → норма 30,0 %",
		"09:02 server-b · disk.free_pct · /data предупреждение → критично 4,0 %",
		"07:05 laptop-a · замолчал нет отчётов 2,0 д",
		"09:05 server-b · disk.free_pct · /data критично → предупреждение 12,0 %",
		"08:05 server-b · disk.free_pct · /data норма → критично 3,0 %",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("changes\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i, dot := range []string{"ok", "ok", "critical", "critical", "warning", "critical"} {
		if !strings.Contains(raw[i], `<i class="dot dot-`+dot+`">`) {
			t.Errorf("change %d carries no %s dot: %s", i, dot, raw[i])
		}
	}
	if !strings.Contains(raw[0], `href="/debug?lang=ru"`) {
		t.Errorf("the silence links elsewhere: %s", raw[0])
	}
	if !strings.Contains(raw[1], `href="/history?label.fs=ext4&amp;label.mount=%2Fdata&amp;label.removable=false&amp;lang=ru&amp;metric=disk.free_pct&amp;node=server-b"`) {
		t.Errorf("the series links elsewhere: %s", raw[1])
	}
	days := regexp.MustCompile(`<h3 class="day">([^<]*)</h3>`).FindAllStringSubmatch(body, -1)
	if len(days) != 2 || days[0][1] != "Сегодня" || days[1][1] != "Вчера" {
		t.Errorf("days = %v, want today then yesterday", days)
	}
}

// spec: timeline.md#changes — the newest 50, of the nodes that take part only, and a hub
// with no change says so.
func TestTheChangesOfTheNodesThatTakePart(t *testing.T) {
	var asked []string
	store := timelineStore(reportingNode("server-b"), reportingNode("server-z"), reportingNode("laptop-a"))
	store.asked = &asked
	body := showTimeline(t, store, "/timeline")
	sort.Strings(asked)
	if strings.Join(asked, ",") != "laptop-a,server-b" {
		t.Errorf("asked the log about %v", asked)
	}
	if !strings.Contains(body, "No level has changed yet") {
		t.Errorf("an empty log says nothing: %s", body)
	}

	for i := range 60 {
		store.events = append(store.events, dataChange(time.Duration(i-60)*time.Minute, "ok", "warning", 12))
	}
	if _, got := changesOf(showTimeline(t, store, "/timeline")); len(got) != 50 || !strings.HasPrefix(got[0], "10:04") {
		t.Errorf("listed %d changes, the first %q; want the newest 50", len(got), got[0])
	}
}

// spec: timeline.md#lanes — the hours and the hover titles are the reader's, in a zone
// half an hour off UTC too.
func TestTheLanesSpeakTheReadersZone(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/timeline", nil)
	req.AddCookie(&http.Cookie{Name: "tz", Value: "Asia/Kolkata"})
	rec := httptest.NewRecorder()
	routesWith(t, timelineStore(reportingNode("laptop-a")), func() time.Time { return lastSeen }).ServeHTTP(rec, req)
	_, cells := lanesOf(rec.Body.String())
	// 10:05 UTC is 15:35 in Kolkata.
	if lane := cells["laptop-a"]; len(lane) != 24 || lane[23][1] != "15:00 reporting, nothing watched" {
		t.Errorf("last cell = %v, want the Kolkata hour", lane)
	}
}

// spec: timeline.md#page — every word in Russian, the language kept on every link.
func TestTheTimelineInRussian(t *testing.T) {
	store := timelineStore(reportingNode("laptop-a"), reportingNode("server-b"))
	store.levels = []storage.State{silenceLevel("laptop-a", "critical")}
	store.events = []storage.Transition{silenceChange(-time.Hour, "ok", "critical"), dataChange(-time.Minute, "ok", "warning", 12)}
	body := showTimeline(t, store, "/timeline?lang=ru")
	for _, word := range []string{"Сейчас", "Последние 24 часа", "Что менялось", "Сегодня", "на связи, без порогов"} {
		if !strings.Contains(body, word) {
			t.Errorf("the Russian timeline lacks %q", word)
		}
	}
	for _, found := range hrefPattern.FindAllStringSubmatch(body, -1) {
		if strings.HasPrefix(found[1], "/") && !strings.Contains(found[1], "lang=ru") {
			t.Errorf("a link drops the language: %s", found[1])
		}
	}
}

// spec: timeline.md#page — a failed read answers as mission control does.
func TestTheTimelineFailsAsMissionControlDoes(t *testing.T) {
	store := timelineStore(reportingNode("laptop-a"))
	store.err = errors.New("disk on fire")
	clock := func() time.Time { return lastSeen }
	board, timeline := getState(t, store, "/board", clock), getState(t, store, "/timeline", clock)
	if timeline.Code != board.Code || timeline.Body.String() != board.Body.String() {
		t.Errorf("timeline = %d %q, board = %d %q", timeline.Code, timeline.Body, board.Code, board.Body)
	}

	store.err, store.pointsErr = nil, errors.New("points on fire")
	failed := getState(t, store, "/timeline", clock)
	if failed.Code != board.Code || failed.Body.String() != board.Body.String() {
		t.Errorf("a failed read of the points = %d %q, want mission control's failure", failed.Code, failed.Body)
	}
}

// spec: timeline.md#model — a tick that lands while the page is being read shows no level
// shorter than it was: the change it wrote still closes the span before it.
func TestAChangeAtTheWindowsFirstInstantIsShown(t *testing.T) {
	root := map[string]string{"mount": "/", "fs": "apfs", "removable": "false"}
	volume := storage.Subject{Node: "laptop-a", Metric: "disk.free_pct", Labels: root}
	store := timelineStore(reportingNode("laptop-a"))
	// Entered at the first cell's first instant, then forgotten: only the change remains.
	store.events = []storage.Transition{
		{Subject: volume, At: lastSeen.Add(-23*time.Hour - 5*time.Minute), From: "ok", To: "critical", FromSince: lastSeen.Add(-30 * time.Hour)},
	}
	_, cells := lanesOf(showTimeline(t, store, "/timeline"))
	if got := cells["laptop-a"][0][0]; got != "critical" {
		t.Errorf("the 11:00 cell = %q, want critical", got)
	}
}

func TestATickDuringTheReadKeepsTheLevelBeforeIt(t *testing.T) {
	root := map[string]string{"mount": "/", "fs": "apfs", "removable": "false"}
	volume := storage.Subject{Node: "laptop-a", Metric: "disk.free_pct", Labels: root}
	store := timelineStore(reportingNode("laptop-a"))
	store.levels = []storage.State{{Subject: volume, Level: "critical", Since: lastSeen.Add(time.Minute)}}
	store.events = []storage.Transition{
		{Subject: volume, At: lastSeen.Add(-2 * time.Hour), From: "ok", To: "warning", FromSince: lastSeen.Add(-30 * time.Hour)},
		{Subject: volume, At: lastSeen.Add(time.Minute), From: "warning", To: "critical", FromSince: lastSeen.Add(-2 * time.Hour)},
	}
	_, cells := lanesOf(showTimeline(t, store, "/timeline"))
	var got []string
	for _, cell := range cells["laptop-a"] {
		got = append(got, cell[0])
	}
	// 08:05 warning, held until the tick after the page's instant: 08:00 to 10:00.
	if want := strings.Repeat("ok ", 21) + "warning warning warning"; strings.Join(got, " ") != want {
		t.Errorf("laptop-a = %v", got)
	}
}
