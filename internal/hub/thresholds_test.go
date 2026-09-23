package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/storage"
)

// dataSeries is the one series every test on this page is opened for, and dataAddress the
// way a link addresses it: every label it carries, named.
var dataSeries = storage.SeriesRef{
	Node:   "server-b",
	Metric: "disk.free_bytes",
	Labels: map[string]string{"mount": "/data", "fs": "ext4", "removable": "false"},
}

const dataAddress = "/thresholds?label.fs=ext4&label.mount=%2Fdata&label.removable=false" +
	"&metric=disk.free_bytes&node=server-b"

// judged is a store holding the series the hub has seen and what each of them is judged
// by, so a save can be read back.
type judged struct {
	stored
	series []storage.SeriesNewest
	held   map[string]storage.Threshold
	// excluded holds the series excluded from anomalies.
	excluded map[string]bool
	// exclusionErr fails the read of whether a series is excluded.
	exclusionErr error
	// unreadable fails the read of one series' threshold while the series itself still
	// lists: what is stored for it is there and cannot be understood.
	unreadable error
}

func holding(series ...storage.SeriesRef) judged {
	out := judged{held: map[string]storage.Threshold{}, excluded: map[string]bool{}}
	for _, ref := range series {
		out.series = append(out.series, storage.SeriesNewest{SeriesRef: ref})
	}
	return out
}

func key(ref storage.SeriesRef) string {
	return ref.Node + "\x00" + ref.Metric + "\x00" + storage.LabelKey(ref.Labels)
}

func (j judged) Series(_ context.Context, sel storage.Selection) ([]storage.SeriesNewest, error) {
	var out []storage.SeriesNewest
	for _, one := range j.series {
		if one.Metric == sel.Metric && (sel.Node == "" || one.Node == sel.Node) {
			out = append(out, one)
		}
	}
	return out, j.err
}

func (j judged) ThresholdOf(_ context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error) {
	if j.unreadable != nil {
		return storage.Threshold{}, false, j.unreadable
	}
	th, ok := j.held[key(ref)]
	return th, ok, nil
}

func (j judged) Configure(_ context.Context, ref storage.SeriesRef, th *storage.Threshold, exclude bool) error {
	if th == nil {
		delete(j.held, key(ref))
	} else {
		j.held[key(ref)] = *th
	}
	if exclude {
		j.excluded[key(ref)] = true
	} else {
		delete(j.excluded, key(ref))
	}
	return nil
}

func (j judged) Excluded(_ context.Context, ref storage.SeriesRef) (bool, error) {
	return j.excluded[key(ref)], j.exclusionErr
}

