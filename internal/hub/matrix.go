package hub

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/notify"
)

// matrixView is the labelled series of one node that share a family and their label keys:
// a row per label set, a column per metric (docs/specs/history.md#page).
type matrixView struct {
	// Family heads the corner; it is empty for metric ids without a dot.
	Family  string
	Columns []columnView
	Rows    []matrixRowView
}

type columnView struct {
	Metric string
	Label  string
}

type matrixRowView struct {
	Labels string
	// Cells follow Columns; a nil cell is a metric this label set shows no series of.
	Cells     []*seriesCell
	Collected string
	// Stale marks the row's time when every series it shows is stale, its cells then
	// left unmarked.
	Stale string
}

// seriesCell is one series in a matrix, shown as its row would show it but for the link to
// its thresholds, which its chart carries instead.
type seriesCell struct {
	rowView
	// Hover names the level, or that there is none, and the series' own time.
	Hover string
	// Named is Level when it is worth naming beside the value: warning or critical.
	Named *levelView
}

// layOut splits a node's shown series, in the order seriesBefore gives them, into the rows
// of its own that unlabelled series get and the matrices labelled ones are gathered in.
func layOut(printer *i18n.Printer, node string, series []seriesRow, lang string) ([]rowView, []matrixView) {
	var own []rowView
	gathered := map[string][]seriesRow{}
	for _, row := range series {
		if len(row.labels) == 0 {
			own = append(own, rowOf(printer, node, row, lang))
			continue
		}
		family, dotted := familyOf(row.metric)
		// %q keeps a key holding a comma from reading like two keys.
		key := fmt.Sprintf("%s\x00%t\x00%q", family, dotted, slices.Sorted(maps.Keys(row.labels)))
		gathered[key] = append(gathered[key], row)
	}
	matrices := make([]matrixView, 0, len(gathered))
	for _, key := range slices.Sorted(maps.Keys(gathered)) {
		matrices = append(matrices, matrixOf(printer, node, gathered[key], lang))
	}
	return own, matrices
}

// familyOf is the part of a metric id before its first dot; an id without one is a family
// of its own.
func familyOf(metric string) (family string, dotted bool) {
	family, _, dotted = strings.Cut(metric, ".")
	return family, dotted
}

func matrixOf(printer *i18n.Printer, node string, series []seriesRow, lang string) matrixView {
	var out matrixView
	family, dotted := familyOf(series[0].metric)
	if dotted {
		out.Family = family
	}
	metrics := map[string]bool{}
	for _, row := range series {
		metrics[row.metric] = true
	}
	for _, metric := range slices.Sorted(maps.Keys(metrics)) {
		label := metric
		if dotted {
			label = strings.TrimPrefix(metric, family+".")
		}
		out.Columns = append(out.Columns, columnView{Metric: metric, Label: label})
	}

	// The series come grouped by label set, so a set's series are consecutive.
	for start := 0; start < len(series); {
		end := start + 1
		for end < len(series) && maps.Equal(series[end].labels, series[start].labels) {
			end++
		}
		out.Rows = append(out.Rows, setRowOf(printer, node, series[start:end], out.Columns, lang))
		start = end
	}
	return out
}

// setRowOf is the matrix row of one label set.
func setRowOf(printer *i18n.Printer, node string, set []seriesRow, columns []columnView, lang string) matrixRowView {
	out := matrixRowView{Labels: notify.Describe(printer, set[0].labels), Cells: make([]*seriesCell, len(columns))}
	newest, allStale := set[0], true
	for _, row := range set {
		if row.ts.After(newest.ts) {
			newest = row
		}
		allStale = allStale && row.stale
	}
	out.Collected = printer.Time(newest.ts)
	if allStale {
		out.Stale = printer.T("value.stale")
	}
	for i, column := range columns {
		for _, row := range set {
			if row.metric == column.Metric {
				out.Cells[i] = seriesCellOf(printer, node, row, !allStale, lang)
			}
		}
	}
	return out
}

func seriesCellOf(printer *i18n.Printer, node string, row seriesRow, markStale bool, lang string) *seriesCell {
	out := &seriesCell{rowView: rowOf(printer, node, row, lang)}
	word := printer.T("level.none")
	if out.Level != nil {
		word = out.Level.Word
		if *row.level != evaluate.OK {
			out.Named = out.Level
		}
	}
	out.Hover = word + " · " + out.Collected
	if !markStale {
		out.Stale = ""
	}
	return out
}
