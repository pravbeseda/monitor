package evaluate_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// recorder is a channel that keeps what it was handed, and fails for the mounts it is told
// to fail for.
type recorder struct {
	mu         sync.Mutex
	messages   []evaluate.Message
	digests    [][]evaluate.Message
	digestedAt []time.Time
	unwatched  []int
	failing    map[string]bool
	// digestFails makes the daily summary refuse, which is what leaves the window open.
	digestFails bool
}

func (r *recorder) Notify(_ context.Context, m evaluate.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing[m.Labels["mount"]] {
		return errors.New("the channel is down")
	}
	r.messages = append(r.messages, m)
	return nil
}

func (r *recorder) Digest(_ context.Context, at time.Time, entries []evaluate.Message, unwatched int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.digestFails {
		return errors.New("the channel is down")
	}
	r.digestedAt = append(r.digestedAt, at)
	r.digests = append(r.digests, entries)
	r.unwatched = append(r.unwatched, unwatched)
	return nil
}

// unwatchedCounts is what each digest said about the series nobody has configured.
func (r *recorder) unwatchedCounts() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.unwatched...)
}

func (r *recorder) summaries() [][]evaluate.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]evaluate.Message(nil), r.digests...)
}

func (r *recorder) sent() []evaluate.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]evaluate.Message(nil), r.messages...)
}

// delivered runs one tick and returns what left the hub.
func delivered(t *testing.T, db *storage.SQLite, at time.Time, target evaluate.Target) []evaluate.Message {
	t.Helper()
	channel := &recorder{}
	pass(t, evaluatorWith(db, channel, at, target))
	return channel.sent()
}

// spec: evaluation.md#notifications — entering critical is instant, whatever the level it
// came from.
func TestEnteringCriticalFromOKIsInstant(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	got := delivered(t, db, tick, watching(t))
	if len(got) != 1 || got[0].From != evaluate.OK || got[0].To != evaluate.Critical {
		t.Fatalf("entering critical delivered %+v", got)
	}
}

// spec: evaluation.md#notifications — warning → critical is instant too.
func TestWarningToCriticalIsInstant(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(3))
	got := delivered(t, db, later, watching(t))
	if len(got) != 1 || got[0].From != evaluate.Warning || got[0].To != evaluate.Critical {
		t.Fatalf("warning to critical delivered %+v", got)
	}
}

// spec: evaluation.md#notifications — leaving critical is instant, both to warning and to
// ok (ADR 0016).
func TestLeavingCriticalIsInstant(t *testing.T) {
	for _, leaving := range []struct {
		name string
		free float64
		want evaluate.Level
	}{
		{"to warning", gb(6), evaluate.Warning},
		{"to ok", gb(24), evaluate.OK},
	} {
		t.Run(leaving.name, func(t *testing.T) {
			db := open(t)
			collect(t, db, tick, volume("/"), gb(3))
			pass(t, evaluator(db, tick, watching(t)))

			later := tick.Add(time.Minute)
			collect(t, db, later, volume("/"), leaving.free)
			got := delivered(t, db, later, watching(t))
			if len(got) != 1 || got[0].From != evaluate.Critical || got[0].To != leaving.want {
				t.Fatalf("leaving critical delivered %+v, want a message reaching %v", got, leaving.want)
			}
		})
	}
}

// spec: evaluation.md#notifications — a transition that never touches critical waits for
// the digest.
func TestTransitionsAwayFromCriticalWaitForTheDigest(t *testing.T) {
	for _, quiet := range []struct {
		name string
		free float64
		want string
	}{
		{"ok to warning", gb(9), "warning"},
		{"warning to ok", gb(40), "ok"},
	} {
		t.Run(quiet.name, func(t *testing.T) {
			db := open(t)
			collect(t, db, tick, volume("/"), gb(9))
			if quiet.want == "ok" {
				pass(t, evaluator(db, tick, watching(t)))
			}
			later := tick.Add(time.Minute)
			collect(t, db, later, volume("/"), quiet.free)
			if got := delivered(t, db, later, watching(t)); len(got) != 0 {
				t.Fatalf("a transition outside critical delivered %+v", got)
			}
			if got := levelOf(t, db, "disk.free_bytes", "/").Level; got != quiet.want {
				t.Fatalf("the subject is %q, want %q: the change is recorded, only the message waits", got, quiet.want)
			}
		})
	}
}

// spec: evaluation.md#notifications — an instant-delivery event newer than the subject's
// last_notified_at is delivered now, whatever the level has become since.
func TestAnUndeliveredEventIsSentOnALaterTick(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	down := &recorder{failing: map[string]bool{"/": true}}
	pass(t, evaluatorWith(db, down, tick, watching(t)))

	// The volume recovers before the channel does: the message still names the change
	// that was recorded, not the level the subject has reached since.
	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(3))
	got := delivered(t, db, later, watching(t))
	if len(got) != 1 || got[0].From != evaluate.OK || got[0].To != evaluate.Critical {
		t.Fatalf("the retry delivered %+v, want the recorded change", got)
	}
}

