package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *SQLite {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func ingest(node string, receivedAt time.Time, ms ...Measurement) Ingest {
	return Ingest{
		Node:          node,
		AgentVersion:  "0.1.0",
		ConfigVersion: "7",
		ReceivedAt:    receivedAt,
		Manifest:      []SensorStatus{{Sensor: "disk", Applicable: true}},
		Measurements:  ms,
	}
}

var collected = time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

func free(mount string, value float64) Measurement {
	return Measurement{
		Metric: "disk.free_bytes",
		Labels: map[string]string{"mount": mount},
		Value:  value,
		TS:     collected,
	}
}

// spec: ingest.md#storage — valid request: measurements stored, last-seen set to receipt time.
func TestSaveIngestStoresMeasurementsAndLastSeen(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", received, free("/", 123))); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	got := db.measurements(t, "laptop-a")
	if len(got) != 1 || got[0].Value != 123 || got[0].Labels["mount"] != "/" || !got[0].TS.Equal(collected) {
		t.Fatalf("stored measurements = %+v, want one point of 123 at %v on /", got, collected)
	}
	if node := db.node(t, "laptop-a"); !node.LastSeen.Equal(received) {
		t.Errorf("last-seen = %v, want the hub receipt time %v", node.LastSeen, received)
	}
}

// spec: ingest.md#storage — measurements empty: last-seen updated, nothing else.
func TestSaveIngestWithoutMeasurementsUpdatesLastSeen(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 5, 0, 0, time.UTC)

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", received)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	if got := db.measurements(t, "laptop-a"); len(got) != 0 {
		t.Errorf("stored measurements = %+v, want none", got)
	}
	if node := db.node(t, "laptop-a"); !node.LastSeen.Equal(received) {
		t.Errorf("last-seen = %v, want %v", node.LastSeen, received)
	}
}

// spec: ingest.md#storage — identical measurement (same node, metric, labels, ts) is skipped.
func TestSaveIngestIsIdempotent(t *testing.T) {
	db := open(t)
	first := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)
	second := first.Add(5 * time.Minute)

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", first, free("/", 123))); err != nil {
		t.Fatalf("first SaveIngest: %v", err)
	}
	if err := db.SaveIngest(context.Background(), ingest("laptop-a", second, free("/", 123))); err != nil {
		t.Fatalf("retry SaveIngest: %v", err)
	}

	if got := db.measurements(t, "laptop-a"); len(got) != 1 {
		t.Errorf("stored measurements = %+v, want the retry to change nothing", got)
	}
	if node := db.node(t, "laptop-a"); !node.LastSeen.Equal(second) {
		t.Errorf("last-seen = %v, want the retry to advance it to %v", node.LastSeen, second)
	}
}

func TestSaveIngestKeepsMeasurementsOfDifferentVolumesApart(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

	err := db.SaveIngest(context.Background(),
		ingest("laptop-a", received, free("/", 123), free("/data", 456)))
	if err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	if got := db.measurements(t, "laptop-a"); len(got) != 2 {
		t.Errorf("stored measurements = %+v, want one per mount", got)
	}
}

// spec: ingest.md#storage — a manifest that differs replaces the stored one.
func TestSaveIngestReplacesManifest(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", received)); err != nil {
		t.Fatalf("first SaveIngest: %v", err)
	}
	next := ingest("laptop-a", received.Add(time.Minute))
	next.Manifest = []SensorStatus{{Sensor: "disk", Applicable: true}, {Sensor: "battery", Applicable: false}}
	if err := db.SaveIngest(context.Background(), next); err != nil {
		t.Fatalf("second SaveIngest: %v", err)
	}

	node := db.node(t, "laptop-a")
	if len(node.Manifest) != 2 || node.Manifest[1].Sensor != "battery" || node.Manifest[1].Applicable {
		t.Errorf("stored manifest = %+v, want it replaced by the newer one", node.Manifest)
	}
}

