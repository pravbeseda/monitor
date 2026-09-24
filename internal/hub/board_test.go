package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
)

// The board is a consumer of the state and nothing else, so its rows are tested against
// states written by hand: what the state says is state.md's to test.

func lvl(level evaluate.Level) *evaluate.Level { return &level }

func reading(node, metric string, labels map[string]string, value float64) state.Subject {
	return state.Subject{
		Node: node, Metric: metric, Labels: labels,
		Unit: history.UnitOf(metric), Value: &value, TS: lastSeen.Add(-time.Hour),
	}
}

func judgedAt(s state.Subject, level evaluate.Level) state.Subject {
	s.Watched, s.Level, s.Since = true, lvl(level), lastSeen.Add(-2*time.Hour)
	return s
}

func watchedOnly(s state.Subject) state.Subject {
	s.Watched = true
	return s
}

func staled(s state.Subject) state.Subject {
	s.Stale = true
	return s
}

func ranked(s state.Subject, rank int) state.Subject {
	score := -5.0
	s.Anomaly = &anomaly.Anomaly{Norm: 40e9, Score: &score, Rank: rank}
	return s
}

func silenceOf(node string, level evaluate.Level) state.Subject {
	return state.Subject{Node: node, Metric: evaluate.SilenceMetric, Labels: map[string]string{}, Watched: true, Level: lvl(level)}
}

func free(node, mount string, value float64) state.Subject {
	return reading(node, "disk.free_bytes", map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}, value)
}

func freePct(node, mount string, value float64) state.Subject {
	return reading(node, "disk.free_pct", map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}, value)
}

// stateOf is a state holding the given subjects on configured nodes, each node silent
// only when its silence subject says so. Its levels and counts follow from the subjects
// the way state.md says they do.
func stateOf(subjects ...state.Subject) state.State {
	out := state.State{At: lastSeen, Subjects: subjects}
	nodes := map[string]*state.Node{}
	var order []string
	for _, s := range subjects {
		n, seen := nodes[s.Node]
		if !seen {
			n = &state.Node{Node: s.Node, Configured: true, LastSeen: lastSeen}
			nodes[s.Node] = n
			order = append(order, s.Node)
		}
		if s.Metric != evaluate.SilenceMetric && s.Watched {
			n.Watched++
			out.Watched++
		}
		if !s.Stale && s.Level != nil && (n.Level == nil || *s.Level > *n.Level) {
			n.Level = s.Level
		}
	}
	for _, name := range order {
		n := nodes[name]
		if n.Level != nil && (out.Level == nil || *n.Level > *out.Level) {
			out.Level = n.Level
		}
		out.Nodes = append(out.Nodes, *n)
	}
	return out
}

func showBoard(t *testing.T, current state.State, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	hub.Board(func(context.Context) (state.State, error) { return current, nil }).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

var itemPattern = regexp.MustCompile(`(?s)<li class="item[^"]*">.*?</li>`)

// items are the board's items in the order it shows them.
func items(body string) []string {
	return itemPattern.FindAllString(body, -1)
}

var headlinePattern = regexp.MustCompile(`(?s)<h1 class="headline[^"]*">(.*?)</h1>`)

func headline(t *testing.T, body string) string {
	t.Helper()
	found := headlinePattern.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("no headline in %s", body)
	}
	return strings.TrimSpace(found[1])
}

func oneItem(t *testing.T, body string) string {
	t.Helper()
	all := items(body)
	if len(all) != 1 {
		t.Fatalf("items = %d, want 1: %v", len(all), all)
	}
	return all[0]
}

func containsAll(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not carry %q: %s", what, want, text)
		}
	}
}