// openForm reads the page of one series.
func openForm(t *testing.T, store judged, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

// saveForm posts the form the page draws, from this hub's own origin unless another one
// is named.
func saveForm(t *testing.T, store judged, target string, form url.Values, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin == "" {
		origin = "http://" + req.Host
	}
	if origin != "none" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(rec, req)
	return rec
}

// form is a save that leaves the series shown when it is unusual, as the form is drawn for
// a series nobody excluded.
func form(direction, warning, critical string) url.Values {
	return url.Values{
		"direction": {direction},
		"warning":   {warning},
		"critical":  {critical},
		"anomalies": {"show"},
	}
}

func excluding(values url.Values) url.Values {
	values.Set("anomalies", "exclude")
	return values
}

// inputs is every <input> the page drew, each as the text of its attributes.
func inputs(body string) []string {
	var out []string
	for _, tag := range strings.Split(body, "<input ")[1:] {
		attributes, _, _ := strings.Cut(tag, ">")
		out = append(out, attributes)
	}
	return out
}

// checked reports whether the form's radio button for one direction is the selected one.
func checked(body, direction string) bool {
	for _, input := range inputs(body) {
		if strings.Contains(input, `value="`+direction+`"`) {
			return strings.Contains(input, "checked")
		}
	}
	return false
}

// valueOf is what one field of the form carries.
func valueOf(t *testing.T, body, field string) string {
	t.Helper()
	for _, input := range inputs(body) {
		if !strings.Contains(input, `name="`+field+`"`) {
			continue
		}
		_, after, _ := strings.Cut(input, `value="`)
		value, _, _ := strings.Cut(after, `"`)
		return value
	}
	t.Fatalf("no %s field in %s", field, body)
	return ""
}

// spec: thresholds.md#form — a series with nothing configured shows an empty form and says
// the series has no level until a value is set.
func TestThresholdFormIsEmptyWhenNothingIsConfigured(t *testing.T) {
	status, body := openForm(t, holding(dataSeries), dataAddress)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if !checked(body, "below") || checked(body, "above") {
		t.Errorf("page = %q, want the direction below", body)
	}
	if got := valueOf(t, body, "warning"); got != "" {
		t.Errorf("warning = %q, want it blank", got)
	}
	if got := valueOf(t, body, "critical"); got != "" {
		t.Errorf("critical = %q, want it blank", got)
	}
	if !strings.Contains(body, "Nothing is set, so this series has no level") {
		t.Errorf("page = %q, want it to say the series is not judged", body)
	}
	if !strings.Contains(body, "server-b · disk.free_bytes") || !strings.Contains(body, "/data · ext4") {
		t.Errorf("page = %q, want it to name the series it configures", body)
	}
}

// spec: thresholds.md#form — a series with a configuration shows it as stored, in the unit
// the metric implies.
func TestThresholdFormShowsWhatIsStored(t *testing.T) {
	store := holding(dataSeries)
	warning, critical := 20e9, 30e9
	store.held[key(dataSeries)] = storage.Threshold{
		Series: dataSeries, Direction: storage.Above, Warning: &warning, Critical: &critical,
	}

	status, body := openForm(t, store, dataAddress)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if !checked(body, "above") || checked(body, "below") {
		t.Errorf("page = %q, want the stored direction", body)
	}
	if got := valueOf(t, body, "warning"); got != "20GB" {
		t.Errorf("warning = %q, want the stored size", got)
	}
	if got := valueOf(t, body, "critical"); got != "30GB" {
		t.Errorf("critical = %q, want the stored size", got)
	}
	if strings.Contains(body, "Nothing is set") {
		t.Errorf("page = %q, want no unwatched note on a configured series", body)
	}
}

// spec: thresholds.md#form — a series the hub has never stored is a 404 as a page, and a
// query naming fewer labels than the series carries names no series at all.
func TestThresholdFormRefusesAnUnknownSeries(t *testing.T) {
	store := holding(dataSeries)
	for _, target := range []string{
		"/thresholds?metric=disk.free_bytes&node=laptop-a&label.fs=ext4&label.mount=%2Fdata&label.removable=false",
		"/thresholds?metric=disk.free_bytes&node=server-b&label.mount=%2Fdata",
		"/thresholds?metric=disk.free_bytes&node=server-b&label.fs=ext4&label.mount=%2Fdata&label.removable=false&label.extra=1",
	} {
		if status, body := openForm(t, store, target); status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404: %s", target, status, body)
		}
	}
}

// spec: thresholds.md#form — a method other than GET or POST is refused.
func TestThresholdPageRefusesOtherMethods(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPut} {
		rec := httptest.NewRecorder()
		routesWith(t, holding(dataSeries), at).
			ServeHTTP(rec, httptest.NewRequest(method, dataAddress, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d, want 405", method, rec.Code)
		}
	}
}

// spec: thresholds.md#saving — a save whose Origin is another site, or none at all, is
// refused as a page and stores nothing.
func TestThresholdSaveRefusesAnotherOrigin(t *testing.T) {
	for _, origin := range []string{"http://elsewhere.example", "none"} {
		store := holding(dataSeries)

		rec := saveForm(t, store, dataAddress, form("below", "20GB", "10GB"), origin)

		if rec.Code != http.StatusForbidden {
			t.Errorf("origin %s answered %d, want 403", origin, rec.Code)
		}
		if len(store.held) != 0 {
			t.Errorf("origin %s stored %+v, want nothing stored", origin, store.held)
		}
	}
}

// spec: thresholds.md#saving — a consistent save is stored and answered with a redirect,
// so that reloading it saves nothing twice.
func TestThresholdSaveStoresAndRedirects(t *testing.T) {
	store := holding(dataSeries)

	rec := saveForm(t, store, dataAddress, form("below", "20GB", "10GB"), "")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != dataAddress {
		t.Errorf("Location = %q, want the form of the same series", got)
	}
	held, ok := store.held[key(dataSeries)]
	if !ok {
		t.Fatalf("nothing stored for the series; store = %+v", store.held)
	}
	if held.Direction != storage.Below || held.Warning == nil || *held.Warning != 20e9 ||
		held.Critical == nil || *held.Critical != 10e9 {
		t.Errorf("stored %+v, want 20 GB and 10 GB below", held)
	}
}