// spec: evaluation.md#notifications — a subject that has never been notified is due any
// instant-delivery event it carries.
func TestASubjectNeverNotifiedIsDue(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	// A transition writes the level and the event together, which is the history this
	// row is about: the change is recorded, nothing has been told about it yet.
	subject := storage.Subject{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/")}
	if err := db.ApplyTransition(ctx, storage.Transition{
		Subject: subject, At: tick.Add(-time.Hour), From: "ok", To: "critical",
		Direction: string(storage.Below),
		FromSince: tick.Add(-2 * time.Hour),
		Readings:  map[string]float64{"disk.free_bytes": 3e9},
	}); err != nil {
		t.Fatalf("seed an undelivered event: %v", err)
	}
	collect(t, db, tick, volume("/"), gb(3))

	got := delivered(t, db, tick, watching(t))
	if len(got) != 1 || got[0].To != evaluate.Critical {
		t.Fatalf("a subject with an empty last_notified_at delivered %+v", got)
	}
}

// spec: evaluation.md#notifications — an unresolved critical repeats at most once a day.
func TestAnUnresolvedCriticalRepeatsDaily(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	soon := tick.Add(23 * time.Hour)
	collect(t, db, soon, volume("/"), gb(3))
	if got := delivered(t, db, soon, watching(t)); len(got) != 0 {
		t.Fatalf("a critical under a day old repeated: %+v", got)
	}

	due := tick.Add(24 * time.Hour)
	collect(t, db, due, volume("/"), gb(3))
	got := delivered(t, db, due, watching(t))
	if len(got) != 1 || got[0].From != evaluate.Critical || got[0].To != evaluate.Critical {
		t.Fatalf("a day-old critical delivered %+v, want one repeat", got)
	}
}

// spec: evaluation.md#notifications — a subject that has not moved and is not critical says
// nothing.
func TestAnUnchangedSubjectBelowCriticalSaysNothing(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(9))
	pass(t, evaluator(db, tick, watching(t)))

	later := tick.Add(25 * time.Hour)
	collect(t, db, later, volume("/"), gb(9))
	if got := delivered(t, db, later, watching(t)); len(got) != 0 {
		t.Fatalf("a subject holding warning delivered %+v", got)
	}
}

// spec: evaluation.md#notifications — a failed send leaves the event written and
// last_notified_at where it was, so the next tick delivers it again.
func TestAFailedSendIsRetried(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	down := &recorder{failing: map[string]bool{"/": true}}
	pass(t, evaluatorWith(db, down, tick, watching(t)))

	if got := levelOf(t, db, "disk.free_bytes", "/"); got.Level != "critical" || !got.LastNotifiedAt.IsZero() {
		t.Fatalf("a failed send left level %q notified at %v", got.Level, got.LastNotifiedAt)
	}
	if got := delivered(t, db, tick.Add(time.Minute), watching(t)); len(got) != 1 {
		t.Fatalf("the retry delivered %d messages, want one", len(got))
	}
}

// spec: evaluation.md#notifications — failure is per message: one subject's send failing
// never costs the others theirs.
func TestOneFailedSendDoesNotStopTheRest(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	collect(t, db, tick, volume("/data"), gb(3))

	channel := &recorder{failing: map[string]bool{"/": true}}
	pass(t, evaluatorWith(db, channel, tick, watching(t)))

	got := channel.sent()
	if len(got) != 1 || got[0].Labels["mount"] != "/data" {
		t.Fatalf("one failure delivered %+v, want the other subject's message", got)
	}
}

// spec: evaluation.md#persistence-and-restart — the event is recorded before the message
// goes out, so a hub that dies in between delivers on a later tick.
func TestTheEventIsRecordedBeforeTheMessage(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))

	seen := &beforeSending{db: db}
	pass(t, evaluatorWith(db, seen, tick, watching(t)))
	if seen.level != "critical" {
		t.Fatalf("when the message went out the stored level was %q, want it already recorded", seen.level)
	}
}

// spec: evaluation.md#persistence-and-restart — a restart re-notifies nothing: what was
// delivered is on the record.
func TestARestartReNotifiesNothing(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(3))
	if got := delivered(t, db, later, watching(t)); len(got) != 0 {
		t.Fatalf("a fresh evaluator re-delivered %+v", got)
	}
}

// spec: evaluation.md#persistence-and-restart — two ticks at the same instant over
// unchanged data: the second sends no message.
func TestASecondTickAtTheSameInstantSendsNothing(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	channel := &recorder{}
	e := evaluatorWith(db, channel, tick, watching(t))
	pass(t, e)
	pass(t, e)
	if got := channel.sent(); len(got) != 1 {
		t.Fatalf("two ticks at one instant delivered %d messages, want one", len(got))
	}
}

