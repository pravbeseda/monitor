package hub_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// matrix is one matrix of /debug as the reader meets it: the corner, each column's header
// and the metric it names on hover, and each row's cells by metric.
type matrix struct {
	corner  string
	heads   []string
	metrics []string
	rows    []matrixRow
}

type matrixRow struct {
	name string
	// cells holds the whole <td> of each column, keyed by the metric the column names.
	cells     map[string]string
	collected string
}

var (
	matrixTable = regexp.MustCompile(`(?s)<table class="matrix">.*?</table>`)
	headCell    = regexp.MustCompile(`(?s)<th(?: title="([^"]*)")?>(.*?)</th>`)
	bodyRow     = regexp.MustCompile(`(?s)<tr[^>]*>.*?</tr>`)
	dataCell    = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
)

// matrices are the matrices of a page, in the order it shows them.
func matrices(t *testing.T, body string) []matrix {
	t.Helper()
	var out []matrix
	for _, table := range matrixTable.FindAllString(body, -1) {
		head, rest, _ := strings.Cut(table, "</thead>")
		var m matrix
		heads := headCell.FindAllStringSubmatch(head, -1)
		m.corner = strings.TrimSpace(heads[0][2])
		for _, h := range heads[1 : len(heads)-1] {
			m.metrics = append(m.metrics, h[1])
			m.heads = append(m.heads, strings.TrimSpace(h[2]))
		}
		for _, tr := range bodyRow.FindAllString(rest, -1) {
			tds := dataCell.FindAllStringSubmatch(tr, -1)
			if len(tds) != len(m.metrics)+2 {
				t.Fatalf("row %s has %d cells, want %d", tr, len(tds), len(m.metrics)+2)
			}
			row := matrixRow{name: strings.TrimSpace(tds[0][1]), cells: map[string]string{}, collected: tds[len(tds)-1][0]}
			for i, metric := range m.metrics {
				row.cells[metric] = tds[i+1][0]
			}
			m.rows = append(m.rows, row)
		}
		out = append(out, m)
	}
	return out
}

// ownRows are the rows of the series without labels, in the table above the matrices.
func ownRows(t *testing.T, body string) []string {
	t.Helper()
	above, _, _ := strings.Cut(body, `<table class="matrix">`)
	own := rows(above)
	if len(own) == 0 {
		t.Fatalf("no row of its own: %s", body)
	}
	return own
}

// cellOf is the cell of one series of one volume, found by its mount point.
func cellOf(t *testing.T, body, metric, mount string) string {
	t.Helper()
	return matrixRowOf(t, body, mount).cells[metric]
}

// matrixRowOf is the matrix row of one volume, found by its mount point.
func matrixRowOf(t *testing.T, body, mount string) matrixRow {
	t.Helper()
	for _, m := range matrices(t, body) {
		for _, row := range m.rows {
			if row.name == mount || strings.HasPrefix(row.name, mount+" · ") {
				return row
			}
		}
	}
	t.Fatalf("no matrix row for %s in %s", mount, body)
	return matrixRow{}
}

func siteValue(name, metric string, value float64) storage.Value {
	return storage.Value{Metric: metric, Sensor: "access_log", Labels: site(name), Value: value, TS: lastSeen}
}

func sitesNode(values ...storage.Value) stored {
	return stored{states: []storage.NodeState{{Node: "sites", LastSeen: lastSeen, Values: values}}}
}

// spec: history.md#page — the labelled series of one node make a matrix: a row per label
// set, a column per metric, each value linked to its chart; the family in the corner and
// left out of the headers, the whole id on hover; a dash where a label set has no such
// series.
func TestDebugShowsTheSitesAsOneMatrix(t *testing.T) {
	var values []storage.Value
	for _, metric := range []string{"site.server_error_pct", "site.requests_per_min", "site.response_p95_seconds"} {
		values = append(values, siteValue("shop-c", metric, 2), siteValue("blog-a", metric, 1))
	}
	values = values[:len(values)-1] // blog-a has reported no response time

	body := showDebug(t, sitesNode(values...), "/debug")

	got := matrices(t, body)
	if len(got) != 1 {
		t.Fatalf("%d matrices, want one: %s", len(got), body)
	}
	m := got[0]
	if m.corner != "site" {
		t.Errorf("corner = %q, want the family", m.corner)
	}
	if want := []string{"requests_per_min", "response_p95_seconds", "server_error_pct"}; strings.Join(m.heads, ",") != strings.Join(want, ",") {
		t.Errorf("headers = %v, want %v", m.heads, want)
	}
	if want := "site.requests_per_min"; m.metrics[0] != want {
		t.Errorf("first column names %q on hover, want %q", m.metrics[0], want)
	}
	if len(m.rows) != 2 || m.rows[0].name != "site=blog-a" || m.rows[1].name != "site=shop-c" {
		t.Fatalf("rows = %v, want blog-a then shop-c", m.rows)
	}
	if cell := m.rows[0].cells["site.response_p95_seconds"]; !strings.Contains(cell, ">—<") || strings.Contains(cell, "href") {
		t.Errorf("blog-a's response time = %s, want a dash", cell)
	}
	cell := m.rows[1].cells["site.server_error_pct"]
	if !strings.Contains(cell, "/history?") || !strings.Contains(cell, "label.site=shop-c") || !strings.Contains(cell, "metric=site.server_error_pct") {
		t.Errorf("shop-c's server errors = %s, want a link to their chart", cell)
	}
	if strings.Contains(body, "/thresholds?") {
		t.Errorf("a cell links to the thresholds page: %s", body)
	}
}

