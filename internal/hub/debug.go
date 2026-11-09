package hub

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
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
	GeneralLabel   string
	ValueLabel     string
	ThresholdLabel string
	TreeLabel      string
	Search         searchView
	Filters        []filterView
	Legend         []markView
	Tree           []treeNode
	Panes          []nodeView
	NothingMatches string
	ResetLabel     string
	ResetURL       string
}

// searchView is the search field; Show and Lang go along with what is typed.
type searchView struct {
	Value, Label, Show, Lang string
}

type filterView struct {
	Label string
	URL   string
	Count int
	On    bool
}

// markView is a mark before a value: its glyph, its colour and the words a screen reader
// says for it.
type markView struct {
	Glyph, Class, Word string
}

// treeNode is one node in the left pane. URL is set when it has no groups to link to.
type treeNode struct {
	Name    string
	URL     string
	Current bool
	Groups  []treeGroup
}

// treeGroup is one group's entry in the left pane.
type treeGroup struct {
	Label   string
	URL     string
	Current bool
	Counts  []markCount
}

// markCount is how many series of a group carry a mark.
type markCount struct {
	Count int
	markView
}

type nodeView struct {
	Name     string
	Version  string
	LastSeen string
	// Level is shown beside the name only when it is worth a glance: warning or critical.
	Level  *levelView
	Silent string
	// Unwatched says how many of this node's series nobody has given a threshold, so a
	// series left unconfigured is visible rather than quietly unjudged (ADR 0032).
	Unwatched string
	// Rows are the series without labels; the labelled ones are gathered in Matrices.
	Rows     []rowView
	Matrices []matrixView
	// Empty says why a node shows no series: nothing measured yet, or nothing current.
	Empty string
}

// levelView is a level as the reader sees it: its word, and the mark that sets it apart.
type levelView struct {
	Word  string
	Class string
}

// rowView is one series (ADR 0033): its metric, its newest value and what marks it.
type rowView struct {
	// Name is what the row says; Metric, its id, is shown on hover.
	Name   string
	Metric string
	Value  string
	// Thresholds addresses the series' settings, which lead on to its chart
	// (docs/specs/history.md#page).
	Thresholds string
	// Marks stand before the value; ValueClass colours it.
	Marks      []markView
	ValueClass string
	// Judged says what the series is judged by, Hover what its marks stand for and its time.
	Judged string
	Hover  string
	// Dim is a series shown only because a neighbour in its matrix row matched a filter.
	Dim bool
}

// The filters of docs/specs/history.md#table, as the address spells them.
const (
	showAll       = "all"
	showProblems  = "problems"
	showUnusual   = "unusual"
	showUnwatched = "unwatched"
)

// generalGroup addresses a node's series without labels. A matrix's key always holds a
// separator, so no family can be spelled like it.
const generalGroup = "general"

// debugQuery is what the address asks of the page; anything it does not understand reads as
// not asked.
type debugQuery struct {
	node, group, show, search, lang string
}

func debugQueryOf(values url.Values) debugQuery {
	query := debugQuery{
		node: values.Get("node"), group: values.Get("group"),
		search: strings.TrimSpace(values.Get("q")), lang: language(values),
	}
	if show := values.Get("show"); slices.Contains([]string{showAll, showProblems, showUnusual, showUnwatched}, show) {
		query.show = show
	}
	return query
}

func (q debugQuery) filtering() bool { return q.show != "" || q.search != "" }

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
		if err := debugTemplate.Execute(w, debugOf(printer, current, debugQueryOf(values))); err != nil {
			slog.Error("render the page", "error", err)
		}
	})
}

// debugNode is one node before the page decides what to draw of it.
type debugNode struct {
	view   nodeView
	shown  []seriesRow
	groups []debugGroup
}

// debugGroup is what one entry of the left pane opens: a node's series without labels, or
// one of its matrices.
type debugGroup struct {
	key, label string
	rows       []rowView
	matrix     *matrixView
	problems   int
	worst      itemKind
	unusual    int
}