// spec: thresholds.md#saving — a `_bytes` value written 10GB is stored as 10 000 000 000
// and shown back as a size.
func TestThresholdSaveTakesASizeOnABytesSeries(t *testing.T) {
	store := holding(dataSeries)

	if rec := saveForm(t, store, dataAddress, form("below", "10GB", ""), ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body)
	}
	held := store.held[key(dataSeries)]
	if held.Warning == nil || *held.Warning != 10e9 {
		t.Fatalf("stored %+v, want the decimal gigabytes it is written in", held)
	}
	if held.Critical != nil {
		t.Errorf("critical = %v, want a series with only a warning", *held.Critical)
	}

	_, body := openForm(t, store, dataAddress)
	if got := valueOf(t, body, "warning"); got != "10GB" {
		t.Errorf("warning = %q, want the size shown back as one", got)
	}
}

// spec: thresholds.md#saving — a save with both fields empty removes the configuration.
func TestThresholdSaveWithBothFieldsEmptyClears(t *testing.T) {
	store := holding(dataSeries)
	warning := 20e9
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: storage.Below, Warning: &warning}

	if rec := saveForm(t, store, dataAddress, form("below", "", "  "), ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body)
	}
	if len(store.held) != 0 {
		t.Fatalf("store = %+v, want the configuration removed", store.held)
	}
	if _, body := openForm(t, store, dataAddress); !strings.Contains(body, "Nothing is set") {
		t.Errorf("page = %q, want the series unjudged again", body)
	}
}

// spec: thresholds.md#saving — a refused save stores nothing and leaves what the reader
// typed in the form.
func TestThresholdSaveRefusesWhatItCannotStore(t *testing.T) {
	tests := []struct {
		name    string
		form    url.Values
		message string
	}{
		{"a value that is not a finite number", form("below", "soon", ""), "The warning value is not a number."},
		{"a critical that is not a number", form("below", "20GB", "NaN"), "The critical value is not a number."},
		{"a percentage on a size", form("below", "12%", ""), "The warning value is not a number."},
		{"critical not beyond warning", form("below", "10GB", "10GB"), "Critical must be strictly beyond warning"},
		{"critical the wrong side of warning", form("above", "10GB", "5GB"), "Critical must be strictly beyond warning"},
		{"a direction that is neither", form("sideways", "20GB", "10GB"), "Choose one direction."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := holding(dataSeries)

			rec := saveForm(t, store, dataAddress, tc.form, "")

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.message) {
				t.Errorf("page = %q, want it to name what is wrong: %q", body, tc.message)
			}
			if len(store.held) != 0 {
				t.Errorf("store = %+v, want nothing stored", store.held)
			}
			if got := valueOf(t, body, "warning"); got != tc.form.Get("warning") {
				t.Errorf("warning = %q, want what the reader typed, %q", got, tc.form.Get("warning"))
			}
			if got := valueOf(t, body, "critical"); got != tc.form.Get("critical") {
				t.Errorf("critical = %q, want what the reader typed, %q", got, tc.form.Get("critical"))
			}
		})
	}
}

// spec: thresholds.md#saving — a save for a series the hub has never stored is a 404 and
// stores nothing.
func TestThresholdSaveRefusesAnUnknownSeries(t *testing.T) {
	store := holding(dataSeries)
	target := "/thresholds?metric=disk.free_bytes&node=laptop-a&label.fs=ext4&label.mount=%2Fdata&label.removable=false"

	rec := saveForm(t, store, target, form("below", "20GB", "10GB"), "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
	if len(store.held) != 0 {
		t.Errorf("store = %+v, want nothing stored", store.held)
	}
}

// spec: thresholds.md#saving — two saves for one series leave the later one stored: a form
// states the whole configuration rather than patching it.
func TestThresholdSaveReplacesTheWholeConfiguration(t *testing.T) {
	store := holding(dataSeries)

	saveForm(t, store, dataAddress, form("below", "20GB", "10GB"), "")
	saveForm(t, store, dataAddress, form("above", "30GB", ""), "")

	held := store.held[key(dataSeries)]
	if held.Direction != storage.Above || held.Warning == nil || *held.Warning != 30e9 || held.Critical != nil {
		t.Errorf("stored %+v, want only what the later save stated", held)
	}
}

// spec: thresholds.md#form — a series of a node the file no longer names still takes a
// form, saying the series is not judged while its node is not configured.
func TestThresholdFormSaysWhenTheNodeIsNotConfigured(t *testing.T) {
	loose := dataSeries
	loose.Node = "server-c"
	target := "/thresholds?metric=disk.free_bytes&node=server-c&label.fs=ext4&label.mount=%2Fdata&label.removable=false"

	status, body := openForm(t, holding(loose), target)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want the form as ever: %s", status, body)
	}
	if !strings.Contains(body, "The configuration no longer names this node") {
		t.Errorf("page = %q, want it to say the series is not judged", body)
	}
}

