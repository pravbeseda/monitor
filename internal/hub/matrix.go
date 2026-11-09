package hub

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
)

// matrixView is the labelled series of one node that share a family and their label keys:
// a row per label set, a column per metric (docs/specs/history.md#page).
type matrixView struct {
	// Family heads the corner; it is empty for metric ids without a dot.
	Family  string
	Columns []columnView
	Rows    []matrixRowView
	// key addresses the matrix in a link; keys are its label keys.
	key  string
	keys []string
}

type columnView struct {
	Metric string
	Label  string
}

type matrixRowView struct {
	// Labels names the row by its values; Hover by the names of its labels.
	Labels string
	Hover  string
	// Cells follow Columns; a nil cell is a metric this label set shows no series of.
	Cells []*rowView
}

// layOut splits a node's shown series, in the order seriesBefore gives them, into the rows
// of its own that unlabelled series get and the matrices labelled ones are gathered in.
func layOut(printer *i18n.Printer, book names.Book, node string, series []seriesRow, lang string) ([]rowView, []matrixView) {
	var own []rowView
	var groups []*matrixGroup
	for _, row := range series {
		if len(row.labels) == 0 {
			view := rowOf(printer, node, row, lang)
			view.Name = notify.Name(printer, book.Metrics[row.metric], row.metric, row.labels)
			own = append(own, view)
			continue
		}
		family := familyKey(row.metric)
		keys := sortedKeys(row.labels)
		at := slices.IndexFunc(groups, func(g *matrixGroup) bool { return g.family == family && slices.Equal(g.keys, keys) })
		if at < 0 {
			at = len(groups)
			groups = append(groups, &matrixGroup{family: family, keys: keys})
		}
		groups[at].series = append(groups[at].series, row)
	}
	// Byte by byte, as the state compares labels (docs/specs/state.md#ordering).
	slices.SortFunc(groups, func(a, b *matrixGroup) int {
		return cmp.Or(strings.Compare(a.family, b.family), slices.Compare(a.keys, b.keys))
	})
	matrices := make([]matrixView, 0, len(groups))
	for _, group := range groups {
		matrix := matrixOf(printer, book, node, group.series, lang)
		matrix.key, matrix.keys = matrixKey(group.family, group.keys), group.keys
		matrices = append(matrices, matrix)
	}
	return own, matrices
}

func sortedKeys(labels map[string]string) []string { return slices.Sorted(maps.Keys(labels)) }

// matrixKey addresses a matrix: its family, its dot kept, and its label keys.
func matrixKey(family string, keys []string) string {
	return family + "|" + strings.Join(keys, "\x00")
}

// matrixGroup is the series one matrix gathers: one family — its dot kept, so that an id
// without one stays a family of its own — and one label key set.
type matrixGroup struct {
	family string
	keys   []string
	series []seriesRow
}

// familyOf is the part of a metric id before its first dot; an id without one is a family
// of its own.
func familyOf(metric string) (family string, dotted bool) {
	family, _, dotted = strings.Cut(metric, ".")
	return family, dotted
}

// familyKey keeps the dot of a family, so that an id without one stays a family of its own.
func familyKey(metric string) string {
	family, dotted := familyOf(metric)
	if dotted {
		family += "."
	}
	return family
}

func matrixOf(printer *i18n.Printer, book names.Book, node string, series []seriesRow, lang string) matrixView {
	var out matrixView
	family, dotted := familyOf(series[0].metric)
	if dotted {
		out.Family = book.Family(printer.Locale(), family)
	}
	metrics := map[string]bool{}
	for _, row := range series {
		metrics[row.metric] = true
	}
	for _, metric := range slices.Sorted(maps.Keys(metrics)) {
		out.Columns = append(out.Columns, columnView{Metric: metric, Label: columnLabel(printer, book, metric)})
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

// columnLabel heads a column of one metric: its name with the labels it refers to left out,
// since the labels are the rows' to say; its id less its family when that leaves nothing.
func columnLabel(printer *i18n.Printer, book names.Book, metric string) string {
	if template, named := book.Metrics[metric].In(printer.Locale()); named && names.Bare(template) != "" {
		return names.Bare(template)
	}
	if family, dotted := familyOf(metric); dotted {
		return strings.TrimPrefix(metric, family+".")
	}
	return metric
}

// setRowOf is the matrix row of one label set.
func setRowOf(printer *i18n.Printer, node string, set []seriesRow, columns []columnView, lang string) matrixRowView {
	out := matrixRowView{
		Labels: rowName(printer, set[0].labels),
		Hover:  notify.Describe(printer, set[0].labels),
		Cells:  make([]*rowView, len(columns)),
	}
	for i, column := range columns {
		for _, row := range set {
			if row.metric == column.Metric {
				cell := rowOf(printer, node, row, lang)
				out.Cells[i] = &cell
			}
		}
	}
	return out
}

// rowName names a matrix row. A volume reads as it does everywhere; anything else by its
// values alone, the matrix being all one key set, quoted where a value would not read back.
func rowName(printer *i18n.Printer, labels map[string]string) string {
	if _, volume := labels["mount"]; volume {
		return notify.Describe(printer, labels)
	}
	parts := make([]string, 0, len(labels))
	for _, key := range sortedKeys(labels) {
		value := labels[key]
		if value == "" || strings.Contains(value, ",") {
			value = strconv.Quote(value)
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ", ")
}
