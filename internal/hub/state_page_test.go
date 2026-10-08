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

// judging configures every node with the disk sensor every minute and a one-hour silence
// window, which is what the index page ages its rows by.
func judging() func(node string) (evaluate.Target, bool) {
	return configured(time.Minute, time.Hour)
}

func showJudged(t *testing.T, store hub.Snapshots, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	hub.Debug(hub.ReadState(store, judging(), noNorms{}, func() time.Time { return lastSeen })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func mounted(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "apfs", "removable": "false"}
}

// pair is the two series of one volume, both collected at ts by the disk sensor.
func pair(labels map[string]string, ts time.Time) []storage.Value {
	return []storage.Value{
		{Metric: "disk.free_bytes", Sensor: "disk", Labels: labels, Value: 40e9, TS: ts},
		{Metric: "disk.free_pct", Sensor: "disk", Labels: labels, Value: 31.25, TS: ts},
	}
}

// watched is the threshold that makes one series judged, and judged below.
func watched(node, metric string, labels map[string]string) storage.Threshold {
	warning, critical := 20e9, 10e9
	return storage.Threshold{
		Series:    storage.SeriesRef{Node: node, Metric: metric, Labels: labels},
		Direction: storage.Below,
		Warning:   &warning,
		Critical:  &critical,
	}
}

// level is what a tick left behind for one series, under the direction watched stores.
func level(node, metric string, labels map[string]string, name string) storage.State {
	return storage.State{
		Subject:   storage.Subject{Node: node, Metric: metric, Labels: labels},
		Level:     name,
		Direction: string(storage.Below),
		Since:     lastSeen.Add(-time.Hour),
	}
}

// silenceLevel is what a tick left behind for a node's own silence, which is judged under
// no direction at all.
func silenceLevel(node, name string) storage.State {
	return storage.State{
		Subject: storage.Subject{Node: node, Metric: evaluate.SilenceMetric},
		Level:   name,
		Since:   lastSeen.Add(-time.Hour),
	}
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

// spec: state.md#page — a series in a matrix has its value marked by its level, ok too, the
// three marked apart, warning and critical named beside it and any level on hover.
func TestPageShowsTheLevelOfEverySeries(t *testing.T) {
	values := append(pair(mounted("/"), lastSeen), pair(mounted("/data"), lastSeen)...)
	values = append(values, pair(mounted("/scratch"), lastSeen)...)
	body := showJudged(t, stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}},
		thresholds: []storage.Threshold{
			watched("laptop-a", "disk.free_bytes", mounted("/")),
			watched("laptop-a", "disk.free_bytes", mounted("/data")),
			watched("laptop-a", "disk.free_bytes", mounted("/scratch")),
		},
		levels: []storage.State{
			level("laptop-a", "disk.free_bytes", mounted("/"), "ok"),
			level("laptop-a", "disk.free_bytes", mounted("/data"), "warning"),
			level("laptop-a", "disk.free_bytes", mounted("/scratch"), "critical"),
		},
	}, "/")

	marks := map[string]string{}
	for mount, want := range map[string]string{"/": "ok", "/data": "warning", "/scratch": "critical"} {
		cell := cellOf(t, body, "disk.free_bytes", mount)
		if !strings.Contains(cell, `title="`+want+` · `) {
			t.Errorf("cell of %s = %s, want %q on hover", mount, cell, want)
		}
		if named := strings.Contains(cell, ">"+want+"<"); named != (want != "ok") {
			t.Errorf("cell of %s = %s: the word %q beside the value is %v", mount, cell, want, named)
		}
		mark := levelClass.FindString(cell)
		if mark == "" {
			t.Errorf("cell of %s carries no level mark: %s", mount, cell)
		}
		marks[mark] = mount
		// The other series of the same volume is judged separately, and is not judged here.
		if other := cellOf(t, body, "disk.free_pct", mount); levelClass.MatchString(other) {
			t.Errorf("cell of %s = %s, want the unwatched series of the volume to carry no level", mount, other)
		}
	}
	if len(marks) != 3 {
		t.Errorf("three levels share marks: %v", marks)
	}
}

