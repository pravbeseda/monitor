package hub_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

// judging configures every node to judge its volumes by the product defaults, with the disk
// sensor every minute and a one-hour silence window.
func judging(t *testing.T) func(node string) (evaluate.Target, bool) {
	t.Helper()
	disk, ok := evaluate.Lookup("disk")
	if !ok {
		t.Fatal("the hub implements no disk rule")
	}
	return func(node string) (evaluate.Target, bool) {
		return evaluate.Target{
			Node:         node,
			SilenceAfter: time.Hour,
			Intervals:    map[string]time.Duration{"disk": time.Minute},
			Rules:        map[string]evaluate.Rule{"disk": disk.Default},
		}, true
	}
}

func showJudged(t *testing.T, store hub.Snapshots, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	hub.Page(hub.ReadState(store, judging(t), func() time.Time { return lastSeen })).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func mounted(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "apfs", "removable": "false"}
}

func pair(labels map[string]string, ts time.Time) []storage.Value {
	return []storage.Value{
		{Metric: "disk.free_bytes", Labels: labels, Value: 40e9, TS: ts},
		{Metric: "disk.free_pct", Labels: labels, Value: 31.25, TS: ts},
	}
}

func level(node, rule string, labels map[string]string, name string) storage.State {
	return storage.State{
		Subject: storage.Subject{Node: node, Rule: rule, Labels: labels},
		Level:   name,
		Since:   lastSeen.Add(-time.Hour),
	}
}

// rowOf is the table row of one volume.
func rowOf(t *testing.T, body, mount string) string {
	t.Helper()
	for _, row := range regexp.MustCompile(`(?s)<tr>.*?</tr>`).FindAllString(body, -1) {
		if strings.Contains(row, "<td>"+mount+" · ") {
			return row
		}
	}
	t.Fatalf("no row for %s in %s", mount, body)
	return ""
}

// headingOf is the heading of one node.
func headingOf(t *testing.T, body, node string) string {
	t.Helper()
	heading := regexp.MustCompile(`(?s)<h2>` + node + `.*?</h2>\s*<p class="meta">.*?</p>`).FindString(body)
	if heading == "" {
		t.Fatalf("no heading for %s in %s", node, body)
	}
	return heading
}

var levelClass = regexp.MustCompile(`class="level-(ok|warning|critical)"`)

// spec: state.md#page — every volume row carries its level, and the three levels are marked
// apart.
func TestPageShowsTheLevelOfEveryVolume(t *testing.T) {
	values := append(pair(mounted("/"), lastSeen), pair(mounted("/data"), lastSeen)...)
	values = append(values, pair(mounted("/scratch"), lastSeen)...)
	body := showJudged(t, stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}},
		levels: []storage.State{
			level("laptop-a", "disk", mounted("/"), "ok"),
			level("laptop-a", "disk", mounted("/data"), "warning"),
			level("laptop-a", "disk", mounted("/scratch"), "critical"),
		},
	}, "/")

	marks := map[string]string{}
	for mount, want := range map[string]string{"/": "ok", "/data": "warning", "/scratch": "critical"} {
		row := rowOf(t, body, mount)
		if !strings.Contains(row, ">"+want+"<") {
			t.Errorf("row of %s = %s, want the word %q", mount, row, want)
		}
		mark := levelClass.FindString(row)
		if mark == "" {
			t.Errorf("row of %s carries no level mark: %s", mount, row)
		}
		marks[mark] = mount
	}
	if len(marks) != 3 {
		t.Errorf("three levels share marks: %v", marks)
	}
}

// spec: state.md#page — a dash where there is no level: a volume not judged yet, and a
// reading.
func TestPageShowsADashWithoutALevel(t *testing.T) {
	values := append(pair(mounted("/"), lastSeen),
		storage.Value{Metric: "disk.free_bytes", Labels: mounted("/half"), Value: 1e9, TS: lastSeen})
	body := showJudged(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/")

	for _, mount := range []string{"/", "/half"} {
		if row := rowOf(t, body, mount); !strings.Contains(row, "<td>—</td>") || levelClass.MatchString(row) {
			t.Errorf("row of %s = %s, want a dash and no level", mount, row)
		}
	}
}

// spec: state.md#page — a node at warning or critical carries that level beside its name;
// one at ok or with none carries nothing.
func TestPageShowsTheLevelOfANode(t *testing.T) {
	body := showJudged(t, stored{
		states: []storage.NodeState{
			{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)},
			{Node: "server-b", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)},
			{Node: "server-c", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)},
			{Node: "server-d", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)},
		},
		levels: []storage.State{
			level("laptop-a", "disk", mounted("/"), "critical"),
			level("server-b", "disk", mounted("/"), "ok"),
			level("server-d", "disk", mounted("/"), "warning"),
		},
	}, "/")

	if heading := headingOf(t, body, "laptop-a"); !strings.Contains(heading, `class="level-critical">critical<`) {
		t.Errorf("heading = %s, want critical beside the name", heading)
	}
	if heading := headingOf(t, body, "server-d"); !strings.Contains(heading, `class="level-warning">warning<`) {
		t.Errorf("heading = %s, want warning beside the name", heading)
	}
	if heading := headingOf(t, body, "laptop-a"); strings.Contains(heading, "silent") {
		t.Errorf("heading = %s: a critical volume marked the node silent", heading)
	}
	for _, node := range []string{"server-b", "server-c"} {
		if heading := headingOf(t, body, node); levelClass.MatchString(heading) {
			t.Errorf("heading = %s, want nothing beside the name", heading)
		}
	}
}

// spec: state.md#page — a node whose silence subject is critical is marked silent.
func TestPageMarksASilentNode(t *testing.T) {
	store := stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)}},
		levels: []storage.State{level("laptop-a", evaluate.SilenceRule, nil, "critical")},
	}
	if heading := headingOf(t, showJudged(t, store, "/"), "laptop-a"); !strings.Contains(heading, "silent") {
		t.Errorf("heading = %s, want the node marked silent", heading)
	}
	if heading := headingOf(t, showJudged(t, store, "/?lang=ru"), "laptop-a"); !strings.Contains(heading, "молчит") || !strings.Contains(heading, "критично") {
		t.Errorf("heading = %s, want the silent mark and the level in Russian", heading)
	}
}

// spec: state.md#page — a stale volume still shows its level beside the "no fresh data"
// mark.
func TestPageShowsTheLevelOfAStaleVolume(t *testing.T) {
	body := showJudged(t, stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen.Add(-time.Hour))}},
		levels: []storage.State{level("laptop-a", "disk", mounted("/"), "warning")},
	}, "/")
	row := rowOf(t, body, "/")
	if !strings.Contains(row, ">warning<") || !strings.Contains(row, "no fresh data") {
		t.Errorf("row = %s, want the level beside the stale mark", row)
	}
}

// spec: state.md#page — every level word in Russian.
func TestPageShowsLevelsInRussian(t *testing.T) {
	body := showJudged(t, stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)}},
		levels: []storage.State{level("laptop-a", "disk", mounted("/"), "warning")},
	}, "/?lang=ru")
	if row := rowOf(t, body, "/"); !strings.Contains(row, ">предупреждение<") {
		t.Errorf("row = %s, want the level in Russian", row)
	}
}
