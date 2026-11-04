package hub

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/state"
)

// The sites are one table, a row per site, sorted on the server by the column the reader
// clicked (docs/specs/overview.md#site-table, ADR 0057).

// sitesView is the sites table: its headers, then a row per site.
type sitesView struct {
	Headers []headerView
	Rows    []siteRowView
}

// headerView is a column's header: Link sorts by it, empty for a column that does not sort;
// Sort and Arrow say the direction while the rows are sorted by it.
type headerView struct {
	Column string
	Text   string
	Link   string
	Sort   string
	Arrow  string
}

type siteRowView struct {
	Name     string
	Class    string
	Word     string
	Link     string
	Requests lineView
	Day      *chart
	// Rest are the value cells after the line of the day.
	Rest []lineView
	Also []alsoView
	More string
	// Seen is when the site last reported, said once it stopped.
	Seen string
	// values are the cells' series' values by column, nil where the site has none.
	values map[string]*float64
}

// alsoView is what needs attention that no column shows.
type alsoView struct {
	Name  string
	Value valueView
}

// siteRowOf lays a site's row out from its slots: the first the requests, drawn with its
// line of the day, the rest value cells, then what needs attention beyond them.
func (it overviewItem) siteRowOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, store ThresholdReader, attention []item, lang string) (siteRowView, error) {
	out := siteRowView{Name: it.name, Class: cellNames[it.cell].class, Word: printer.T(cellNames[it.cell].word),
		Link: it.link(lang, ""), Seen: it.lastReported(printer), values: map[string]*float64{}}
	plan, err := it.linedPlan(ctx, store, attention, siteSlots)
	if err != nil {
		return out, err
	}
	cells := make([]lineView, len(siteSlots))
	for i, one := range siteSlots {
		line := linePlan{key: one.key}
		if plan.slots != nil {
			line = plan.slots[i]
		}
		cells[i] = it.lineViewOf(printer, line, "")
		if line.own != nil {
			out.values[one.key] = line.own.Value
		}
	}
	out.Requests, out.Rest = cells[0], cells[1:]
	if plan.slots != nil && plan.slots[0].own != nil {
		if out.Day, err = lineOf(ctx, printer, reader, *plan.slots[0].own, history.DefaultWindow, sparkFrame); err != nil {
			return out, err
		}
	}
	attended := it.attended(attention)
	for _, line := range plan.extras {
		// A computer's card falls back to series nothing needs; "Also" lists only what does.
		if !slices.ContainsFunc(attended, func(s state.Subject) bool { return sameSeries(s, *line.own) }) {
			continue
		}
		out.Also = append(out.Also, alsoView{Name: it.rowName(printer, *line.own), Value: valueOf(printer, *line.own, false)})
	}
	if plan.more > 0 {
		out.More = fmt.Sprintf(printer.T("overview.more"), plan.more)
	}
	return out, nil
}

// siteSort is the column the rows are sorted by and whether from the largest down; a
// column the table does not have sorts nothing.
type siteSort struct {
	column     string
	descending bool
}

func siteSortOf(value string) siteSort {
	out := siteSort{column: strings.TrimPrefix(value, "-"), descending: strings.HasPrefix(value, "-")}
	if out.column != "site" && !slices.ContainsFunc(siteSlots, func(s slot) bool { return s.key == out.column }) {
		return siteSort{}
	}
	return out
}

func (s siteSort) String() string {
	if s.descending {
		return "-" + s.column
	}
	return s.column
}

// sortRows sorts the rows by the column: by name for the site, by value for the rest, a row
// with no value last whichever the direction, equal ones by name. With no column the rows
// keep the page order.
func (s siteSort) sortRows(rows []siteRowView) {
	if s.column == "" {
		return
	}
	slices.SortStableFunc(rows, func(a, b siteRowView) int {
		if s.column == "site" {
			return s.direct(byName(a.Name, b.Name))
		}
		first, second := a.values[s.column], b.values[s.column]
		switch {
		case first == nil && second == nil:
			return byName(a.Name, b.Name)
		case first == nil:
			return 1
		case second == nil:
			return -1
		}
		return cmp.Or(s.direct(cmp.Compare(*first, *second)), byName(a.Name, b.Name))
	})
}

func (s siteSort) direct(order int) int {
	if s.descending {
		return -order
	}
	return order
}

// byName orders items by name: a capital as its small letter, names equal but for case by
// their bytes.
func byName(a, b string) int {
	return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
}

// siteColumns are the table's column keys in the order a row lays its cells out, taken from
// siteSlots so the headers cannot drift from the cells beneath them.
func siteColumns() []string {
	out := []string{"site", siteSlots[0].key, "day"}
	for _, one := range siteSlots[1:] {
		out = append(out, one.key)
	}
	return append(out, "also")
}

// sitesHeaders are the table's headers, each that sorts linking to the sort it gives: its
// first click sorts the site by name and a value from the largest down, a click on the
// sorted column reverses it.
func sitesHeaders(printer *i18n.Printer, sorted siteSort, lang string) []headerView {
	var out []headerView
	for _, column := range siteColumns() {
		header := headerView{Column: column, Text: printer.T("overview." + column)}
		if siteSortOf(column).column != "" {
			first := siteSort{column: column, descending: column != "site"}
			next := first
			if sorted.column == column {
				next.descending = !sorted.descending
				header.Sort, header.Arrow = "ascending", "▲"
				if sorted.descending {
					header.Sort, header.Arrow = "descending", "▼"
				}
			}
			header.Link = sortLink(next, lang)
		}
		out = append(out, header)
	}
	return out
}

func sortLink(s siteSort, lang string) string {
	values := url.Values{"sort": {s.String()}}
	if lang != "" {
		values.Set("lang", lang)
	}
	return "/overview?" + values.Encode()
}
