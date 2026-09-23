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

// collect stores one push of one volume's free space, the way an agent's request would,
// and makes it a subject by storing the threshold of docs/specs/evaluation.md#levels:
// warning at 10 GB, critical at 4 GB.
func collect(t *testing.T, db *storage.SQLite, at time.Time, labels map[string]string, free float64) {
	t.Helper()
	collectFrom(t, db, "disk", at, labels, free)
}

// collectFrom is collect naming the sensor behind the measurement: "" is what an agent too
// old to name it sends, and what a rebuilt series table leaves behind (ADR 0034).
func collectFrom(t *testing.T, db *storage.SQLite, sensor string, at time.Time, labels map[string]string, free float64) {
	t.Helper()
	in := storage.Ingest{
		Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: at,
		Measurements: []storage.Measurement{
			{Metric: "disk.free_bytes", Sensor: sensor, Labels: labels, Value: free, TS: at},
		},
	}
	if err := db.SaveIngest(context.Background(), in); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	retune(t, db, labels, num(gb(10)), num(gb(4)))
}

// unwatch clears what one volume is judged by, which is what saving an empty form does.
func unwatch(t *testing.T, db *storage.SQLite, labels map[string]string) {
	t.Helper()
	ref := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: labels}
	if err := db.Configure(context.Background(), ref, nil, false); err != nil {
		t.Fatalf("Configure: %v", err)
	}
}

