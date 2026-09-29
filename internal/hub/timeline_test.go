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

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// logged is a store that also keeps an event log and the anomalies recorded beside it.
type logged struct {
	stored
	events    []storage.Transition
	anomalies []storage.Anomaly
	// asked records the nodes the page asked recent events of.
	asked *[]string
}

func (l logged) AnomaliesSince(_ context.Context, from time.Time) ([]storage.Anomaly, error) {
	var out []storage.Anomaly
	for _, record := range l.anomalies {
		if record.Ended.IsZero() || record.Ended.After(from) {
			out = append(out, record)
		}
	}
	return out, l.err
}

// RecentAnomalies hands back every record of the named nodes: the page takes the newest of
// what it is given, and a test holds few enough.
func (l logged) RecentAnomalies(_ context.Context, nodes []string, _ int) ([]storage.Anomaly, error) {
	named := map[string]bool{}
	for _, node := range nodes {
		named[node] = true
	}
	var out []storage.Anomaly
	for _, record := range l.anomalies {
		if named[record.Node] {
			out = append(out, record)
		}
	}
	return out, l.err
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

// nowPattern is the panel headed "Now".
var nowPattern = regexp.MustCompile(`(?s)<section class="panel now">.*?</section>`)

func nowOf(t *testing.T, body string) string {
	t.Helper()
	now := nowPattern.FindString(body)
	if now == "" {
		t.Fatalf("no now panel in %s", body)
	}
	return now
}

// spec: timeline.md#now
func TestNowIsTheListOfWhatNeedsAttention(t *testing.T) {
	for _, tc := range []struct {
		name     string
		current  state.State
		headline string
		items    []string
		notice   bool
	}{
		{"a volume critical, a node silent, a series ranking", stateOf(
			silenceOf("server-a", evaluate.OK), ranked(judgedAt(freePct("server-a", "/", 3.5), evaluate.OK), 1),
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/data", 1e9), evaluate.Critical),
			silenceOf("server-c", evaluate.Critical),
		), "critical", []string{"server-b", "server-c", "server-a"}, false},
		{"everything ok, nothing ranking", stateOf(
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK),
		), "All is well", nil, false},
		{"nothing watched", stateOf(
			silenceOf("server-b", evaluate.OK), free("server-b", "/", 40e9),
		), "Nothing is judged yet", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := nowOf(t, showAttention(t, tc.current, "/timeline"))
			if got := headline(t, now); got != tc.headline {
				t.Errorf("headline = %q, want %q", got, tc.headline)
			}
			got := items(now)
			if len(got) != len(tc.items) {
				t.Fatalf("items = %v, want %d", got, len(tc.items))
			}
			for i, node := range tc.items {
				if !strings.Contains(got[i], node) {
					t.Errorf("item %d = %s, want %s's", i, got[i], node)
				}
			}
			if notice := strings.Contains(now, "Nothing here is being judged yet"); notice != tc.notice {
				t.Errorf("nothing-judged notice shown: %v, want %v", notice, tc.notice)
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
	want := "silent=silent no-data=no fresh data critical=critical warning=warning unusual=unusual ok=ok reporting=reporting, nothing watched"
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
	if !strings.Contains(body, "Nothing has changed yet") {
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

// spec: timeline.md#page — a failed read answers as /debug does.
func TestTheTimelineFailsAsTheTableDoes(t *testing.T) {
	store := timelineStore(reportingNode("laptop-a"))
	store.err = errors.New("disk on fire")
	clock := func() time.Time { return lastSeen }
	table, timeline := getState(t, store, "/debug", clock), getState(t, store, "/timeline", clock)
	if timeline.Code != table.Code || timeline.Body.String() != table.Body.String() {
		t.Errorf("timeline = %d %q, table = %d %q", timeline.Code, timeline.Body, table.Code, table.Body)
	}

	store.err, store.pointsErr = nil, errors.New("points on fire")
	failed := getState(t, store, "/timeline", clock)
	if failed.Code != table.Code || failed.Body.String() != table.Body.String() {
		t.Errorf("a failed read of the points = %d %q, want the table's failure", failed.Code, failed.Body)
	}
}

// spec: timeline.md#model — a tick that lands while the page is being read shows no level
// shorter than it was: the change it wrote still closes the span before it.
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

// spec: timeline.md#model — a level whose threshold was removed shows in the hour it began,
// the window's first instant included.
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

// recordOf is a record of server-b's /data volume that began at began from lastSeen, ended
// at ended unless ended is zero.
func recordOf(began, ended time.Duration, value float64, back *float64) storage.Anomaly {
	record := storage.Anomaly{
		Subject: storage.Subject{Node: "server-b", Metric: "disk.free_pct", Labels: dataVolume},
		Began:   lastSeen.Add(began), Value: value, Low: 20, High: 60, Back: back,
	}
	if ended != 0 {
		record.Ended = lastSeen.Add(ended)
	}
	return record
}

func backAt(v float64) *float64 { return &v }

// spec: timeline.md#changes — an anomaly's start and end are changes beside the levels', a
// withdrawal lists no end, and at one instant an anomaly comes before a transition.
func TestTheChangesListAnomalies(t *testing.T) {
	store := timelineStore(reportingNode("server-b"))
	store.events = []storage.Transition{dataChange(-2*time.Hour, "ok", "warning", 12)}
	store.anomalies = []storage.Anomaly{
		recordOf(-3*time.Hour, -2*time.Hour, 5, backAt(40)),
		recordOf(-90*time.Minute, -70*time.Minute, 90, nil),
		recordOf(-30*time.Minute, 0, 95, nil),
	}
	body := showTimeline(t, store, "/timeline")
	raw, got := changesOf(body)
	want := []string{
		"09:35 server-b · disk.free_pct · /data unusual 95.0% usually 20.0% to 60.0%",
		"08:35 server-b · disk.free_pct · /data unusual 90.0% usually 20.0% to 60.0%",
		"08:05 server-b · disk.free_pct · /data usual again 40.0%",
		"08:05 server-b · disk.free_pct · /data ok → warning 12.0%",
		"07:05 server-b · disk.free_pct · /data unusual 5.0% usually 20.0% to 60.0%",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("changes\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i, dot := range []string{"unusual", "unusual", "usual", "warning", "unusual"} {
		if !strings.Contains(raw[i], `<i class="dot dot-`+dot+`">`) {
			t.Errorf("change %d carries no %s dot: %s", i, dot, raw[i])
		}
	}
	if !strings.Contains(raw[0], `href="/history?label.fs=ext4&amp;label.mount=%2Fdata&amp;label.removable=false&amp;metric=disk.free_pct&amp;node=server-b"`) {
		t.Errorf("the anomaly links elsewhere: %s", raw[0])
	}
}

// spec: timeline.md#changes — a band of one value reads as that value, a negative band
// reads plainly, and a volume's band speaks the reader's language and unit.
func TestAnAnomalysBandReadsInItsUnit(t *testing.T) {
	flat := recordOf(-time.Hour, 0, 1, nil)
	flat.Low, flat.High = 0, 0
	negative := recordOf(-50*time.Minute, 0, 5, nil)
	negative.Metric, negative.Low, negative.High = "temperature", -1, 1
	bytes := recordOf(-40*time.Minute, 0, 5e9, nil)
	bytes.Metric, bytes.Low, bytes.High = "disk.free_bytes", 10e9, 12e9

	store := timelineStore(reportingNode("server-b"))
	store.anomalies = []storage.Anomaly{flat}
	if _, got := changesOf(showTimeline(t, store, "/timeline")); len(got) != 1 || !strings.HasSuffix(got[0], "usually 0.0%") {
		t.Errorf("a flat band reads %q", got)
	}
	store.anomalies = []storage.Anomaly{negative}
	if _, got := changesOf(showTimeline(t, store, "/timeline")); len(got) != 1 || !strings.HasSuffix(got[0], "usually -1.00 to 1.00") {
		t.Errorf("a negative band reads %q", got)
	}
	store.anomalies = []storage.Anomaly{bytes}
	if _, got := changesOf(showTimeline(t, store, "/timeline?lang=ru")); len(got) != 1 || !strings.HasSuffix(got[0], "необычно 5,0 ГБ обычно от 10,0 ГБ до 12,0 ГБ") {
		t.Errorf("a volume's band in Russian reads %q", got)
	}
}

// spec: timeline.md#changes — two anomalies at one instant list in the reverse of the
// node order, and anomalies count towards the newest 50 with the transitions.
func TestAnomaliesShareTheNewestFifty(t *testing.T) {
	store := timelineStore(reportingNode("laptop-a"), reportingNode("server-b"))
	a := recordOf(-10*time.Minute, 0, 90, nil)
	a.Node = "laptop-a"
	store.anomalies = []storage.Anomaly{a, recordOf(-10*time.Minute, 0, 90, nil)}
	if _, got := changesOf(showTimeline(t, store, "/timeline")); len(got) != 2 || !strings.Contains(got[0], "server-b") {
		t.Errorf("changes = %q, want server-b's first", got)
	}

	store.anomalies = nil
	for i := range 20 {
		store.anomalies = append(store.anomalies, recordOf(time.Duration(2*i-60)*time.Minute, time.Duration(2*i-59)*time.Minute, 90, backAt(40)))
	}
	for i := range 40 {
		store.events = append(store.events, dataChange(time.Duration(i-100)*time.Minute, "ok", "warning", 12))
	}
	// 40 anomaly entries and 40 transitions: the newest 50 are the 40 entries and the ten
	// newest transitions.
	_, got := changesOf(showTimeline(t, store, "/timeline"))
	if len(got) != 50 || !strings.Contains(got[49], "ok → warning") || !strings.HasPrefix(got[49], "08:55") {
		t.Errorf("listed %d changes, the last %q", len(got), got[len(got)-1])
	}
}

// spec: timeline.md#lanes — a lane paints the hours an anomaly the store recorded was
// open, while its series was fresh.
func TestTheLanesReadTheAnomalies(t *testing.T) {
	root := map[string]string{"mount": "/", "fs": "apfs", "removable": "false"}
	store := timelineStore(reportingNode("laptop-a"))
	store.anomalies = []storage.Anomaly{{
		Subject: storage.Subject{Node: "laptop-a", Metric: "disk.free_pct", Labels: root},
		Began:   lastSeen.Add(-150 * time.Minute), Ended: lastSeen.Add(-70 * time.Minute), Back: backAt(40),
	}}
	_, cells := lanesOf(showTimeline(t, store, "/timeline"))
	var got []string
	for _, cell := range cells["laptop-a"] {
		got = append(got, cell[0])
	}
	// 07:35 to 08:55: the 07:00 and 08:00 cells.
	if want := strings.Repeat("reporting ", 20) + "unusual unusual reporting reporting"; strings.Join(got, " ") != want {
		t.Errorf("laptop-a = %v", got)
	}
	if title := cells["laptop-a"][20][1]; title != "07:00 unusual" {
		t.Errorf("hover = %q", title)
	}
}
