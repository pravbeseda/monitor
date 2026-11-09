package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/storage"
)

var thresholdTemplate = template.Must(template.ParseFS(templates,
	"templates/thresholds.html", "templates/chart.html", "templates/shell.html"))

// settingsFrame is the form's chart: no axes, the stored values drawn across it.
var settingsFrame = frame{width: 400, height: 84, top: 6, bottom: 2}

// ThresholdStore is what the page needs of persistence: the series it may be opened for,
// what one of them is judged by, and whether it is excluded from anomalies
// (docs/specs/thresholds.md).
type ThresholdStore interface {
	NameReader
	Series(ctx context.Context, sel storage.Selection) ([]storage.SeriesNewest, error)
	ThresholdOf(ctx context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error)
	Excluded(ctx context.Context, ref storage.SeriesRef) (bool, error)
	// Configure stores what the form states in one step: the threshold, nil to clear it,
	// and whether the series is excluded from anomalies.
	Configure(ctx context.Context, ref storage.SeriesRef, th *storage.Threshold, exclude bool) error
	// ConfigureEvery stores th — nil to clear it — for every series of own's metric, and
	// the switch for own alone.
	ConfigureEvery(ctx context.Context, own storage.SeriesRef, th *storage.Threshold, exclude bool) error
}

// thresholdView is the form as the template sees it: every string translated, every value
// as the reader typed it or as it is stored.
type thresholdView struct {
	shell
	// Series names the series; Metric, its id, is shown on hover.
	Series string
	Metric string
	Node   string
	// Unit says what the fields take, empty for a plain number.
	Unit string
	// Chart is the series' last day, nil when it has no point there or they cannot be read,
	// and Now its newest value. Units is the JSON of what a value may be written in, by
	// which a script places a typed value on the chart.
	Chart      *chart
	Now        string
	Units      string
	Saved      string
	Action     string
	Below      bool
	Excluded   bool
	Warning    string
	Critical   string
	Error      string
	Unwatched  string
	Detached   string
	Unreadable string

	DirectionLabel string
	BelowLabel     string
	AboveLabel     string
	WarningLabel   string
	CriticalLabel  string
	BlankHint      string
	SwitchLabel    string
	SaveLabel      string
	CancelLabel    string
	CloseLabel     string
	BackLabel      string
	BackURL        string
	AllLabel       string
	AllURL         string
	EveryLabel     string
	// What the dialog says beyond the form (docs/specs/web.md#dialog) comes with the form,
	// so that it can say it when the hub no longer answers.
	UnsavedLabel string
	DiscardLabel string
	StayLabel    string
	UnsentLabel  string

	// Every lists the series "Confirm" will change, once the reader asked to apply the form
	// to every series of its metric (docs/specs/thresholds.md#all).
	Every        []everyView
	EveryNote    string
	ConfirmLabel string
}

// everyView is one series a confirmation will change: its name and what it is judged by now.
type everyView struct {
	Series  string
	Current string
}

// Thresholds serves the page that sets what one series is judged by, and the posts it
// accepts: a save, and applying the form to every series of its metric
// (docs/specs/thresholds.md). Beside it only seeding writes a threshold, at ingest.
func Thresholds(store ThresholdStore, reader history.Reader, configured func(node string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zoneOf(r))
		shellHeaders(w)

		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, printer.T("error.method"), http.StatusMethodNotAllowed)
			return
		}
		// A save from another site is refused before anything is read: what it names is
		// none of its business either (ADR 0032).
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, printer.T("error.origin"), http.StatusForbidden)
			return
		}
		ref, err := seriesOf(values)
		if err != nil {
			http.Error(w, printer.T("error.query"), http.StatusBadRequest)
			return
		}
		stored, err := exists(r.Context(), store, ref)
		if err != nil {
			slog.Error("read the series", "error", err)
			http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
			return
		}
		if !stored {
			http.Error(w, printer.T("error.unknown_series"), http.StatusNotFound)
			return
		}

		name := namesFrom(r.Context(), store).Metrics[ref.Metric]
		if r.Method == http.MethodPost {
			save(w, r, store, reader, printer, name, ref, language(values))
			return
		}
		showForm(r.Context(), w, store, reader, printer, name, ref, language(values), configured(ref.Node), values.Has(savedParam))
	})
}