// spec: thresholds.md#form — a series whose stored threshold this build cannot read shows
// the form with the unreadable values blank and says saving will replace it.
func TestThresholdFormSaysWhenTheStoredConfigurationCannotBeRead(t *testing.T) {
	store := holding(dataSeries)
	warning := 20e9
	store.held[key(dataSeries)] = storage.Threshold{
		Series: dataSeries, Direction: storage.Direction("sideways"), Warning: &warning,
	}

	status, body := openForm(t, store, dataAddress)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want the form as ever: %s", status, body)
	}
	if !strings.Contains(body, "The stored configuration could not be read") {
		t.Errorf("page = %q, want it to say saving will replace what is there", body)
	}
	for _, field := range []string{"warning", "critical"} {
		if got := valueOf(t, body, field); got != "" {
			t.Errorf("%s = %q, want the unreadable value left blank", field, got)
		}
	}
	if strings.Contains(body, errFailed.Error()) {
		t.Errorf("page = %q, want the storage error kept to the log", body)
	}
}

// spec: thresholds.md#form — the page in Russian takes every label and message from the
// Russian catalogue.
func TestThresholdFormSpeaksTheReadersLanguage(t *testing.T) {
	status, body := openForm(t, holding(dataSeries), dataAddress+"&lang=ru")

	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, body)
	}
	for _, want := range []string{"Чем оценивается эта серия", "ниже порога", "Сохранить", "Ничего не задано"} {
		if !strings.Contains(body, want) {
			t.Errorf("page = %q, want %q", body, want)
		}
	}
}

// spec: thresholds.md#form — a read that fails is a failure of the panel, not a corrupt
// threshold: a reader must not be invited to replace a configuration that is intact.
func TestThresholdFormFailsLoudlyWhenTheStoreCannotBeRead(t *testing.T) {
	store := holding(dataSeries)
	store.unreadable = errFailed

	status, body := openForm(t, store, dataAddress)

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", status, body)
	}
	if strings.Contains(body, "saving replaces it") {
		t.Errorf("page = %q, want no invitation to overwrite an intact threshold", body)
	}
}

// spec: thresholds.md#saving — a size no round unit names is shown back as the number it
// is, so redrawing the form and saving it does not rewrite the threshold.
func TestThresholdFormShowsAnUnroundSizeExactly(t *testing.T) {
	store := holding(dataSeries)
	warning := 20123456789.0
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: storage.Below, Warning: &warning}

	_, body := openForm(t, store, dataAddress)

	if got := valueOf(t, body, "warning"); got != "20123456789" {
		t.Fatalf("warning = %q, want the stored number itself", got)
	}
}

// pctSeries is the same volume's proportional series: the page is not particular to
// sizes, and neither is its parsing.
var pctSeries = storage.SeriesRef{
	Node:   "server-b",
	Metric: "disk.free_pct",
	Labels: map[string]string{"mount": "/data", "fs": "ext4", "removable": "false"},
}

const pctAddress = "/thresholds?label.fs=ext4&label.mount=%2Fdata&label.removable=false" +
	"&metric=disk.free_pct&node=server-b"

// spec: thresholds.md#form — the same form for any series the hub has values for.
func TestThresholdFormTakesAnyMetric(t *testing.T) {
	store := holding(pctSeries)
	warning := 15.5
	store.held[key(pctSeries)] = storage.Threshold{Series: pctSeries, Direction: storage.Below, Warning: &warning}

	status, body := openForm(t, store, pctAddress)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if got := valueOf(t, body, "warning"); got != "15.5" {
		t.Errorf("warning = %q, want the percentage as the number it is", got)
	}
	if !strings.Contains(body, "percent") {
		t.Errorf("page = %q, want it to say which unit it is asking for", body)
	}
}