// spec: history.md#page — series without labels are rows of their own, ordered by metric
// id, in one table above the matrices, each linking to what judges it.
func TestDebugShowsUnlabelledSeriesAsRowsAboveTheMatrices(t *testing.T) {
	plain := func(metric string) storage.Value {
		return storage.Value{Metric: metric, Sensor: "disk", Value: 1, TS: lastSeen}
	}
	values := append(pair(mounted("/"), lastSeen), plain("load.avg_15m"), plain("kern.memorystatus_level"))
	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	own := ownRows(t, body)
	if len(own) != 2 || !strings.Contains(own[0], "<td>kern.memorystatus_level</td>") || !strings.Contains(own[1], "<td>load.avg_15m</td>") {
		t.Fatalf("rows above the matrix = %q, want the two unlabelled series by metric id", own)
	}
	for _, row := range own {
		if !strings.Contains(row, "/thresholds?") {
			t.Errorf("row %s does not link to what judges it", row)
		}
	}
	if got := matrices(t, body); len(got) != 1 || len(got[0].rows) != 1 {
		t.Errorf("matrices = %v, want the volume as one", got)
	}
}

// spec: history.md#page — one matrix per label key set and family, ordered by family, then
// by label keys; an id with no dot is a family of its own, its column headed whole and the
// corner empty.
func TestDebugShowsAMatrixPerFamilyAndLabelKeys(t *testing.T) {
	unit := func(metric, name string) storage.Value {
		return storage.Value{Metric: metric, Sensor: "disk", Labels: map[string]string{"unit": name}, Value: 1, TS: lastSeen}
	}
	values := append(pair(mounted("/"), lastSeen),
		unit("uptime", "a"),
		storage.Value{Metric: "smart.temp_c", Sensor: "disk", Labels: mounted("/"), Value: 40, TS: lastSeen},
		unit("disk.queue", "a"),
	)
	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	got := matrices(t, body)
	var shape []string
	for _, m := range got {
		shape = append(shape, m.corner+":"+strings.Join(m.heads, ","))
	}
	want := []string{"disk:free_bytes,free_pct", "disk:queue", "smart:temp_c", ":uptime"}
	if strings.Join(shape, " ") != strings.Join(want, " ") {
		t.Errorf("matrices = %v, want %v", shape, want)
	}
}

// spec: history.md#page — a matrix row shows the newest of its series' times, each cell its
// own on hover, beside the level or its absence.
func TestDebugDatesAMatrixRowByItsNewestSeries(t *testing.T) {
	values := pair(mounted("/"), lastSeen)
	values[1].TS = lastSeen.Add(-30 * time.Second)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	row := matrixRowOf(t, body, "/")
	if !strings.Contains(row.collected, "2026-08-28 10:05 UTC") {
		t.Errorf("row dated %s, want its newest series' time", row.collected)
	}
	if cell := row.cells["disk.free_pct"]; !strings.Contains(cell, `title="no level · 2026-08-28 10:04 UTC"`) {
		t.Errorf("cell = %s, want its own time and its lack of a level on hover", cell)
	}
}

// spec: history.md#page — a stale series in a matrix has its cell marked; a row whose every
// series is stale has its time marked once instead.
func TestDebugMarksAStaleCellOrAWhollyStaleRow(t *testing.T) {
	values := append(pair(mounted("/"), lastSeen), pair(mounted("/data"), lastSeen.Add(-time.Hour))...)
	values[1].TS = lastSeen.Add(-time.Hour)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	root := matrixRowOf(t, body, "/")
	if !strings.Contains(root.cells["disk.free_pct"], "no fresh data") || strings.Contains(root.cells["disk.free_bytes"]+root.collected, "no fresh data") {
		t.Errorf("row of / = %v, want only its stale cell marked", root)
	}
	data := matrixRowOf(t, body, "/data")
	if !strings.Contains(data.collected, "no fresh data") || strings.Contains(data.cells["disk.free_bytes"]+data.cells["disk.free_pct"], "no fresh data") {
		t.Errorf("row of /data = %v, want its time marked once and its cells unmarked", data)
	}
}

// spec: history.md#page — a label set whose every series is left out has no row.
func TestDebugLeavesOutTheRowOfAnUnpluggedVolume(t *testing.T) {
	stick := map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"}
	values := append(pair(mounted("/"), lastSeen), pair(stick, lastSeen.Add(-time.Hour))...)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	if got := matrices(t, body); len(got) != 1 || len(got[0].rows) != 1 || got[0].rows[0].name != "/ · apfs" {
		t.Errorf("matrices = %v, want / alone", got)
	}
}