// spec: mission-control.md#items
func TestTheBoardShowsNoItemWhenNothingNeedsAttention(t *testing.T) {
	for name, current := range map[string]state.State{
		"every watched series ok, no anomaly, no node silent": stateOf(
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK)),
		"a laptop asleep inside its silence_after, every series stale": stateOf(
			silenceOf("laptop-a", evaluate.OK), staled(judgedAt(free("laptop-a", "/", 40e9), evaluate.OK))),
		"a node still reporting whose every series is stale": stateOf(
			silenceOf("server-b", evaluate.OK), staled(judgedAt(free("server-b", "/", 40e9), evaluate.OK)),
			staled(freePct("server-b", "/", 30))),
		"an unwatched series whose values stopped arriving": stateOf(
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK),
			staled(freePct("server-b", "/data", 30))),
		"a watched removable volume, unplugged": stateOf(
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK),
			staled(judgedAt(reading("server-b", "disk.free_bytes", map[string]string{"mount": "/backup", "removable": "true"}, 1e9), evaluate.Critical))),
		"a watched series whose stored threshold this build cannot read, fresh and not ranking": stateOf(
			silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK),
			watchedOnly(free("server-b", "/data", 1e9))),
	} {
		t.Run(name, func(t *testing.T) {
			if got := items(showBoard(t, current, "/")); len(got) != 0 {
				t.Fatalf("items = %v, want none", got)
			}
		})
	}
}

// spec: mission-control.md#items — a silent node is one item, and its series none of their
// own.
func TestASilentNodeIsOneItem(t *testing.T) {
	body := showBoard(t, stateOf(
		silenceOf("server-b", evaluate.Critical),
		staled(judgedAt(free("server-b", "/", 1e9), evaluate.Critical)),
	), "/")
	containsAll(t, "the silence item", oneItem(t, body), "server-b", "silent", "2026-08-28 10:05 UTC", `href="/debug"`)
}

// spec: mission-control.md#items — a volume at critical.
func TestALevelItemCarriesTheLevelAndSinceWhen(t *testing.T) {
	body := showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/data", 1e9), evaluate.Critical)), "/")
	containsAll(t, "the level item", oneItem(t, body),
		"server-b", "disk.free_bytes", "/data", "1.0 GB", ">critical<", "2026-08-28 08:05 UTC",
		`href="/history?label.fs=ext4&amp;label.mount=%2Fdata&amp;label.removable=false&amp;metric=disk.free_bytes&amp;node=server-b"`)
}

// spec: mission-control.md#items — a series at critical that also ranks is one item, with
// its usual value.
func TestALevelThatRanksIsOneItem(t *testing.T) {
	body := showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), ranked(judgedAt(free("server-b", "/data", 1e9), evaluate.Critical), 1)), "/")
	containsAll(t, "the level item", oneItem(t, body), ">critical<", "usually 40.0 GB")
}

// spec: mission-control.md#items — a watched series at ok that ranks, and an unwatched one
// named by a label other than a mount.
func TestAnAnomalyItemCarriesTheUsualValue(t *testing.T) {
	body := showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), ranked(judgedAt(free("server-b", "/data", 1e9), evaluate.OK), 1)), "/")
	item := oneItem(t, body)
	containsAll(t, "the anomaly item", item, "server-b", "/data", "1.0 GB", "usually 40.0 GB")
	linksTo(t, item, "/history", "server-b", "disk.free_bytes", "/data")
	linksTo(t, item, "/thresholds", "server-b", "disk.free_bytes", "/data")
	if strings.Contains(item, ">ok<") {
		t.Errorf("the anomaly item carries a level word: %s", item)
	}

	queue := ranked(reading("server-b", "queue.depth", map[string]string{"queue": "payments"}, 900), 1)
	queue.Anomaly.Norm = 12
	containsAll(t, "the anomaly item", oneItem(t, showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), queue), "/")),
		"queue=payments", "usually 12")
}

// spec: mission-control.md#items — a watched series gone stale on a node still reporting
// others, whatever the reason: a failing sensor, or one the configuration switched off.
func TestAWatchedSeriesWithNoFreshData(t *testing.T) {
	body := showBoard(t, stateOf(
		silenceOf("server-b", evaluate.OK),
		judgedAt(free("server-b", "/", 40e9), evaluate.OK),
		staled(judgedAt(free("server-b", "/data", 1e9), evaluate.Warning)),
	), "/")
	item := oneItem(t, body)
	containsAll(t, "the no-fresh-data item", item, "/data", "no fresh data since 2026-08-28 09:05 UTC")
	linksTo(t, item, "/history", "server-b", "disk.free_bytes", "/data")
	linksTo(t, item, "/thresholds", "server-b", "disk.free_bytes", "/data")
	if strings.Contains(item, "warning") {
		t.Errorf("the no-fresh-data item carries its stale level: %s", item)
	}
}