// showForm draws the form over what is stored.
func showForm(ctx context.Context, w http.ResponseWriter, store ThresholdStore, reader history.Reader,
	printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string, configured, saved bool,
) {
	view := emptyForm(printer, name, ref, lang)
	if saved {
		view.Saved = printer.T("threshold.saved")
	}
	if !configured {
		view.Detached = printer.T("threshold.detached")
	}
	excluded, err := store.Excluded(ctx, ref)
	if err != nil {
		slog.Error("read an exclusion", "node", ref.Node, "metric", ref.Metric, "error", err)
		http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
		return
	}
	view.Excluded = excluded
	th, watched, err := store.ThresholdOf(ctx, ref)
	switch {
	case err != nil:
		slog.Error("read a threshold", "node", ref.Node, "metric", ref.Metric, "error", err)
		http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
		return
	case !watched:
		view.Unwatched = printer.T("threshold.unwatched")
	case !th.Readable():
		// Something is stored that nothing judges by, so the form says so and offers
		// blank fields rather than a configuration the hub is not honouring.
		view.Unreadable = printer.T("threshold.unreadable")
	default:
		view.Below = th.Direction != storage.Above
		view.Warning, view.Critical = typed(ref.Metric, th.Warning), typed(ref.Metric, th.Critical)
	}
	view.Chart, view.Now = lastDay(ctx, printer, reader, ref, th)
	drawForm(w, view)
}

// lastDay is the series' last day as the form draws it, and its newest value. Reading it
// never costs the form, so a failure only leaves the chart out (thresholds.md#form).
func lastDay(ctx context.Context, printer *i18n.Printer, reader history.Reader, ref storage.SeriesRef, th storage.Threshold) (*chart, string) {
	series, window, err := reader.ReadOne(ctx, ref, history.DefaultWindow)
	if err != nil {
		slog.Error("read a series for its form", "node", ref.Node, "metric", ref.Metric, "error", err)
		return nil, ""
	}
	if series == nil || len(series.Points) == 0 {
		return nil, ""
	}
	drawn := draw(printer, *series, window, settingsFrame, levelsIn(th)...)
	return &drawn, format(printer, ref.Metric, series.Points[len(series.Points)-1].Value)
}

// save replaces the whole configuration of one series: a form states it, never patches it.
func save(w http.ResponseWriter, r *http.Request, store ThresholdStore, reader history.Reader,
	printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string,
) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, printer.T("error.query"), http.StatusBadRequest)
		return
	}
	var direction storage.Direction
	switch r.PostForm.Get("direction") {
	case string(storage.Below):
		direction = storage.Below
	case string(storage.Above):
		direction = storage.Above
	default:
		refuseForm(w, reader, printer, name, ref, lang, r, "threshold.bad_direction")
		return
	}
	// A form is the whole configuration, so a save that says nothing about the switch —
	// a form drawn before it existed — is not taken as either answer.
	anomalies := r.PostForm.Get("anomalies")
	exclude := anomalies == "exclude"
	if !exclude && anomalies != "show" {
		refuseForm(w, reader, printer, name, ref, lang, r, "threshold.bad_switch")
		return
	}

	warning, err := parseValue(ref.Metric, r.PostForm.Get("warning"))
	if err != nil {
		refuseForm(w, reader, printer, name, ref, lang, r, "threshold.bad_warning")
		return
	}
	critical, err := parseValue(ref.Metric, r.PostForm.Get("critical"))
	if err != nil {
		refuseForm(w, reader, printer, name, ref, lang, r, "threshold.bad_critical")
		return
	}
	if !storage.Ordered(direction, warning, critical) {
		refuseForm(w, reader, printer, name, ref, lang, r, "threshold.unordered")
		return
	}

	// Clearing both values withdraws the question: the subject loses its level with no
	// event and no recovery (docs/specs/evaluation.md#configuration-changes).
	var th *storage.Threshold
	if warning != nil || critical != nil {
		th = &storage.Threshold{Direction: direction, Warning: warning, Critical: critical}
	}
	switch r.PostForm.Get("apply") {
	case "every":
		askEvery(w, r, store, reader, printer, name, ref, lang, th == nil)
		return
	case "confirm":
		err = store.ConfigureEvery(r.Context(), ref, th, exclude)
	default:
		err = store.Configure(r.Context(), ref, th, exclude)
	}
	if err != nil {
		slog.Error("save a configuration", "node", ref.Node, "metric", ref.Metric, "error", err)
		http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
		return
	}
	// A redirect, so that reloading the answer saves nothing twice.
	http.Redirect(w, r, thresholdLink(ref.Node, ref.Metric, ref.Labels, lang)+"&"+savedParam+"=1", http.StatusSeeOther)
}

