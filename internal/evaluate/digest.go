package evaluate

import (
	"context"
	"log/slog"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// watching counts what a tick judged and what it left alone, so the digest can say that
// a series nobody configured exists at all (docs/specs/evaluation.md#digest).
type watching struct {
	reporting bool
	watched   int
	unwatched int
}

// nothingWatched is a hub with nodes reporting and no series judged at all — a fresh
// installation, or a database restored without its thresholds.
func (w watching) nothingWatched() bool { return w.reporting && w.watched == 0 }

// Schedule is when the daily digest goes out. The zone comes from the file rather than
// from the host, so moving the hub to another machine cannot move the hour it arrives.
type Schedule struct {
	Hour, Minute int
	Location     *time.Location
}

// mostRecent is the latest occurrence of the configured hour at or before now. An hour a
// DST change removes is normalised forward by time.Date, so a day is never skipped.
func (s Schedule) mostRecent(now time.Time) time.Time {
	location := s.Location
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)
	at := func(day time.Time) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), s.Hour, s.Minute, 0, 0, location)
	}
	if occurrence := at(local); !occurrence.After(local) {
		return occurrence
	}
	return at(local.AddDate(0, 0, -1))
}

// openDigestWindow returns where the current digest window begins, writing it down the
// first time this database is evaluated at all. A window that exists only in memory would
// be lost by a pass that stops before its digest, and the events it already recorded would
// then fall outside every window there is.
func (e *Evaluator) openDigestWindow(ctx context.Context) (time.Time, error) {
	since, opened, err := e.store.LastDigestAt(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if opened {
		return since, nil
	}
	// A database nothing has evaluated yet starts at this hub's first run, so history is
	// never replayed.
	return e.started, e.store.SetLastDigestAt(ctx, e.started)
}

// digest sends the day's summary when the tick crosses the configured hour. The window is
// closed even when there was nothing to say, so a warning that appears after the hour
// waits for tomorrow rather than going out at once.
func (e *Evaluator) digest(ctx context.Context, subjects []Subject, watch watching, since, now time.Time) error {
	// The occurrence decides whether a digest is due; the tick time records what has been
	// reported, which is why it is the mark below.
	occurrence := e.schedule.mostRecent(now)
	if !occurrence.After(since) {
		return nil
	}

	entries, err := e.entries(ctx, subjects, since, now, occurrence)
	if err != nil {
		return err
	}
	// A hub that judges nothing says so every day until something is set: silence while
	// all is well means nothing when nothing is being watched.
	if len(entries) > 0 || watch.nothingWatched() {
		if err := e.send(ctx, func(ctx context.Context) error {
			return e.notifier.Digest(ctx, occurrence, entries, watch.unwatched)
		}); err != nil {
			// The window stays open, so the next tick sends the same one again.
			slog.Error("deliver the digest", "at", occurrence, "error", err)
			return nil
		}
	}
	// The window ends where it was read, not at the hour it speaks for: stamping the
	// occurrence would leave everything recorded since inside tomorrow's window too.
	return e.store.SetLastDigestAt(ctx, now)
}

// entries is what the digest lists: every transition of the window that was not delivered
// at once, and every subject standing in warning, one line per subject, in the order
// subjects come out in. A frozen subject's transition still counts — it was recorded from
// values that were fresh — while its standing reading does not, because that reading is
// stale (docs/specs/evaluation.md#digest).
func (e *Evaluator) entries(ctx context.Context, subjects []Subject, since, now, occurrence time.Time) ([]Message, error) {
	events, err := e.store.EventsBetween(ctx, since, now)
	if err != nil {
		return nil, err
	}
	// The window is read newest-last, so a subject ends up with the last thing that
	// happened to it and is listed once.
	newest := make(map[string]storage.Transition, len(events))
	for _, event := range events {
		if key, err := event.Key(); err == nil {
			newest[key] = event
		}
	}

	var out []Message
	for _, subject := range subjects {
		key, err := subject.Key()
		if err != nil {
			continue
		}
		// Everything critical touches was delivered at once (ADR 0016). A subject whose
		// last move was such a change has nothing left to report here — but it is still
		// listed below if it is standing in warning now.
		if event, changed := newest[key]; changed && !instant(event) {
			from := storedLevel(event.From, subject.Node, subject.Metric)
			to := storedLevel(event.To, subject.Node, subject.Metric)
			out = append(out, message(subject, from, to, event.Readings, event.FromSince, event.At))
			continue
		}
		// A stale reading says nothing about now, so a frozen subject is not listed as
		// standing in warning.
		if subject.Level == Warning && !subject.Frozen {
			out = append(out, message(subject, Warning, Warning, subject.Readings, subject.Since, occurrence))
		}
	}
	return out, nil
}