// spec: mission-control.md#items — a node the configuration no longer names.
func TestAForgottenNodeHasNoItem(t *testing.T) {
	current := stateOf(silenceOf("server-b", evaluate.OK), ranked(free("server-b", "/", 1e9), 1),
		staled(judgedAt(free("server-b", "/data", 1e9), evaluate.Critical)))
	current.Nodes[0].Configured = false
	if got := items(showBoard(t, current, "/")); len(got) != 0 {
		t.Fatalf("items = %v, want none", got)
	}
}

func order(t *testing.T, body string, marks ...string) {
	t.Helper()
	all := items(body)
	if len(all) != len(marks) {
		t.Fatalf("items = %d, want %d: %v", len(all), len(marks), all)
	}
	for i, mark := range marks {
		if !strings.Contains(all[i], mark) {
			t.Errorf("item %d = %s, want %q", i+1, all[i], mark)
		}
	}
}

// spec: mission-control.md#order
func TestTheMostUrgentComesFirst(t *testing.T) {
	order(t, showBoard(t, stateOf(
		silenceOf("server-a", evaluate.OK), judgedAt(free("server-a", "/a", 1e9), evaluate.Warning),
		silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/b", 1e9), evaluate.Critical),
		silenceOf("server-c", evaluate.Critical),
	), "/"), "server-b", "server-c", "server-a")
}

// spec: mission-control.md#order — a volume's series together.
func TestAVolumesSeriesSitTogether(t *testing.T) {
	order(t, showBoard(t, stateOf(
		silenceOf("server-b", evaluate.OK),
		judgedAt(free("server-b", "/a", 1e9), evaluate.Critical),
		judgedAt(free("server-b", "/b", 1e9), evaluate.Critical),
		judgedAt(freePct("server-b", "/a", 1), evaluate.Critical),
		judgedAt(freePct("server-b", "/b", 1), evaluate.Critical),
	), "/"), "disk.free_bytes · /a", "disk.free_pct · /a", "disk.free_bytes · /b", "disk.free_pct · /b")
}

func rankedOn(n int, levels map[int]evaluate.Level) state.State {
	subjects := []state.Subject{}
	for i := 1; i <= n; i++ {
		node := "server-" + string(rune('a'+n-i))
		one := ranked(free(node, "/", 1e9), i)
		if level, judged := levels[i]; judged {
			one = judgedAt(one, level)
		}
		subjects = append(subjects, silenceOf(node, evaluate.OK), one)
	}
	return stateOf(subjects...)
}

// spec: mission-control.md#order — anomalies in rank order, five of them, and a line for
// the rest.
func TestAnomaliesComeByRankFiveAtMost(t *testing.T) {
	order(t, showBoard(t, rankedOn(3, nil), "/"), "server-c", "server-b", "server-a")

	body := showBoard(t, rankedOn(5, nil), "/")
	if len(items(body)) != 5 || strings.Contains(body, "more unusual") {
		t.Errorf("five ranking: items = %d, want five and no line", len(items(body)))
	}

	// Ranks 1 to 6 sit on server-f to server-a: the five shown are f to b.
	body = showBoard(t, rankedOn(6, nil), "/")
	order(t, body, "server-f", "server-e", "server-d", "server-c", "server-b")
	containsAll(t, "the board", body, `<a href="/debug">more unusual series: 1</a>`)

	// Ranks 1 and 2, on server-g and server-f, are level items and come first.
	body = showBoard(t, rankedOn(7, map[int]evaluate.Level{1: evaluate.Critical, 2: evaluate.Critical}), "/")
	order(t, body, "server-f", "server-g", "server-e", "server-d", "server-c", "server-b", "server-a")
	if strings.Contains(body, "more unusual") {
		t.Error("seven ranking, two critical: a line after the anomalies")
	}
	for _, level := range items(body)[:2] {
		containsAll(t, "a level item", level, ">critical<")
	}
}