func debugOf(printer *i18n.Printer, current state.State, query debugQuery) debugView {
	lang := query.lang
	out := debugView{
		shell:          shellOf(printer, "page.title", "debug", lang),
		Version:        version.Current,
		LastSeenLabel:  printer.T("node.last_seen"),
		GeneralLabel:   printer.T("group.general"),
		ValueLabel:     printer.T("table.free"),
		ThresholdLabel: printer.T("table.threshold"),
		TreeLabel:      printer.T("page.all_series"),
		Search:         searchView{Value: query.search, Label: printer.T("filter.search"), Show: query.show, Lang: lang},
		Legend: []markView{
			warningMark(printer), criticalMark(printer), unusualMark(printer.T("timeline.unusual")), staleMark(printer),
		},
	}
	if len(current.Nodes) == 0 {
		out.Empty = printer.T("page.empty")
	}
	// A hub that watches nothing must say so: it looks exactly like one where nothing is
	// wrong (docs/specs/state.md#page).
	if current.Watched == 0 {
		out.NothingWatched = printer.T("page.nothing_watched")
	}
	rows, silent := rowsByNode(current), silentNodes(current)
	nodes := make([]debugNode, 0, len(current.Nodes))
	var every []seriesRow
	for _, reported := range current.Nodes {
		node := debugNode{view: nodeLineOf(printer, reported, silent[reported.Node])}
		for _, row := range rows[reported.Node] {
			if !unplugged(row.stale, row.labels) {
				node.shown = append(node.shown, row)
			}
		}
		every = append(every, node.shown...)
		switch {
		case len(rows[reported.Node]) == 0:
			node.view.Empty = printer.T("node.no_values")
		case len(node.shown) == 0:
			node.view.Empty = printer.T("node.no_current")
		}
		node.groups = groupsOf(printer, current.Names, reported.Node, node.shown, lang)
		nodes = append(nodes, node)
	}
	out.Filters = filtersOf(printer, query, every)

	nodeAt, groupAt := -1, -1
	if query.filtering() {
		out.Panes = filteredPanes(printer, current.Names, nodes, query, silent)
		if len(out.Panes) == 0 && len(nodes) > 0 {
			out.NothingMatches, out.ResetLabel, out.ResetURL = printer.T("filter.nothing"), printer.T("filter.reset"), debugLink(lang, nil)
		}
	} else {
		nodeAt, groupAt = chosen(nodes, query)
		if nodeAt >= 0 {
			pane := nodes[nodeAt].view
			if groupAt >= 0 {
				group := nodes[nodeAt].groups[groupAt]
				pane.Rows = group.rows
				if group.matrix != nil {
					pane.Matrices = []matrixView{*group.matrix}
				}
			}
			out.Panes = []nodeView{pane}
		}
	}
	for i, node := range nodes {
		entry := treeNode{Name: node.view.Name}
		if len(node.groups) == 0 {
			entry.URL = debugLink(lang, url.Values{"node": {node.view.Name}})
			entry.Current = i == nodeAt
		}
		for j, group := range node.groups {
			entry.Groups = append(entry.Groups, treeGroupOf(printer, node.view.Name, group, lang, i == nodeAt && j == groupAt))
		}
		out.Tree = append(out.Tree, entry)
	}
	return out
}

// nodeLineOf is what stands beside a node's name, before its series are drawn.
func nodeLineOf(printer *i18n.Printer, reported state.Node, silent bool) nodeView {
	node := nodeView{
		Name:     reported.Node,
		Version:  reported.AgentVersion,
		LastSeen: printer.Time(reported.LastSeen),
	}
	if reported.Level != nil && *reported.Level != evaluate.OK {
		node.Level = levelOf(printer, reported.Level)
	}
	if silent {
		node.Silent = printer.T("node.silent")
	}
	if reported.Unwatched > 0 {
		node.Unwatched = fmt.Sprintf(printer.T("node.unwatched"), reported.Unwatched)
	}
	return node
}

