package hub

import (
	"context"
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

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/timeline"
)

var overviewTemplate = template.Must(template.ParseFS(templates, "templates/overview.html", "templates/shell.html"))

const (
	// cardRowsShown is how many rows a card holds; the rest are counted.
	cardRowsShown = 6
	// linedRows is how many of a card's rows carry a line of their last day.
	linedRows = 2
	// fallbackRows is how many series a card shows when nothing needs attention and no
	// default applies (docs/specs/overview.md#model).
	fallbackRows = 2
)

// sparkFrame is a card's line: no axes, the whole box the plot.
var sparkFrame = frame{width: 120, height: 28, top: 2, bottom: 2}

// Layout is what the overview reads of the configuration: a node's class, and the node that
// serves a site the file lists.
type Layout struct {
	Class func(node string) (string, bool)
	Host  func(site string) (string, bool)
}

func layoutOf(cfg *config.Config) Layout {
	return Layout{
		Class: func(node string) (string, bool) {
			entry, known := cfg.Node(node)
			return entry.Class, known
		},
		Host: cfg.HostOf,
	}
}

// section groups items by who collects them, in the order the page shows them.
type section int

const (
	sectionComputers section = iota
	sectionSites
	sectionServices
)

var sectionKeys = [...]struct{ plural, singular string }{
	sectionComputers: {"overview.computers", "overview.computer"},
	sectionSites:     {"overview.sites", "overview.site"},
	sectionServices:  {"overview.services", "overview.service"},
}

// cardOrder is the order of a section's cards by state: the worst first, and no fresh data
// last, since a laptop asleep is the everyday case.
var cardOrder = []timeline.Cell{
	timeline.Silent, timeline.Critical, timeline.Warning, timeline.Unusual,
	timeline.OK, timeline.Reporting, timeline.NoFreshData,
}

// overviewItem is one node, or one site of the sites node, before it is translated.
type overviewItem struct {
	// site and host are set on a site: its label, and the node the file names as serving it.
	name, node, site, host string
	section                section
	// series are the item's series in its page's order; a node's silence is none of them.
	series     []state.Subject
	cell       timeline.Cell
	lastReport time.Time
	// names is every name the sensors declared, the item's metrics among them.
	names names.Book
}

// overviewItems are the items the state holds, by section and then as their cards stand.
func overviewItems(current state.State, layout Layout) []overviewItem {
	silent := silentNodes(current)
	byNode := map[string][]state.Subject{}
	for _, s := range current.Subjects {
		if s.Metric != evaluate.SilenceMetric && s.Value != nil {
			byNode[s.Node] = append(byNode[s.Node], s)
		}
	}

	var out []overviewItem
	for _, node := range current.Nodes {
		class, known := layout.Class(node.Node)
		if !node.Configured || !known {
			continue
		}
		if class == config.SitesClass {
			out = append(out, siteItems(node.Node, byNode[node.Node], silent, layout)...)
			continue
		}
		one := overviewItem{name: node.Node, node: node.Node, series: byNode[node.Node], lastReport: node.LastSeen}
		if class == config.ServiceClass {
			one.section = sectionServices
		}
		one.cell = cellOf(one.series, silent[node.Node])
		out = append(out, one)
	}
	for i := range out {
		out[i].names = current.Names
		sort.SliceStable(out[i].series, func(a, b int) bool { return pageBefore(out[i].series[a], out[i].series[b]) })
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.section != b.section:
			return a.section < b.section
		case a.cell != b.cell:
			return slices.Index(cardOrder, a.cell) < slices.Index(cardOrder, b.cell)
		}
		return a.name < b.name
	})
	return out
}

// siteItems split the sites node into its sites: each the file lists, holding a series.
func siteItems(node string, series []state.Subject, silent map[string]bool, layout Layout) []overviewItem {
	bySite := map[string][]state.Subject{}
	for _, s := range series {
		if name, labelled := s.Labels["site"]; labelled {
			bySite[name] = append(bySite[name], s)
		}
	}
	var out []overviewItem
	for name, own := range bySite {
		host, listed := layout.Host(name)
		if !listed {
			continue
		}
		one := overviewItem{name: name, node: node, site: name, host: host, section: sectionSites, series: own}
		for _, s := range own {
			if s.TS.After(one.lastReport) {
				one.lastReport = s.TS
			}
		}
		one.cell = cellOf(own, silent[node] || silent[host])
		out = append(out, one)
	}
	return out
}

