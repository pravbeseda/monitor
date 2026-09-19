package evaluate_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

func open(t *testing.T) *storage.SQLite {
	t.Helper()
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// collect stores one push of one volume's two series, the way an agent's request would.
func collect(t *testing.T, db *storage.SQLite, at time.Time, labels map[string]string, free, pct float64) {
	t.Helper()
	in := storage.Ingest{
		Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: at,
		Measurements: []storage.Measurement{
			{Metric: "disk.free_bytes", Labels: labels, Value: free, TS: at},
			{Metric: "disk.free_pct", Labels: labels, Value: pct, TS: at},
		},
	}
	if err := db.SaveIngest(context.Background(), in); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
}

// beat records that the node was heard from, carrying no measurement.
func beat(t *testing.T, db *storage.SQLite, at time.Time) {
	t.Helper()
	if err := db.SaveIngest(context.Background(), storage.Ingest{
		Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: at,
	}); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
}

// evaluator runs against a fixed clock: the tick is idempotent at a fixed instant.
func evaluator(store evaluate.Store, at time.Time, targets ...evaluate.Target) *evaluate.Evaluator {
	return evaluatorWith(store, &recorder{}, at, targets...)
}

func evaluatorWith(store evaluate.Store, channel evaluate.Notifier, at time.Time, targets ...evaluate.Target) *evaluate.Evaluator {
	// Started is the tick itself, so the digest of a database that has never digested is
	// not due: these tests are about levels and messages, not about the daily summary.
	return evaluate.New(evaluate.Options{
		Store: store, Notifier: channel, Targets: targets,
		Digest:  evaluate.Schedule{Hour: 9, Location: time.UTC},
		Started: at,
		Now:     func() time.Time { return at },
	})
}

func pass(t *testing.T, e *evaluate.Evaluator) {
	t.Helper()
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
}

// levelOf returns the stored level of one subject, or "" when it has none.
func levelOf(t *testing.T, db *storage.SQLite, rule, mount string) storage.State {
	t.Helper()
	snapshot, err := db.Snapshot(context.Background(), []string{"critical"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	states := snapshot.States
	for _, state := range states {
		if state.Rule == rule && state.Labels["mount"] == mount {
			return state
		}
	}
	return storage.State{}
}

func logged(t *testing.T, db *storage.SQLite) []storage.Transition {
	t.Helper()
	events, err := db.EventsBetween(context.Background(), time.Time{}, tick.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("EventsBetween: %v", err)
	}
	return events
}

// spec: evaluation.md#persistence-and-restart — a subject's first evaluation at ok appears
// with `since` set to that instant and writes no event: nothing changed.
func TestFirstEvaluationAtOKWritesNoEvent(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 40e9, 31.25)
	pass(t, evaluator(db, tick, watching(t)))

	state := levelOf(t, db, "disk", "/")
	if state.Level != "ok" || !state.Since.Equal(tick) {
		t.Fatalf("the first evaluation left level %q since %v", state.Level, state.Since)
	}
	if got := logged(t, db); len(got) != 0 {
		t.Fatalf("a first evaluation at ok wrote %d events", len(got))
	}
}

// spec: evaluation.md#persistence-and-restart — a subject first seen in warning appears
// with one event whose previous level is ok.
func TestFirstEvaluationInWarningWritesOneEvent(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	pass(t, evaluator(db, tick, watching(t)))

	if state := levelOf(t, db, "disk", "/"); state.Level != "warning" {
		t.Fatalf("the first evaluation left level %q, want warning", state.Level)
	}
	events := logged(t, db)
	if len(events) != 1 || events[0].From != "ok" || events[0].To != "warning" {
		t.Fatalf("a first evaluation in warning wrote %+v", events)
	}
}

// spec: evaluation.md#persistence-and-restart — two ticks at the same instant over
// unchanged data: the second writes no event.
func TestASecondTickOverUnchangedDataWritesNothing(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	e := evaluator(db, tick, watching(t))
	pass(t, e)
	before := levelOf(t, db, "disk", "/")
	pass(t, e)

	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("two ticks over unchanged data wrote %d events, want one", len(got))
	}
	if after := levelOf(t, db, "disk", "/"); after.Level != before.Level || !after.Since.Equal(before.Since) {
		t.Fatalf("the second tick moved the subject to %q since %v", after.Level, after.Since)
	}
}

// seedUnreadable stores a level for the root volume that this build cannot parse, the way a
// newer hub might have written it.
func seedUnreadable(t *testing.T, db *storage.SQLite) {
	t.Helper()
	subject := storage.Subject{Node: "server-b", Rule: "disk", Labels: volume("/")}
	if err := db.ApplyTransition(context.Background(), storage.Transition{
		Subject: subject, At: tick.Add(-time.Hour), From: "ok", To: "puce",
		FromSince: tick.Add(-2 * time.Hour), Readings: map[string]float64{},
	}); err != nil {
		t.Fatalf("seed an unreadable level: %v", err)
	}
}

// spec: evaluation.md#persistence-and-restart — a stored level this build does not know
// leaves that subject evaluated as if it were new.
func TestAnUnreadableStoredLevelIsEvaluatedAsNew(t *testing.T) {
	db := open(t)
	seedUnreadable(t, db)

	collect(t, db, tick, volume("/"), 19e9, 14.84)
	pass(t, evaluator(db, tick, watching(t)))

	events := logged(t, db)
	if len(events) != 2 || events[1].From != "ok" || events[1].To != "warning" {
		t.Fatalf("an unreadable level produced %+v, want a transition out of ok", events)
	}
}

// spec: evaluation.md#persistence-and-restart — a stored level this build does not know
// leaves that subject evaluated as if it were new, so one that stays ok is stored as ok
// with `since` at that tick, and no event is written.
func TestAnUnreadableStoredLevelThatStaysOKIsReplaced(t *testing.T) {
	db := open(t)
	seedUnreadable(t, db)

	collect(t, db, tick, volume("/"), 40e9, 31.25)
	pass(t, evaluator(db, tick, watching(t)))

	if state := levelOf(t, db, "disk", "/"); state.Level != "ok" || !state.Since.Equal(tick) {
		t.Fatalf("the tick left level %q since %v, want ok since %v", state.Level, state.Since, tick)
	}
	if events := logged(t, db); len(events) != 1 {
		t.Fatalf("replacing an unreadable level wrote %+v, want only the seeded event", events)
	}
}

// spec: evaluation.md#persistence-and-restart — the level does not change: `since` is
// untouched by a later tick.
func TestALaterTickAtTheSameLevelKeepsSince(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 40e9, 31.25)
	pass(t, evaluator(db, tick, watching(t)))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), 40e9, 31.25)
	pass(t, evaluator(db, later, watching(t)))

	if state := levelOf(t, db, "disk", "/"); state.Level != "ok" || !state.Since.Equal(tick) {
		t.Fatalf("the later tick left level %q since %v, want ok since %v", state.Level, state.Since, tick)
	}
}

