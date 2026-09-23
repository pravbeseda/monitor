package hub

import (
	"context"
	"html/template"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/storage"
)

var thresholdTemplate = template.Must(template.ParseFS(templates,
	"templates/thresholds.html", "templates/shell.html"))

// ThresholdStore is what the page needs of persistence: the series it may be opened for,
// what one of them is judged by, and whether it is excluded from anomalies
// (docs/specs/thresholds.md).
type ThresholdStore interface {
	Series(ctx context.Context, sel storage.Selection) ([]storage.SeriesNewest, error)
	ThresholdOf(ctx context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error)
	Excluded(ctx context.Context, ref storage.SeriesRef) (bool, error)
	// Configure stores what the form states in one step: the threshold, nil to clear it,
	// and whether the series is excluded from anomalies.
	Configure(ctx context.Context, ref storage.SeriesRef, th *storage.Threshold, exclude bool) error
}

// thresholdView is the form as the template sees it: every string translated, every value
// as the reader typed it or as it is stored.
type thresholdView struct {
	shell
	Series     string
	Volume     string
	Unit       string
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
	UnusualLabel   string
	ShowLabel      string
	ExcludeLabel   string
	SaveLabel      string
	BackLabel      string
	BackURL        string
}

// Thresholds serves the page that sets what one series is judged by, and the save it
// accepts (docs/specs/thresholds.md). Nothing else writes a threshold.
func Thresholds(store ThresholdStore, configured func(node string) bool) http.Handler {
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

		if r.Method == http.MethodPost {
			save(w, r, store, printer, ref, language(values))
			return
		}
		showForm(r.Context(), w, store, printer, ref, language(values), configured(ref.Node))
	})
}

// showForm draws the form over what is stored.
func showForm(ctx context.Context, w http.ResponseWriter, store ThresholdStore,
	printer *i18n.Printer, ref storage.SeriesRef, lang string, configured bool,
) {
	view := emptyForm(printer, ref, lang)
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
	case !evaluate.Readable(th):
		// Something is stored that nothing judges by, so the form says so and offers
		// blank fields rather than a configuration the hub is not honouring.
		view.Unreadable = printer.T("threshold.unreadable")
	default:
		view.Below = th.Direction != storage.Above
		view.Warning, view.Critical = typed(ref.Metric, th.Warning), typed(ref.Metric, th.Critical)
	}
	drawForm(w, view)
}

// save replaces the whole configuration of one series: a form states it, never patches it.
func save(w http.ResponseWriter, r *http.Request, store ThresholdStore,
	printer *i18n.Printer, ref storage.SeriesRef, lang string,
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
		refuseForm(w, printer, ref, lang, r, "threshold.bad_direction")
		return
	}
	// A form is the whole configuration, so a save that says nothing about the switch —
	// a form drawn before it existed — is not taken as either answer.
	anomalies := r.PostForm.Get("anomalies")
	exclude := anomalies == "exclude"
	if !exclude && anomalies != "show" {
		refuseForm(w, printer, ref, lang, r, "threshold.bad_switch")
		return
	}

	warning, err := parseValue(ref.Metric, r.PostForm.Get("warning"))
	if err != nil {
		refuseForm(w, printer, ref, lang, r, "threshold.bad_warning")
		return
	}
	critical, err := parseValue(ref.Metric, r.PostForm.Get("critical"))
	if err != nil {
		refuseForm(w, printer, ref, lang, r, "threshold.bad_critical")
		return
	}
	if !ordered(direction, warning, critical) {
		refuseForm(w, printer, ref, lang, r, "threshold.unordered")
		return
	}

	// Clearing both values withdraws the question: the subject loses its level with no
	// event and no recovery (docs/specs/evaluation.md#configuration-changes).
	var th *storage.Threshold
	if warning != nil || critical != nil {
		th = &storage.Threshold{Series: ref, Direction: direction, Warning: warning, Critical: critical}
	}
	if err := store.Configure(r.Context(), ref, th, exclude); err != nil {
		slog.Error("save a configuration", "node", ref.Node, "metric", ref.Metric, "error", err)
		http.Error(w, printer.T("error.storage"), http.StatusInternalServerError)
		return
	}
	// A redirect, so that reloading the answer saves nothing twice.
	http.Redirect(w, r, thresholdLink(ref.Node, ref.Metric, ref.Labels, lang), http.StatusSeeOther)
}

// refuseForm redraws the form with what the reader typed and says what is wrong with it.
func refuseForm(w http.ResponseWriter, printer *i18n.Printer, ref storage.SeriesRef, lang string,
	r *http.Request, reason string,
) {
	view := emptyForm(printer, ref, lang)
	view.Below = r.PostForm.Get("direction") != string(storage.Above)
	view.Excluded = r.PostForm.Get("anomalies") == "exclude"
	view.Warning, view.Critical = r.PostForm.Get("warning"), r.PostForm.Get("critical")
	view.Error = printer.T(reason)
	w.WriteHeader(http.StatusBadRequest)
	drawForm(w, view)
}

