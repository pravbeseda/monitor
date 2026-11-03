package storage

import (
	"context"
	"reflect"
	"testing"
)

// flagRef is a backup job's failure flag on node.
func flagRef(node, backup, job string) SeriesRef {
	return SeriesRef{Node: node, Metric: "backup.failed", Labels: map[string]string{"backup": backup, "job": job}}
}

func flag(ref SeriesRef, value float64) Measurement {
	return Measurement{Metric: ref.Metric, Labels: ref.Labels, Value: value, TS: collected}
}

// failedDefault is the backup sensor's default for its failure flag.
var failedDefault = Threshold{Direction: Above, Warning: value(0)}

// declaring is a request from node storing measurements whose manifest declares defaults.
func declaring(node string, defaults map[string]Threshold, ms ...Measurement) Ingest {
	in := ingest(node, collected, ms...)
	in.Defaults = defaults
	return in
}

func saveIngest(t *testing.T, db *SQLite, in Ingest) {
	t.Helper()
	if err := db.SaveIngest(context.Background(), in); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
}

// storeOutside writes a threshold as a build before seeding did: without marking anything.
func storeOutside(t *testing.T, db *SQLite, th Threshold) {
	t.Helper()
	if err := saveThreshold(context.Background(), db.db, th); err != nil {
		t.Fatalf("saveThreshold: %v", err)
	}
}

func thresholdOf(t *testing.T, db *SQLite, ref SeriesRef) *Threshold {
	t.Helper()
	th, ok, err := db.ThresholdOf(context.Background(), ref)
	if err != nil {
		t.Fatalf("ThresholdOf: %v", err)
	}
	if !ok {
		return nil
	}
	return &th
}

func expectThreshold(t *testing.T, db *SQLite, ref SeriesRef, want *Threshold) {
	t.Helper()
	got := thresholdOf(t, db, ref)
	if want != nil {
		with := *want
		with.Series = ref
		want = &with
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("threshold of %v = %+v, want %+v", ref.Labels, got, want)
	}
}

var defaults = map[string]Threshold{"backup.failed": failedDefault}

// spec: thresholds.md#defaults — the first measurements of two series in a request declaring
// the default are judged by it.
func TestSeedingTheFirstMeasurements(t *testing.T) {
	db := open(t)
	web, vpn := flagRef("server-b", "web", "copy"), flagRef("server-b", "vpn", "copy")
	saveIngest(t, db, declaring("server-b", defaults, flag(web, 0), flag(vpn, 0), flag(web, 0)))
	expectThreshold(t, db, web, &failedDefault)
	expectThreshold(t, db, vpn, &failedDefault)
}

// spec: thresholds.md#defaults — a series stored before this build with no threshold, or
// whose threshold was removed before it, is seeded at its next measurement declaring the
// default; one with a threshold keeps it, and is not seeded once the reader clears it.
func TestSeedingSeriesStoredBefore(t *testing.T) {
	db := open(t)
	bare, kept := flagRef("server-b", "web", "copy"), flagRef("server-b", "web", "verify")
	saveIngest(t, db, ingest("server-b", collected, flag(bare, 0), flag(kept, 0)))
	own := Threshold{Series: kept, Direction: Above, Critical: value(0)}
	storeOutside(t, db, own)

	saveIngest(t, db, declaring("server-b", defaults, flag(bare, 0), flag(kept, 0)))
	expectThreshold(t, db, bare, &failedDefault)
	expectThreshold(t, db, kept, &own)

	configure(t, db, kept, nil, false)
	saveIngest(t, db, declaring("server-b", defaults, flag(kept, 0)))
	expectThreshold(t, db, kept, nil)
}

// spec: thresholds.md#defaults — what a save of this build changed is never seeded over:
// a removal, a threshold set before the default was declared, an edit of a seeded one.
func TestSeedingRespectsWhatTheReaderSaved(t *testing.T) {
	db := open(t)
	removed, early, edited := flagRef("server-b", "web", "copy"), flagRef("server-b", "web", "verify"), flagRef("server-b", "vpn", "copy")
	saveIngest(t, db, declaring("server-b", defaults, flag(removed, 0), flag(edited, 0)))
	saveIngest(t, db, ingest("server-b", collected, flag(early, 0)))

	configure(t, db, removed, nil, false)
	mine := Threshold{Series: early, Direction: Above, Critical: value(0)}
	configure(t, db, early, &mine, false)
	edit := Threshold{Series: edited, Direction: Above, Critical: value(0.5)}
	configure(t, db, edited, &edit, false)

	saveIngest(t, db, declaring("server-b", defaults, flag(removed, 1), flag(early, 1), flag(edited, 1)))
	expectThreshold(t, db, removed, nil)
	expectThreshold(t, db, early, &mine)
	expectThreshold(t, db, edited, &edit)
}

