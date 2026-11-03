package ingest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// defaultsOf posts body and returns the defaults ingest handed storage, one line each.
func defaultsOf(t *testing.T, body string) []string {
	t.Helper()
	var got []string
	for metric, th := range savedOf(t, body).Defaults {
		line := metric + " " + string(th.Direction)
		if th.Warning != nil {
			line += fmt.Sprintf(" warning %g", *th.Warning)
		}
		if th.Critical != nil {
			line += fmt.Sprintf(" critical %g", *th.Critical)
		}
		got = append(got, line)
	}
	slices.Sort(got)
	return got
}

func expectDefaults(t *testing.T, got []string, want ...string) {
	t.Helper()
	expectSorted(t, "defaults", got, want...)
}

// spec: thresholds.md#defaults — a default of a metric the request carries a measurement of
// reaches storage; one of a metric it does not carry is dropped.
func TestKeepsTheDefaultsOfWhatTheRequestCarries(t *testing.T) {
	expectDefaults(t, defaultsOf(t, declaring(`"thresholds": {
		"disk.free_bytes": {"direction": "below", "warning": 10e9, "critical": 2e9},
		"disk.free_pct": {"direction": "below", "warning": 10}}`)),
		"disk.free_bytes below warning 1e+10 critical 2e+09")
}

// spec: thresholds.md#defaults — a default belongs to the metric, whichever entry declares
// it, and of two entries the later one wins.
func TestADefaultBelongsToTheMetric(t *testing.T) {
	body := strings.Replace(declaring(`"thresholds": {"disk.free_bytes": {"direction": "below", "warning": 1}}`),
		`}],`, `}, {"sensor": "other", "applicable": true, "thresholds": {"disk.free_bytes": {"direction": "below", "warning": 2}}}],`, 1)
	expectDefaults(t, defaultsOf(t, body), "disk.free_bytes below warning 2")
}

// spec: thresholds.md#defaults — an entry with no thresholds is an older agent's, and accepted.
func TestAcceptsAManifestWithoutDefaults(t *testing.T) {
	expectDefaults(t, defaultsOf(t, declaring(`"names": {"disk.free_bytes": {"en": "Free"}}`)))
}

// spec: thresholds.md#defaults — thresholds that are not an object of objects drop every
// default of the entry, never its names or the request.
func TestIgnoresMalformedDefaults(t *testing.T) {
	for _, odd := range []string{"5", "null"} {
		body := declaring(`"names": {"disk.free_bytes": {"en": "Free"}},
			"thresholds": {"disk.free_bytes": {"direction": "below", "warning": 1}, "x": ` + odd + `}`)
		expectDefaults(t, defaultsOf(t, body))
		expect(t, namesOf(t, body), "metric disk.free_bytes en Free")
	}
}

// spec: thresholds.md#defaults — a default with critical only, or a value null, is kept with
// what it says.
func TestKeepsAPartialDefault(t *testing.T) {
	expectDefaults(t, defaultsOf(t, declaring(`"thresholds": {"disk.free_bytes": {"direction": "below", "warning": null, "critical": 3}}`)),
		"disk.free_bytes below critical 3")
}

// spec: thresholds.md#defaults — a default the store would not hold is dropped alone.
func TestIgnoresADefaultTheStoreWouldNotHold(t *testing.T) {
	for _, bad := range []string{
		`{"warning": 1}`,
		`{"direction": "sideways", "warning": 1}`,
		`{"direction": "below", "warning": "30h"}`,
		`{"direction": "below", "warning": 1e400}`,
		`{"direction": "below"}`,
		`{"direction": "below", "warning": null, "critical": null}`,
		`{"direction": "below", "warning": 1, "critical": 1}`,
		`{"direction": "above", "warning": 2, "critical": 1}`,
		`{"direction": "below", "warning": 1, "warn": 2}`,
		`{"Direction": "below", "warning": 1}`,
	} {
		t.Run(bad, func(t *testing.T) {
			expectDefaults(t, defaultsOf(t, declaring(`"thresholds": {"disk.free_bytes": `+bad+`}`)))
		})
	}
	t.Run("beside a good one", func(t *testing.T) {
		body := strings.Replace(declaring(`"thresholds": {"disk.free_pct": {"direction": "below"}, "disk.free_bytes": {"direction": "below", "warning": 1}}`),
			`"measurements": [`, `"measurements": [{"metric": "disk.free_pct", "labels": {"mount": "/"}, "value": 1}, `, 1)
		expectDefaults(t, defaultsOf(t, body), "disk.free_bytes below warning 1")
	})
}

// spec: backup-sensor.md#applicability — the backup sensor's defaults, as the agent sends
// them (agent_test.go pins the entry) and the hub reads them, are both accepted.
func TestTheBackupSensorsDefaultsAreAccepted(t *testing.T) {
	body := `{
  "node": "laptop-a", "agent_version": "0.1.0", "config_version": "", "ts": "2026-08-28T10:00:00Z",
  "manifest": [{"sensor": "backup", "applicable": true, "thresholds": ` +
		`{"backup.failed":{"direction":"above","warning":0},"backup.success_age_seconds":{"direction":"above","warning":108000,"critical":172800}}}],
  "measurements": [
    {"metric": "backup.failed", "labels": {"backup": "web", "job": "copy"}, "value": 0},
    {"metric": "backup.success_age_seconds", "labels": {"backup": "web", "job": "copy"}, "value": 60},
    {"metric": "backup.archives", "labels": {"backup": "web", "job": "copy"}, "value": 3}]
}`
	expectDefaults(t, defaultsOf(t, body),
		"backup.failed above warning 0",
		"backup.success_age_seconds above warning 108000 critical 172800")
}
