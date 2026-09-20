package storage

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var (
	tickOne = time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	tickTwo = tickOne.Add(time.Minute)
)

func volume(mount string) Subject {
	return Subject{Node: "server-b", Metric: "disk", Labels: map[string]string{"mount": mount, "fs": "ext4"}}
}

func transition(subject Subject, at time.Time, from, to string) Transition {
	return Transition{
		Subject:   subject,
		At:        at,
		From:      from,
		To:        to,
		FromSince: at.Add(-time.Hour),
		Readings:  map[string]float64{"disk.free_bytes": 9e9, "disk.free_pct": 7.03},
	}
}

func (s *SQLite) statesByMount(t *testing.T) map[string]State {
	t.Helper()
	loaded, err := loadStates(context.Background(), s.db)
	if err != nil {
		t.Fatalf("read states: %v", err)
	}
	out := make(map[string]State, len(loaded))
	for _, state := range loaded {
		out[state.Labels["mount"]] = state
	}
	return out
}

// spec: evaluation.md#persistence-and-restart — a level change writes the state and the
// event together, and `since` is the instant of the change.
func TestApplyTransitionWritesBoth(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	state := db.statesByMount(t)["/"]
	if state.Level != "warning" || !state.Since.Equal(tickOne) {
		t.Fatalf("the state is %q since %v", state.Level, state.Since)
	}
	if !state.LastNotifiedAt.IsZero() {
		t.Fatalf("a fresh state claims a delivery at %v", state.LastNotifiedAt)
	}

	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 1 || events[0].From != "ok" || events[0].To != "warning" {
		t.Fatalf("the event log holds %+v", events)
	}
	if events[0].Readings["disk.free_bytes"] != 9e9 {
		t.Fatalf("readings = %+v, want the values that produced the change", events[0].Readings)
	}
	if !events[0].FromSince.Equal(tickOne.Add(-time.Hour)) {
		t.Fatalf("from_since = %v, want when the subject entered the level it left", events[0].FromSince)
	}
}

// spec: evaluation.md#persistence-and-restart — a reader never sees one without the other,
// so a failure inside the transaction leaves neither the level nor the event.
func TestApplyTransitionIsAtomic(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	// The log refuses the append rather than being dropped, so both tables can still be
	// read afterwards and the assertions below mean something.
	if _, err := db.db.Exec(`
		CREATE TRIGGER refuse_events BEFORE INSERT ON events
		BEGIN SELECT RAISE(ABORT, 'the event log is unavailable'); END`); err != nil {
		t.Fatalf("break the event log: %v", err)
	}
	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "warning")); err == nil {
		t.Fatal("a transition was reported stored with no event log to store it in")
	}
	if _, err := db.db.Exec(`DROP TRIGGER refuse_events`); err != nil {
		t.Fatalf("restore the event log: %v", err)
	}
	if got := db.statesByMount(t); len(got) != 0 {
		t.Fatalf("states = %+v, want none after a failed transition", got)
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none after a failed transition", events)
	}
}

// A transition that cannot be encoded is refused before anything is written.
func TestApplyTransitionRefusesUnencodableReadings(t *testing.T) {
	db := open(t)
	broken := transition(volume("/"), tickOne, "ok", "warning")
	broken.Readings = map[string]float64{"disk.free_bytes": math.NaN()}
	if err := db.ApplyTransition(context.Background(), broken); err == nil {
		t.Fatal("a reading that is not a number was stored")
	}
}