// cellOf is an item's state, in the words of the timeline's lanes: the first that holds.
func cellOf(series []state.Subject, silent bool) timeline.Cell {
	if silent {
		return timeline.Silent
	}
	worst, fresh := timeline.Reporting, false
	for _, s := range series {
		if s.Stale {
			continue
		}
		fresh = true
		if s.Level != nil {
			worst = min(worst, timeline.CellFor(*s.Level))
		}
		if s.Anomaly != nil && s.Anomaly.Rank > 0 {
			worst = min(worst, timeline.Unusual)
		}
	}
	if !fresh {
		return timeline.NoFreshData
	}
	return worst
}

// pageBefore orders an item's series as its page charts them: by family; inside one, the
// series without labels by metric id, then the labelled ones as /debug orders its matrices,
// their rows and their columns (docs/specs/overview.md#model).
func pageBefore(a, b state.Subject) bool {
	if first, second := familyKey(a.Metric), familyKey(b.Metric); first != second {
		return first < second
	}
	if bare, other := len(a.Labels) == 0, len(b.Labels) == 0; bare != other {
		return bare
	}
	if first, second := slices.Sorted(maps.Keys(a.Labels)), slices.Sorted(maps.Keys(b.Labels)); !slices.Equal(first, second) {
		return slices.Compare(first, second) < 0
	}
	return seriesBefore(a.Labels, a.Metric, b.Labels, b.Metric)
}

// shownSeries are the series an item's page charts: /debug's, an unplugged volume left out.
func (it overviewItem) shownSeries() []state.Subject {
	var out []state.Subject
	for _, s := range it.series {
		if !unplugged(s.Stale, s.Labels) {
			out = append(out, s)
		}
	}
	return out
}

// ownLabels are a series' labels as the item names it: a site leaves out its own label.
func (it overviewItem) ownLabels(s state.Subject) map[string]string {
	if it.site == "" {
		return s.Labels
	}
	labels := maps.Clone(s.Labels)
	delete(labels, "site")
	return labels
}

func (it overviewItem) owns(s state.Subject) bool {
	return s.Node == it.node && (it.site == "" || s.Labels["site"] == it.site)
}

// defaultMetrics are the product's choice of what says most about a kind of item, in the
// order a card shows them.
var defaultMetrics = []string{
	"disk.free_pct", "memory.available_pct", "load.avg_15m", "systemd.failed_units",
	"timemachine.backup_age_seconds",
	"site.requests_per_min", "site.server_error_pct", "site.response_p95_seconds",
	"gdrive.free_pct",
}

// cardSeries are the series a card shows rows for, and how many more qualified: what needs
// attention, in the list's order without its cap, then the defaults, then — when neither
// gave one — the first the page charts.
func (it overviewItem) cardSeries(attention []item) ([]state.Subject, int) {
	var out []state.Subject
	add := func(s state.Subject) {
		if !slices.ContainsFunc(out, func(o state.Subject) bool { return o.Metric == s.Metric && maps.Equal(o.Labels, s.Labels) }) {
			out = append(out, s)
		}
	}
	for _, one := range attention {
		if !one.silence() && it.owns(one.subject) {
			add(one.subject)
		}
	}
	for _, metric := range defaultMetrics {
		if chosen, found := it.defaultOf(metric); found {
			add(chosen)
		}
	}
	if len(out) == 0 {
		shown := it.shownSeries()
		out = shown[:min(fallbackRows, len(shown))]
	}
	if len(out) > cardRowsShown {
		return out[:cardRowsShown], len(out) - cardRowsShown
	}
	return out, 0
}

// defaultOf picks the item's series of a default metric: the least free volume that is not
// removable, the oldest backup, a fresh one before a stale one, a tie to the first in the
// page's order.
func (it overviewItem) defaultOf(metric string) (state.Subject, bool) {
	var pool []state.Subject
	for _, s := range it.series {
		if s.Metric == metric && s.Labels["removable"] != "true" {
			pool = append(pool, s)
		}
	}
	if fresh := slices.DeleteFunc(slices.Clone(pool), func(s state.Subject) bool { return s.Stale }); len(fresh) > 0 {
		pool = fresh
	}
	if len(pool) == 0 {
		return state.Subject{}, false
	}
	chosen := pool[0]
	for _, s := range pool[1:] {
		switch {
		case metric == "disk.free_pct" && *s.Value < *chosen.Value,
			metric == "timemachine.backup_age_seconds" && *s.Value > *chosen.Value:
			chosen = s
		}
	}
	return chosen, true
}