// askEvery redraws the form as typed, with every series of its metric that "Confirm" will
// change and what each is judged by now; nothing is stored (docs/specs/thresholds.md#all).
func askEvery(w http.ResponseWriter, r *http.Request, store ThresholdStore, reader history.Reader,
	printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string, removing bool,
) {
	all, err := store.Series(r.Context(), storage.Selection{Metric: ref.Metric})
	if err != nil {
		slog.Error("read the series of a metric", "metric", ref.Metric, "error", err)
		http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
		return
	}
	heading := func(s storage.SeriesNewest) string { return notify.Heading(printer, name, s.Metric, s.Labels) }
	slices.SortStableFunc(all, func(a, b storage.SeriesNewest) int {
		return cmp.Or(strings.Compare(a.Node, b.Node), strings.Compare(heading(a), heading(b)))
	})
	view := typedForm(r.Context(), reader, printer, name, ref, lang, r)
	for _, s := range all {
		th, watched, err := store.ThresholdOf(r.Context(), s.SeriesRef)
		if err != nil {
			slog.Error("read a threshold", "node", s.Node, "metric", s.Metric, "error", err)
			http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
			return
		}
		view.Every = append(view.Every, everyView{
			Series:  s.Node + " · " + heading(s),
			Current: currentOf(printer, th, watched),
		})
	}
	view.EveryNote = printer.T("threshold.every_replace")
	if removing {
		view.EveryNote = printer.T("threshold.every_remove")
	}
	view.ConfirmLabel = printer.T("threshold.confirm")
	drawForm(w, view)
}

// currentOf says what a series is judged by now, as the form would write it.
func currentOf(printer *i18n.Printer, th storage.Threshold, watched bool) string {
	switch {
	case !watched:
		return printer.T("threshold.every_none")
	case !th.Readable():
		return printer.T("threshold.every_unreadable")
	}
	parts := []string{printer.T("threshold." + string(th.Direction))}
	if th.Warning != nil {
		parts = append(parts, printer.T("level.warning")+" "+typed(th.Series.Metric, th.Warning))
	}
	if th.Critical != nil {
		parts = append(parts, printer.T("level.critical")+" "+typed(th.Series.Metric, th.Critical))
	}
	return strings.Join(parts, ", ")
}

// refuseForm redraws the form with what the reader typed and says what is wrong with it.
func refuseForm(w http.ResponseWriter, reader history.Reader, printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string,
	r *http.Request, reason string,
) {
	view := typedForm(r.Context(), reader, printer, name, ref, lang, r)
	view.Error = printer.T(reason)
	w.WriteHeader(http.StatusBadRequest)
	drawForm(w, view)
}

// typedForm is the form holding what the reader posted, its chart with no stored value on
// it: what the reader typed is what a script draws there.
func typedForm(ctx context.Context, reader history.Reader, printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string, r *http.Request) thresholdView {
	view := emptyForm(printer, name, ref, lang)
	view.Chart, view.Now = lastDay(ctx, printer, reader, ref, storage.Threshold{})
	view.Below = r.PostForm.Get("direction") != string(storage.Above)
	view.Excluded = r.PostForm.Get("anomalies") == "exclude"
	view.Warning, view.Critical = r.PostForm.Get("warning"), r.PostForm.Get("critical")
	return view
}

