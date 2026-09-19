package evaluate

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// sendTimeout is how long a channel is given to answer. A notifier that does not return
// cannot hold evaluation open: the send is abandoned, counted as a failure, and the event
// is delivered again on a later tick.
var sendTimeout = 10 * time.Second

// Evaluator runs the hub's evaluation pass. It is the only writer of the event log
// (ADR 0015): silence, levels, the daily repeat and the digest are one pass on one clock,
// never a step inside an ingest request.
type Evaluator struct {
	store    Store
	notifier Notifier
	now      func() time.Time
	// targets is the configuration as evaluation reads it, resolved once at startup: the
	// file is never re-read while the hub runs.
	targets  []Target
	schedule Schedule
	// started is where the digest window begins on a database that has never digested.
	started time.Time
	// running keeps two passes from overlapping. A tick that would start while the
	// previous one is still going gives way rather than queueing behind it.
	running atomic.Bool
}

// Options is what the hub hands the evaluator at startup. Everything here is resolved
// once: the configuration file is never re-read while the hub runs.
type Options struct {
	Store    Store
	Notifier Notifier
	Targets  []Target
	Digest   Schedule
	// Started is when this hub first ran, which is where the digest window begins on a
	// database that has never digested: history is never replayed.
	Started time.Time
	// Now is the clock the pass reads. A tick is idempotent at a fixed instant.
	Now func() time.Time
}

// New builds the evaluator the hub ticks.
func New(o Options) *Evaluator {
	return &Evaluator{
		store:    o.Store,
		notifier: o.Notifier,
		now:      o.Now,
		targets:  o.Targets,
		schedule: o.Digest,
		started:  o.Started,
	}
}

// Tick runs one pass against one consistent view of the data, taken at the instant it
// evaluates for: a measurement that arrives while a tick is running belongs to the next
// tick, never to half of this one.
func (e *Evaluator) Tick(ctx context.Context) error {
	if !e.running.CompareAndSwap(false, true) {
		slog.Warn("evaluation tick skipped: the previous pass is still running")
		return nil
	}
	defer e.running.Store(false)

	// The window is opened before anything is recorded: a pass that stops halfway must
	// not leave an event behind with no digest window reaching back over it.
	since, err := e.openDigestWindow(ctx)
	if err != nil {
		return err
	}

	snapshot, err := e.store.Snapshot(ctx, instantLevels())
	if err != nil {
		return err
	}
	now := e.now()
	newest := make(map[string]storage.Transition, len(snapshot.Newest))
	for _, event := range snapshot.Newest {
		if key, err := event.Key(); err == nil {
			newest[key] = event
		}
	}

	subjects := Subjects(e.targets, snapshot, now)
	for _, subject := range subjects {
		// A hub asked to stop evaluates no further subject; what it already recorded
		// stays recorded, and the next start picks the rest up.
		if err := ctx.Err(); err != nil {
			return err
		}
		key, err := subject.Key()
		if err != nil {
			slog.Error("identify a subject", "node", subject.Node, "rule", subject.Rule, "error", err)
			continue
		}
		// Stale values judge nothing, so a frozen subject writes no state and no event —
		// but a message recorded from fresh values and never delivered is still owed,
		// because delivery is driven by the record and not by the data behind it.
		if !subject.Frozen {
			change, err := e.record(ctx, subject, now)
			if err != nil {
				return err
			}
			// Only a change a channel could owe a message for replaces what the snapshot
			// carried: a quieter one must not hide an instant event that never got out.
			if change != nil && instant(*change) {
				newest[key] = *change
			}
		}
		event, recorded := newest[key]
		e.deliver(ctx, subject, event, recorded, now)
	}
	// The digest runs last, so a warning this pass recorded is in the window it closes.
	return e.digest(ctx, subjects, since, now)
}

// deliver sends what the subject is owed, if anything, and records the delivery only once
// the channel has taken it: a failure leaves last_notified_at where it was, so the next
// tick tries again. Failure is per message, so one subject never costs the others theirs.
func (e *Evaluator) deliver(ctx context.Context, subject Subject, newest storage.Transition, recorded bool, now time.Time) {
	message, owed := due(subject, newest, recorded, now)
	if !owed {
		return
	}
	if err := e.send(ctx, func(ctx context.Context) error {
		return e.notifier.Notify(ctx, message)
	}); err != nil {
		slog.Error("deliver a notification",
			"node", subject.Node, "rule", subject.Rule, "error", err)
		return
	}
	if err := e.store.RecordNotified(ctx, subject.Subject, now); err != nil {
		slog.Error("record a delivery",
			"node", subject.Node, "rule", subject.Rule, "error", err)
	}
}

// send hands one delivery to the channel and gives up on it after sendTimeout. The
// abandoned call keeps its buffered slot, so a channel that answers late does not block a
// goroutine for good. Every delivery goes through here, the digest included: one stuck
// send would otherwise hold the pass open and every tick after it would be skipped.
func (e *Evaluator) send(ctx context.Context, deliver func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	answered := make(chan error, 1)
	go func() { answered <- deliver(ctx) }()
	select {
	case err := <-answered:
		return err
	case <-ctx.Done():
		return fmt.Errorf("the channel did not answer within %v: %w", sendTimeout, ctx.Err())
	}
}

// record writes what the pass made of one subject. A change is a level and an event
// together. A subject that has not moved is written only when no readable level was stored
// for it, so a first evaluation at ok appears without an event, an unreadable level is
// replaced, and `since` survives every tick that follows.
func (e *Evaluator) record(ctx context.Context, subject Subject, now time.Time) (*storage.Transition, error) {
	if !subject.Changed() {
		if subject.Restored {
			return nil, nil
		}
		return nil, e.store.SaveState(ctx, storage.State{
			Subject: subject.Subject,
			Level:   subject.Level.String(),
			Since:   subject.Since,
		})
	}
	change := storage.Transition{
		Subject:   subject.Subject,
		At:        now,
		From:      subject.Previous.String(),
		To:        subject.Level.String(),
		FromSince: subject.Since,
		Readings:  subject.Readings,
	}
	if err := e.store.ApplyTransition(ctx, change); err != nil {
		return nil, err
	}
	return &change, nil
}

// Interval is how often evaluation runs (ADR 0015). No configuration key changes it: the
// pass is cheap, and a slower one would only delay every alert by the difference.
const Interval = time.Minute

// Run evaluates every `every` until ctx is cancelled. A pass that fails is logged and the
// next one starts from the same snapshot, so nothing is retried by hand.
func (e *Evaluator) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
				slog.Error("evaluation pass", "error", err)
			}
		}
	}
}
