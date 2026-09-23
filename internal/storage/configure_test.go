package storage

import (
	"context"
	"reflect"
	"testing"
)

func excluded(t *testing.T, db *SQLite, ref SeriesRef) bool {
	t.Helper()
	got, err := db.Excluded(context.Background(), ref)
	if err != nil {
		t.Fatalf("Excluded: %v", err)
	}
	return got
}

func configure(t *testing.T, db *SQLite, ref SeriesRef, th *Threshold, exclude bool) {
	t.Helper()
	if err := db.Configure(context.Background(), ref, th, exclude); err != nil {
		t.Fatalf("Configure: %v", err)
	}
}

// spec: thresholds.md#saving — the switch turned off excludes the series, on again clears it.
func TestAnExclusionRoundTrips(t *testing.T) {
	db := open(t)
	ref := bytesRef("server-b", "/")
	if excluded(t, db, ref) {
		t.Fatal("excluded before anything was saved")
	}
	configure(t, db, ref, nil, true)
	if !excluded(t, db, ref) {
		t.Fatal("not excluded after the switch was turned off")
	}
	configure(t, db, ref, nil, true)
	configure(t, db, ref, nil, false)
	if excluded(t, db, ref) {
		t.Fatal("still excluded after the switch was turned on")
	}
}

// spec: thresholds.md#saving — the switch turned off with both values blank: the threshold
// removed and the exclusion stored, the two separate.
func TestAnExclusionOutlivesItsThreshold(t *testing.T) {
	db := open(t)
	ref := bytesRef("server-b", "/")
	th := Threshold{Series: ref, Direction: Below, Warning: value(10e9)}
	configure(t, db, ref, &th, false)
	configure(t, db, ref, nil, true)
	if _, ok, _ := db.ThresholdOf(context.Background(), ref); ok {
		t.Fatal("the threshold survived a save with both values blank")
	}
	if !excluded(t, db, ref) {
		t.Fatal("the exclusion was not stored")
	}
}

// spec: thresholds.md#saving — a save states the whole configuration: threshold and switch.
func TestConfigureStoresTheThresholdToo(t *testing.T) {
	db := open(t)
	ref := bytesRef("server-b", "/")
	want := Threshold{Series: ref, Direction: Above, Critical: value(8)}
	configure(t, db, ref, &want, true)
	got, ok, err := db.ThresholdOf(context.Background(), ref)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("ThresholdOf = %+v, %v, %v; want %+v", got, ok, err, want)
	}
}

// The state reads exclusions in the same view as the rest of the snapshot.
func TestTheSnapshotCarriesExclusions(t *testing.T) {
	db := open(t)
	configure(t, db, bytesRef("server-b", "/"), nil, true)
	configure(t, db, bytesRef("server-a", "/data"), nil, true)
	snap, err := db.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []SeriesRef{bytesRef("server-a", "/data"), bytesRef("server-b", "/")}
	if !reflect.DeepEqual(snap.Excluded, want) {
		t.Fatalf("Excluded = %+v, want %+v", snap.Excluded, want)
	}
}