// spec: evaluation.md#persistence-and-restart — a tick that would start while the previous
// one is still running is skipped.
func TestOverlappingTicksAreSkipped(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	held := &blocking{Store: db, entered: make(chan struct{}), release: make(chan struct{})}
	e := evaluator(held, tick, watching(t))

	done := make(chan error, 1)
	go func() { done <- e.Tick(context.Background()) }()
	<-held.entered
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("the skipped tick returned %v, want it to give way quietly", err)
	}
	close(held.release)
	if err := <-done; err != nil {
		t.Fatalf("the held tick returned %v", err)
	}
	if got := held.passes.Load(); got != 1 {
		t.Fatalf("%d passes ran at once, want one", got)
	}
}

// spec: evaluation.md#persistence-and-restart — a hub asked to stop mid-tick evaluates no
// further subject, and what it already recorded stays recorded.
func TestAStoppedTickEvaluatesNoFurtherSubject(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	collect(t, db, tick, volume("/data"), 19e9, 14.84)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopping := &cancelling{Store: db, cancel: cancel}
	if err := evaluator(stopping, tick, watching(t)).Tick(ctx); err == nil {
		t.Fatal("a stopped tick reported success")
	}
	if levelOf(t, db, "disk", "/").Level != "warning" {
		t.Fatal("the change recorded before the stop was lost")
	}
	if got := levelOf(t, db, "disk", "/data").Level; got != "" {
		t.Fatalf("the subject after the stop was evaluated to %q", got)
	}
}

// spec: evaluation.md#freezing — the node reports again, so evaluation resumes on the next
// tick and a changed level writes one event.
func TestASilentNodeResumesWhenItReports(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 5e9, 3.91)

	silent := tick.Add(silenceAfter + time.Minute)
	pass(t, evaluator(db, silent, watching(t)))
	if got := levelOf(t, db, "disk", "/").Level; got != "" {
		t.Fatalf("a silent node's volume was evaluated to %q", got)
	}

	back := silent.Add(time.Minute)
	collect(t, db, back, volume("/"), 5e9, 3.91)
	pass(t, evaluator(db, back, watching(t)))

	var changes []storage.Transition
	for _, event := range logged(t, db) {
		if event.Rule == "disk" {
			changes = append(changes, event)
		}
	}
	if len(changes) != 1 || changes[0].From != "ok" || changes[0].To != "critical" {
		t.Fatalf("resuming wrote %+v for the volume, want one change out of ok", changes)
	}
	if got := levelOf(t, db, "disk", "/").Level; got != "critical" {
		t.Fatalf("the volume resumed at %q, want critical", got)
	}
}