// spec: evaluation.md#node-silence — a node that falls silent is reported at once, and its
// return is reported too.
func TestSilenceIsReportedAndSoIsItsRecovery(t *testing.T) {
	db := open(t)
	beat(t, db, tick)
	silent := tick.Add(silenceAfter + time.Minute)
	got := delivered(t, db, silent, watching(t))
	if len(got) != 1 || got[0].Metric != evaluate.SilenceMetric || got[0].To != evaluate.Critical {
		t.Fatalf("a silent node delivered %+v", got)
	}

	back := silent.Add(time.Minute)
	beat(t, db, back)
	got = delivered(t, db, back, watching(t))
	if len(got) != 1 || got[0].Metric != evaluate.SilenceMetric || got[0].To != evaluate.OK {
		t.Fatalf("a returning node delivered %+v", got)
	}
}

// beforeSending reads the stored level at the moment the message is handed over.
type beforeSending struct {
	db    *storage.SQLite
	level string
}

func (b *beforeSending) Notify(ctx context.Context, m evaluate.Message) error {
	snapshot, err := b.db.Snapshot(ctx, []string{"critical"})
	if err != nil {
		return err
	}
	for _, state := range snapshot.States {
		if state.Metric == m.Metric && state.Labels["mount"] == m.Labels["mount"] {
			b.level = state.Level
		}
	}
	return nil
}

func (b *beforeSending) Digest(context.Context, time.Time, []evaluate.Message, int) error { return nil }

// spec: evaluation.md#node-silence — a node still silent says nothing again until a day has
// passed, and then says it once more.
func TestSilenceRepeatsOnceADay(t *testing.T) {
	db := open(t)
	beat(t, db, tick)
	silent := tick.Add(silenceAfter + time.Minute)
	if got := delivered(t, db, silent, watching(t)); len(got) != 1 {
		t.Fatalf("the first silence delivered %d messages", len(got))
	}

	soon := silent.Add(23 * time.Hour)
	if got := delivered(t, db, soon, watching(t)); len(got) != 0 {
		t.Fatalf("a silence under a day old repeated: %+v", got)
	}

	due := silent.Add(24 * time.Hour)
	got := delivered(t, db, due, watching(t))
	if len(got) != 1 || got[0].Metric != evaluate.SilenceMetric || got[0].From != evaluate.Critical {
		t.Fatalf("a day-old silence delivered %+v, want one repeat", got)
	}
}

// spec: evaluation.md#freezing — a frozen subject sends no repeat, however long ago it was
// last notified.
func TestAFrozenCriticalSendsNoRepeat(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	// A day later the volume is long stale, and the node has kept reporting, so only
	// freezing can hold the repeat back.
	due := tick.Add(24 * time.Hour)
	beat(t, db, due)
	if got := delivered(t, db, due, watching(t)); len(got) != 0 {
		t.Fatalf("a stale critical repeated: %+v", got)
	}
}

// spec: evaluation.md#freezing — a watched series that names no sensor freezes like any
// other once past its node's longest interval, so it repeats no more than a stale one.
func TestAFrozenCriticalThatNamesNoSensorSendsNoRepeat(t *testing.T) {
	db := open(t)
	collectFrom(t, db, "", tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	due := tick.Add(24 * time.Hour)
	beat(t, db, due)
	if got := delivered(t, db, due, watching(t)); len(got) != 0 {
		t.Fatalf("a critical series naming no sensor repeated: %+v", got)
	}
}

// spec: evaluation.md#notifications — an instant event newer than `last_notified_at` is
// delivered whatever the level has become since, so a quieter change recorded after it
// must not bury it.
func TestAFailedInstantEventSurvivesAQuieterChange(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	pass(t, evaluator(db, tick, watching(t)))

	// The recovery out of critical is instant, and its send fails.
	leaving := tick.Add(time.Minute)
	collect(t, db, leaving, volume("/"), gb(6))
	down := &recorder{failing: map[string]bool{"/": true}}
	pass(t, evaluatorWith(db, down, leaving, watching(t)))

	// The volume then leaves warning too, which nothing delivers at once.
	quiet := leaving.Add(time.Minute)
	collect(t, db, quiet, volume("/"), gb(40))
	got := delivered(t, db, quiet, watching(t))

	if len(got) != 1 || got[0].From != evaluate.Critical || got[0].To != evaluate.Warning {
		t.Fatalf("the retry delivered %+v, want the recovery out of critical", got)
	}
}

// spec: evaluation.md#freezing — freezing withholds judgement of stale values, not a
// message already recorded from fresh ones: a send that failed before the subject froze is
// still tried again.
func TestAFrozenSubjectStillDeliversWhatItAlreadyRecorded(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3))
	down := &recorder{failing: map[string]bool{"/": true}}
	pass(t, evaluatorWith(db, down, tick, watching(t)))

	// The volume stops reporting, so it is long stale by the time the channel recovers;
	// the node itself keeps its heartbeat, so only freezing could hold the message back.
	frozen := tick.Add(staleAfter + time.Minute)
	beat(t, db, frozen)
	got := delivered(t, db, frozen, watching(t))

	if len(got) != 1 || got[0].To != evaluate.Critical || got[0].Labels["mount"] != "/" {
		t.Fatalf("the frozen subject delivered %+v, want the message it had recorded", got)
	}
}