// spec: thresholds.md#saving — a percentage is saved as the number it is.
func TestThresholdSaveTakesAPercentage(t *testing.T) {
	store := holding(pctSeries)

	rec := saveForm(t, store, pctAddress, url.Values{
		"direction": {"below"}, "warning": {"12.5"}, "critical": {"5"}, "anomalies": {"show"},
	}, "")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body)
	}
	held := store.held[key(pctSeries)]
	if held.Warning == nil || *held.Warning != 12.5 || held.Critical == nil || *held.Critical != 5 {
		t.Fatalf("stored %+v, want the two percentages", held)
	}
}

// spec: thresholds.md#form — a query that names no series at all is refused as a page.
func TestThresholdFormRefusesAQueryThatNamesNoSeries(t *testing.T) {
	store := holding(dataSeries)

	for _, target := range []string{
		"/thresholds",
		"/thresholds?metric=disk.free_bytes",
		"/thresholds?metric=&node=server-b",
		"/thresholds?metric=disk.free_bytes&node=server-b&window=7d",
	} {
		if status, body := openForm(t, store, target); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", target, status, body)
		}
	}
}

// spec: thresholds.md#form — the Russian form shows a size the way its own fields take
// one, so a reader can save what they were shown.
func TestTheRussianFormShowsASizeItWouldAccept(t *testing.T) {
	store := holding(dataSeries)
	warning := 10737418240.0
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: storage.Below, Warning: &warning}

	_, body := openForm(t, store, dataAddress+"&lang=ru")

	shown := valueOf(t, body, "warning")
	if shown != "10737418240" {
		t.Fatalf("warning = %q, want the stored number itself", shown)
	}
	rec := saveForm(t, store, dataAddress+"&lang=ru", url.Values{
		"direction": {"below"}, "warning": {shown}, "anomalies": {"show"},
	}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("saving what the page showed = %d, want 303: %s", rec.Code, rec.Body)
	}
	if held := store.held[key(dataSeries)]; held.Warning == nil || *held.Warning != warning {
		t.Fatalf("stored %+v, want the value unchanged by a round trip through the form", held)
	}
}

// spec: thresholds.md#saving — a number that stops being finite once its unit is applied
// is refused, so the store never holds one.
func TestThresholdSaveRefusesASizeThatOverflows(t *testing.T) {
	store := holding(dataSeries)

	rec := saveForm(t, store, dataAddress, url.Values{"direction": {"below"}, "warning": {"1e300GB"}, "anomalies": {"show"}}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	if held, stored := store.held[key(dataSeries)]; stored {
		t.Fatalf("stored %+v, want nothing: an infinity is no threshold", held)
	}
}

// spec: thresholds.md#form — the address names one series, and label sets are compared
// as the storage keys them: two different sets must not read alike.
func TestThresholdFormTellsLabelSetsApart(t *testing.T) {
	odd := storage.SeriesRef{
		Node:   "server-b",
		Metric: "disk.free_bytes",
		Labels: map[string]string{"a": "b,c=d"},
	}
	store := holding(odd)

	status, body := openForm(t, store, "/thresholds?label.a=b&label.c=d&metric=disk.free_bytes&node=server-b")

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: a different label set is a different series: %s", status, body)
	}
}

// spec: thresholds.md#form — the page is not refreshed under the reader: nothing on a
// form changes by itself, and a refresh would replace what they typed or the refusal
// they are reading.
func TestThresholdPageIsNotRefreshed(t *testing.T) {
	store := holding(dataSeries)

	_, body := openForm(t, store, dataAddress)
	if strings.Contains(body, `<meta name="monitor-live"`) {
		t.Fatalf("the form page declares itself live: %s", body)
	}

	rec := saveForm(t, store, dataAddress, url.Values{"direction": {"below"}, "warning": {"nonsense"}, "anomalies": {"show"}}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `<meta name="monitor-live"`) {
		t.Fatalf("the refused save declares itself live, so the error would be refreshed away: %s", rec.Body)
	}
}

