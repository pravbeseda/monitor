package evaluate

import (
	"context"
	"log/slog"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// repeatAfter is how long an unresolved critical stays quiet before it is said again
// (ADR 0006). Nothing below critical repeats at all: that is what the digest is for.
const repeatAfter = 24 * time.Hour

// Message is one notification as every channel receives it. It carries the same fields
// whatever produced it, so a channel formats rather than decides
// (docs/specs/evaluation.md#messages).
type Message struct {
	Node   string
	Metric string
	Labels map[string]string
	// From is the level the subject left and To the level it reached. A repeat and a
	// digest entry for a subject that has not moved carry the same level in both.
	From, To Level
	// Readings are the values that produced the message, keyed by metric id. The silence
	// subject has none: its input is hub receipt time.
	Readings map[string]float64
	// Since is when the subject entered the level it is leaving, which is how long a
	// reader is told it had been there.
	Since time.Time
	// At is when the change was recorded, or the instant of a repeat.
	At time.Time
}

// Notifier is where a notification leaves the hub. What is worth sending is decided before
// a channel is called (ADR 0006, ADR 0016); a channel formats and delivers.
type Notifier interface {
	// Notify delivers one message.
	Notify(ctx context.Context, m Message) error
	// Digest delivers the day's summary, and how many series nobody has given a
	// threshold — a hub watching nothing has to say so on the channel its operator
	// reads, or it looks exactly like a hub where nothing is wrong (ADR 0032). It is
	// called when there is something to say, which includes that.
	Digest(ctx context.Context, at time.Time, entries []Message, unwatched int) error
}

// instantLevels are the levels whose transitions ADR 0016 delivers at once: entering
// critical, or leaving it for either lower level. It is the one statement of that rule —
// the store is handed it rather than knowing it, so the two cannot drift apart.
func instantLevels() []string { return []string{Critical.String()} }

// instant reports whether an event is one a channel is told about on its own.
func instant(change storage.Transition) bool {
	for _, level := range instantLevels() {
		if change.To == level || change.From == level {
			return true
		}
	}
	return false
}

// storedLevel reads a level out of the event log, which outlives any one build. A name
// this build cannot read comes back as `ok` — the message has to say something — and the
// fact is logged, so a reader who was told the wrong thing can find out why.
func storedLevel(text, node, metric string) Level {
	found, readable := ParseLevel(text)
	if !readable {
		slog.Warn("a recorded level this build does not know",
			"node", node, "metric", metric, "level", text)
	}
	return found
}

// message is one notification about a subject. The subject's identity is always the same
// three fields; what a message is about is the rest.
func message(s Subject, from, to Level, readings map[string]float64, since, at time.Time) Message {
	return Message{
		Node: s.Node, Metric: s.Metric, Labels: s.Labels,
		From: from, To: to, Readings: readings, Since: since, At: at,
	}
}

// due decides what a subject is owed. Delivery is driven by the subject's newest event
// against last_notified_at rather than by what changed on this tick, so a send that failed
// is tried again instead of being lost.
//
// A frozen subject is owed the first kind and not the second: the event was recorded from
// fresh values and is still undelivered, while a repeat would be a statement about values
// nobody may judge any more.
func due(s Subject, newest storage.Transition, recorded bool, now time.Time) (Message, bool) {
	// An event recorded before the level this subject holds began is not owed: clearing a
	// threshold forgets the level but keeps the log, and setting one again starts a new
	// subject rather than inheriting somebody else's undelivered news
	// (docs/specs/evaluation.md#configuration-changes).
	if recorded && instant(newest) && newest.At.After(s.LastNotifiedAt) && !newest.At.Before(s.Since) {
		from := storedLevel(newest.From, s.Node, s.Metric)
		to := storedLevel(newest.To, s.Node, s.Metric)
		return message(s, from, to, newest.Readings, newest.FromSince, newest.At), true
	}
	if s.Frozen {
		return Message{}, false
	}
	if s.Level == Critical && (s.LastNotifiedAt.IsZero() || now.Sub(s.LastNotifiedAt) >= repeatAfter) {
		return message(s, s.Level, s.Level, s.Readings, s.Since, now), true
	}
	return Message{}, false
}