func emptyForm(printer *i18n.Printer, name names.Text, ref storage.SeriesRef, lang string) thresholdView {
	unit := history.UnitOf(ref.Metric)
	view := thresholdView{
		shell:          still(printer, "threshold.title", "", lang),
		Series:         notify.Heading(printer, name, ref.Metric, ref.Labels),
		Metric:         ref.Metric,
		Node:           ref.Node,
		Units:          unitsJSON(unit),
		Action:         thresholdLink(ref.Node, ref.Metric, ref.Labels, lang),
		Below:          true,
		DirectionLabel: printer.T("threshold.direction"),
		BelowLabel:     printer.T("threshold.below"),
		AboveLabel:     printer.T("threshold.above"),
		WarningLabel:   printer.T("level.warning"),
		CriticalLabel:  printer.T("level.critical"),
		BlankHint:      printer.T("threshold.blank_hint"),
		SwitchLabel:    printer.T("threshold.unusual_switch"),
		SaveLabel:      printer.T("threshold.save"),
		CancelLabel:    printer.T("threshold.cancel"),
		CloseLabel:     printer.T("threshold.close"),
		EveryLabel:     printer.T("threshold.every"),
		BackLabel:      printer.T("threshold.back"),
		BackURL:        historyLink(ref.Node, ref.Metric, ref.Labels, lang, ""),
		AllLabel:       printer.T("page.all_series"),
		AllURL:         pageLink("/debug", lang),
		UnsavedLabel:   printer.T("threshold.unsaved"),
		DiscardLabel:   printer.T("threshold.discard"),
		StayLabel:      printer.T("threshold.stay"),
		UnsentLabel:    printer.T("threshold.unsent"),
	}
	if unit != history.Number {
		view.Unit = printer.T("unit." + string(unit))
	}
	return view
}

// unitsJSON maps what a value of this unit may be written in, lower-cased, to its scale.
func unitsJSON(unit history.Unit) string {
	scales := map[string]float64{}
	for _, written := range writtenUnits[unit] {
		scales[strings.ToLower(written.suffix)] = written.scale
	}
	encoded, err := json.Marshal(scales)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func drawForm(w http.ResponseWriter, view thresholdView) {
	if err := thresholdTemplate.Execute(w, view); err != nil {
		slog.Error("render the threshold page", "error", err)
	}
}

// seriesOf reads the series a link names. Every label the series carries has to be there:
// a threshold belongs to one series, never to a family of them.
func seriesOf(values url.Values) (storage.SeriesRef, error) {
	query, err := history.ParseSelection(values, "lang", savedParam)
	if err != nil {
		return storage.SeriesRef{}, err
	}
	if query.Node == "" {
		return storage.SeriesRef{}, errNoNode
	}
	return storage.SeriesRef{Node: query.Node, Metric: query.Metric, Labels: maps.Clone(query.Labels)}, nil
}

// savedParam marks the address of the form a save answered with, so that it can say so.
const savedParam = "saved"

var errNoNode = refusalError("a threshold belongs to one series, so its node is named")

type refusalError string

func (e refusalError) Error() string { return string(e) }

// exists reports whether the hub holds this exact series: a threshold is set on something
// that reports (docs/specs/thresholds.md#form).
func exists(ctx context.Context, store ThresholdStore, ref storage.SeriesRef) (bool, error) {
	stored, err := store.Series(ctx, storage.Selection{Metric: ref.Metric, Node: ref.Node})
	if err != nil {
		return false, err
	}
	// Compared as storage keys them, not as a page renders them: LabelKey escapes
	// nothing, so `{"a": "b,c=d"}` and `{"a": "b", "c": "d"}` read alike in it.
	for _, candidate := range stored {
		if maps.Equal(candidate.Labels, ref.Labels) {
			return true, nil
		}
	}
	return false, nil
}

// sameOrigin reports whether a save came from this hub's own page.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == r.Host
}

