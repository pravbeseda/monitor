package hub

import (
	"maps"
	"slices"
	"time"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
)

// panelView is one group of an item's family as its page shows it: a panel of the family's
// own fills Lines, the generic one Columns and Rows (docs/specs/overview.md#panels).
type panelView struct {
	Title   string
	Class   string
	Word    string
	Lines   []panelLine
	Columns []string
	Rows    []panelRow
}

type panelLine struct {
	Name    string
	Figures []figureView
}

type panelRow struct {
	Name string
	// Cells follow Columns; a nil cell is a metric the row has no series of.
	Cells []*figureView
}

// figureView is one series of a panel: its newest value, the mark a card's row gives it, and
// the link to its chart.
type figureView struct {
	Text    string
	History string
	Mark    *levelView
}

// panelSeries is a series of a panel with the labels the panel names it by.
type panelSeries struct {
	state.Subject
	named map[string]string
}

// ownPanels are the families the overview skin draws a panel of its own for; any other
// grouped family gets the generic one (ADR 0051).
var ownPanels = map[string]func(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) []panelLine{
	"backup": backupLines,
}

// groupOf is the value of the label a series' family is grouped by, when it is grouped and
// the series carries that label.
func (it overviewItem) groupOf(s state.Subject) (string, bool) {
	label, grouped := it.names.Groups[names.FamilyOf(s.Metric)]
	if !grouped {
		return "", false
	}
	value, carried := s.Labels[label]
	return value, carried
}

// panelsOf draws a family's grouped series, given in the page order, as one panel per value
// of its group label, ordered by that value byte by byte.
func panelsOf(printer *i18n.Printer, it overviewItem, series []state.Subject, window time.Duration, lang string) []panelView {
	if len(series) == 0 {
		return nil
	}
	family := names.FamilyOf(series[0].Metric)
	label := it.names.Groups[family]
	byValue := map[string][]state.Subject{}
	for _, s := range series {
		value, _ := it.groupOf(s)
		byValue[value] = append(byValue[value], s)
	}
	link := func(s state.Subject) string { return historyLink(s.Node, s.Metric, s.Labels, lang, windowName(window)) }

	out := make([]panelView, 0, len(byValue))
	for _, value := range slices.Sorted(maps.Keys(byValue)) {
		group := byValue[value]
		cell := cellOf(group, false)
		panel := panelView{Title: notify.Quoted(value), Class: cellNames[cell].class, Word: printer.T(cellNames[cell].word)}
		named := make([]panelSeries, 0, len(group))
		for _, s := range group {
			labels := maps.Clone(it.ownLabels(s))
			delete(labels, label)
			named = append(named, panelSeries{Subject: s, named: labels})
		}
		if own, found := ownPanels[family]; found {
			panel.Lines = own(printer, it.names, named, link)
		} else {
			panel.Columns, panel.Rows = genericOf(printer, it.names, named, link)
		}
		out = append(out, panel)
	}
	return out
}

// labelSets splits a panel's series, in the page order, by the labels the panel names them
// by, each set in the order its first series comes.
func labelSets(series []panelSeries) [][]panelSeries {
	var out [][]panelSeries
	for _, s := range series {
		at := slices.IndexFunc(out, func(set []panelSeries) bool { return maps.Equal(set[0].named, s.named) })
		if at < 0 {
			out = append(out, nil)
			at = len(out) - 1
		}
		out[at] = append(out[at], s)
	}
	return out
}

// genericOf lays a group out as a table: a row per label set, a column per metric.
func genericOf(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) ([]string, []panelRow) {
	metrics := map[string]bool{}
	for _, s := range series {
		metrics[s.Metric] = true
	}
	ids := slices.Sorted(maps.Keys(metrics))
	columns := make([]string, 0, len(ids))
	for _, metric := range ids {
		columns = append(columns, columnLabel(printer, book, metric))
	}
	var rows []panelRow
	for _, set := range labelSets(series) {
		row := panelRow{Name: notify.Describe(printer, set[0].named), Cells: make([]*figureView, len(ids))}
		for _, s := range set {
			row.Cells[slices.Index(ids, s.Metric)] = &figureView{Text: format(printer, s.Metric, *s.Value), History: link(s.Subject), Mark: markOf(printer, s.Subject)}
		}
		rows = append(rows, row)
	}
	return columns, rows
}
