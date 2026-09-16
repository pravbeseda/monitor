package hub

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

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
	Locale         i18n.Locale
	Title          string
	Version        string
	Empty          string
	NoValues       string
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
	Values   []valueView
}

type valueView struct {
	Metric    string
	Volume    string
	Value     string
	Collected string
	// History addresses the drill-down page of this series (docs/specs/history.md#page).
	History string
}

// Page renders the latest state of every node.
func Page(store storage.Storage) http.Handler {
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
		if err := pageTemplate.Execute(w, index(printer, states, language(values))); err != nil {
			slog.Error("render the page", "error", err)
		}
	})
}

func index(printer *i18n.Printer, states []storage.NodeState, lang string) view {
	out := view{
		Locale:         printer.Locale(),
		Title:          printer.T("page.title"),
		Version:        version.Current,
		Empty:          printer.T("page.empty"),
		NoValues:       printer.T("node.no_values"),
		LastSeenLabel:  printer.T("node.last_seen"),
		MetricLabel:    printer.T("table.metric"),
		VolumeLabel:    printer.T("table.volume"),
		ValueLabel:     printer.T("table.free"),
		CollectedLabel: printer.T("table.collected"),
		Nodes:          make([]nodeView, 0, len(states)),
	}
	for _, state := range states {
		node := nodeView{
			Name:     state.Node,
			Version:  state.AgentVersion,
			LastSeen: printer.Time(state.LastSeen),
			Values:   make([]valueView, 0, len(state.Values)),
		}
		for _, value := range state.Values {
			node.Values = append(node.Values, valueView{
				Metric:    value.Metric,
				Volume:    volume(printer, value.Labels),
				Value:     format(printer, value.Metric, value.Value),
				Collected: printer.Time(value.TS),
				History:   historyLink(state.Node, value.Metric, value.Labels, lang, ""),
			})
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out
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
