package storage

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func value(v float64) *float64 { return &v }

func save(t *testing.T, db *SQLite, th Threshold) {
	t.Helper()
	if err := db.SaveThreshold(context.Background(), th); err != nil {
		t.Fatalf("SaveThreshold: %v", err)
	}
}

func thresholds(t *testing.T, db *SQLite) []Threshold {
	t.Helper()
	got, err := db.Thresholds(context.Background())
	if err != nil {
		t.Fatalf("Thresholds: %v", err)
	}
	return got
}

func bytesRef(node, mount string) SeriesRef {
	return SeriesRef{Node: node, Metric: "disk.free_bytes", Labels: map[string]string{"mount": mount}}
}

// spec: thresholds.md#saving — a saved direction and two values come back as they went in.
func TestThresholdRoundTrips(t *testing.T) {
	db := open(t)
	want := Threshold{Series: bytesRef("server-b", "/"), Direction: Below, Warning: value(10e9), Critical: value(4e9)}
	save(t, db, want)

	got, ok, err := db.ThresholdOf(context.Background(), want.Series)
	if err != nil {
		t.Fatalf("ThresholdOf: %v", err)
	}
	if !ok {
		t.Fatal("ThresholdOf: not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ThresholdOf = %+v, want %+v", got, want)
	}
}

// spec: thresholds.md#form — a series nobody configured has no threshold to read.
func TestThresholdOfUnconfiguredSeries(t *testing.T) {
	db := open(t)
	_, ok, err := db.ThresholdOf(context.Background(), bytesRef("server-b", "/"))
	if err != nil {
		t.Fatalf("ThresholdOf: %v", err)
	}
	if ok {
		t.Fatal("ThresholdOf: found a threshold nobody stored")
	}
}

// spec: thresholds.md#saving — a form is a statement of the whole configuration, not a
// patch: the later save is what is stored, down to a value it leaves out.
func TestSaveThresholdReplacesWhatWasThere(t *testing.T) {
	db := open(t)
	ref := bytesRef("server-b", "/")
	save(t, db, Threshold{Series: ref, Direction: Below, Warning: value(10e9), Critical: value(4e9)})
	save(t, db, Threshold{Series: ref, Direction: Above, Warning: value(4)})

	got, _, err := db.ThresholdOf(context.Background(), ref)
	if err != nil {
		t.Fatalf("ThresholdOf: %v", err)
	}
	want := Threshold{Series: ref, Direction: Above, Warning: value(4)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ThresholdOf = %+v, want %+v", got, want)
	}
}

// spec: thresholds.md#saving — clearing both values removes the configuration.
func TestDeleteThresholdRemovesIt(t *testing.T) {
	db := open(t)
	ref := bytesRef("server-b", "/")
	save(t, db, Threshold{Series: ref, Direction: Below, Warning: value(10e9)})
	if err := db.DeleteThreshold(context.Background(), ref); err != nil {
		t.Fatalf("DeleteThreshold: %v", err)
	}
	if _, ok, _ := db.ThresholdOf(context.Background(), ref); ok {
		t.Fatal("ThresholdOf: the threshold outlived its removal")
	}
	if err := db.DeleteThreshold(context.Background(), ref); err != nil {
		t.Fatalf("DeleteThreshold of nothing: %v", err)
	}
}

// spec: state.md#ordering — subjects come by node, then metric, then labels.
func TestThresholdsComeOrdered(t *testing.T) {
	db := open(t)
	save(t, db, Threshold{Series: bytesRef("server-b", "/data"), Direction: Below, Warning: value(1)})
	save(t, db, Threshold{Series: bytesRef("server-b", "/"), Direction: Below, Warning: value(2)})
	save(t, db, Threshold{Series: ref("server-b", "/"), Direction: Below, Warning: value(3)})
	save(t, db, Threshold{Series: bytesRef("laptop-a", "/"), Direction: Below, Warning: value(4)})

	var got [][2]string
	for _, th := range thresholds(t, db) {
		got = append(got, [2]string{th.Series.Node, th.Series.Metric + " " + th.Series.Labels["mount"]})
	}
	want := [][2]string{
		{"laptop-a", "disk.free_bytes /"},
		{"server-b", "disk.free_bytes /"},
		{"server-b", "disk.free_bytes /data"},
		{"server-b", "disk.free_pct /"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Thresholds order = %v, want %v", got, want)
	}
}

// spec: ingest.md#storage — the series keeps the sensor its newest value named.
func TestSeriesKeepsSensorOfNewestValue(t *testing.T) {
	db := open(t)
	m := pct("/", collected, 50)
	m.Sensor = "disk"
	store(t, db, "server-b", m)

	late := pct("/", collected.Add(-time.Hour), 51)
	late.Sensor = "elsewhere"
	store(t, db, "server-b", late)

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-2*time.Hour))
	if len(got) != 1 {
		t.Fatalf("Newest returned %d series, want 1", len(got))
	}
	if got[0].Sensor != "disk" {
		t.Fatalf("sensor = %q, want %q: a late measurement does not rename the series", got[0].Sensor, "disk")
	}
}

// spec: ingest.md#storage — a measurement naming no sensor leaves the series without one.
func TestSeriesWithoutSensor(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected, 50))

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	if len(got) != 1 {
		t.Fatalf("Newest returned %d series, want 1", len(got))
	}
	if got[0].Sensor != "" {
		t.Fatalf("sensor = %q, want empty", got[0].Sensor)
	}
}