// spec: thresholds.md#defaults — a save that only turns the switch leaves the series to be
// seeded, and seeding leaves the switch alone.
func TestSeedingAfterTheSwitchAlone(t *testing.T) {
	db := open(t)
	ref := flagRef("server-b", "web", "copy")
	saveIngest(t, db, ingest("server-b", collected, flag(ref, 0)))
	configure(t, db, ref, nil, true)
	saveIngest(t, db, declaring("server-b", defaults, flag(ref, 0)))
	expectThreshold(t, db, ref, &failedDefault)
	if !excluded(t, db, ref) {
		t.Error("seeding turned the switch back on")
	}
}

// spec: thresholds.md#defaults — a threshold this build cannot read is left as stored.
func TestSeedingLeavesAnUnreadableThreshold(t *testing.T) {
	db := open(t)
	ref := flagRef("server-b", "web", "copy")
	saveIngest(t, db, ingest("server-b", collected, flag(ref, 0)))
	odd := Threshold{Series: ref, Direction: "sideways", Warning: value(1)}
	storeOutside(t, db, odd)
	saveIngest(t, db, declaring("server-b", defaults, flag(ref, 0)))
	expectThreshold(t, db, ref, &odd)
}

// spec: thresholds.md#defaults — a seeded series keeps what was seeded when a later release
// declares another default; a series not seeded yet takes the new one.
func TestSeedingIsOnce(t *testing.T) {
	db := open(t)
	old, fresh := flagRef("server-b", "web", "copy"), flagRef("server-b", "vpn", "copy")
	saveIngest(t, db, declaring("server-b", defaults, flag(old, 0)))
	next := Threshold{Direction: Above, Critical: value(0)}
	saveIngest(t, db, declaring("server-b", map[string]Threshold{"backup.failed": next}, flag(old, 0), flag(fresh, 0)))
	expectThreshold(t, db, old, &failedDefault)
	expectThreshold(t, db, fresh, &next)
}

// spec: thresholds.md#defaults — a measurement a host stores for the sites node seeds the
// sites node's series.
func TestSeedingTheSeriesOfAnotherNode(t *testing.T) {
	db := open(t)
	m := Measurement{Node: "sites", Metric: "site.server_error_pct", Labels: map[string]string{"site": "blog-a"}, Value: 1, TS: collected}
	share := Threshold{Direction: Above, Critical: value(5)}
	saveIngest(t, db, declaring("server-b", map[string]Threshold{"site.server_error_pct": share}, m))
	expectThreshold(t, db, SeriesRef{Node: "sites", Metric: m.Metric, Labels: m.Labels}, &share)
}

func configureEvery(t *testing.T, db *SQLite, metric string, th *Threshold, own SeriesRef, exclude bool) {
	t.Helper()
	if err := db.ConfigureEvery(context.Background(), metric, th, own, exclude); err != nil {
		t.Fatalf("ConfigureEvery: %v", err)
	}
}

// spec: thresholds.md#all — "Confirm" replaces the threshold of every series of the metric
// on every node, readable or not; the switch is the opened series' only.
func TestConfiguringEverySeries(t *testing.T) {
	db := open(t)
	refs := []SeriesRef{
		flagRef("server-b", "web", "copy"), flagRef("server-b", "vpn", "copy"),
		flagRef("server-c", "web", "copy"), flagRef("server-c", "web", "verify"),
	}
	for _, ref := range refs {
		saveIngest(t, db, ingest(ref.Node, collected, flag(ref, 0)))
	}
	saveIngest(t, db, ingest("server-b", collected, free("/", 1)))
	storeOutside(t, db, Threshold{Series: refs[3], Direction: "sideways", Warning: value(1)})
	configure(t, db, refs[2], nil, true)

	every := Threshold{Direction: Above, Critical: value(0)}
	configureEvery(t, db, "backup.failed", &every, refs[0], true)
	for _, ref := range refs {
		expectThreshold(t, db, ref, &every)
	}
	if got := thresholdOf(t, db, bytesRef("server-b", "/")); got != nil {
		t.Errorf("another metric's series was changed: %+v", got)
	}
	if !excluded(t, db, refs[0]) || excluded(t, db, refs[1]) || !excluded(t, db, refs[2]) {
		t.Error("the switch reached a series other than the opened one")
	}

	t.Run("blank", func(t *testing.T) {
		configureEvery(t, db, "backup.failed", nil, refs[0], false)
		saveIngest(t, db, declaring("server-b", defaults, flag(refs[0], 1), flag(refs[1], 1)))
		for _, ref := range refs {
			expectThreshold(t, db, ref, nil)
		}
	})

	t.Run("a series appearing after", func(t *testing.T) {
		later := flagRef("server-d", "web", "copy")
		saveIngest(t, db, declaring("server-d", defaults, flag(later, 0)))
		expectThreshold(t, db, later, &failedDefault)
	})
}