// groupsOf is a node's entries in the order of the left pane: "General", then its matrices.
func groupsOf(printer *i18n.Printer, book names.Book, node string, shown []seriesRow, lang string) []debugGroup {
	own, matrices := layOut(printer, book, node, shown, lang)
	var groups []debugGroup
	if len(own) > 0 {
		groups = append(groups, debugGroup{key: generalGroup, label: printer.T("group.general"), rows: own})
	}
	labels := map[string]int{}
	for i := range matrices {
		labels[matrixLabel(matrices[i])]++
	}
	for i := range matrices {
		m := &matrices[i]
		label := matrixLabel(*m)
		if labels[label] > 1 {
			label += " (" + strings.Join(m.keys, ", ") + ")"
		}
		groups = append(groups, debugGroup{key: m.key, label: label, matrix: m})
	}
	at := make(map[string]int, len(groups))
	for i, group := range groups {
		at[group.key] = i
	}
	for _, row := range shown {
		key := generalGroup
		if len(row.labels) > 0 {
			key = matrixKey(familyKey(row.metric), sortedKeys(row.labels))
		}
		at := at[key]
		if row.problem != nil {
			if groups[at].problems == 0 || *row.problem < groups[at].worst {
				groups[at].worst = *row.problem
			}
			groups[at].problems++
		}
		if row.unusual() {
			groups[at].unusual++
		}
	}
	return groups
}

// matrixLabel names a matrix on the left: its family, or its one metric when its id has no
// dot and so no family.
func matrixLabel(m matrixView) string {
	if m.Family != "" {
		return m.Family
	}
	return m.Columns[0].Label
}

// chosen is which node and group the right pane shows when no filter is on, -1 for none
// (docs/specs/history.md#table).
func chosen(nodes []debugNode, query debugQuery) (node, group int) {
	if at := slices.IndexFunc(nodes, func(n debugNode) bool { return n.view.Name == query.node }); at >= 0 {
		groups := nodes[at].groups
		if query.group != "" {
			if found := slices.IndexFunc(groups, func(g debugGroup) bool { return g.key == query.group }); found >= 0 {
				return at, found
			}
		} else {
			if len(groups) == 0 {
				return at, -1
			}
			return at, max(firstTroubled(groups), 0)
		}
	}
	for i, n := range nodes {
		if found := firstTroubled(n.groups); found >= 0 {
			return i, found
		}
	}
	for i, n := range nodes {
		if len(n.groups) > 0 {
			return i, 0
		}
	}
	if len(nodes) > 0 {
		return 0, -1
	}
	return -1, -1
}

func firstTroubled(groups []debugGroup) int {
	return slices.IndexFunc(groups, func(g debugGroup) bool { return g.problems > 0 })
}

func treeGroupOf(printer *i18n.Printer, node string, group debugGroup, lang string, current bool) treeGroup {
	out := treeGroup{
		Label:   group.label,
		URL:     debugLink(lang, url.Values{"node": {node}, "group": {group.key}}),
		Current: current,
	}
	if group.problems > 0 {
		mark := staleMark(printer)
		switch group.worst {
		case itemCritical:
			mark = criticalMark(printer)
		case itemWarning:
			mark = warningMark(printer)
		}
		out.Counts = append(out.Counts, markCount{Count: group.problems, markView: mark})
	}
	if group.unusual > 0 {
		out.Counts = append(out.Counts, markCount{Count: group.unusual, markView: unusualMark(printer.T("timeline.unusual"))})
	}
	return out
}

// filtersOf is the four filters, each counting the whole hub whatever is chosen; a click on
// one keeps the search.
func filtersOf(printer *i18n.Printer, query debugQuery, every []seriesRow) []filterView {
	count := func(keep func(seriesRow) bool) int {
		total := 0
		for _, row := range every {
			if keep(row) {
				total++
			}
		}
		return total
	}
	out := []filterView{
		{Label: printer.T("filter.all"), URL: filterLink(showAll, query), Count: len(every), On: query.show == showAll},
		{Label: printer.T("filter.problems"), URL: filterLink(showProblems, query), Count: count(func(r seriesRow) bool { return r.problem != nil }), On: query.show == showProblems},
		{Label: printer.T("filter.unusual"), URL: filterLink(showUnusual, query), Count: count(seriesRow.unusual), On: query.show == showUnusual},
		{Label: printer.T("filter.unwatched"), URL: filterLink(showUnwatched, query), Count: count(func(r seriesRow) bool { return !r.watched }), On: query.show == showUnwatched},
	}
	return out
}

