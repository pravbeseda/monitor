package evaluate

import (
	"log/slog"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// Target is one configured node as evaluation reads it. The hub resolves the layers of
// ADR 0010 and hands over what a tick needs; none of it ever reaches an agent, and none
// of it carries a threshold — those are stored beside the measurements (ADR 0032).
type Target struct {
	Node         string
	SilenceAfter time.Duration
	// Intervals is the interval each sensor this node runs resolves to. A series whose
	// sensor is absent — switched off, or named by no layer — is frozen: nothing will
	// refresh it.
	Intervals map[string]time.Duration
}

// Frozen reports whether a value of a sensor, stamped at ts, is past judging at now. A
// silent node freezes everything under it, whatever produced it. Otherwise a series whose
// newest value names no sensor has no freshness rule at all and is never frozen on age;
// one whose node does not run that sensor is frozen outright, because nothing will
// refresh it; and the rest age out after three intervals. It is exported because the
// state reports the same verdict (docs/specs/state.md#staleness).
func (t Target) Frozen(sensor string, lastSeen, ts, now time.Time) bool {
	if t.silent(lastSeen, now) {
		return true
	}
	if sensor == "" {
		return false
	}
	interval, runs := t.Intervals[sensor]
	if !runs {
		return true
	}
	return now.Sub(ts) > StaleFactor*interval
}

// Ages reports whether a series has a freshness rule at all: a silent node ages
// everything, and otherwise only a series that names a sensor can be judged stale
// (docs/specs/state.md#staleness).
func (t Target) Ages(sensor string, lastSeen, now time.Time) bool {
	return sensor != "" || t.silent(lastSeen, now)
}

func (t Target) silent(lastSeen, now time.Time) bool {
	return now.Sub(lastSeen) > t.SilenceAfter
}

// Subject is one thing that has a level, as one tick sees it: a series — the triple
// (node, metric, labels) — with what it was, what it is now, and the value that decided
// it. Only a watched series is one: an unconfigured series is stored and displayed,
// never judged (ADR 0032).
type Subject struct {
	storage.Subject
	// Previous is the level the subject held when the tick began, and Since is when it
	// reached that level. A subject with no stored state was previously ok.
	Previous Level
	Since    time.Time
	// Level is what the subject is at now.
	Level Level
	// Direction is what it is judged under now, and what is stored with the level: a
	// level earned under the other direction is dropped rather than held.
	Direction storage.Direction
	// Restored says Previous and Since were read back from a stored level rather than
	// assumed for a subject with none, one this build cannot read, or one earned under
	// another direction.
	Restored bool
	// LastNotifiedAt is zero until a message about this subject has been delivered.
	LastNotifiedAt time.Time
	// Readings are the values that produced Level, keyed by metric id. A subject is one
	// series, so this holds its own value alone; the silence subject has none.
	Readings map[string]float64
	// Frozen says the value behind the subject is stale, so it was not judged: it keeps
	// its level and its Since, writes no event, sends no repeat, and is left out of the
	// digest.
	Frozen bool
}

// Changed reports whether this tick moved the subject, which is what writes an event.
func (s Subject) Changed() bool { return !s.Frozen && s.Level != s.Previous }

// Subjects is what one tick evaluates: every node's silence, and every series a threshold
// is stored for, in the order messages leave in — by node, then metric, then labels.
func Subjects(targets []Target, snap storage.Snapshot, now time.Time) []Subject {
	reported := make(map[string]storage.NodeState, len(snap.Nodes))
	for _, node := range snap.Nodes {
		reported[node.Node] = node
	}
	stored := make(map[string]storage.State, len(snap.States))
	for _, state := range snap.States {
		if key, err := state.Key(); err == nil {
			stored[key] = state
		}
	}
	watched := make(map[string]storage.Threshold, len(snap.Thresholds))
	for _, threshold := range snap.Thresholds {
		key, err := storage.Subject{
			Node: threshold.Series.Node, Metric: threshold.Series.Metric, Labels: threshold.Series.Labels,
		}.Key()
		if err != nil {
			slog.Error("identify a threshold", "node", threshold.Series.Node,
				"metric", threshold.Series.Metric, "error", err)
			continue
		}
		watched[key] = threshold
	}

	var out []Subject
	for _, target := range targets {
		node, ever := reported[target.Node]
		if !ever {
			continue // a node the file lists and no agent has installed is not an incident.
		}
		// Silence and freezing read one clock, so a node that has just fallen silent freezes
		// its other subjects in this tick rather than the next.
		out = append(out, silenceSubject(target, target.silent(node.LastSeen, now), stored, now))
		out = append(out, seriesSubjects(target, node, watched, stored, now)...)
	}
	sortSubjects(out)
	return out
}

func silenceSubject(target Target, silent bool, stored map[string]storage.State, now time.Time) Subject {
	subject := Subject{Subject: storage.Subject{Node: target.Node, Metric: SilenceMetric}}
	restore(&subject, stored, now)
	subject.Level = OK
	if silent {
		subject.Level = Critical
	}
	return subject
}

// seriesSubjects builds one subject per watched series of a node. A series nobody has
// given a threshold is not judged at all, so it produces no subject, no event and no
// message (ADR 0032).
func seriesSubjects(target Target, node storage.NodeState, watched map[string]storage.Threshold,
	stored map[string]storage.State, now time.Time,
) []Subject {
	var out []Subject
	for _, value := range node.Values {
		subject := Subject{
			Subject:  storage.Subject{Node: target.Node, Metric: value.Metric, Labels: value.Labels},
			Readings: map[string]float64{value.Metric: value.Value},
			Frozen:   target.Frozen(value.Sensor, node.LastSeen, value.TS, now),
		}
		key, err := subject.Key()
		if err != nil {
			slog.Error("identify a subject", "node", subject.Node, "metric", subject.Metric, "error", err)
			continue
		}
		threshold, isWatched := watched[key]
		if !isWatched {
			continue
		}
		if !Readable(threshold) {
			slog.Warn("a stored threshold this build cannot read",
				"node", subject.Node, "metric", subject.Metric, "direction", string(threshold.Direction))
			continue
		}
		subject.Direction = threshold.Direction
		restore(&subject, stored, now)
		subject.Level = subject.Previous
		if !subject.Frozen {
			subject.Level = levelOf(threshold, subject.Previous, value.Value)
		}
		out = append(out, subject)
	}
	return out
}

// restore fills in what a restart left behind. A level this build cannot read is treated
// as a new subject rather than guessed at: corrupt data must not stop the hub watching the
// rest. A level earned under another direction is dropped the same way: hysteresis holds
// a level against the comparison that created it, and the mirrored band would hold a
// subject at a value that is now perfectly good.
func restore(s *Subject, stored map[string]storage.State, now time.Time) {
	s.Since = now
	key, err := s.Key()
	if err != nil {
		slog.Error("identify a subject", "node", s.Node, "metric", s.Metric, "error", err)
		return
	}
	state, known := stored[key]
	if !known {
		return
	}
	s.LastNotifiedAt = state.LastNotifiedAt
	level, readable := ParseLevel(state.Level)
	if !readable {
		slog.Warn("a stored level this build does not know",
			"node", s.Node, "metric", s.Metric, "level", state.Level)
		return
	}
	if storage.Direction(state.Direction) != s.Direction {
		slog.Info("a level was earned under another direction and is dropped",
			"node", s.Node, "metric", s.Metric, "stored", state.Direction, "now", string(s.Direction))
		return
	}
	s.Previous, s.Since, s.Restored = level, state.Since, true
}

// sortSubjects puts messages and digest entries in the order the spec names, and breaks
// the remaining ties on the encoded subject so that two series sharing node, metric and
// mount still come out the same way on every tick.
func sortSubjects(subjects []Subject) {
	sort.Slice(subjects, func(i, j int) bool {
		a, b := subjects[i], subjects[j]
		switch {
		case a.Node != b.Node:
			return a.Node < b.Node
		// A node's silence is the statement about the node itself, so it leads its
		// series whatever the metric ids sort like (docs/specs/state.md#ordering).
		case (a.Metric == SilenceMetric) != (b.Metric == SilenceMetric):
			return a.Metric == SilenceMetric
		case a.Metric != b.Metric:
			return a.Metric < b.Metric
		}
		first, _ := a.Key()
		second, _ := b.Key()
		return first < second
	})
}
