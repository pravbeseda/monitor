package hub

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
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
	CollectedLabel string
	Nodes          []nodeView
}

type nodeView struct {
	Name     string
	Version  string
	LastSeen string
	Rows     []rowView
	// Empty says why a node has no rows: nothing measured yet, or nothing current.
	Empty string
}

// rowView is what one sensor measured about one volume, or one series of a metric no rule
// declares (docs/specs/history.md#page).
type rowView struct {
	Metric    string
	Volume    string
	Values    []valueView
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

// Page renders the latest state of every node, leaving out or marking what evaluation holds
// frozen: targets resolves a node as evaluation reads it (docs/specs/history.md#page).
func Page(store storage.Storage, targets func(node string) (evaluate.Target, bool), now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))

		states, err := store.States(r.Context())
		if err != nil {
			slog.Error("read node states", "error", err)
			// Plain text rather than a page, but a failure is still live state.
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
			return
		}

		shellHeaders(w)
		if err := pageTemplate.Execute(w, index(printer, states, targets, now(), language(values))); err != nil {
			slog.Error("render the page", "error", err)
		}
	})
}

func index(printer *i18n.Printer, states []storage.NodeState, targets func(node string) (evaluate.Target, bool), now time.Time, lang string) view {
	out := view{
		shell:          shellOf(printer, "page.title"),
		Version:        version.Current,
		Empty:          printer.T("page.empty"),
		LastSeenLabel:  printer.T("node.last_seen"),
		MetricLabel:    printer.T("table.metric"),
		VolumeLabel:    printer.T("table.volume"),
		ValueLabel:     printer.T("table.free"),
		CollectedLabel: printer.T("table.collected"),
		Nodes:          make([]nodeView, 0, len(states)),
	}
	for _, state := range states {
		target, configured := targets(state.Node)
		node := nodeView{
			Name:     state.Node,
			Version:  state.AgentVersion,
			LastSeen: printer.Time(state.LastSeen),
		}
		for _, group := range byRow(state.Values) {
			first := group.series[0]
			row := rowView{Metric: group.key.name, Volume: volume(printer, first.Labels)}
			oldest := first.TS
			for _, value := range group.series {
				if value.TS.Before(oldest) {
					oldest = value.TS
				}
				row.Values = append(row.Values, valueView{
					Metric:  value.Metric,
					Value:   format(printer, value.Metric, value.Value),
					History: historyLink(state.Node, value.Metric, value.Labels, lang, ""),
				})
			}
			row.Collected = printer.Time(oldest)
			// Aged by its older series, as evaluation freezes the volume.
			if configured && group.key.declared && target.Frozen(group.key.name, state.LastSeen, oldest, now) {
				if first.Labels["removable"] == "true" {
					continue
				}
				row.Stale = printer.T("value.stale")
			}
			node.Rows = append(node.Rows, row)
		}
		switch {
		case len(state.Values) == 0:
			node.Empty = printer.T("node.no_values")
		case len(node.Rows) == 0:
			node.Empty = printer.T("node.no_current")
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out
}

// rowKey identifies the series one row shows: one sensor's, or one undeclared metric's,
// sharing every label.
type rowKey struct {
	name     string
	declared bool
	labels   string
}

type seriesRow struct {
	key    rowKey
	series []storage.Value
}

// byRow groups values into rows ordered by name, then by labels, whichever of a row's
// series happens to be stored first.
func byRow(values []storage.Value) []seriesRow {
	var rows []seriesRow
	position := map[rowKey]int{}
	for _, value := range values {
		labels, err := storage.Subject{Labels: value.Labels}.Key()
		if err != nil {
			slog.Error("group a series into a row", "metric", value.Metric, "error", err)
			continue
		}
		key := rowKey{name: value.Metric, labels: labels}
		if sensor, declared := evaluate.SensorOf(value.Metric); declared {
			key.name, key.declared = sensor, true
		}
		at, seen := position[key]
		if !seen {
			at = len(rows)
			position[key] = at
			rows = append(rows, seriesRow{key: key})
		}
		rows[at].series = append(rows[at].series, value)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].key, rows[j].key
		if a.name != b.name {
			return a.name < b.name
		}
		return a.labels < b.labels
	})
	return rows
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
