package hub

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/version"
)

//go:embed templates/*.html
var templates embed.FS

var debugTemplate = template.Must(template.ParseFS(templates, "templates/debug.html", "templates/shell.html"))

// debugView is the page as the template sees it: every string is already translated and every
// number already formatted, so the template holds no logic and no English.
type debugView struct {
	shell
	Version        string
	Empty          string
	NothingWatched string
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
	// Unwatched says how many of this node's series nobody has given a threshold, so a
	// volume left unconfigured is visible rather than quietly unjudged (ADR 0032).
	Unwatched string
	Rows      []rowView
	// Empty says why a node has no rows: nothing measured yet, or nothing current.
	Empty string
}

// levelView is a level as the reader sees it: its word, and the mark that sets it apart.
type levelView struct {
	Word  string
	Class string
}

// rowView is one series (ADR 0033): its metric, the volume its labels name, its newest
// value and what judges it. A nil Level is shown as a dash.
type rowView struct {
	Metric string
	Volume string
	Value  string
	// History addresses the drill-down page of this series, Thresholds the page that sets
	// what it is judged by (docs/specs/history.md#page, docs/specs/thresholds.md).
	History    string
	Thresholds string
	SetLabel   string
	Level      *levelView
	Collected  string
	// Stale is the translated mark of a row that stopped arriving, empty on a fresh one.
	Stale string
	// Unusual marks a series that ranks, beside its usual value, so the ones mission
	// control leaves off past its fifth can be found here (docs/specs/state.md#page).
	Unusual string
}

// Debug renders the state of every node — the debug view of docs/specs/state.md#page —
// leaving out or marking what the state calls stale (docs/specs/history.md#page).
func Debug(read StateReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))

		current, err := read(r.Context())
		if err != nil {
			storageFailure(w, printer, err)
			return
		}

		shellHeaders(w)
		if err := debugTemplate.Execute(w, debugOf(printer, current, language(values))); err != nil {
			slog.Error("render the page", "error", err)
		}
	})
}

func debugOf(printer *i18n.Printer, current state.State, lang string) debugView {
	out := debugView{
		shell:          shellOf(printer, "page.title", "debug", lang),
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
	// A hub that watches nothing must say so: it looks exactly like one where nothing is
	// wrong (docs/specs/state.md#page).
	if current.Watched == 0 {
		out.NothingWatched = printer.T("page.nothing_watched")
	}
	rows, silent := rowsByNode(current), silentNodes(current)
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
		if reported.Unwatched > 0 {
			node.Unwatched = fmt.Sprintf(printer.T("node.unwatched"), reported.Unwatched)
		}
		for _, row := range rows[reported.Node] {
			if unplugged(row.stale, row.labels) {
				continue
			}
			node.Rows = append(node.Rows, rowOf(printer, reported.Node, row, lang))
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

func rowOf(printer *i18n.Printer, node string, row seriesRow, lang string) rowView {
	out := rowView{
		Metric:     row.metric,
		Volume:     volume(printer, row.labels),
		Value:      format(printer, row.metric, row.value),
		History:    historyLink(node, row.metric, row.labels, lang, ""),
		Thresholds: thresholdLink(node, row.metric, row.labels, lang),
		SetLabel:   printer.T("table.set"),
		Level:      levelOf(printer, row.level),
		Collected:  printer.Time(row.ts),
	}
	if row.stale {
		out.Stale = printer.T("value.stale")
	}
	if row.anomaly != nil && row.anomaly.Rank > 0 {
		out.Unusual = fmt.Sprintf(printer.T("value.unusual"), format(printer, row.metric, row.anomaly.Norm))
	}
	return out
}

func levelOf(printer *i18n.Printer, level *evaluate.Level) *levelView {
	if level == nil {
		return nil
	}
	return &levelView{Word: printer.T("level." + level.String()), Class: "level-" + level.String()}
}

// seriesRow is one series before it is translated.
type seriesRow struct {
	metric  string
	labels  map[string]string
	value   float64
	ts      time.Time
	level   *evaluate.Level
	stale   bool
	anomaly *anomaly.Anomaly
}

// rowsByNode is each node's rows, one per series, in the order seriesBefore gives them.
func rowsByNode(current state.State) map[string][]seriesRow {
	rows := map[string][]seriesRow{}
	for _, subject := range current.Subjects {
		if subject.Value == nil {
			continue
		}
		rows[subject.Node] = append(rows[subject.Node], seriesRow{
			metric:  subject.Metric,
			labels:  subject.Labels,
			value:   *subject.Value,
			ts:      subject.TS,
			level:   subject.Level,
			stale:   subject.Stale,
			anomaly: subject.Anomaly,
		})
	}
	for _, list := range rows {
		sort.SliceStable(list, func(i, j int) bool {
			return seriesBefore(list[i].labels, list[i].metric, list[j].labels, list[j].metric)
		})
	}
	return rows
}

// seriesBefore orders the series of one node the way every page lists them: the series of
// one volume together, by metric inside the group. The grouping is the pages' own: the
// state privileges no label (docs/specs/state.md#ordering).
func seriesBefore(aLabels map[string]string, aMetric string, bLabels map[string]string, bMetric string) bool {
	if first, second := storage.LabelKey(aLabels), storage.LabelKey(bLabels); first != second {
		return first < second
	}
	return aMetric < bMetric
}

// silentNodes is which nodes the state holds silent: their silence subject is critical.
func silentNodes(current state.State) map[string]bool {
	silent := map[string]bool{}
	for _, subject := range current.Subjects {
		if subject.Metric == evaluate.SilenceMetric {
			silent[subject.Node] = subject.Level != nil && *subject.Level == evaluate.Critical
		}
	}
	return silent
}

// unplugged is a removable volume that stopped arriving: no page shows it as a reading
// (docs/specs/history.md#page).
func unplugged(stale bool, labels map[string]string) bool {
	return stale && labels["removable"] == "true"
}

// storageFailure answers a page whose state cannot be read: plain text rather than a page,
// but a failure is still live state.
func storageFailure(w http.ResponseWriter, printer *i18n.Printer, err error) {
	slog.Error("read the state", "error", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
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

// format renders a value in the unit its metric id declares, the way every other surface
// renders one (docs/specs/history.md#wire-format).
func format(printer *i18n.Printer, metric string, value float64) string {
	return notify.Value(printer, metric, value)
}

// pageLink addresses a page that takes no query of its own, in the reader's language.
func pageLink(path, lang string) string {
	if lang == "" {
		return path
	}
	return path + "?" + url.Values{"lang": {lang}}.Encode()
}