// parseValue reads one field of the form. An empty field is a level with no value; a size
// or a duration may be written with a unit, and everything else is a plain number.
func parseValue(metric, written string) (*float64, error) {
	text := strings.TrimSpace(written)
	if text == "" {
		return nil, nil
	}
	text, scale := splitUnit(text, writtenUnits[history.UnitOf(metric)])
	value, err := number(text)
	if err != nil {
		return nil, err
	}
	// A finite number can stop being one once its unit is applied: 1e300GB is not a size.
	if value *= scale; math.IsInf(value, 0) {
		return nil, errNotANumber
	}
	return &value, nil
}

var errNotANumber = refusalError("not a finite number")

// number reads what a level can be judged against: NaN is never entered and never left,
// and an infinity is no measurement, so neither is a threshold.
func number(text string) (float64, error) {
	// A single comma is the decimal separator the Russian interface writes values with.
	trimmed := strings.Replace(strings.TrimSpace(text), ",", ".", 1)
	// Go's own literal spellings are not this field's grammar: 0x1p10 is a typo, not 1024.
	if strings.ContainsAny(trimmed, "xXpP_") {
		return 0, errNotANumber
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errNotANumber
	}
	return value, nil
}

type writtenUnit struct {
	suffix string
	scale  float64
}

// writtenUnits are the units a value of a metric may be typed in
// (docs/specs/thresholds.md#model). Each list runs from the smallest unit to the largest,
// so that typed can walk it backwards, with the unit a bare number already counts in last,
// so that "10gb" is not read as "10g" and a "b". Sizes are decimal, the way disks are sold
// and the way every size in this project is written.
var writtenUnits = map[history.Unit][]writtenUnit{
	history.Bytes:    {{"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"PB", 1e15}, {"B", 1}},
	history.Duration: {{"m", 60}, {"h", 60 * 60}, {"d", 24 * 60 * 60}, {"s", 1}},
}

// splitUnit cuts the first of units that ends the value off it, whatever its case, and
// returns what it scales by. A value written with no unit is already in the metric's own.
func splitUnit(text string, units []writtenUnit) (string, float64) {
	lowered := strings.ToLower(text)
	for _, unit := range units {
		if written, found := strings.CutSuffix(lowered, strings.ToLower(unit.suffix)); found {
			return written, unit.scale
		}
	}
	return text, 1
}

// typed renders a stored value back into the field it was typed in, exactly and in a
// spelling this same form accepts: a size or a duration takes the largest unit that
// divides it without a remainder, and anything else is the plain number. Rendering it the
// way a page renders a reading would round it, and a reader who saved the form as drawn
// would store the rounding (docs/specs/thresholds.md#saving).
func typed(metric string, value *float64) string {
	if value == nil {
		return ""
	}
	plain := strconv.FormatFloat(*value, 'f', -1, 64)
	// Largest unit first, so a value reads the way it was written rather than in the
	// smallest unit that happens to divide it.
	units := writtenUnits[history.UnitOf(metric)]
	for i := len(units) - 1; i >= 0; i-- {
		unit := units[i]
		if unit.scale == 1 {
			continue
		}
		// Multiplying back is what a save does, so it must give the stored value again: past
		// 2^53 a quotient can round to a whole number the stored one is not a multiple of.
		whole := *value / unit.scale
		if whole == math.Trunc(whole) && math.Abs(whole) >= 1 && whole*unit.scale == *value {
			return strconv.FormatFloat(whole, 'f', -1, 64) + unit.suffix
		}
	}
	return plain
}

// thresholdLink addresses the page that sets what one series is judged by, named the way
// a history link names it (docs/specs/history.md#page).
func thresholdLink(node, metric string, labels map[string]string, lang string) string {
	return "/thresholds?" + seriesQuery(node, metric, labels, lang).Encode()
}