// spec: evaluation.md#persistence-and-restart — a subject seen for the first time at ok
// appears with its `since` and writes no event.
func TestSaveStateWritesNoEvent(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.SaveState(ctx, State{Subject: volume("/"), Level: "ok", Since: tickOne}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if got := db.statesByMount(t)["/"]; got.Level != "ok" || !got.Since.Equal(tickOne) {
		t.Fatalf("the state is %q since %v", got.Level, got.Since)
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("a first sighting at ok wrote %+v", events)
	}
}

// spec: evaluation.md#persistence-and-restart — the level holding leaves `since` alone
// while a delivery may still be recorded.
func TestRecordNotifiedLeavesTheLevelAlone(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if err := db.RecordNotified(ctx, volume("/"), tickTwo); err != nil {
		t.Fatalf("RecordNotified: %v", err)
	}

	got := db.statesByMount(t)["/"]
	if !got.Since.Equal(tickOne) {
		t.Fatalf("since = %v, want the delivery to leave it at %v", got.Since, tickOne)
	}
	if !got.LastNotifiedAt.Equal(tickTwo) {
		t.Fatalf("last-notified = %v, want %v", got.LastNotifiedAt, tickTwo)
	}

	// The next level change must not forget what the subject was already told about, or a
	// restart would re-notify every open incident.
	if err := db.ApplyTransition(ctx, transition(volume("/"), tickTwo.Add(time.Minute), "critical", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if got := db.statesByMount(t)["/"]; !got.LastNotifiedAt.Equal(tickTwo) {
		t.Fatalf("last-notified = %v, want a level change to leave it at %v", got.LastNotifiedAt, tickTwo)
	}
}

// A delivery recorded against a subject that has no state is no record at all, and the
// message would go out again on every tick that follows.
func TestRecordNotifiedRefusesAnUnknownSubject(t *testing.T) {
	db := open(t)
	if err := db.RecordNotified(context.Background(), volume("/absent"), tickOne); err == nil {
		t.Fatal("a delivery was recorded against a subject with no state")
	}
}

// spec: evaluation.md#persistence-and-restart — a stored level this build does not know is
// replaced as if the subject were new: no event, and what it was told about is kept.
func TestSaveStateReplacesAStoredLevel(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "puce")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if err := db.RecordNotified(ctx, volume("/"), tickOne); err != nil {
		t.Fatalf("RecordNotified: %v", err)
	}
	if err := db.SaveState(ctx, State{Subject: volume("/"), Level: "ok", Since: tickTwo}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got := db.statesByMount(t)["/"]
	if got.Level != "ok" || !got.Since.Equal(tickTwo) || !got.LastNotifiedAt.Equal(tickOne) {
		t.Fatalf("state = %q since %v notified %v, want ok since %v notified %v",
			got.Level, got.Since, got.LastNotifiedAt, tickTwo, tickOne)
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo.Add(time.Minute))
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("replacing a level wrote %+v, want only the seeded event", events)
	}
}

// spec: evaluation.md#persistence-and-restart — the same transition written twice is one
// event: a retry of a change is not a second change.
func TestApplyTransitionTwiceRecordsOneEvent(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	for range 2 {
		if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "warning")); err != nil {
			t.Fatalf("ApplyTransition: %v", err)
		}
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want one for a retried transition", len(events))
	}
}

// spec: evaluation.md#persistence-and-restart — after a restart every subject is as it was
// and nothing is re-notified.
func TestStatesSurviveAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	first, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	if err := first.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if err := first.RecordNotified(ctx, volume("/"), tickTwo); err != nil {
		t.Fatalf("RecordNotified: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.Close() }()

	got := second.statesByMount(t)["/"]
	if got.Level != "critical" || !got.Since.Equal(tickOne) || !got.LastNotifiedAt.Equal(tickTwo) {
		t.Fatalf("the state came back as %+v", got)
	}
}

// Delivery is driven by the newest event of a subject against what it was last notified
// about, so the tick has to be able to read exactly that.
func TestNewestEventPerSubject(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	tickThree := tickTwo.Add(time.Minute)

	// The volume alerts, recovers to warning and then to ok. Only the first two could
	// owe a message; the last must not hide the one before it.
	for _, step := range []struct {
		at       time.Time
		from, to string
	}{
		{tickOne, "ok", "critical"},
		{tickTwo, "critical", "warning"},
		{tickThree, "warning", "ok"},
	} {
		if err := db.ApplyTransition(ctx, transition(volume("/"), step.at, step.from, step.to)); err != nil {
			t.Fatalf("ApplyTransition: %v", err)
		}
	}
	// A volume that never touched critical owes nothing at all.
	if err := db.ApplyTransition(ctx, transition(volume("/data"), tickOne, "ok", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	newest, err := newestEvents(ctx, db.db, []string{"critical"})
	if err != nil {
		t.Fatalf("read the newest events: %v", err)
	}
	if len(newest) != 1 {
		t.Fatalf("the newest owed events cover %d subjects, want the one that reached critical", len(newest))
	}
	if got := newest[0]; got.Labels["mount"] != "/" || got.To != "warning" || !got.At.Equal(tickTwo) {
		t.Fatalf("the newest owed event is %+v, want the recovery out of critical", got)
	}
}

// spec: evaluation.md#persistence-and-restart — data written by a newer hub makes the hub
// refuse to start rather than judge subjects against a shape it does not know.
func TestOpeningANewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if _, err := db.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set a future schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := OpenSQLite(path); err == nil {
		t.Fatal("a database from a newer build was opened")
	}
}

// SQLite reads the path as a URI and decodes it, so a directory whose name carries one of
// its metacharacters must still open the file that was asked for.
func TestOpeningAPathWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b?c%2Fd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the directory: %v", err)
	}
	path := filepath.Join(dir, "monitor.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database was created somewhere else: %v", err)
	}
}