// spec: evaluation.md#configuration-changes — a threshold edited while a subject is in
// warning: the next tick evaluates with the new numbers, as an ordinary transition.
func TestAnEditedThresholdIsAnOrdinaryTransition(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	pass(t, evaluator(db, tick, watching(t)))

	stricter := watching(t)
	rule := stricter.Rules["disk"]
	rule.Critical.Floor = 20e9
	stricter.Rules = map[string]evaluate.Rule{"disk": rule}
	pass(t, evaluator(db, tick.Add(time.Minute), stricter))

	events := logged(t, db)
	if len(events) != 2 || events[1].From != "warning" || events[1].To != "critical" {
		t.Fatalf("editing a threshold produced %+v", events)
	}
}

// spec: evaluation.md#configuration-changes — `silence_after` widened while a node is
// silent-critical: the next tick finds it inside the new window and recovers it.
func TestAWidenedSilenceWindowRecoversTheNode(t *testing.T) {
	db := open(t)
	beat(t, db, tick)
	silent := tick.Add(silenceAfter + time.Minute)
	pass(t, evaluator(db, silent, watching(t)))
	if got := levelOf(t, db, "silence", "").Level; got != "critical" {
		t.Fatalf("the node is %q past its window, want critical", got)
	}

	widened := watching(t)
	widened.SilenceAfter = 24 * time.Hour
	pass(t, evaluator(db, silent.Add(time.Minute), widened))

	if got := levelOf(t, db, "silence", "").Level; got != "ok" {
		t.Fatalf("widening the window left the node at %q, want ok", got)
	}
}

// spec: evaluation.md#configuration-changes — a node removed from the file has no subjects,
// its stored states are left untouched, and no recovery is notified.
func TestANodeRemovedFromTheFileIsLeftAlone(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	pass(t, evaluator(db, tick, watching(t)))

	pass(t, evaluator(db, tick.Add(time.Minute)))

	if got := levelOf(t, db, "disk", "/"); got.Level != "warning" || !got.Since.Equal(tick) {
		t.Fatalf("a removed node's state became %q since %v", got.Level, got.Since)
	}
	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("removing a node wrote %d events, want none beyond the first", len(got))
	}
}

// spec: evaluation.md#configuration-changes — a rule the configuration no longer resolves
// leaves its subjects untouched and unevaluated.
func TestARuleTheConfigurationNoLongerCarriesIsLeftAlone(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)
	pass(t, evaluator(db, tick, watching(t)))

	ruleless := watching(t)
	ruleless.Rules = map[string]evaluate.Rule{}
	pass(t, evaluator(db, tick.Add(time.Minute), ruleless))

	if got := levelOf(t, db, "disk", "/"); got.Level != "warning" || !got.Since.Equal(tick) {
		t.Fatalf("a rule nobody resolves left the state at %q since %v", got.Level, got.Since)
	}
	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("dropping a rule wrote %d events, want none beyond the first", len(got))
	}
}

// spec: evaluation.md#configuration-changes — a sensor interval lowered while the agent
// still holds the old one shrinks `stale_after` first, so a healthy subject may freeze.
func TestALoweredIntervalFreezesAHealthySubject(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), 19e9, 14.84)

	hasty := watching(t)
	hasty.Intervals = map[string]time.Duration{"disk": time.Minute}
	pass(t, evaluator(db, tick.Add(4*time.Minute), hasty))

	if got := levelOf(t, db, "disk", "/").Level; got != "" {
		t.Fatalf("a subject older than the shrunk window was evaluated to %q", got)
	}
}

// blocking holds a pass inside Snapshot, so a second tick can be started while the first
// one is still running.
type blocking struct {
	evaluate.Store
	entered chan struct{}
	release chan struct{}
	passes  atomic.Int32
}

func (b *blocking) Snapshot(ctx context.Context, owed []string) (storage.Snapshot, error) {
	b.passes.Add(1)
	b.entered <- struct{}{}
	<-b.release
	return b.Store.Snapshot(ctx, owed)
}

// cancelling stops the hub the moment the first change is recorded.
type cancelling struct {
	evaluate.Store
	cancel context.CancelFunc
}

func (c *cancelling) ApplyTransition(ctx context.Context, change storage.Transition) error {
	err := c.Store.ApplyTransition(ctx, change)
	c.cancel()
	return err
}
