package hub

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

var itemTemplate = template.Must(template.ParseFS(templates,
	"templates/item.html", "templates/chart.html", "templates/changes.html", "templates/shell.html"))

// itemChanges is how many of the newest changes an item's page lists.
const itemChanges = 20

// siteChangesRead is how many changes of a site's node and host are read to find the site's
// own among them. shortcut: a site whose node and host logged more than this many changes
// since its newest twenty shows fewer than twenty; filter in storage when one does.
const siteChangesRead = 500

// ItemStore is what an item's page reads beside the state and the points: what each series
// is judged by, and the log of what changed.
type ItemStore interface {
	ThresholdOf(ctx context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error)
	EventLog
}

// itemPageView is an item's page as the template sees it.
type itemPageView struct {
	shell
	Message      string
	Name         string
	Kind         string
	Class        string
	Word         string
	LastReport   string
	Numbers      []cardRowView
	WindowLabel  string
	Windows      []windowLink
	Zone         string
	None         string
	Families     []familyView
	SetLabel     string
	NoData       string
	ChangesLabel string
	NoChanges    string
	Days         []dayView
}

type familyView struct {
	key    string
	Name   string
	Charts []chartView
}

type chartView struct {
	Name       string
	Value      string
	Mark       *levelView
	Usual      string
	Collected  string
	History    string
	Thresholds string
	Chart      *chart
}

// Item renders one item's page: its numbers, a chart of every series over a window, and
// what changed (docs/specs/overview.md#item-page).
func Item(read StateReader, reader history.Reader, store ItemStore, layout Layout, targets func(node string) (evaluate.Target, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))
		lang := language(values)

		node, site, window, refusal := itemQuery(values, layout)
		if refusal != "" {
			refuseItem(w, printer, http.StatusBadRequest, refusal, lang)
			return
		}
		current, err := read(r.Context())
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		it, found := findItem(current, layout, node, site)
		if !found {
			refuseItem(w, printer, http.StatusNotFound, "item.unknown", lang)
			return
		}
		view, err := itemPageOf(r.Context(), printer, reader, store, targets, current, it, window, lang)
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		shellHeaders(w)
		if err := itemTemplate.Execute(w, view); err != nil {
			slog.Error("render an item's page", "error", err)
		}
	})
}

// itemQuery reads the address: a node, a site when the node is the sites node, a window,
// and the language. A refusal is the catalogue key of the reason; a node the configuration
// does not name is left to be not found (docs/specs/overview.md#addresses).
func itemQuery(values url.Values, layout Layout) (node, site string, window time.Duration, refusal string) {
	for name, given := range values {
		switch {
		case name != "node" && name != "lang" && name != "window" && name != history.LabelPrefix+"site":
			return "", "", 0, "history.error.unknown_parameter"
		case len(given) > 1:
			return "", "", 0, "history.error.repeated_parameter"
		case given[0] == "" && name != "lang":
			return "", "", 0, "history.error.empty_parameter"
		}
	}
	node, site, window = values.Get("node"), values.Get(history.LabelPrefix+"site"), history.DefaultWindow
	if node == "" {
		return "", "", 0, "item.error.node"
	}
	if raw := values.Get("window"); raw != "" {
		parsed, err := history.ParseWindow(raw)
		if err != nil {
			return "", "", 0, "history.error.window"
		}
		window = parsed
	}
	if class, known := layout.Class(node); known {
		switch sites := class == config.SitesClass; {
		case sites && site == "":
			return "", "", 0, "item.error.site"
		case !sites && site != "":
			return "", "", 0, "item.error.not_sites"
		}
	}
	return node, site, window, ""
}

func findItem(current state.State, layout Layout, node, site string) (overviewItem, bool) {
	for _, it := range overviewItems(current, layout) {
		if it.node == node && it.site == site {
			return it, true
		}
	}
	return overviewItem{}, false
}

// refuseItem answers an address that names no item, on a page that is not refreshed: one
// left open on an item that is gone stays as it was, under the notice, rather than turn
// into the refusal (docs/specs/overview.md#addresses).
func refuseItem(w http.ResponseWriter, printer *i18n.Printer, status int, key, lang string) {
	page := itemPageView{shell: still(printer, "overview.title", "", lang), Message: printer.T(key)}
	shellHeaders(w)
	w.WriteHeader(status)
	if err := itemTemplate.Execute(w, page); err != nil {
		slog.Error("render an item's refusal", "error", err)
	}
}

func itemPageOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, store ItemStore,
	targets func(node string) (evaluate.Target, bool), current state.State, it overviewItem, window time.Duration, lang string,
) (itemPageView, error) {
	out := itemPageView{
		shell:        shellOf(printer, "overview.title", "", lang),
		Name:         it.name,
		Kind:         printer.T(sectionKeys[it.section].singular),
		Class:        cellNames[it.cell].class,
		Word:         printer.T(cellNames[it.cell].word),
		LastReport:   fmt.Sprintf(printer.T("overview.last_report"), printer.Time(it.lastReport)),
		WindowLabel:  printer.T("history.window"),
		Zone:         printer.Zone(current.At),
		SetLabel:     printer.T("history.set"),
		NoData:       printer.T("history.empty"),
		ChangesLabel: printer.T("timeline.changes"),
		NoChanges:    printer.T("timeline.no_changes"),
	}
	out.Title = it.name
	marked, _ := offeredFor(window)
	for _, offer := range offered {
		out.Windows = append(out.Windows, windowLink{Label: offer, URL: it.link(lang, offer), Current: offer == marked})
	}

	shown := it.shownSeries()
	if len(shown) == 0 {
		out.None = printer.T("overview.no_series")
	} else {
		rows, _ := it.cardSeries(itemsOf(current))
		for _, s := range rows {
			out.Numbers = append(out.Numbers, cardRowView{Name: it.rowName(printer, s), Value: format(printer, s.Metric, *s.Value), Mark: markOf(printer, s)})
		}
	}
	for _, s := range shown {
		view, err := smallChartOf(ctx, printer, reader, store, it, s, window, lang)
		if err != nil {
			return out, err
		}
		if n := len(out.Families); n == 0 || out.Families[n-1].key != familyKey(s.Metric) {
			family, _ := familyOf(s.Metric)
			out.Families = append(out.Families, familyView{key: familyKey(s.Metric), Name: it.names.Family(printer.Locale(), family)})
		}
		last := &out.Families[len(out.Families)-1]
		last.Charts = append(last.Charts, view)
	}

	changes, anomalies, err := changesOf(ctx, store, it)
	if err != nil {
		return out, err
	}
	out.Days = changeDays(printer, current.Names, changes, anomalies, targets, current.At, lang, itemChanges)
	return out, nil
}

// smallChartOf is one series of the item over the window, its stored threshold drawn
// across it when this build can read one.
func smallChartOf(ctx context.Context, printer *i18n.Printer, reader history.Reader, store ItemStore,
	it overviewItem, s state.Subject, window time.Duration, lang string,
) (chartView, error) {
	out := chartView{
		Name:       it.chartName(printer, s),
		Value:      format(printer, s.Metric, *s.Value),
		Mark:       markOf(printer, s),
		Collected:  printer.Time(s.TS),
		History:    historyLink(s.Node, s.Metric, s.Labels, lang, windowName(window)),
		Thresholds: thresholdLink(s.Node, s.Metric, s.Labels, lang),
	}
	if s.Anomaly != nil && s.Anomaly.Rank > 0 {
		out.Usual = fmt.Sprintf(printer.T("attention.usually"), format(printer, s.Metric, s.Anomaly.Norm))
	}

	ref := storage.SeriesRef{Node: s.Node, Metric: s.Metric, Labels: s.Labels}
	th, stored, err := store.ThresholdOf(ctx, ref)
	if err != nil {
		return out, err
	}
	var levels []threshold
	if stored && evaluate.Readable(th) {
		if th.Warning != nil {
			levels = append(levels, threshold{value: *th.Warning, level: evaluate.Warning})
		}
		if th.Critical != nil {
			levels = append(levels, threshold{value: *th.Critical, level: evaluate.Critical})
		}
	}
	out.Chart, err = lineOf(ctx, printer, reader, s, window, smallFrame, levels...)
	return out, err
}

// windowName spells a window the way the history page's links do: as the link offered for
// it, otherwise in whole days, hours or minutes.
func windowName(window time.Duration) string {
	if offer, found := offeredFor(window); found {
		return offer
	}
	switch {
	case window%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", window/(24*time.Hour))
	case window%time.Hour == 0:
		return fmt.Sprintf("%dh", window/time.Hour)
	}
	return fmt.Sprintf("%dm", window/time.Minute)
}

// changesOf is the log an item's page lists: a node's every change; a site's own series'
// changes and the silences of its node and its host.
func changesOf(ctx context.Context, store ItemStore, it overviewItem) ([]storage.Transition, []storage.Anomaly, error) {
	if it.site == "" {
		changes, err := store.RecentEvents(ctx, []string{it.node}, itemChanges)
		if err != nil {
			return nil, nil, err
		}
		anomalies, err := store.RecentAnomalies(ctx, []string{it.node}, itemChanges)
		return changes, anomalies, err
	}

	read, err := store.RecentEvents(ctx, []string{it.node, it.host}, siteChangesRead)
	if err != nil {
		return nil, nil, err
	}
	var changes []storage.Transition
	for _, change := range read {
		if change.Metric == evaluate.SilenceMetric || (change.Node == it.node && change.Labels["site"] == it.site) {
			changes = append(changes, change)
		}
	}
	held, err := store.RecentAnomalies(ctx, []string{it.node}, siteChangesRead)
	if err != nil {
		return nil, nil, err
	}
	var anomalies []storage.Anomaly
	for _, record := range held {
		if record.Labels["site"] == it.site {
			anomalies = append(anomalies, record)
		}
	}
	return changes, anomalies, nil
}