// retune stores what one volume is judged by, which is what editing the form does.
func retune(t *testing.T, db *storage.SQLite, labels map[string]string, warning, critical *float64) {
	t.Helper()
	th := storage.Threshold{
		Series:    storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: labels},
		Direction: storage.Below,
		Warning:   warning,
		Critical:  critical,
	}
	if err := db.Configure(context.Background(), th.Series, &th, false); err != nil {
		t.Fatalf("Configure: %v", err)
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
func levelOf(t *testing.T, db *storage.SQLite, metric, mount string) storage.State {
	t.Helper()
	snapshot, err := db.Snapshot(context.Background(), []string{"critical"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	states := snapshot.States
	for _, state := range states {
		if state.Metric == metric && state.Labels["mount"] == mount {
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
	collect(t, db, tick, volume("/"), gb(40))
	pass(t, evaluator(db, tick, watching(t)))

	state := levelOf(t, db, "disk.free_bytes", "/")
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
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	if state := levelOf(t, db, "disk.free_bytes", "/"); state.Level != "warning" {
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
	collect(t, db, tick, volume("/"), gb(9))
	e := evaluator(db, tick, watching(t))
	pass(t, e)
	before := levelOf(t, db, "disk.free_bytes", "/")
	pass(t, e)

	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("two ticks over unchanged data wrote %d events, want one", len(got))
	}
	if after := levelOf(t, db, "disk.free_bytes", "/"); after.Level != before.Level || !after.Since.Equal(before.Since) {
		t.Fatalf("the second tick moved the subject to %q since %v", after.Level, after.Since)
	}
}

// seedUnreadable stores a level for the root volume that this build cannot parse, the way a
// newer hub might have written it.
func seedUnreadable(t *testing.T, db *storage.SQLite) {
	t.Helper()
	subject := storage.Subject{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/")}
	if err := db.ApplyTransition(context.Background(), storage.Transition{
		Subject: subject, At: tick.Add(-time.Hour), From: "ok", To: "puce",
		Direction: string(storage.Below),
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

	collect(t, db, tick, volume("/"), gb(9))
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

	collect(t, db, tick, volume("/"), gb(40))
	pass(t, evaluator(db, tick, watching(t)))

	if state := levelOf(t, db, "disk.free_bytes", "/"); state.Level != "ok" || !state.Since.Equal(tick) {
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
	collect(t, db, tick, volume("/"), gb(40))
	pass(t, evaluator(db, tick, watching(t)))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(40))
	pass(t, evaluator(db, later, watching(t)))

	if state := levelOf(t, db, "disk.free_bytes", "/"); state.Level != "ok" || !state.Since.Equal(tick) {
		t.Fatalf("the later tick left level %q since %v, want ok since %v", state.Level, state.Since, tick)
	}
}

// spec: evaluation.md#persistence-and-restart — a tick that would start while the previous
// one is still running is skipped.
func TestOverlappingTicksAreSkipped(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))
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
	collect(t, db, tick, volume("/"), gb(9))
	collect(t, db, tick, volume("/data"), gb(9))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopping := &cancelling{Store: db, cancel: cancel}
	if err := evaluator(stopping, tick, watching(t)).Tick(ctx); err == nil {
		t.Fatal("a stopped tick reported success")
	}
	if levelOf(t, db, "disk.free_bytes", "/").Level != "warning" {
		t.Fatal("the change recorded before the stop was lost")
	}
	if got := levelOf(t, db, "disk.free_bytes", "/data").Level; got != "" {
		t.Fatalf("the subject after the stop was evaluated to %q", got)
	}
}

// spec: evaluation.md#freezing — the node reports again, so evaluation resumes on the next
// tick and a changed level writes one event.
func TestASilentNodeResumesWhenItReports(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))

	silent := tick.Add(silenceAfter + time.Minute)
	pass(t, evaluator(db, silent, watching(t)))
	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "" {
		t.Fatalf("a silent node's volume was evaluated to %q", got)
	}

	back := silent.Add(time.Minute)
	collect(t, db, back, volume("/"), gb(3))
	pass(t, evaluator(db, back, watching(t)))

	var changes []storage.Transition
	for _, event := range logged(t, db) {
		if event.Metric == "disk.free_bytes" {
			changes = append(changes, event)
		}
	}
	if len(changes) != 1 || changes[0].From != "ok" || changes[0].To != "critical" {
		t.Fatalf("resuming wrote %+v for the volume, want one change out of ok", changes)
	}
	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "critical" {
		t.Fatalf("the volume resumed at %q, want critical", got)
	}
}

// spec: evaluation.md#configuration-changes — a threshold edited while a subject is in
// warning: the next tick evaluates with the new numbers, as an ordinary transition.
func TestAnEditedThresholdIsAnOrdinaryTransition(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	retune(t, db, volume("/"), num(gb(10)), num(gb(20)))
	pass(t, evaluator(db, tick.Add(time.Minute), watching(t)))

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
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	pass(t, evaluator(db, tick.Add(time.Minute)))

	if got := levelOf(t, db, "disk.free_bytes", "/"); got.Level != "warning" || !got.Since.Equal(tick) {
		t.Fatalf("a removed node's state became %q since %v", got.Level, got.Since)
	}
	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("removing a node wrote %d events, want none beyond the first", len(got))
	}
}

// spec: evaluation.md#configuration-changes — every threshold removed while the subject
// stands in warning: it stops being a subject, its level is forgotten, and nothing is
// written or announced — the question was withdrawn, not answered.
func TestClearingAThresholdForgetsTheLevel(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	unwatch(t, db, volume("/"))
	pass(t, evaluator(db, tick.Add(time.Minute), watching(t)))

	if got := levelOf(t, db, "disk.free_bytes", "/"); got.Level != "" {
		t.Fatalf("the level outlived the threshold it answered: %q", got.Level)
	}
	if got := logged(t, db); len(got) != 1 {
		t.Fatalf("clearing a threshold wrote %d events, want only the first one", len(got))
	}
}

// spec: evaluation.md#configuration-changes — a threshold set again on a subject whose
// configuration was removed is judged as a new one: previous level ok.
func TestAThresholdSetAgainStartsFromOK(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))
	unwatch(t, db, volume("/"))
	pass(t, evaluator(db, tick.Add(time.Minute), watching(t)))

	retune(t, db, volume("/"), num(gb(10)), num(gb(4)))
	pass(t, evaluator(db, tick.Add(2*time.Minute), watching(t)))

	events := logged(t, db)
	if len(events) != 2 || events[1].From != "ok" || events[1].To != "critical" {
		t.Fatalf("re-setting a threshold produced %+v, want a fresh ok → critical", events)
	}
}

// spec: evaluation.md#configuration-changes — a sensor interval lowered while the agent
// still holds the old one shrinks `stale_after` first, so a healthy subject may freeze.
func TestALoweredIntervalFreezesAHealthySubject(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))

	hasty := watching(t)
	hasty.Intervals = map[string]time.Duration{"disk": time.Minute}
	pass(t, evaluator(db, tick.Add(4*time.Minute), hasty))

	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "" {
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

// spec: evaluation.md#configuration-changes — a threshold set again is judged as a new
// subject, so news the log still holds about the old one is not delivered a second time.
func TestAThresholdSetAgainDeliversNoOldNews(t *testing.T) {
	db := open(t)
	channel := &recorder{}
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluatorWith(db, channel, tick, watching(t)))
	if len(channel.sent()) != 1 {
		t.Fatalf("the first critical sent %d messages, want one", len(channel.sent()))
	}

	unwatch(t, db, volume("/"))
	pass(t, evaluatorWith(db, channel, tick.Add(time.Minute), watching(t)))

	// The volume is emptied and reports healthy before anyone watches it again.
	back := tick.Add(2 * time.Minute)
	collect(t, db, back, volume("/"), gb(40))
	pass(t, evaluatorWith(db, channel, back, watching(t)))

	if got := channel.sent(); len(got) != 1 {
		t.Fatalf("a re-set threshold delivered %+v, want nothing beyond the first critical", got[1:])
	}
	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "ok" {
		t.Fatalf("the re-set subject is %q, want ok: it is judged by what it reports now", got)
	}
}

