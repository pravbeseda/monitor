package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func failed(at time.Time, value float64, notes ...string) Measurement {
	return Measurement{Metric: "systemd.failed_units", Sensor: "systemd", Value: value, TS: at, Notes: notes}
}

// newestNotes is what the snapshot says the failed-units series of server-b carries now.
func newestNotes(t *testing.T, db *SQLite) []string {
	t.Helper()
	snap, err := db.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, node := range snap.Nodes {
		for _, value := range node.Values {
			if node.Node == "server-b" && value.Metric == "systemd.failed_units" {
				return value.Notes
			}
		}
	}
	t.Fatalf("no failed-units series in %+v", snap.Nodes)
	return nil
}

// spec: ingest.md#notes — the series' newest value carries the notes it came with, in their
// order; a newer value replaces them, notes or none; a late one changes nothing; a duplicate
// keeps the notes stored first.
func TestTheSeriesCarriesItsNewestValuesNotes(t *testing.T) {
	steps := []struct {
		name string
		in   Measurement
		want []string
	}{
		{"two notes, in their order", failed(collected, 2, "b.service (exit-code)", "a.service (timeout)"),
			[]string{"b.service (exit-code)", "a.service (timeout)"}},
		{"a duplicate with other notes", failed(collected, 2, "c.service (exit-code)"),
			[]string{"b.service (exit-code)", "a.service (timeout)"}},
		{"a late value with notes", failed(collected.Add(-time.Hour), 1, "d.service (exit-code)"),
			[]string{"b.service (exit-code)", "a.service (timeout)"}},
		{"the next value, with none", failed(collected.Add(time.Hour), 0), nil},
		{"the next value, with one", failed(collected.Add(2*time.Hour), 1, "e.service (exit-code)"),
			[]string{"e.service (exit-code)"}},
	}
	db := open(t)
	for _, step := range steps {
		store(t, db, "server-b", step.in)
		if got := newestNotes(t, db); !slices.Equal(got, step.want) {
			t.Fatalf("after %s: notes = %q, want %q", step.name, got, step.want)
		}
	}
}

// spec: ingest.md#notes — the hub restarts, and the series' newest value still carries its
// notes.
func TestNotesOutliveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	store(t, db, "server-b", failed(collected, 1, "a.service (exit-code)"))
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if got := newestNotes(t, reopened); !slices.Equal(got, []string{"a.service (exit-code)"}) {
		t.Fatalf("notes after a restart = %q, want the stored one", got)
	}
}

// spec: evaluation.md#messages — a change keeps the notes of the value that produced it,
// whatever the series carries since.
func TestAnEventKeepsTheNotesOfItsValue(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	change := transition(volume("/"), tickOne, "ok", "critical")
	change.Notes = []string{"a.service (exit-code)", "b.service (timeout)"}
	if err := db.ApplyTransition(ctx, change); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	quiet := transition(volume("/data"), tickOne, "ok", "critical")
	if err := db.ApplyTransition(ctx, quiet); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	between, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickOne)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	recent, err := db.RecentEvents(ctx, []string{"server-b"}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	snap, err := db.Snapshot(ctx, []string{"critical"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for name, events := range map[string][]Transition{"between": between, "recent": recent, "owed": snap.Newest} {
		if len(events) != 2 {
			t.Fatalf("%s: %d events, want 2", name, len(events))
		}
		for _, event := range events {
			want := change.Notes
			if event.Labels["mount"] == "/data" {
				want = nil
			}
			if !slices.Equal(event.Notes, want) {
				t.Errorf("%s: notes of %s = %q, want %q", name, event.Labels["mount"], event.Notes, want)
			}
		}
	}
}

// notesStep is the index of the migration that gives the series and the event log their
// notes. A database rewound to it replays that step on rows that already exist.
const notesStep = 11

// Replaying the step keeps every series, its sensor name included, and every event: the
// tables are rebuilt by copying their rows.
func TestReplayingTheNotesStepKeepsTheSeriesAndTheEvents(t *testing.T) {
	if notesStep != len(migrations)-1 {
		t.Fatalf("notesStep = %d is not the last of %d migrations", notesStep, len(migrations))
	}
	path := filepath.Join(t.TempDir(), "monitor.db")
	old, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	store(t, old, "server-b", failed(collected, 1, "a.service (exit-code)"))
	if err := old.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if _, err := old.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, notesStep)); err != nil {
		t.Fatalf("rewind the schema version: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()
	series, err := db.Series(ctx, Selection{Metric: "systemd.failed_units"})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series) != 1 || series[0].Sensor != "systemd" {
		t.Fatalf("series = %+v, want the one stored, its sensor kept", series)
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickOne)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 1 || events[0].To != "critical" {
		t.Fatalf("events = %+v, want the one recorded", events)
	}
}

// spec: state.md#listing — a series whose values named different units over time is one
// series: no note becomes a series of its own.
func TestNotesNeverBecomeSeries(t *testing.T) {
	db := open(t)
	store(t, db, "server-b",
		failed(collected, 1, "a.service (exit-code)"),
		failed(collected.Add(time.Hour), 1, "b.service (exit-code)"),
		failed(collected.Add(2*time.Hour), 2, "b.service (exit-code)", "c.service (timeout)"),
	)
	snap, err := db.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Nodes) != 1 || len(snap.Nodes[0].Values) != 1 {
		t.Fatalf("values = %+v, want the one series", snap.Nodes)
	}
	var rows int
	if err := db.db.QueryRow(`SELECT count(*) FROM series`).Scan(&rows); err != nil {
		t.Fatalf("count the series: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d series stored, want 1", rows)
	}
}
