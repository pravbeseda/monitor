package hub

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
)

// panelView is one group of an item's family as its page shows it: a table, laid out by a
// panel of the family's own or by the generic one (docs/specs/overview.md#panels).
type panelView struct {
	Title   string
	Class   string
	Word    string
	Columns []columnView
	Rows    []panelRow
}

type panelRow struct {
	// Name links to a series and takes its mark when the row stands for one.
	Name figureView
	// Cells follow Columns; a nil cell is a metric the row has no series of.
	Cells []*figureView
}

// figureView is one series of a panel: its newest value, the link to its chart, and the
// class and words of the mark a card's row gives it — the words said on hover and to a
// screen reader, not shown. Without a link it is plain text.
type figureView struct {
	Text    string
	History string
	Class   string
	Hint    string
	// Title names the figure's series on hover and to a screen reader, its Hint after.
	Title string
}

// figureOf is a series of a panel shown as text, marked as a card's row would be.
func figureOf(printer *i18n.Printer, s state.Subject, text string, link func(state.Subject) string) figureView {
	figure := figureView{Text: text, History: link(s)}
	if mark := markOf(printer, s); mark != nil {
		figure.Class, figure.Hint = mark.Class, mark.Word
	}
	return figure
}

// panelSeries is a series of a panel with the labels the panel names it by.
type panelSeries struct {
	state.Subject
	named map[string]string
}

// layout lays a group's series out as a panel's columns and rows.
type layout func(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) ([]columnView, []panelRow)

// ownPanels are the families the overview skin draws a panel of its own for; any other
// grouped family gets the generic one (ADR 0051).
var ownPanels = map[string]layout{
	"backup": backupTable,
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
		lay, own := ownPanels[family]
		if !own {
			lay = genericOf
		}
		panel.Columns, panel.Rows = lay(printer, it.names, named, link)
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

// column is one metric of a panel's table and how a value of it reads.
type column struct {
	metric string
	say    func(printer *i18n.Printer, value float64) string
}

// columnsOf are a column per metric of the series but the known ones, in metric id order,
// formatted as /debug formats them.
func columnsOf(series []panelSeries, known map[string]bool) []column {
	metrics := map[string]bool{}
	for _, s := range series {
		if !known[s.Metric] {
			metrics[s.Metric] = true
		}
	}
	out := make([]column, 0, len(metrics))
	for _, metric := range slices.Sorted(maps.Keys(metrics)) {
		out = append(out, column{metric: metric, say: formatting(metric)})
	}
	return out
}

// whole is a count: rounded half away from zero, not grouped.
func whole(value float64) string { return strconv.FormatFloat(math.Round(value), 'f', 0, 64) }

// formatting reads a value of metric as /debug formats it.
func formatting(metric string) func(*i18n.Printer, float64) string {
	return func(printer *i18n.Printer, value float64) string { return format(printer, metric, value) }
}

// tableOf lays label sets out as rows under columns, each row named by name and each column
// headed as a /debug matrix's column.
func tableOf(printer *i18n.Printer, book names.Book, sets [][]panelSeries, columns []column, name func([]panelSeries) figureView, link func(state.Subject) string) ([]columnView, []panelRow) {
	heads := make([]columnView, 0, len(columns))
	for _, c := range columns {
		heads = append(heads, columnView{Metric: c.metric, Label: columnLabel(printer, book, c.metric)})
	}
	rows := make([]panelRow, 0, len(sets))
	for _, set := range sets {
		row := panelRow{Name: name(set), Cells: make([]*figureView, len(columns))}
		for _, s := range set {
			if at := slices.IndexFunc(columns, func(c column) bool { return c.metric == s.Metric }); at >= 0 {
				cell := figureOf(printer, s.Subject, columns[at].say(printer, *s.Value), link)
				row.Cells[at] = &cell
			}
		}
		rows = append(rows, row)
	}
	return heads, rows
}

// genericOf lays a group out as a table: a row per label set, a column per metric.
func genericOf(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) ([]columnView, []panelRow) {
	columns := columnsOf(series, nil)
	return tableOf(printer, book, labelSets(series), columns, func(set []panelSeries) figureView {
		return figureView{Text: notify.Describe(printer, set[0].named)}
	}, link)
}