// spec: evaluation.md#configuration-changes — a frozen subject that is watched again says
// nothing at all: its values are stale, and the log is not news.
func TestAThresholdSetAgainOnAFrozenSubjectIsSilent(t *testing.T) {
	db := open(t)
	channel := &recorder{}
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluatorWith(db, channel, tick, watching(t)))
	unwatch(t, db, volume("/"))

	stale := tick.Add(staleAfter + time.Minute)
	beat(t, db, stale)
	retune(t, db, volume("/"), num(gb(10)), num(gb(4)))
	for _, at := range []time.Time{stale, stale.Add(time.Minute), stale.Add(2 * time.Minute)} {
		pass(t, evaluatorWith(db, channel, at, watching(t)))
	}

	if got := channel.sent(); len(got) != 1 {
		t.Fatalf("a frozen re-set subject delivered %d messages, want only the original critical", len(got))
	}
}

// spec: evaluation.md#configuration-changes — `critical` removed while a subject stands
// in critical: the fall is an ordinary transition, and leaving critical is announced.
func TestRemovingCriticalAnnouncesTheFall(t *testing.T) {
	db := open(t)
	channel := &recorder{}
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluatorWith(db, channel, tick, watching(t)))

	retune(t, db, volume("/"), num(gb(10)), nil)
	later := tick.Add(time.Minute)
	pass(t, evaluatorWith(db, channel, later, watching(t)))

	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "warning" {
		t.Fatalf("the subject is %q, want warning: critical has no value to hold it", got)
	}
	events := logged(t, db)
	if len(events) != 2 || events[1].From != "critical" || events[1].To != "warning" {
		t.Fatalf("removing critical produced %+v, want an ordinary transition", events)
	}
	sent := channel.sent()
	if len(sent) != 2 || sent[1].From != evaluate.Critical || sent[1].To != evaluate.Warning {
		t.Fatalf("leaving critical delivered %+v, want it announced at once", sent)
	}
}

// spec: evaluation.md#configuration-changes — a threshold configured for a frozen subject
// is stored, and judged on the first tick that finds the values fresh again.
func TestAThresholdOnAFrozenSubjectWaitsForFreshValues(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	unwatch(t, db, volume("/"))

	stale := tick.Add(staleAfter + time.Minute)
	beat(t, db, stale)
	retune(t, db, volume("/"), num(gb(10)), num(gb(4)))
	pass(t, evaluator(db, stale, watching(t)))
	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "" {
		t.Fatalf("a frozen subject was judged to %q", got)
	}

	back := stale.Add(time.Minute)
	collect(t, db, back, volume("/"), gb(3))
	pass(t, evaluator(db, back, watching(t)))
	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "critical" {
		t.Fatalf("the subject is %q once its values are fresh, want critical", got)
	}
}

// spec: evaluation.md#configuration-changes — every threshold removed while the subject
// is frozen: it stops being a subject at once.
func TestClearingAThresholdOnAFrozenSubjectForgetsItAtOnce(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	stale := tick.Add(staleAfter + time.Minute)
	beat(t, db, stale)
	unwatch(t, db, volume("/"))
	pass(t, evaluator(db, stale, watching(t)))

	if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != "" {
		t.Fatalf("the frozen subject kept level %q after its threshold was cleared", got)
	}
}

// spec: thresholds.md#effects — a threshold cleared for a subject standing in critical
// announces no recovery: the question was withdrawn, not answered.
func TestClearingAThresholdAnnouncesNoRecovery(t *testing.T) {
	db := open(t)
	channel := &recorder{}
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluatorWith(db, channel, tick, watching(t)))

	unwatch(t, db, volume("/"))
	pass(t, evaluatorWith(db, channel, tick.Add(time.Minute), watching(t)))

	if got := channel.sent(); len(got) != 1 {
		t.Fatalf("clearing a critical threshold delivered %+v, want only the original alert", got[1:])
	}
}
