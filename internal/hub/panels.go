package hub

import (
	"fmt"
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
var ownPanels = map[string]func(printer *i18n.Printer, it overviewItem, series []panelSeries, link func(state.Subject) string) []panelLine{
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
			panel.Lines = own(printer, it, named, link)
		} else {
			panel.Columns, panel.Rows = genericOf(printer, it, named, link)
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
func genericOf(printer *i18n.Printer, it overviewItem, series []panelSeries, link func(state.Subject) string) ([]string, []panelRow) {
	metrics := map[string]bool{}
	for _, s := range series {
		metrics[s.Metric] = true
	}
	ids := slices.Sorted(maps.Keys(metrics))
	columns := make([]string, 0, len(ids))
	for _, metric := range ids {
		columns = append(columns, columnLabel(printer, it.names, metric))
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

// backupFigures are the backup panel's words for the metrics it knows, in the order a line
// shows them; each takes the value as the panel formats it.
var backupFigures = []struct {
	metric string
	key    string
}{
	{"backup.failed", "panel.backup.run"},
	{"backup.success_age_seconds", "panel.backup.succeeded"},
	{"backup.latest_archive_age_seconds", "panel.backup.newest"},
	{"backup.archives", "panel.backup.archives"},
	{"backup.stored_bytes", "panel.backup.stored"},
	{"backup.latest_archive_original_bytes", "panel.backup.original"},
}

// backupLines are the backup panel: a line per job, by its name, then a line per other
// label set in the page order (docs/specs/overview.md#panels).
func backupLines(printer *i18n.Printer, it overviewItem, series []panelSeries, link func(state.Subject) string) []panelLine {
	sets := labelSets(series)
	isJob := func(set []panelSeries) bool {
		_, has := set[0].named["job"]
		return has && len(set[0].named) == 1
	}
	jobs := slices.DeleteFunc(slices.Clone(sets), func(set []panelSeries) bool { return !isJob(set) })
	slices.SortStableFunc(jobs, func(a, b []panelSeries) int {
		switch first, second := a[0].named["job"], b[0].named["job"]; {
		case first < second:
			return -1
		case first > second:
			return 1
		}
		return 0
	})
	others := slices.DeleteFunc(sets, isJob)

	out := make([]panelLine, 0, len(sets))
	for _, set := range append(jobs, others...) {
		line := panelLine{Name: notify.Describe(printer, set[0].named)}
		if isJob(set) {
			line.Name = notify.Quoted(set[0].named["job"])
		}
		byMetric := map[string]state.Subject{}
		for _, s := range set {
			byMetric[s.Metric] = s.Subject
		}
		for _, known := range backupFigures {
			if s, found := byMetric[known.metric]; found {
				line.Figures = append(line.Figures, figureView{Text: backupFigure(printer, known.metric, known.key, *s.Value), History: link(s), Mark: markOf(printer, s)})
				delete(byMetric, known.metric)
			}
		}
		for _, metric := range slices.Sorted(maps.Keys(byMetric)) {
			s := byMetric[metric]
			text := columnLabel(printer, it.names, metric) + " " + format(printer, metric, *s.Value)
			line.Figures = append(line.Figures, figureView{Text: text, History: link(s), Mark: markOf(printer, s)})
		}
		out = append(out, line)
	}
	return out
}

// backupFigure is one known figure in the backup panel's words.
func backupFigure(printer *i18n.Printer, metric, key string, value float64) string {
	switch {
	case metric == "backup.failed" && value == 0:
		return printer.T("panel.backup.ok")
	case metric == "backup.failed" && value == 1:
		return printer.T("panel.backup.failed")
	case metric == "backup.archives":
		return fmt.Sprintf(printer.T(key), strconv.FormatFloat(math.Round(value), 'f', 0, 64))
	}
	return fmt.Sprintf(printer.T(key), format(printer, metric, value))
}