// spec: evaluation.md#digest — the window is read from the recorded transitions, and the
// mark of the last digest survives a restart.
func TestDigestWindow(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if _, marked, err := db.LastDigestAt(ctx); err != nil || marked {
		t.Fatalf("a fresh database claims a digest: marked=%v err=%v", marked, err)
	}
	if err := db.SetLastDigestAt(ctx, tickOne); err != nil {
		t.Fatalf("SetLastDigestAt: %v", err)
	}
	if err := db.SetLastDigestAt(ctx, tickTwo); err != nil {
		t.Fatalf("SetLastDigestAt again: %v", err)
	}
	at, marked, err := db.LastDigestAt(ctx)
	if err != nil || !marked || !at.Equal(tickTwo) {
		t.Fatalf("the digest mark is %v (marked=%v, err=%v)", at, marked, err)
	}

	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if err := db.ApplyTransition(ctx, transition(volume("/data"), tickTwo, "ok", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	window, err := db.EventsBetween(ctx, tickOne, tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(window) != 1 || window[0].Labels["mount"] != "/data" {
		t.Fatalf("the window holds %+v, want the transition after tickOne", window)
	}
}

// A database written by stage 1 gains the new tables when it is opened by this one: the
// measurements it holds are the only copy anyone has.
func TestOpeningAStageOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	old, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	measurement := Measurement{Metric: "disk.free_bytes", Labels: map[string]string{"mount": "/"}, Value: 9e9, TS: tickOne}
	if err := old.SaveIngest(ctx, ingest("server-b", tickOne, measurement)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if _, err := old.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("rewind the schema version: %v", err)
	}
	if _, err := old.db.Exec(`DROP TABLE states; DROP TABLE events; DROP TABLE meta`); err != nil {
		t.Fatalf("drop the stage 2 tables: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	migrated, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen a stage 1 database: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	if err := migrated.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "warning")); err != nil {
		t.Fatalf("the migrated database refuses a transition: %v", err)
	}
	snap, err := migrated.Snapshot(ctx, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	nodes := snap.Nodes
	if len(nodes) != 1 || len(nodes[0].Values) != 1 {
		t.Fatalf("the migration lost history: %+v", nodes)
	}
}

// subjectIsASeries is the index of the migration that rekeys the levels and the event log
// on the metric. A database rewound to it is one the previous schema wrote.
const subjectIsASeries = 5

// spec: evaluation.md#configuration-changes — a level was keyed on a rule before, and a
// rule cannot be translated into a series, so nothing judged survives the upgrade. What was
// measured does: the measurements and the series they imply are the only copy anyone has.
func TestOpeningADatabaseKeyedOnRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	old, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	measurement := Measurement{
		Metric: "disk.free_bytes",
		Sensor: "disk",
		Labels: map[string]string{"mount": "/"},
		Value:  9e9,
		TS:     tickOne,
	}
	if err := old.SaveIngest(ctx, ingest("server-b", tickOne, measurement)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	// The shape those tables had while a subject was a rule over two series, with a level
	// and a transition stored under it.
	if _, err := old.db.Exec(`DROP TABLE states; DROP TABLE events`); err != nil {
		t.Fatalf("drop the tables keyed on the metric: %v", err)
	}
	if _, err := old.db.Exec(migrations[1]); err != nil {
		t.Fatalf("restore the tables keyed on the rule: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO states (node, rule, labels, level, since)
		 VALUES ('server-b', 'disk', '{"mount":"/"}', 'critical', '2026-08-29T10:00:00.000Z')`,
		`INSERT INTO events (at, node, rule, labels, from_level, to_level, from_since, readings)
		 VALUES ('2026-08-29T10:00:00.000Z', 'server-b', 'disk', '{"mount":"/"}', 'ok', 'critical', '', '{}')`,
		fmt.Sprintf(`PRAGMA user_version = %d`, subjectIsASeries),
	} {
		if _, err := old.db.Exec(statement); err != nil {
			t.Fatalf("seed the previous schema: %v", err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	migrated, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen a database keyed on rules: %v", err)
	}
	defer func() { _ = migrated.Close() }()

	snap, err := migrated.Snapshot(ctx, []string{"critical"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.States) != 0 || len(snap.Newest) != 0 {
		t.Fatalf("the upgrade kept %d level(s) and %d event(s), want neither", len(snap.States), len(snap.Newest))
	}
	if len(snap.Nodes) != 1 || len(snap.Nodes[0].Values) != 1 || snap.Nodes[0].Values[0].Sensor != "disk" {
		t.Fatalf("the upgrade lost history: %+v", snap.Nodes)
	}

	// The tables are keyed on the metric now, which a level written under the new shape
	// and read back is what proves.
	if err := migrated.SaveState(ctx, State{Subject: volume("/"), Level: "ok", Since: tickTwo}); err != nil {
		t.Fatalf("SaveState on the migrated database: %v", err)
	}
	if got := migrated.statesByMount(t)["/"]; got.Metric != "disk" || got.Level != "ok" {
		t.Fatalf("the migrated state is %+v, want one keyed on the metric", got)
	}
}

// spec: evaluation.md#configuration-changes — the last threshold of a subject is removed,
// so it stops being a subject: its level is forgotten. The log is not: what happened did
// happen, and only this subject's level goes.
func TestDeleteStateForgetsOneSubjectAndKeepsTheLog(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if err := db.ApplyTransition(ctx, transition(volume("/data"), tickOne, "ok", "warning")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	if err := db.DeleteState(ctx, volume("/")); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}

	states := db.statesByMount(t)
	if _, kept := states["/"]; kept {
		t.Fatalf("the level of / outlived its threshold: %+v", states["/"])
	}
	if got := states["/data"]; got.Level != "warning" {
		t.Fatalf("the level of /data is %q, want the other volume left alone", got.Level)
	}
	events, err := db.EventsBetween(ctx, tickOne.Add(-time.Minute), tickTwo)
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("the log holds %+v, want both transitions: forgetting a level is not forgetting the log", events)
	}

	// Forgetting a subject that has no level is what a tick does on every pass after the
	// first, so it cannot be an error.
	if err := db.DeleteState(ctx, volume("/")); err != nil {
		t.Fatalf("DeleteState of a subject with no level: %v", err)
	}
}

// spec: evaluation.md#the-tick — the thresholds a tick judges by come from the same view as
// the values, so a save that lands while a tick runs belongs to the next one.
func TestSnapshotCarriesTheStoredThresholds(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	for _, th := range []Threshold{
		{Series: bytesRef("server-b", "/data"), Direction: Below, Warning: value(20e9)},
		{Series: bytesRef("server-b", "/"), Direction: Below, Warning: value(10e9), Critical: value(4e9)},
	} {
		save(t, db, th)
	}

	snapshot, err := db.Snapshot(ctx, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []Threshold{
		{Series: bytesRef("server-b", "/"), Direction: Below, Warning: value(10e9), Critical: value(4e9)},
		{Series: bytesRef("server-b", "/data"), Direction: Below, Warning: value(20e9)},
	}
	if !reflect.DeepEqual(snapshot.Thresholds, want) {
		t.Fatalf("snapshot thresholds = %+v, want %+v", snapshot.Thresholds, want)
	}

	// A save after the read is not in it: the snapshot is what the tick judges by.
	save(t, db, Threshold{Series: bytesRef("laptop-a", "/"), Direction: Below, Warning: value(1)})
	if len(snapshot.Thresholds) != 2 {
		t.Fatalf("the snapshot grew to %d thresholds after a later save", len(snapshot.Thresholds))
	}
}

// spec: evaluation.md#the-tick — one view of the data, taken at the instant the tick
// evaluates for.
func TestSnapshotReadsEveryPartTogether(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	measurement := Measurement{Metric: "disk.free_bytes", Labels: map[string]string{"mount": "/"}, Value: 9e9, TS: tickOne}
	if err := db.SaveIngest(ctx, ingest("server-b", tickOne, measurement)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	snapshot, err := db.Snapshot(ctx, []string{"critical"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 1 || len(snapshot.Nodes[0].Values) != 1 {
		t.Fatalf("nodes = %+v, want one node with one series", snapshot.Nodes)
	}
	if len(snapshot.States) != 1 || snapshot.States[0].Level != "critical" {
		t.Fatalf("states = %+v, want the stored level", snapshot.States)
	}
	if len(snapshot.Newest) != 1 || snapshot.Newest[0].To != "critical" {
		t.Fatalf("newest = %+v, want the transition just written", snapshot.Newest)
	}
}

// A subject is identified by its node, its rule and its labels together, which is what
// lets a tick hold one in a map.
func TestSubjectKey(t *testing.T) {
	same, err := volume("/").Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	again, err := Subject{Node: "server-b", Metric: "disk", Labels: map[string]string{"fs": "ext4", "mount": "/"}}.Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if same != again {
		t.Fatalf("keys differ for one subject: %q and %q", same, again)
	}
	other, err := volume("/data").Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if same == other {
		t.Fatal("two volumes share one key")
	}
}

// A caller that owes nothing is asking for nothing, so naming no level is not a licence to
// return every transition there is.
func TestNewestEventsWithoutAnOwedLevelReturnsNothing(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.ApplyTransition(ctx, transition(volume("/"), tickOne, "ok", "critical")); err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}

	newest, err := newestEvents(ctx, db.db, nil)
	if err != nil {
		t.Fatalf("read the newest events: %v", err)
	}
	if len(newest) != 0 {
		t.Fatalf("no owed level returned %d events, want none", len(newest))
	}
}