// spec: state.md#page — no level where there is none: a series nothing watches, and one
// watched but not judged yet; a dash in a row of its own, an unmarked value in a matrix.
func TestPageShowsADashWithoutALevel(t *testing.T) {
	load := storage.Value{Metric: "load.avg_15m", Sensor: "disk", Value: 1, TS: lastSeen}
	body := showJudged(t, stored{
		states:     []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: append(pair(mounted("/"), lastSeen), load)}},
		thresholds: []storage.Threshold{watched("laptop-a", "disk.free_bytes", mounted("/")), watched("laptop-a", "load.avg_15m", nil)},
	}, "/")

	for _, metric := range []string{"disk.free_bytes", "disk.free_pct"} {
		if cell := cellOf(t, body, metric, "/"); levelClass.MatchString(cell) || !strings.Contains(cell, `title="no level · `) {
			t.Errorf("cell of %s = %s, want no level marked, and that there is none on hover", metric, cell)
		}
	}
	// The level cell of a row of its own carries the dash and the link that sets what
	// judges the series.
	if own := ownRows(t, body)[0]; !strings.Contains(own, "<td>— <a class=\"set\"") || levelClass.MatchString(own) {
		t.Errorf("row = %s, want a dash and no level", own)
	}
}

// spec: state.md#page — a node at warning or critical carries that level beside its name;
// one at ok or with none carries nothing.
func TestPageShowsTheLevelOfANode(t *testing.T) {
	var states []storage.NodeState
	var thresholds []storage.Threshold
	for _, node := range []string{"laptop-a", "server-b", "server-c", "server-d"} {
		states = append(states, storage.NodeState{Node: node, LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)})
		thresholds = append(thresholds, watched(node, "disk.free_bytes", mounted("/")))
	}
	body := showJudged(t, stored{
		states:     states,
		thresholds: thresholds,
		levels: []storage.State{
			level("laptop-a", "disk.free_bytes", mounted("/"), "critical"),
			level("server-b", "disk.free_bytes", mounted("/"), "ok"),
			level("server-d", "disk.free_bytes", mounted("/"), "warning"),
		},
	}, "/")

	if heading := headingOf(t, body, "laptop-a"); !strings.Contains(heading, `class="level-critical">critical<`) {
		t.Errorf("heading = %s, want critical beside the name", heading)
	}
	if heading := headingOf(t, body, "server-d"); !strings.Contains(heading, `class="level-warning">warning<`) {
		t.Errorf("heading = %s, want warning beside the name", heading)
	}
	if heading := headingOf(t, body, "laptop-a"); strings.Contains(heading, "silent") {
		t.Errorf("heading = %s: a critical series marked the node silent", heading)
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
		levels: []storage.State{silenceLevel("laptop-a", "critical")},
	}
	if heading := headingOf(t, showJudged(t, store, "/"), "laptop-a"); !strings.Contains(heading, "silent") {
		t.Errorf("heading = %s, want the node marked silent", heading)
	}
	heading := headingOf(t, showJudged(t, store, "/?lang=ru"), "laptop-a")
	if !strings.Contains(heading, "молчит") || !strings.Contains(heading, "критично") {
		t.Errorf("heading = %s, want the silent mark and the level in Russian", heading)
	}
}

// spec: state.md#page — a stale series keeps its level, on its value in a matrix whose row's
// time carries the "no fresh data" mark.
func TestPageShowsTheLevelOfAStaleSeries(t *testing.T) {
	body := showJudged(t, stored{
		states:     []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen.Add(-time.Hour))}},
		thresholds: []storage.Threshold{watched("laptop-a", "disk.free_bytes", mounted("/"))},
		levels:     []storage.State{level("laptop-a", "disk.free_bytes", mounted("/"), "warning")},
	}, "/")

	row := matrixRowOf(t, body, "/")
	if cell := row.cells["disk.free_bytes"]; !strings.Contains(cell, `class="level-warning"`) || !strings.Contains(cell, ">warning<") {
		t.Errorf("cell = %s, want its level kept", cell)
	}
	if !strings.Contains(row.collected, "no fresh data") {
		t.Errorf("row dated %s, want it marked", row.collected)
	}
}

// spec: state.md#page — every level word in Russian, on hover too.
func TestPageShowsLevelsInRussian(t *testing.T) {
	body := showJudged(t, stored{
		states:     []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: pair(mounted("/"), lastSeen)}},
		thresholds: []storage.Threshold{watched("laptop-a", "disk.free_bytes", mounted("/"))},
		levels:     []storage.State{level("laptop-a", "disk.free_bytes", mounted("/"), "warning")},
	}, "/?lang=ru")

	if cell := cellOf(t, body, "disk.free_bytes", "/"); !strings.Contains(cell, ">предупреждение<") || !strings.Contains(cell, `title="предупреждение · `) {
		t.Errorf("cell = %s, want the level in Russian", cell)
	}
	if cell := cellOf(t, body, "disk.free_pct", "/"); !strings.Contains(cell, `title="нет уровня · `) {
		t.Errorf("cell = %s, want the lack of a level in Russian", cell)
	}
}