// spec: history.md#page — a series left out under a column another label set carries has a
// dash in its cell.
func TestDebugShowsADashForALeftOutSeries(t *testing.T) {
	stick := map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"}
	values := append(pair(mounted("/"), lastSeen), pair(stick, lastSeen)...)
	values[3].TS = lastSeen.Add(-time.Hour)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	row := matrixRowOf(t, body, "/Volumes/stick-a")
	if cell := row.cells["disk.free_pct"]; !strings.Contains(cell, ">—<") || strings.Contains(cell, "href") {
		t.Errorf("cell = %s, want a dash", cell)
	}
}

// spec: history.md#page — a matrix left with no row is not shown.
func TestDebugLeavesOutAMatrixWithNoRow(t *testing.T) {
	stick := map[string]string{"mount": "/Volumes/stick-a", "fs": "apfs", "removable": "true"}
	load := storage.Value{Metric: "load.avg_15m", Sensor: "disk", Value: 1, TS: lastSeen}
	values := append(pair(stick, lastSeen.Add(-time.Hour)), load)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	if strings.Contains(body, `class="matrix"`) || len(ownRows(t, body)) != 1 {
		t.Errorf("page = %s, want the load alone and no matrix", body)
	}
}

// spec: state.md#page — a row of its own carries its level in words, keeps it beside the
// "no fresh data" mark when stale, and is marked unusual beside its usual value.
func TestDebugShowsTheLevelAndMarksOfARowOfItsOwn(t *testing.T) {
	plain := func(metric string, value float64, age time.Duration) storage.Value {
		return storage.Value{Metric: metric, Sensor: "disk", Value: value, TS: lastSeen.Add(-age)}
	}
	body := showDebug(t, stored{
		states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: []storage.Value{
			plain("load.avg_15m", 9, time.Hour),
			plain("memory.available_bytes", 5e9, 0),
		}}},
		thresholds: []storage.Threshold{watched("laptop-a", "load.avg_15m", nil)},
		levels:     []storage.State{level("laptop-a", "load.avg_15m", nil, "critical")},
		points:     map[string][]storage.Point{"memory.available_bytes": usualFree()},
	}, "/debug")

	own := ownRows(t, body)
	if !strings.Contains(own[0], `class="level-critical">critical<`) || !strings.Contains(own[0], "no fresh data") {
		t.Errorf("row = %s, want its level beside the stale mark", own[0])
	}
	if !strings.Contains(own[1], "unusual, usually 40.0 GB") {
		t.Errorf("row = %s, want it marked unusual beside its usual value", own[1])
	}
}

// spec: history.md#page — label sets named alike come by their labels.
func TestDebugOrdersLabelSetsNamedAlikeByTheirLabels(t *testing.T) {
	onZFS := map[string]string{"mount": "/", "fs": "zfs", "removable": "false"}
	onAPFS := map[string]string{"mount": "/", "fs": "apfs", "removable": "false"}
	values := append(pair(onZFS, lastSeen), pair(onAPFS, lastSeen)...)

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	if got := matrices(t, body); len(got) != 1 || len(got[0].rows) != 2 || got[0].rows[0].name != "/ · apfs" {
		t.Errorf("matrices = %v, want / on apfs first", got)
	}
}

// spec: history.md#page — one matrix per label key set, even for keys that would read alike
// once joined.
func TestDebugKeepsLabelKeySetsApartWhateverTheirKeysHold(t *testing.T) {
	values := []storage.Value{
		{Metric: "x.y", Labels: map[string]string{"a,b": "1", "c": "2"}, Value: 1, TS: lastSeen},
		{Metric: "x.y", Labels: map[string]string{"a": "1", "b,c": "2"}, Value: 1, TS: lastSeen},
	}

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	if got := matrices(t, body); len(got) != 2 {
		t.Errorf("matrices = %v, want one per label key set", got)
	}
}

// spec: history.md#page — matrices of one family come by their label keys, compared byte
// by byte (state.md#ordering), whatever bytes the keys hold.
func TestDebugOrdersMatricesByLabelKeysByteByByte(t *testing.T) {
	values := []storage.Value{
		{Metric: "x.y", Labels: map[string]string{" ": "1"}, Value: 1, TS: lastSeen},
		{Metric: "x.y", Labels: map[string]string{"\n": "1"}, Value: 1, TS: lastSeen},
	}

	body := showDebug(t, stored{states: []storage.NodeState{{Node: "laptop-a", LastSeen: lastSeen, Values: values}}}, "/debug")

	newline, space := strings.Index(body, "label.%0A="), strings.Index(body, "label.&#43;=")
	if newline < 0 || space < 0 || newline > space {
		t.Errorf("the newline key at %d, the space key at %d: want the newline's matrix first", newline, space)
	}
}