// spec: mission-control.md#order — a warning before an anomaly, an anomaly before a series
// with no fresh data.
func TestLevelsThenAnomaliesThenStaleSeries(t *testing.T) {
	order(t, showBoard(t, stateOf(
		silenceOf("server-a", evaluate.OK),
		judgedAt(free("server-a", "/", 40e9), evaluate.OK),
		staled(judgedAt(free("server-a", "/stale", 1e9), evaluate.OK)),
		silenceOf("server-b", evaluate.OK),
		ranked(free("server-b", "/a", 1e9), 1),
		judgedAt(free("server-b", "/b", 1e9), evaluate.Warning),
	), "/"), "/b", "/a", "/stale")
}

// spec: mission-control.md#headline
func TestTheHeadline(t *testing.T) {
	configuredOut := stateOf(silenceOf("server-b", evaluate.OK), free("server-b", "/", 40e9))
	configuredOut.Nodes[0].Configured, configuredOut.Level = false, nil
	firstReport := stateOf(judgedAt(free("server-b", "/", 40e9), evaluate.OK))
	firstReport.Subjects[0].Level, firstReport.Nodes[0].Level, firstReport.Level = nil, nil, nil
	for _, tc := range []struct {
		name    string
		current state.State
		want    string
		notice  bool
	}{
		{"an empty hub", state.State{At: lastSeen}, "No node has reported yet", false},
		{"one series critical", stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 1e9), evaluate.Critical)), "critical", false},
		{"one series warning", stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 1e9), evaluate.Warning)), "warning", false},
		{"nothing watched, one node silent", stateOf(silenceOf("server-b", evaluate.Critical), free("server-b", "/", 40e9)), "critical", true},
		{"nothing watched, one anomaly", stateOf(silenceOf("server-b", evaluate.OK), ranked(free("server-b", "/", 1e9), 1)), "Nothing is judged yet", true},
		{"nothing watched, no item", stateOf(silenceOf("server-b", evaluate.OK), free("server-b", "/", 40e9)), "Nothing is judged yet", true},
		{"every node configured-out", configuredOut, "Nothing is judged yet", true},
		{"the only node reported for the first time, before the next tick", firstReport, "Nothing is judged yet", false},
		{"everything ok and one anomaly", stateOf(silenceOf("server-b", evaluate.OK), ranked(judgedAt(free("server-b", "/", 1e9), evaluate.OK), 1)), "Nothing past a threshold", false},
		{"everything ok and a series with no fresh data", stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK), staled(judgedAt(free("server-b", "/data", 1e9), evaluate.OK))), "Nothing past a threshold", false},
		{"everything ok, nothing else", stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK)), "All is well", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := showBoard(t, tc.current, "/")
			if got := headline(t, body); got != tc.want {
				t.Errorf("headline = %q, want %q", got, tc.want)
			}
			if got := strings.Contains(body, "Nothing here is being judged yet"); got != tc.notice {
				t.Errorf("nothing-judged notice = %v, want %v", got, tc.notice)
			}
		})
	}
}

// spec: mission-control.md#headline — a level headline is marked as that level.
func TestALevelHeadlineIsMarked(t *testing.T) {
	for _, level := range []evaluate.Level{evaluate.Critical, evaluate.Warning} {
		body := showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 1e9), level)), "/")
		containsAll(t, "the page", body, `class="headline level-`+level.String()+`"`)
	}
}

// spec: mission-control.md#headline — the rows whose headline sits above an item.
func TestTheHeadlineSitsAboveTheItems(t *testing.T) {
	for name, current := range map[string]state.State{
		"nothing watched, one anomaly":    stateOf(silenceOf("server-b", evaluate.OK), ranked(free("server-b", "/", 1e9), 1)),
		"everything ok and one anomaly":   stateOf(silenceOf("server-b", evaluate.OK), ranked(judgedAt(free("server-b", "/", 1e9), evaluate.OK), 1)),
		"everything ok and no fresh data": stateOf(silenceOf("server-b", evaluate.OK), judgedAt(free("server-b", "/", 40e9), evaluate.OK), staled(judgedAt(free("server-b", "/data", 1e9), evaluate.OK))),
	} {
		t.Run(name, func(t *testing.T) {
			if got := items(showBoard(t, current, "/")); len(got) != 1 {
				t.Fatalf("items = %d, want the one below the headline", len(got))
			}
		})
	}
}