// rowName is a series as a row names it: its name without what the card already says
// (docs/specs/metric-names.md#where).
func (it overviewItem) rowName(printer *i18n.Printer, s state.Subject) string {
	return notify.Name(printer, it.names.Metrics[s.Metric], s.Metric, it.ownLabels(s))
}

// chartName is a series as a small chart names it: its row name with the volume's
// filesystem and its being removable.
func (it overviewItem) chartName(printer *i18n.Printer, s state.Subject) string {
	return notify.Heading(printer, it.names.Metrics[s.Metric], s.Metric, it.ownLabels(s))
}

func (it overviewItem) link(lang, window string) string {
	values := url.Values{"node": {it.node}}
	if it.site != "" {
		values.Set(history.LabelPrefix+"site", it.site)
	}
	if lang != "" {
		values.Set("lang", lang)
	}
	if window != "" {
		values.Set("window", window)
	}
	return "/item?" + values.Encode()
}

// overviewView is the overview as the template sees it.
type overviewView struct {
	shell
	Empty    string
	Sections []sectionView
}

type sectionView struct {
	section section
	counts  map[timeline.Cell]int
	Key     string
	Heading string
	Cards   []cardView
}

type cardView struct {
	Name  string
	Class string
	Word  string
	Link  string
	None  string
	// Main are the card's main numbers, side by side; Rest are the rows after them.
	Main []cardRowView
	Rest []cardRowView
	More string
	// Reason closes the card: why it holds its state (docs/specs/overview.md#card).
	Reason string
}

type cardRowView struct {
	Name  string
	Value string
	Mark  *levelView
	Line  *chart
}

// Overview renders the overview skin: a card per item, grouped by who collects it
// (docs/specs/overview.md).
func Overview(read StateReader, reader history.Reader, store ThresholdReader, layout Layout) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))
		lang := language(values)

		current, err := read(r.Context())
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		view, err := overviewOf(r.Context(), printer, reader, store, current, layout, lang)
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		shellHeaders(w)
		if err := overviewTemplate.Execute(w, view); err != nil {
			slog.Error("render the overview", "error", err)
		}
	})
}

func overviewOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, store ThresholdReader, current state.State, layout Layout, lang string) (overviewView, error) {
	out := overviewView{shell: shellOf(printer, "overview.title", "overview", lang)}
	found := overviewItems(current, layout)
	if len(found) == 0 {
		out.Empty = printer.T("page.empty")
		return out, nil
	}
	attention := itemsOf(current)
	for _, it := range found {
		if n := len(out.Sections); n == 0 || out.Sections[n-1].section != it.section {
			out.Sections = append(out.Sections, sectionView{section: it.section, counts: map[timeline.Cell]int{}, Key: sectionKeys[it.section].plural})
		}
		view, err := cardOf(ctx, printer, reader, store, it, attention, lang)
		if err != nil {
			return out, err
		}
		section := &out.Sections[len(out.Sections)-1]
		section.Cards = append(section.Cards, view)
		section.counts[it.cell]++
	}
	for i := range out.Sections {
		out.Sections[i].Heading = headingOf(printer, out.Sections[i])
	}
	return out, nil
}

// headingOf is a section's name, how many items it holds, and how many are in each state
// other than ok and reporting, in the order cards stand.
func headingOf(printer *i18n.Printer, section sectionView) string {
	parts := []string{printer.T(section.Key), fmt.Sprint(len(section.Cards))}
	for _, cell := range cardOrder {
		if count := section.counts[cell]; count > 0 && cell != timeline.OK && cell != timeline.Reporting {
			parts = append(parts, fmt.Sprintf("%d %s", count, printer.T(cellNames[cell].word)))
		}
	}
	return strings.Join(parts, " · ")
}

func cardOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, store ThresholdReader, it overviewItem, attention []item, lang string) (cardView, error) {
	out := cardView{
		Name:  it.name,
		Class: cellNames[it.cell].class,
		Word:  printer.T(cellNames[it.cell].word),
		Link:  it.link(lang, ""),
	}
	if len(it.shownSeries()) == 0 {
		out.None = printer.T("overview.no_series")
		out.Reason = it.lastReported(printer)
		return out, nil
	}
	shown, more := it.cardSeries(attention)
	var first storage.Threshold
	var firstReadable bool
	for i, s := range shown {
		row := cardRowView{Name: it.rowName(printer, s), Value: format(printer, s.Metric, *s.Value), Mark: markOf(printer, s)}
		if i >= linedRows {
			out.Rest = append(out.Rest, row)
			continue
		}
		th, readable, levels, err := levelsOf(ctx, store, s)
		if err != nil {
			return out, err
		}
		if i == 0 {
			first, firstReadable = th, readable
		}
		if row.Line, err = lineOf(ctx, printer, reader, s, history.DefaultWindow, sparkFrame, levels...); err != nil {
			return out, err
		}
		out.Main = append(out.Main, row)
	}
	if more > 0 {
		out.More = fmt.Sprintf(printer.T("overview.more"), more)
	}
	out.Reason = it.reason(printer, shown, first, firstReadable)
	return out, nil
}

// lastReported is when the item last reported, said only once it is silent or holds no
// fresh data.
func (it overviewItem) lastReported(printer *i18n.Printer) string {
	if it.cell != timeline.Silent && it.cell != timeline.NoFreshData {
		return ""
	}
	return fmt.Sprintf(printer.T("overview.last_report"), printer.Time(it.lastReport))
}

// reason is why the card holds its state, said at its foot (docs/specs/overview.md#card).
// Rows needing attention come first, so a critical or warning card's first row holds its
// level; th is that row's stored threshold, and readable whether this build can read it.
func (it overviewItem) reason(printer *i18n.Printer, shown []state.Subject, th storage.Threshold, readable bool) string {
	switch it.cell {
	case timeline.Silent, timeline.NoFreshData:
		return it.lastReported(printer)
	case timeline.Critical, timeline.Warning:
		name := it.rowName(printer, shown[0])
		value := th.Warning
		if it.cell == timeline.Critical {
			value = th.Critical
		}
		if !readable || value == nil {
			return fmt.Sprintf(printer.T("overview.reason_level"), name, printer.T(cellNames[it.cell].word))
		}
		return fmt.Sprintf(printer.T("overview.reason_"+string(th.Direction)), name, format(printer, shown[0].Metric, *value))
	case timeline.Unusual:
		for _, s := range shown {
			if !s.Stale && s.Anomaly != nil && s.Anomaly.Rank > 0 {
				return it.rowName(printer, s) + " " + usualOf(printer, s)
			}
		}
	case timeline.OK:
		// Under ok the only mark a row can carry is that it holds no fresh data.
		for _, s := range shown {
			if s.Stale {
				return fmt.Sprintf(printer.T("overview.reason_stale"), it.rowName(printer, s), printer.Time(s.TS))
			}
		}
		return printer.T("overview.all_within")
	}
	return ""
}

// markOf is the one mark a row carries: a stale series is marked stale whatever level it
// stored, as attention gives it no level.
func markOf(printer *i18n.Printer, s state.Subject) *levelView {
	switch {
	case s.Stale:
		return &levelView{Class: "level-stale", Word: printer.T("value.stale")}
	case s.Level != nil && *s.Level > evaluate.OK:
		return levelOf(printer, s.Level)
	case s.Anomaly != nil && s.Anomaly.Rank > 0:
		return &levelView{Class: "level-unusual", Word: printer.T("timeline.unusual")}
	}
	return nil
}

// lineOf reads one series over a window and draws it, or answers nil when the window holds
// none of its points. Each series is its own read of exactly that series.
func lineOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, s state.Subject, window time.Duration, in frame, levels ...threshold) (*chart, error) {
	series, span, err := reader.ReadOne(ctx, storage.SeriesRef{Node: s.Node, Metric: s.Metric, Labels: s.Labels}, window)
	if err != nil || series == nil {
		return nil, err
	}
	drawn := draw(printer, *series, span, in, levels...)
	return &drawn, nil
}