func TestSaveIngestKeepsNodesApart(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

	for _, node := range []string{"laptop-a", "server-b"} {
		if err := db.SaveIngest(context.Background(), ingest(node, received, free("/", 123))); err != nil {
			t.Fatalf("SaveIngest for %s: %v", node, err)
		}
	}

	if got := db.measurements(t, "server-b"); len(got) != 1 {
		t.Errorf("server-b measurements = %+v, want its own point", got)
	}
}

func TestOpenSQLiteEnablesWAL(t *testing.T) {
	db := open(t)

	var mode string
	if err := db.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// States feeds the web page: the latest value of every series plus each node's last-seen.
// spec: ingest.md#storage — the uniqueness key holds the timestamp to the millisecond.
func TestSaveIngestKeepsOneMeasurementPerMillisecond(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)
	first := Measurement{Metric: "disk.free_bytes", Labels: map[string]string{"mount": "/"}, Value: 1, TS: collected.Add(100 * time.Microsecond)}
	second := first
	second.Value = 2
	second.TS = collected.Add(900 * time.Microsecond)

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", received, first, second)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	if got := db.measurements(t, "laptop-a"); len(got) != 1 || got[0].Value != 1 {
		t.Errorf("stored %+v, want one reading per millisecond, the first one", got)
	}
}

func TestStatesReturnsTheLatestValueOfEachSeries(t *testing.T) {
	db := open(t)
	first := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)
	older := Measurement{Metric: "disk.free_bytes", Labels: map[string]string{"mount": "/"}, Value: 500, TS: collected}
	newer := Measurement{Metric: "disk.free_bytes", Labels: map[string]string{"mount": "/"}, Value: 400, TS: collected.Add(time.Hour)}

	if err := db.SaveIngest(context.Background(), ingest("laptop-a", first, older, newer, free("/data", 900))); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	states, err := db.States(context.Background())
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(states) != 1 || states[0].Node != "laptop-a" {
		t.Fatalf("states = %+v, want one node", states)
	}
	if !states[0].LastSeen.Equal(first) {
		t.Errorf("last-seen = %v, want %v", states[0].LastSeen, first)
	}
	if len(states[0].Values) != 2 {
		t.Fatalf("values = %+v, want one per series", states[0].Values)
	}
	for _, value := range states[0].Values {
		if value.Labels["mount"] == "/" && value.Value != 400 {
			t.Errorf("value on / = %v, want the newest 400", value.Value)
		}
	}
}

// spec: ingest.md#storage — valid request: node's agent version replaced by the request's.
func TestStatesReturnsTheAgentVersionOfTheLatestRequest(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)
	older := ingest("laptop-a", received)
	newer := ingest("laptop-a", received.Add(5*time.Minute))
	newer.AgentVersion = "0.2.0"

	for _, in := range []Ingest{older, newer} {
		if err := db.SaveIngest(context.Background(), in); err != nil {
			t.Fatalf("SaveIngest: %v", err)
		}
	}

	states, err := db.States(context.Background())
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(states) != 1 || states[0].AgentVersion != "0.2.0" {
		t.Errorf("states = %+v, want the version the upgraded agent reported", states)
	}
}

func TestStatesIncludesANodeThatSentNoMeasurements(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

	if err := db.SaveIngest(context.Background(), ingest("server-b", received)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	states, err := db.States(context.Background())
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(states) != 1 || len(states[0].Values) != 0 {
		t.Errorf("states = %+v, want the node with no values", states)
	}
}

func TestStatesOrdersNodesByName(t *testing.T) {
	db := open(t)
	received := time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)
	for _, node := range []string{"server-b", "laptop-a"} {
		if err := db.SaveIngest(context.Background(), ingest(node, received, free("/", 1))); err != nil {
			t.Fatalf("SaveIngest %s: %v", node, err)
		}
	}

	states, err := db.States(context.Background())
	if err != nil {
		t.Fatalf("States: %v", err)
	}

	if len(states) != 2 || states[0].Node != "laptop-a" || states[1].Node != "server-b" {
		t.Errorf("states = %+v, want them ordered by name", states)
	}
}