func emptyForm(printer *i18n.Printer, ref storage.SeriesRef, lang string) thresholdView {
	return thresholdView{
		shell:          still(printer, "threshold.title"),
		Series:         ref.Node + " · " + ref.Metric,
		Volume:         volume(printer, ref.Labels),
		Unit:           printer.T("unit." + string(history.UnitOf(ref.Metric))),
		Action:         thresholdLink(ref.Node, ref.Metric, ref.Labels, lang),
		Below:          true,
		DirectionLabel: printer.T("threshold.direction"),
		BelowLabel:     printer.T("threshold.below"),
		AboveLabel:     printer.T("threshold.above"),
		WarningLabel:   printer.T("level.warning"),
		CriticalLabel:  printer.T("level.critical"),
		UnusualLabel:   printer.T("threshold.unusual"),
		ShowLabel:      printer.T("threshold.unusual_show"),
		ExcludeLabel:   printer.T("threshold.unusual_exclude"),
		SaveLabel:      printer.T("threshold.save"),
		BackLabel:      printer.T("threshold.back"),
		BackURL:        historyLink(ref.Node, ref.Metric, ref.Labels, lang, ""),
	}
}

func drawForm(w http.ResponseWriter, view thresholdView) {
	if err := thresholdTemplate.Execute(w, view); err != nil {
		slog.Error("render the threshold page", "error", err)
	}
}

// seriesOf reads the series a link names. Every label the series carries has to be there:
// a threshold belongs to one series, never to a family of them.
func seriesOf(values url.Values) (storage.SeriesRef, error) {
	query, err := history.ParseSelection(values, "lang")
	if err != nil {
		return storage.SeriesRef{}, err
	}
	if query.Node == "" {
		return storage.SeriesRef{}, errNoNode
	}
	return storage.SeriesRef{Node: query.Node, Metric: query.Metric, Labels: maps.Clone(query.Labels)}, nil
}

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
// may be written with a decimal unit, and everything else is a plain number.
func parseValue(metric, written string) (*float64, error) {
	text := strings.TrimSpace(written)
	if text == "" {
		return nil, nil
	}
	scale := 1.0
	if history.UnitOf(metric) == history.Bytes {
		text, scale = splitUnit(text)
	}
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
	trimmed := strings.TrimSpace(text)
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

// sizeUnits are decimal, the way disks are sold and the way every size in this project is
// written (docs/specs/thresholds.md#model). "b" comes last so that "10gb" matches "gb".
var sizeUnits = []struct {
	suffix string
	scale  float64
}{{"kb", 1e3}, {"mb", 1e6}, {"gb", 1e9}, {"tb", 1e12}, {"pb", 1e15}, {"b", 1}}

// splitUnit takes the unit off a size and returns what it scales by. A size written with
// no unit is a count of bytes.
func splitUnit(text string) (string, float64) {
	lowered := strings.ToLower(text)
	for _, unit := range sizeUnits {
		if written, found := strings.CutSuffix(lowered, unit.suffix); found {
			return written, unit.scale
		}
	}
	return lowered, 1
}

// typed renders a stored value back into the field it was typed in, exactly and in a
// spelling this same form accepts: a size takes the largest unit that divides it without
// a remainder, and anything else is the plain number. Rendering it the way a page renders
// a reading would round it, and a reader who saved the form as drawn would store the
// rounding (docs/specs/thresholds.md#saving).
func typed(metric string, value *float64) string {
	if value == nil {
		return ""
	}
	plain := strconv.FormatFloat(*value, 'f', -1, 64)
	if history.UnitOf(metric) != history.Bytes || *value == 0 {
		return plain
	}
	// Largest unit first, so a size reads the way it was written rather than in the
	// smallest unit that happens to divide it.
	for i := len(sizeUnits) - 1; i >= 0; i-- {
		unit := sizeUnits[i]
		if unit.scale == 1 {
			continue
		}
		if whole := *value / unit.scale; whole == math.Trunc(whole) && math.Abs(whole) >= 1 {
			return strconv.FormatFloat(whole, 'f', -1, 64) + strings.ToUpper(unit.suffix)
		}
	}
	return plain
}

// ordered reports whether critical is strictly beyond warning in the chosen direction.
// Equal values would leave one of the two levels unreachable.
func ordered(direction storage.Direction, warning, critical *float64) bool {
	if warning == nil || critical == nil {
		return true
	}
	if direction == storage.Above {
		return *critical > *warning
	}
	return *critical < *warning
}

// thresholdLink addresses the page that sets what one series is judged by, named the way
// a history link names it (docs/specs/history.md#page).
func thresholdLink(node, metric string, labels map[string]string, lang string) string {
	return "/thresholds?" + seriesQuery(node, metric, labels, lang).Encode()
}
