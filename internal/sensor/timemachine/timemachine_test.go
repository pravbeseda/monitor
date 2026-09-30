package timemachine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/timemachine"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

func collect(t *testing.T, destinations ...timemachine.Destination) []sensor.Measurement {
	t.Helper()
	s := timemachine.New(func(context.Context) ([]timemachine.Destination, error) {
		return destinations, nil
	}, func() time.Time { return collected })
	got, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, m := range got {
		if m.Metric != "timemachine.backup_age_seconds" || len(m.Labels) != 1 || !m.TS.Equal(collected) {
			t.Fatalf("unexpected measurement %+v", m)
		}
	}
	return got
}

// ages maps each measurement's destination label to its value.
func ages(got []sensor.Measurement) map[string]float64 {
	out := map[string]float64{}
	for _, m := range got {
		out[m.Labels["destination"]] = m.Value
	}
	return out
}

func ago(d time.Duration) time.Time { return collected.Add(-d) }

func assertAges(t *testing.T, got []sensor.Measurement, want map[string]float64) {
	t.Helper()
	have := ages(got)
	if len(got) != len(want) || len(have) != len(want) {
		t.Fatalf("measurements %+v, want %v", got, want)
	}
	for name, age := range want {
		if have[name] != age {
			t.Fatalf("age of %q = %v, want %v (all: %v)", name, have[name], age, have)
		}
	}
}

// spec: timemachine-sensor.md#age — whole seconds since the latest backup, truncated.
func TestCollectCountsWholeSecondsSinceTheLatestBackup(t *testing.T) {
	got := collect(t, timemachine.Destination{
		Name:    "Backups",
		Backups: []time.Time{ago(30 * 24 * time.Hour), ago(26*time.Hour + 900*time.Millisecond)},
	})
	assertAges(t, got, map[string]float64{"Backups": 93600})
}

// spec: timemachine-sensor.md#age — the latest backup counts, whatever order they are listed in.
func TestCollectTakesTheLatestBackupInAnyOrder(t *testing.T) {
	got := collect(t, timemachine.Destination{
		Name:    "Backups",
		Backups: []time.Time{ago(time.Hour), ago(48 * time.Hour), ago(24 * time.Hour)},
	})
	assertAges(t, got, map[string]float64{"Backups": 3600})
}

// spec: timemachine-sensor.md#age — one measurement per destination, labelled by its name.
func TestCollectReportsEachDestination(t *testing.T) {
	got := collect(t,
		timemachine.Destination{Name: "Backups A", Backups: []time.Time{ago(time.Hour)}},
		timemachine.Destination{Name: "Backups B", Backups: []time.Time{ago(2 * time.Hour)}},
	)
	assertAges(t, got, map[string]float64{"Backups A": 3600, "Backups B": 7200})
}

// spec: timemachine-sensor.md#age — a name is carried verbatim.
func TestCollectCarriesTheNameVerbatim(t *testing.T) {
	name := "Резервные копии — Mac"
	got := collect(t, timemachine.Destination{Name: name, Backups: []time.Time{ago(time.Minute)}})
	assertAges(t, got, map[string]float64{name: 60})
}

// spec: timemachine-sensor.md#age — a destination holding no backup is left out.
func TestCollectLeavesOutADestinationWithoutBackups(t *testing.T) {
	got := collect(t,
		timemachine.Destination{Name: "Empty"},
		timemachine.Destination{Name: "Backups", Backups: []time.Time{ago(time.Hour)}},
	)
	assertAges(t, got, map[string]float64{"Backups": 3600})
}

// spec: timemachine-sensor.md#age — a destination with no recorded name is left out.
func TestCollectLeavesOutADestinationWithoutAName(t *testing.T) {
	got := collect(t,
		timemachine.Destination{Backups: []time.Time{ago(time.Hour)}},
		timemachine.Destination{Name: "Backups", Backups: []time.Time{ago(time.Hour)}},
	)
	assertAges(t, got, map[string]float64{"Backups": 3600})
}

// spec: timemachine-sensor.md#age — two destinations sharing a name are both left out.
func TestCollectLeavesOutDestinationsSharingAName(t *testing.T) {
	got := collect(t,
		timemachine.Destination{Name: "Twin", Backups: []time.Time{ago(time.Hour)}},
		timemachine.Destination{Name: "Twin", Backups: []time.Time{ago(2 * time.Hour)}},
		timemachine.Destination{Name: "Other", Backups: []time.Time{ago(3 * time.Hour)}},
	)
	assertAges(t, got, map[string]float64{"Other": 10800})
}

// spec: timemachine-sensor.md#age — a latest backup ahead of the clock is no reading; one at
// the clock reads 0.
func TestCollectLeavesOutABackupInTheFuture(t *testing.T) {
	got := collect(t,
		timemachine.Destination{Name: "Ahead", Backups: []time.Time{ago(time.Hour), collected.Add(time.Minute)}},
		timemachine.Destination{Name: "Backups", Backups: []time.Time{ago(time.Hour)}},
		timemachine.Destination{Name: "Just now", Backups: []time.Time{collected}},
	)
	assertAges(t, got, map[string]float64{"Backups": 3600, "Just now": 0})
}

// spec: timemachine-sensor.md#age — no destination is no measurement and no error.
func TestCollectWithoutDestinations(t *testing.T) {
	if got := collect(t); len(got) != 0 {
		t.Fatalf("measurements %+v, want none", got)
	}
}

// spec: timemachine-sensor.md#age — unreadable preferences are an error, never a zero.
func TestCollectFailsWhenThePreferencesCannotBeRead(t *testing.T) {
	s := timemachine.New(func(context.Context) ([]timemachine.Destination, error) {
		return nil, errors.New("no preferences")
	}, time.Now)
	if got, err := s.Collect(context.Background()); err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and an error", got, err)
	}
}

// spec: timemachine-sensor.md#age — enabled on Linux, the sensor stays quiet.
func TestCollectWithoutTimeMachine(t *testing.T) {
	s := timemachine.New(nil, time.Now)
	if got, err := s.Collect(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want nothing and no error", got, err)
	}
}

// spec: timemachine-sensor.md#applicability — applicable where Time Machine exists.
func TestManifestEntry(t *testing.T) {
	macOS := timemachine.New(func(context.Context) ([]timemachine.Destination, error) { return nil, nil }, time.Now)
	if macOS.Name() != "timemachine" || !macOS.Applicable() {
		t.Fatalf("manifest = %q/%v, want timemachine, applicable", macOS.Name(), macOS.Applicable())
	}
	if linux := timemachine.New(nil, time.Now); linux.Applicable() {
		t.Fatal("applicable without a source, want not applicable")
	}
}
