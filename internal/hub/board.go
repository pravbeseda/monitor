package hub

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
)

var boardTemplate = template.Must(template.ParseFS(templates, "templates/board.html", "templates/shell.html"))

// shownAnomalies is how many anomaly items the board shows before a line counts the rest.
const shownAnomalies = 5

// boardView is mission control as the template sees it: every string translated.
type boardView struct {
	shell
	Headline       string
	HeadlineClass  string
	NothingWatched string
	Items          []itemView
	More           string
	AllSeries      string
	DebugURL       string
	SetLabel       string
}

// itemView is one thing that needs attention. Link leads to the series' chart, or to the
// table for a silent node, which has no chart.
type itemView struct {
	Class      string
	Name       string
	Link       string
	Value      string
	Level      *levelView
	Note       string
	Usual      string
	Thresholds string
}

// Board renders mission control: what needs attention now, most urgent first, and nothing
// the state would not return (docs/specs/mission-control.md).
func Board(read StateReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))

		current, err := read(r.Context())
		if err != nil {
			storageFailure(w, printer, err)
			return
		}

		shellHeaders(w)
		if err := boardTemplate.Execute(w, boardOf(printer, current, language(values))); err != nil {
			slog.Error("render the board", "error", err)
		}
	})
}

// itemKind is what an item is about, in the order of how urgent it is.
type itemKind int

const (
	itemCritical itemKind = iota
	itemWarning
	itemAnomalous
	itemNoFreshData
)

// item is one entry before it is translated. A silent node's item carries its silence
// subject.
type item struct {
	kind    itemKind
	subject state.Subject
}

func (i item) silence() bool { return i.subject.Metric == evaluate.SilenceMetric }

func boardOf(printer *i18n.Printer, current state.State, lang string) boardView {
	out := boardView{
		shell:     shellOf(printer, "board.title"),
		AllSeries: printer.T("page.all_series"),
		DebugURL:  pageLink("/debug", lang),
		SetLabel:  printer.T("table.set"),
	}
	found := itemsOf(current)
	anomalies := 0
	for _, one := range found {
		if one.kind == itemAnomalous {
			if anomalies++; anomalies > shownAnomalies {
				continue
			}
		}
		out.Items = append(out.Items, itemOf(printer, one, current, lang))
	}
	if more := anomalies - shownAnomalies; more > 0 {
		out.More = fmt.Sprintf(printer.T("board.more"), more)
	}

	nothingWatched := current.Watched == 0 && len(current.Nodes) > 0
	if nothingWatched {
		out.NothingWatched = printer.T("page.nothing_watched")
	}
	switch {
	case len(current.Nodes) == 0:
		out.Headline = printer.T("page.empty")
	case current.Level != nil && *current.Level > evaluate.OK:
		mark := levelOf(printer, current.Level)
		out.Headline, out.HeadlineClass = mark.Word, mark.Class
	case current.Level == nil || nothingWatched:
		out.Headline = printer.T("board.nothing_judged")
	case len(found) > 0:
		out.Headline = printer.T("board.nothing_past")
	default:
		out.Headline = printer.T("board.all_well")
	}
	return out
}

// itemsOf picks what needs attention out of the state and orders it
// (docs/specs/mission-control.md#model). Only nodes the configuration names take part.
func itemsOf(current state.State) []item {
	configured := map[string]bool{}
	for _, node := range current.Nodes {
		configured[node.Node] = node.Configured
	}
	silent, reporting := silentNodes(current), map[string]bool{}
	for _, s := range current.Subjects {
		if s.Value != nil && !s.Stale {
			reporting[s.Node] = true
		}
	}

	var out []item
	for _, s := range current.Subjects {
		if !configured[s.Node] {
			continue
		}
		switch {
		case s.Metric == evaluate.SilenceMetric:
			if silent[s.Node] {
				out = append(out, item{kind: itemCritical, subject: s})
			}
		case s.Value == nil:
		case !s.Stale && s.Level != nil && *s.Level == evaluate.Critical:
			out = append(out, item{kind: itemCritical, subject: s})
		case !s.Stale && s.Level != nil && *s.Level == evaluate.Warning:
			out = append(out, item{kind: itemWarning, subject: s})
		case s.Anomaly != nil && s.Anomaly.Rank > 0:
			out = append(out, item{kind: itemAnomalous, subject: s})
		// A node whose every series is stale is asleep, not broken, until its silence
		// says otherwise.
		case s.Watched && s.Stale && !silent[s.Node] && reporting[s.Node] && !unplugged(s.Stale, s.Labels):
			out = append(out, item{kind: itemNoFreshData, subject: s})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.kind != b.kind:
			return a.kind < b.kind
		case a.kind == itemAnomalous:
			return a.subject.Anomaly.Rank < b.subject.Anomaly.Rank
		case a.subject.Node != b.subject.Node:
			return a.subject.Node < b.subject.Node
		case a.silence() != b.silence():
			return a.silence()
		}
		return seriesBefore(a.subject.Labels, a.subject.Metric, b.subject.Labels, b.subject.Metric)
	})
	return out
}

func itemOf(printer *i18n.Printer, one item, current state.State, lang string) itemView {
	s := one.subject
	if one.silence() {
		return itemView{
			Class: "item-critical",
			Name:  s.Node,
			Link:  pageLink("/debug", lang),
			Note:  fmt.Sprintf(printer.T("board.silent"), printer.Time(lastSeenOf(current, s.Node))),
		}
	}

	out := itemView{
		Name:  s.Node + " · " + s.Metric,
		Link:  historyLink(s.Node, s.Metric, s.Labels, lang, ""),
		Value: format(printer, s.Metric, *s.Value),
	}
	if named := notify.Naming(s.Labels); named != "" {
		out.Name += " · " + named
	}
	if s.Anomaly != nil && s.Anomaly.Rank > 0 {
		out.Usual = fmt.Sprintf(printer.T("board.usually"), format(printer, s.Metric, s.Anomaly.Norm))
	}
	switch one.kind {
	case itemCritical, itemWarning:
		out.Level = levelOf(printer, s.Level)
		out.Class = "item-" + s.Level.String()
		out.Note = fmt.Sprintf(printer.T("board.since"), printer.Time(s.Since))
	case itemAnomalous:
		out.Class = "item-unusual"
		out.Thresholds = thresholdLink(s.Node, s.Metric, s.Labels, lang)
	case itemNoFreshData:
		out.Class = "item-stale"
		out.Note = fmt.Sprintf(printer.T("board.stale"), printer.Time(s.TS))
		out.Thresholds = thresholdLink(s.Node, s.Metric, s.Labels, lang)
	}
	return out
}

func lastSeenOf(current state.State, node string) time.Time {
	for _, one := range current.Nodes {
		if one.Node == node {
			return one.LastSeen
		}
	}
	return time.Time{}
}
