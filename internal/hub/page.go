package hub

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/version"
)

//go:embed templates/*.html
var templates embed.FS

var pageTemplate = template.Must(template.ParseFS(templates, "templates/index.html", "templates/shell.html"))

// view is the page as the template sees it: every string is already translated and every
// number already formatted, so the template holds no logic and no English.
type view struct {
	shell
	Version        string
	Empty          string
	LastSeenLabel  string
	MetricLabel    string
	VolumeLabel    string
	ValueLabel     string
	LevelLabel     string
	CollectedLabel string
	Nodes          []nodeView
}

type nodeView struct {
	Name     string
	Version  string
	LastSeen string
	// Level is shown beside the name only when it is worth a glance: warning or critical.
	Level  *levelView
	Silent string
	Rows   []rowView
	// Empty says why a node has no rows: nothing measured yet, or nothing current.
	Empty string
}

// levelView is a level as the reader sees it: its word, and the mark that sets it apart.
type levelView struct {
	Word  string
	Class string
}

// rowView is one subject, or the readings of one volume or one metric no rule declares
// (docs/specs/history.md#page). A nil Level is shown as a dash.
type rowView struct {
	Metric    string
	Volume    string
	Values    []valueView
	Level     *levelView
	Collected string
	// Stale is the translated mark of a row that stopped arriving, empty on a fresh one.
	Stale string
}

type valueView struct {
	Metric string
	Value  string
	// History addresses the drill-down page of this series (docs/specs/history.md#page).
	History string
}

// Page renders the state of every node — the debug view of docs/specs/state.md#page —
// leaving out or marking what the state calls stale (docs/specs/history.md#page).
func Page(read StateReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))

		current, err := read(r.Context())
		if err != nil {
			slog.Error("read the state", "error", err)
			// Plain text rather than a page, but a failure is still live state.
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
			return
		}

		shellHeaders(w)
		if err := pageTemplate.Execute(w, index(printer, current, language(values))); err != nil {
			slog.Error("render the page", "error", err)
		}
	})
}