// spec: thresholds.md#saving — a save from another site is refused before the hub reads
// anything: what it names is none of its business either.
func TestACrossOriginSaveIsRefusedBeforeTheSeriesIsRead(t *testing.T) {
	store := holding(dataSeries)

	// An address naming no series at all: the refusal is still the origin's, which is
	// what makes the proxy's own check answerable ([nginx-requirements.md]).
	rec := saveForm(t, store, "/thresholds", url.Values{}, "https://elsewhere.example")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body)
	}
	if len(store.held) != 0 {
		t.Fatalf("stored %+v, want nothing", store.held)
	}
}

// spec: thresholds.md#form — any series offers the switch, on unless the series is excluded,
// and an unreadable threshold does not hide the exclusion stored beside it.
func TestThresholdFormShowsTheSwitch(t *testing.T) {
	store := holding(dataSeries)
	_, body := openForm(t, store, dataAddress)
	if !checked(body, "show") || checked(body, "exclude") {
		t.Errorf("page = %q, want the switch on", body)
	}

	store.excluded[key(dataSeries)] = true
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: "sideways"}
	_, body = openForm(t, store, dataAddress)
	if !checked(body, "exclude") || checked(body, "show") {
		t.Errorf("page = %q, want the switch off as stored", body)
	}
}

// spec: thresholds.md#saving — the switch turned off excludes the series, and on again
// clears it; with both values blank the threshold goes and the exclusion stays.
func TestThresholdSaveStoresTheSwitch(t *testing.T) {
	store := holding(dataSeries)
	warning := 20e9
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: storage.Below, Warning: &warning}

	if rec := saveForm(t, store, dataAddress, excluding(form("below", "", "")), ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if _, stored := store.held[key(dataSeries)]; stored || !store.excluded[key(dataSeries)] {
		t.Fatalf("held %v, excluded %v; want the threshold gone and the exclusion stored", store.held, store.excluded)
	}

	if rec := saveForm(t, store, dataAddress, form("below", "", ""), ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if store.excluded[key(dataSeries)] {
		t.Fatal("still excluded after the switch was turned on")
	}
}

// spec: thresholds.md#saving — the switch turned off in a save refused for its values
// stores nothing, and the form shows the switch as the reader left it.
func TestARefusedSaveKeepsTheSwitchAsTyped(t *testing.T) {
	store := holding(dataSeries)
	rec := saveForm(t, store, dataAddress, excluding(form("below", "10GB", "20GB")), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.held) != 0 || len(store.excluded) != 0 {
		t.Fatalf("held %v, excluded %v; want nothing stored", store.held, store.excluded)
	}
	if body := rec.Body.String(); !checked(body, "exclude") {
		t.Errorf("page = %q, want the switch off as typed", body)
	}
}

// spec: thresholds.md#saving — a save carrying no value for the switch is refused.
func TestASaveWithoutTheSwitchIsRefused(t *testing.T) {
	for _, value := range []string{"", "maybe"} {
		store := holding(dataSeries)
		values := form("below", "20GB", "10GB")
		if value == "" {
			values.Del("anomalies")
		} else {
			values.Set("anomalies", value)
		}
		rec := saveForm(t, store, dataAddress, values, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("switch %q answered %d, want 400", value, rec.Code)
		}
		if len(store.held) != 0 || len(store.excluded) != 0 {
			t.Errorf("switch %q stored %v and %v, want nothing", value, store.held, store.excluded)
		}
		if !strings.Contains(rec.Body.String(), "whether this series may be shown as unusual") {
			t.Errorf("switch %q: the refusal does not name the switch", value)
		}
	}
}

// spec: thresholds.md#form — the switch in the reader's language.
func TestThresholdSwitchSpeaksTheReadersLanguage(t *testing.T) {
	for target, wants := range map[string][]string{
		dataAddress:              {"When this series is unusual", "show it on mission control", "never show it as unusual"},
		dataAddress + "&lang=ru": {"Когда серия необычна", "показывать в центре управления", "никогда не показывать как необычную"},
	} {
		_, body := openForm(t, holding(dataSeries), target)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not say %q", target, want)
			}
		}
	}
}

// An exclusion that cannot be read fails the form the way a threshold that cannot be read
// does.
func TestThresholdFormFailsWhenTheExclusionCannotBeRead(t *testing.T) {
	store := holding(dataSeries)
	store.exclusionErr = errFailed
	if status, body := openForm(t, store, dataAddress); status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", status, body)
	}
}