// spec: mission-control.md#page — any value and any usual value in the unit its metric id
// declares.
func TestTheBoardFormatsInTheSeriesUnit(t *testing.T) {
	pct := ranked(freePct("server-b", "/", 3.5), 1)
	pct.Anomaly.Norm = 31.5
	containsAll(t, "the anomaly item", oneItem(t, showBoard(t, stateOf(silenceOf("server-b", evaluate.OK), pct), "/")), "3.5%", "usually 31.5%")
}

// linksTo checks that an item carries a link to page naming exactly one series.
func linksTo(t *testing.T, item, page, node, metric, mount string) {
	t.Helper()
	for _, found := range hrefPattern.FindAllStringSubmatch(item, -1) {
		target, err := url.Parse(strings.ReplaceAll(found[1], "&amp;", "&"))
		if err != nil || target.Path != page {
			continue
		}
		q := target.Query()
		if q.Get("node") == node && q.Get("metric") == metric && q.Get("label.mount") == mount {
			return
		}
		t.Errorf("%s link names %v, want %s %s %s", page, q, node, metric, mount)
		return
	}
	t.Errorf("no %s link in %s", page, item)
}

var hrefPattern = regexp.MustCompile(`href="([^"]*)"`)

// spec: mission-control.md#page — the tabs lead to /debug, and every word is in the
// reader's language with the language kept on every link.
func TestTheBoardLinksToEverySeriesInTheReadersLanguage(t *testing.T) {
	current := stateOf(silenceOf("server-b", evaluate.OK), ranked(free("server-b", "/", 1e9), 1))
	if tabs := tabsOf(t, showBoard(t, current, "/board")); len(tabs) != 3 || tabs[2].href != "/debug" || tabs[2].label != "All series" {
		t.Errorf("tabs = %v, want the table's last", tabs)
	}

	body := showBoard(t, current, "/board?lang=ru")
	if tabs := tabsOf(t, body); len(tabs) != 3 || tabs[2].href != "/debug?lang=ru" || tabs[2].label != "Все серии" {
		t.Errorf("Russian tabs = %v, want the table's last", tabs)
	}
	containsAll(t, "the Russian board", body, "обычно 40,0", "Пока ничего не оценивается")
	for _, found := range hrefPattern.FindAllStringSubmatch(body, -1) {
		if !strings.HasPrefix(found[1], "/") {
			continue // the shell's icon, not a link
		}
		if target, _ := url.Parse(strings.ReplaceAll(found[1], "&amp;", "&")); target.Query().Get("lang") != "ru" {
			t.Errorf("a link drops the language: %s", found[1])
		}
	}
	for _, english := range []string{"All series", "usually", "Nothing is judged yet"} {
		if strings.Contains(body, english) {
			t.Errorf("the Russian board still says %q", english)
		}
	}
}

// spec: mission-control.md#page — any time on the page is in the reader's zone.
func TestTheBoardSpeaksTheReadersZone(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "tz", Value: "Europe/Moscow"})
	current := stateOf(silenceOf("server-b", evaluate.Critical))
	hub.Board(func(context.Context) (state.State, error) { return current, nil }).ServeHTTP(rec, req)
	containsAll(t, "the silence item", oneItem(t, rec.Body.String()), "13:05 MSK")
}

// spec: mission-control.md#page — the state cannot be read: the same failure /debug
// answers with.
func TestTheBoardFailsAsTheTableDoes(t *testing.T) {
	failing := func(context.Context) (state.State, error) { return state.State{}, errFailed }
	board, table := httptest.NewRecorder(), httptest.NewRecorder()
	hub.Board(failing).ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
	hub.Debug(failing).ServeHTTP(table, httptest.NewRequest(http.MethodGet, "/debug", nil))
	if board.Code != table.Code || board.Body.String() != table.Body.String() {
		t.Fatalf("board = %d %q, table = %d %q", board.Code, board.Body, table.Code, table.Body)
	}
}

// spec: mission-control.md#page — mission control lives at /board.
func TestMissionControlLivesAtBoard(t *testing.T) {
	rec := getState(t, stored{}, "/board", func() time.Time { return lastSeen })
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<h1 class="headline`) {
		t.Fatalf("GET /board = %d, want mission control: %s", rec.Code, rec.Body)
	}
}
