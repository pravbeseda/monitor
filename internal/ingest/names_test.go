package ingest_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// declaring is a request whose disk sensor declares the given manifest fields, beside one
// measurement of disk.free_bytes.
func declaring(fields string) string {
	return `{
  "node": "laptop-a", "agent_version": "0.1.0", "config_version": "", "ts": "2026-08-28T10:00:00Z",
  "manifest": [{"sensor": "disk", "applicable": true, ` + fields + `}],
  "measurements": [{"metric": "disk.free_bytes", "labels": {"mount": "/"}, "value": 1}]
}`
}

// namesOf posts body and returns the names ingest handed storage, sorted for comparison.
func namesOf(t *testing.T, body string) []string {
	t.Helper()
	h, store, _ := newHandler(t)
	rec := post(t, h, bearer(), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	var got []string
	for _, name := range store.saved[0].Names {
		got = append(got, string(name.Kind)+" "+name.Key+" "+string(name.Locale)+" "+name.Text)
	}
	slices.Sort(got)
	return got
}

func expect(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("names = %q, want %q", got, want)
	}
}

// spec: metric-names.md#ingest — a name of a metric the request carries is kept, in every
// declared locale, and a family a measurement belongs to with it.
func TestKeepsTheNamesOfWhatTheRequestCarries(t *testing.T) {
	got := namesOf(t, declaring(`
		"families": {"disk": {"en": "Disk", "ru": "Диск"}},
		"names": {"disk.free_bytes": {"en": "Free on {mount}", "ru": "Свободно {mount}"}}`))
	expect(t, got,
		"family disk en Disk", "family disk ru Диск",
		"metric disk.free_bytes en Free on {mount}", "metric disk.free_bytes ru Свободно {mount}")
}

// spec: metric-names.md#ingest — a metric or a family the request carries no measurement of
// is not named by it.
func TestIgnoresNamesOfWhatTheRequestDoesNotCarry(t *testing.T) {
	got := namesOf(t, declaring(`
		"families": {"load": {"en": "Load"}},
		"names": {"disk.free_pct": {"en": "Free on {mount}"}, "load.avg_1m": {"en": "Load"}}`))
	expect(t, got)
}

// spec: metric-names.md#ingest — of two entries naming one metric, the later one wins.
func TestTheLaterManifestEntryWins(t *testing.T) {
	body := strings.Replace(declaring(`"names": {"disk.free_bytes": {"en": "First"}}`),
		`}],`, `}, {"sensor": "other", "applicable": true, "names": {"disk.free_bytes": {"en": "Second"}}}],`, 1)
	expect(t, namesOf(t, body), "metric disk.free_bytes en Second")
}

// spec: metric-names.md#ingest — an entry without names is an older agent's, and accepted.
func TestAcceptsAManifestWithoutNames(t *testing.T) {
	expect(t, namesOf(t, strings.ReplaceAll(validBody, "%s", "")))
}

// spec: metric-names.md#ingest — a locale the interface does not have is dropped alone.
func TestIgnoresAnUnknownLocale(t *testing.T) {
	expect(t, namesOf(t, declaring(`"names": {"disk.free_bytes": {"de": "Frei", "en": "Free"}}`)),
		"metric disk.free_bytes en Free")
}

// spec: metric-names.md#ingest — an empty name, one over 80 characters, and one with a
// control or format character are each dropped alone.
func TestIgnoresANameItCannotShow(t *testing.T) {
	for _, bad := range []string{`""`, `"` + strings.Repeat("x", 81) + `"`, `"Free\nresolved"`, `"Free‮on"`} {
		got := namesOf(t, declaring(`"names": {"disk.free_bytes": {"en": `+bad+`, "ru": "Свободно"}}`))
		expect(t, got, "metric disk.free_bytes ru Свободно")
	}
}

// spec: metric-names.md#ingest — names that are not a map of maps of strings are dropped,
// and the request is still accepted.
func TestIgnoresMalformedNames(t *testing.T) {
	for _, bad := range []string{`"names": "x"`, `"names": {"disk.free_bytes": "Free"}`, `"families": [1]`} {
		expect(t, namesOf(t, declaring(bad)))
	}
}

// spec: metric-names.md#ingest — a name belongs to the metric, whichever entry declares it.
func TestKeepsANameDeclaredByAnotherSensorsEntry(t *testing.T) {
	body := strings.Replace(declaring(`"names": {}`),
		`}],`, `}, {"sensor": "other", "applicable": false, "names": {"disk.free_bytes": {"en": "Free"}}}],`, 1)
	expect(t, namesOf(t, body), "metric disk.free_bytes en Free")
}