func filterLink(show string, query debugQuery) string {
	values := url.Values{"show": {show}}
	if query.search != "" {
		values.Set("q", query.search)
	}
	return debugLink(query.lang, values)
}

// debugLink addresses /debug, in the reader's language.
func debugLink(lang string, values url.Values) string {
	if lang != "" {
		if values == nil {
			values = url.Values{}
		}
		values.Set("lang", lang)
	}
	if len(values) == 0 {
		return "/debug"
	}
	return "/debug?" + values.Encode()
}

// filteredPanes is what a filter or a search matches, on every node.
func filteredPanes(printer *i18n.Printer, book names.Book, nodes []debugNode, query debugQuery, silent map[string]bool) []nodeView {
	var panes []nodeView
	for _, node := range nodes {
		pane := node.view
		matched := matching(node.shown, func(row seriesRow) bool {
			return query.matches(printer, book, pane.Name, row)
		})
		pane.Rows, pane.Matrices = layOut(printer, book, pane.Name, matched, query.lang)
		everything := query.show == showAll && query.search == ""
		silentLine := query.show == showProblems && query.search == "" && silent[pane.Name]
		if len(matched) == 0 && !everything && !silentLine {
			continue
		}
		if !everything {
			pane.Empty = ""
		}
		panes = append(panes, pane)
	}
	return panes
}

func (q debugQuery) matches(printer *i18n.Printer, book names.Book, node string, row seriesRow) bool {
	switch q.show {
	case showProblems:
		if row.problem == nil {
			return false
		}
	case showUnusual:
		if !row.unusual() {
			return false
		}
	case showUnwatched:
		if row.watched {
			return false
		}
	}
	if q.search == "" {
		return true
	}
	fields := append([]string{node, notify.Heading(printer, book.Metrics[row.metric], row.metric, row.labels), row.metric},
		slices.Collect(maps.Values(row.labels))...)
	wanted := strings.ToLower(q.search)
	return slices.ContainsFunc(fields, func(field string) bool { return strings.Contains(strings.ToLower(field), wanted) })
}

// matching keeps the series that match, and their neighbours in a matrix row, dimmed: a row
// reads whole or not at all.
func matching(rows []seriesRow, keep func(seriesRow) bool) []seriesRow {
	sets := map[string]bool{}
	setOf := func(row seriesRow) string { return state.SeriesKey("", familyKey(row.metric), row.labels) }
	for _, row := range rows {
		if keep(row) {
			sets[setOf(row)] = true
		}
	}
	var out []seriesRow
	for _, row := range rows {
		matched := keep(row)
		if matched || (len(row.labels) > 0 && sets[setOf(row)]) {
			row.dim = !matched
			out = append(out, row)
		}
	}
	return out
}

func rowOf(printer *i18n.Printer, node string, row seriesRow, lang string) rowView {
	out := rowView{
		Metric:     row.metric,
		Value:      format(printer, row.metric, row.value),
		Thresholds: thresholdLink(node, row.metric, row.labels, lang),
		Judged:     judgedBy(printer, row),
		Dim:        row.dim,
	}
	level := levelOf(printer, row.level)
	hover := []string{printer.T("level.none")}
	if level != nil {
		hover[0] = level.Word
	}
	switch {
	case row.stale:
		// Stale wins, whatever level it stored: a stale level raises nothing else either.
		out.Marks, out.ValueClass = append(out.Marks, staleMark(printer)), "level-stale"
		hover = append(hover, printer.T("value.stale"))
	case level != nil && *row.level == evaluate.Warning:
		out.Marks, out.ValueClass = append(out.Marks, warningMark(printer)), level.Class
	case level != nil && *row.level == evaluate.Critical:
		out.Marks, out.ValueClass = append(out.Marks, criticalMark(printer)), level.Class
	}
	if row.unusual() {
		words := fmt.Sprintf(printer.T("value.unusual"), format(printer, row.metric, row.anomaly.Norm))
		out.Marks = append(out.Marks, unusualMark(words))
		hover = append(hover, words)
	}
	if row.watched {
		hover = append(hover, out.Judged)
	}
	out.Hover = strings.Join(append(hover, printer.Time(row.ts)), " · ")
	return out
}