func index(printer *i18n.Printer, current state.State, lang string) view {
	out := view{
		shell:          shellOf(printer, "page.title"),
		Version:        version.Current,
		Empty:          printer.T("page.empty"),
		LastSeenLabel:  printer.T("node.last_seen"),
		MetricLabel:    printer.T("table.metric"),
		VolumeLabel:    printer.T("table.volume"),
		ValueLabel:     printer.T("table.free"),
		LevelLabel:     printer.T("table.level"),
		CollectedLabel: printer.T("table.collected"),
		Nodes:          make([]nodeView, 0, len(current.Nodes)),
	}
	rows, silent := rowsByNode(current)
	for _, reported := range current.Nodes {
		node := nodeView{
			Name:     reported.Node,
			Version:  reported.AgentVersion,
			LastSeen: printer.Time(reported.LastSeen),
		}
		if reported.Level != nil && *reported.Level != evaluate.OK {
			node.Level = levelOf(printer, reported.Level)
		}
		if silent[reported.Node] {
			node.Silent = printer.T("node.silent")
		}
		for _, group := range rows[reported.Node] {
			if group.stale && group.labels["removable"] == "true" {
				continue
			}
			node.Rows = append(node.Rows, rowOf(printer, reported.Node, group, lang))
		}
		switch {
		case len(rows[reported.Node]) == 0:
			node.Empty = printer.T("node.no_values")
		case len(node.Rows) == 0:
			node.Empty = printer.T("node.no_current")
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out
}

func rowOf(printer *i18n.Printer, node string, group seriesRow, lang string) rowView {
	row := rowView{
		Metric: group.key.name,
		Volume: volume(printer, group.labels),
		Level:  levelOf(printer, group.level),
	}
	oldest := group.values[0].TS
	for _, value := range group.values {
		if value.TS.Before(oldest) {
			oldest = value.TS
		}
		row.Values = append(row.Values, valueView{
			Metric:  value.Metric,
			Value:   format(printer, value.Metric, value.Value),
			History: historyLink(node, value.Metric, group.labels, lang, ""),
		})
	}
	// Aged by its older series, as evaluation freezes the volume.
	row.Collected = printer.Time(oldest)
	if group.stale {
		row.Stale = printer.T("value.stale")
	}
	return row
}

func levelOf(printer *i18n.Printer, level *evaluate.Level) *levelView {
	if level == nil {
		return nil
	}
	return &levelView{Word: printer.T("level." + level.String()), Class: "level-" + level.String()}
}

// rowKey identifies the series one row shows: one sensor's, or one undeclared metric's,
// sharing every label.
type rowKey struct {
	name     string
	declared bool
	labels   string
}

// seriesRow is one row before it is translated: a subject, or readings grouped the way a
// subject would group them.
type seriesRow struct {
	key    rowKey
	labels map[string]string
	values []state.Value
	level  *evaluate.Level
	stale  bool
}

// rowsByNode is each node's rows ordered by name, then by labels, each row's series
// ordered by metric — its subjects, then its readings grouped by sensor and labels — and
// which nodes the state holds silent.
func rowsByNode(current state.State) (map[string][]seriesRow, map[string]bool) {
	rows := map[string][]seriesRow{}
	silent := map[string]bool{}
	for _, subject := range current.Subjects {
		if subject.Rule == evaluate.SilenceRule {
			silent[subject.Node] = subject.Level != nil && *subject.Level == evaluate.Critical
			continue
		}
		row := seriesRow{
			key:    keyOf(subject.Rule, subject.Labels),
			labels: subject.Labels,
			values: subject.Values,
			level:  subject.Level,
			stale:  subject.Stale,
		}
		if definition, known := evaluate.Lookup(subject.Rule); known {
			row.key.name, row.key.declared = definition.Sensor, true
		}
		rows[subject.Node] = append(rows[subject.Node], row)
	}
	position := map[string]map[rowKey]int{}
	for _, reading := range current.Readings {
		key := keyOf(reading.Metric, reading.Labels)
		if sensor, declared := evaluate.SensorOf(reading.Metric); declared {
			key.name, key.declared = sensor, true
		}
		if position[reading.Node] == nil {
			position[reading.Node] = map[rowKey]int{}
		}
		at, seen := position[reading.Node][key]
		if !seen {
			at = len(rows[reading.Node])
			position[reading.Node][key] = at
			rows[reading.Node] = append(rows[reading.Node], seriesRow{key: key, labels: reading.Labels})
		}
		group := &rows[reading.Node][at]
		group.values = append(group.values, reading.Value)
		if reading.Stale != nil && *reading.Stale {
			group.stale = true
		}
	}
	for _, list := range rows {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.key.name != b.key.name {
				return a.key.name < b.key.name
			}
			return storage.LabelKey(a.labels) < storage.LabelKey(b.labels)
		})
		for _, row := range list {
			sort.Slice(row.values, func(i, j int) bool { return row.values[i].Metric < row.values[j].Metric })
		}
	}
	return rows, silent
}

func keyOf(name string, labels map[string]string) rowKey {
	encoded, err := storage.Subject{Labels: labels}.Key()
	if err != nil {
		slog.Error("group a series into a row", "name", name, "error", err)
	}
	return rowKey{name: name, labels: encoded}
}

// volume names the thing a series is about, from the labels the sensor set.
func volume(printer *i18n.Printer, labels map[string]string) string {
	parts := make([]string, 0, 3)
	if mount := labels["mount"]; mount != "" {
		parts = append(parts, mount)
	}
	if fs := labels["fs"]; fs != "" {
		parts = append(parts, fs)
	}
	if labels["removable"] == "true" {
		parts = append(parts, printer.T("label.removable"))
	}
	return strings.Join(parts, " · ")
}

// format renders a value in the unit its metric id declares.
func format(printer *i18n.Printer, metric string, value float64) string {
	switch history.UnitOf(metric) {
	case history.Bytes:
		return printer.Bytes(value)
	case history.Percent:
		return printer.Percent(value)
	default:
		return printer.Number(value)
	}
}
