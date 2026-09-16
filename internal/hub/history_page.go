package hub

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"

	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
)

var historyTemplate = template.Must(template.ParseFS(templates, "templates/history.html", "templates/shell.html"))

// historyView is the drill-down page as the template sees it: every string translated,
// every number formatted, no logic left.
type historyView struct {
	Locale      i18n.Locale
	Title       string
	Index       string
	Heading     string
	LatestLabel string
	Latest      string
	WindowLabel string
	Windows     []windowLink
	Chart       *chart
	Message     string
	Series      []seriesLink
}

type windowLink struct {
	Label   string
	URL     string
	Current bool
}

type seriesLink struct {
	Label string
	URL   string
}

// offered are the windows the page switches between; the first is the one a query naming
// none gets, so it is the one marked current then. They are links, so a window survives a
// reload and a shared address.
var offered = []string{"24h", "7d", "30d"}

// HistoryPage draws one series, or says why it cannot.
func HistoryPage(reader history.Reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))

		query, err := history.ParseQuery(values, "lang")
		if err != nil {
			refuse(w, printer, err)
			return
		}
		result, err := reader.Read(r.Context(), query)
		if err != nil {
			refuse(w, printer, err)
			return
		}
		render(w, http.StatusOK, historyPage(printer, query, result, language(values), values.Get("window")))
	})
}

func historyPage(printer *i18n.Printer, query history.Query, result history.Result, lang, window string) historyView {
	out := historyView{
		Locale:      printer.Locale(),
		Title:       printer.T("history.title"),
		Index:       printer.T("history.index"),
		LatestLabel: printer.T("history.latest"),
		WindowLabel: printer.T("history.window"),
	}
	for _, offer := range offered {
		// The mark follows the window the page is showing, not the string the query
		// spelled it with: 1440m and 24h are one window.
		shown, err := history.ParseQuery(url.Values{"metric": {query.Metric}, "window": {offer}})
		out.Windows = append(out.Windows, windowLink{
			Label:   offer,
			URL:     historyLink(query.Node, query.Metric, query.Labels, lang, offer),
			Current: err == nil && shown.Window == query.Window,
		})
	}

	switch len(result.Series) {
	case 0:
		out.Message = printer.T("history.empty")
	case 1:
		series := result.Series[0]
		newest := series.Points[len(series.Points)-1]
		drawn := draw(printer, series, result.Window)
		out.Heading = series.Node + " · " + series.Metric + " · " + volume(printer, series.Labels)
		out.Latest = format(printer, series.Metric, newest.Value) + " · " + printer.Time(newest.TS)
		out.Chart = &drawn
	default:
		out.Message = printer.T("history.several")
		for _, series := range result.Series {
			out.Series = append(out.Series, seriesLink{
				Label: series.Node + " · " + volume(printer, series.Labels),
				URL:   historyLink(series.Node, series.Metric, series.Labels, lang, window),
			})
		}
	}
	return out
}

// historyLink addresses one series. It names every label the series carries, because a
// filter cannot demand that a series carry no others: a link short of one label would
// reach the ambiguous page instead of the chart (docs/specs/history.md#page).
func historyLink(node, metric string, labels map[string]string, lang, window string) string {
	values := url.Values{}
	values.Set("metric", metric)
	if node != "" {
		values.Set("node", node)
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values.Set(history.LabelPrefix+name, labels[name])
	}
	if lang != "" {
		values.Set("lang", lang)
	}
	if window != "" {
		values.Set("window", window)
	}
	return "/history?" + values.Encode()
}

// language is the ?lang= a link should carry on, canonical: a regional tag the page itself
// honoured must not be dropped from its own links, and one the panel does not speak must
// not be stamped onto them.
func language(values url.Values) string {
	if locale, spoken := i18n.Match(values.Get("lang")); spoken {
		return string(locale)
	}
	return ""
}

// refuse answers a query the page will not draw. The status is the endpoint's; the text is
// the reader's language, which is the one difference between the two surfaces (ADR 0008).
func refuse(w http.ResponseWriter, printer *i18n.Printer, err error) {
	page := historyView{
		Locale: printer.Locale(),
		Title:  printer.T("history.title"),
		Index:  printer.T("history.index"),
	}
	var refusal history.Refusal
	if errors.As(err, &refusal) {
		page.Message = printer.T(refusal.Key)
		render(w, http.StatusBadRequest, page)
		return
	}
	slog.Error("read history", "error", err)
	page.Message = printer.T("error.storage")
	render(w, http.StatusInternalServerError, page)
}

// render writes the page. The headers are set before the status line, because a header set
// after it is discarded.
func render(w http.ResponseWriter, status int, page historyView) {
	shellHeaders(w)
	w.WriteHeader(status)
	if err := historyTemplate.Execute(w, page); err != nil {
		slog.Error("render the history page", "error", err)
	}
}