func warningMark(printer *i18n.Printer) markView {
	return markView{Glyph: "▲", Class: "level-warning", Word: printer.T("level.warning")}
}

func criticalMark(printer *i18n.Printer) markView {
	return markView{Glyph: "■", Class: "level-critical", Word: printer.T("level.critical")}
}

func staleMark(printer *i18n.Printer) markView {
	return markView{Glyph: "◌", Class: "level-stale", Word: printer.T("value.stale")}
}

func unusualMark(words string) markView {
	return markView{Glyph: "◆", Class: "level-unusual", Word: words}
}

// judgedBy says what a series is judged by, in as few characters as a table column allows.
func judgedBy(printer *i18n.Printer, row seriesRow) string {
	switch {
	case !row.watched:
		return printer.T("threshold.every_none")
	case row.threshold == nil:
		return printer.T("threshold.every_unreadable")
	}
	var values []string
	for _, value := range []*float64{row.threshold.Warning, row.threshold.Critical} {
		if value != nil {
			values = append(values, format(printer, row.metric, *value))
		}
	}
	sign := "<"
	if row.threshold.Direction == storage.Above {
		sign = ">"
	}
	return sign + " " + strings.Join(values, " / ")
}

func levelOf(printer *i18n.Printer, level *evaluate.Level) *levelView {
	if level == nil {
		return nil
	}
	return &levelView{Word: printer.T("level." + level.String()), Class: "level-" + level.String()}
}

// seriesRow is one series before it is translated.
type seriesRow struct {
	metric    string
	labels    map[string]string
	value     float64
	ts        time.Time
	level     *evaluate.Level
	stale     bool
	anomaly   *anomaly.Anomaly
	watched   bool
	threshold *storage.Threshold
	// problem is what the timeline would list this series as, nil when it would not, or
	// only as an anomaly (docs/specs/history.md#table).
	problem *itemKind
	dim     bool
}

func (r seriesRow) unusual() bool { return r.anomaly != nil && r.anomaly.Rank > 0 }

// rowsByNode is each node's rows, one per series, in the order seriesBefore gives them.
func rowsByNode(current state.State) map[string][]seriesRow {
	problems := map[string]itemKind{}
	for _, one := range itemsOf(current) {
		if one.kind != itemAnomalous && !one.silence() {
			problems[state.SeriesKey(one.subject.Node, one.subject.Metric, one.subject.Labels)] = one.kind
		}
	}
	rows := map[string][]seriesRow{}
	for _, subject := range current.Subjects {
		if subject.Value == nil {
			continue
		}
		row := seriesRow{
			metric:    subject.Metric,
			labels:    subject.Labels,
			value:     *subject.Value,
			ts:        subject.TS,
			level:     subject.Level,
			stale:     subject.Stale,
			anomaly:   subject.Anomaly,
			watched:   subject.Watched,
			threshold: subject.Threshold,
		}
		if kind, found := problems[state.SeriesKey(subject.Node, subject.Metric, subject.Labels)]; found {
			row.problem = &kind
		}
		rows[subject.Node] = append(rows[subject.Node], row)
	}
	for _, list := range rows {
		sort.SliceStable(list, func(i, j int) bool {
			return seriesBefore(list[i].labels, list[i].metric, list[j].labels, list[j].metric)
		})
	}
	return rows
}

// seriesBefore orders the series of one node the way every page lists them: by what their
// labels name, the series of one label set together, by metric inside the group. The
// grouping is the pages' own: the state privileges no label (docs/specs/state.md#ordering).
func seriesBefore(aLabels map[string]string, aMetric string, bLabels map[string]string, bMetric string) bool {
	if first, second := notify.Naming(aLabels), notify.Naming(bLabels); first != second {
		return first < second
	}
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
