// Package state is the semantic core as every skin reads it: which subjects exist now, the
// level evaluation last stored for each, and whether the values behind it are fresh
// (docs/specs/state.md). It judges nothing and writes nothing.
package state

import (
	"log/slog"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/storage"
)

// State is everything at one instant. A nil Level is no level: nothing under it has one.
// Watched and Unwatched count the subjects a threshold is stored for and those without
// one, because a hub that watches nothing must not look like a hub where nothing is wrong
// (docs/specs/state.md#model).
type State struct {
	At        time.Time
	Level     *evaluate.Level
	Watched   int
	Unwatched int
	Nodes     []Node
	Subjects  []Subject
}

// Node is one node that has reported. Its Level counts only subjects that are not stale:
// what is wrong now by fresh values. Configured says the file still names it, which is a
// different statement from a subject being watched.
type Node struct {
	Node         string
	Configured   bool
	AgentVersion string
	LastSeen     time.Time
	Level        *evaluate.Level
	Watched      int
	Unwatched    int
}

// Subject is a series, plus one per node for its own silence (ADR 0033). Level is nil,
// and Since zero, when evaluation holds no level for it that this build can read — which
// is always so for a series nothing watches. Every subject of a node carries a staleness
// verdict (ADR 0034).
type Subject struct {
	Node    string
	Metric  string
	Labels  map[string]string
	Watched bool
	Level   *evaluate.Level
	Since   time.Time
	Stale   bool
	// Unit, Value and TS are the newest reading of the series. The silence subject has
	// none: its input is the node's last-seen time.
	Unit  history.Unit
	Value *float64
	TS    time.Time
}

// Build reads the state out of one snapshot at now. targets resolves a node as evaluation
// reads it, and false for a node the configuration does not name.
func Build(targets func(node string) (evaluate.Target, bool), snap storage.Snapshot, now time.Time) State {
	out := State{At: now, Nodes: make([]Node, 0, len(snap.Nodes)), Subjects: []Subject{}}

	resolved := map[string]evaluate.Target{}
	var configured []evaluate.Target
	for _, reported := range snap.Nodes {
		if target, known := targets(reported.Node); known {
			resolved[reported.Node] = target
			configured = append(configured, target)
		}
	}

	// A series is watched when a threshold is stored for it, whether or not this build
	// can judge by it: something is set, and saying otherwise would read as "nobody
	// configured this" (docs/specs/state.md#listing).
	set := make(map[string]struct{}, len(snap.Thresholds))
	for _, threshold := range snap.Thresholds {
		set[seriesKey(threshold.Series.Node, threshold.Series.Metric, threshold.Series.Labels)] = struct{}{}
	}

	// Evaluation's own subjects, so the state lists and freezes exactly what a tick would.
	// The level a tick would decide now is not reported: the stored one is (ADR 0030).
	watched := map[string]Subject{}
	for _, subject := range evaluate.Subjects(configured, snap, now) {
		one := Subject{
			Node:    subject.Node,
			Metric:  subject.Metric,
			Labels:  subject.Labels,
			Watched: true,
		}
		if subject.Restored {
			level := subject.Previous
			one.Level, one.Since = &level, subject.Since
		}
		// The silence subject reads hub receipt time, which is never stale.
		one.Stale = subject.Metric != evaluate.SilenceMetric && subject.Frozen
		watched[seriesKey(subject.Node, subject.Metric, subject.Labels)] = one
	}

	for _, reported := range snap.Nodes {
		target, known := resolved[reported.Node]
		node := Node{
			Node:         reported.Node,
			Configured:   known,
			AgentVersion: reported.AgentVersion,
			LastSeen:     reported.LastSeen,
		}

		if silence, judged := watched[seriesKey(reported.Node, evaluate.SilenceMetric, nil)]; judged {
			// Silence is judged without a threshold, so it counts as neither watched nor
			// unwatched: the counts are of the series somebody has to configure, and a
			// hub where they are zero is watching nothing (docs/specs/state.md#model).
			node.Level = worse(node.Level, silence.Level)
			out.Subjects = append(out.Subjects, silence)
		}
		for _, value := range reported.Values {
			key := seriesKey(reported.Node, value.Metric, value.Labels)
			one, judged := watched[key]
			_, isWatched := set[key]
			if !judged {
				one = Subject{Node: reported.Node, Metric: value.Metric, Labels: value.Labels}
			}
			// A threshold this build cannot judge by still makes the series watched: it
			// says "something is set", which a reader has to be able to tell from
			// "nobody set anything" (docs/specs/state.md#listing).
			one.Watched = isWatched
			// A tick freezes the series it judged; everything else — unwatched, or
			// watched by a threshold this build cannot read — is aged here by the same
			// rule, so nothing reads as fresh merely because nothing judged it.
			if !judged || !known {
				one.Stale = staleOf(target, known, reported.LastSeen, value, now)
			}
			one.Unit, one.Value, one.TS = history.UnitOf(value.Metric), &value.Value, value.TS
			if isWatched {
				node.Watched++
				if !one.Stale {
					node.Level = worse(node.Level, one.Level)
				}
			} else if known {
				// A node the file no longer names cannot be given a threshold that would
				// be judged, so its series are not counted as waiting for one; the digest
				// counts the same population (docs/specs/evaluation.md#digest).
				node.Unwatched++
			}
			out.Subjects = append(out.Subjects, one)
		}

		out.Watched += node.Watched
		out.Unwatched += node.Unwatched
		out.Level = worse(out.Level, node.Level)
		out.Nodes = append(out.Nodes, node)
	}

	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Node < out.Nodes[j].Node })
	sortSubjects(out.Subjects)
	return out
}

// staleOf ages a series nothing watches by the same rule evaluation freezes a subject by.
// A node the file no longer names is stale whatever its values say: nothing will refresh
// them (docs/specs/state.md#staleness).
func staleOf(target evaluate.Target, configured bool, lastSeen time.Time, value storage.Value, now time.Time) bool {
	if !configured {
		return true
	}
	return target.Frozen(value.Sensor, lastSeen, value.TS, now)
}

// seriesKey identifies a series by the encoding evaluation keys a subject on, so the two
// cannot drift apart.
func seriesKey(node, metric string, labels map[string]string) string {
	key, err := storage.Subject{Node: node, Metric: metric, Labels: labels}.Key()
	if err != nil {
		slog.Error("identify a series", "node", node, "metric", metric, "error", err)
	}
	return key
}

// worse is the more severe of two levels, either of which may be absent.
func worse(a, b *evaluate.Level) *evaluate.Level {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

// sortSubjects orders by node, a node's silence first, then metric, then labels. No
// label is privileged: grouping a volume's series is the page's own rendering
// (docs/specs/state.md#ordering).
func sortSubjects(subjects []Subject) {
	sort.Slice(subjects, func(i, j int) bool {
		a, b := subjects[i], subjects[j]
		switch {
		case a.Node != b.Node:
			return a.Node < b.Node
		case (a.Metric == evaluate.SilenceMetric) != (b.Metric == evaluate.SilenceMetric):
			return a.Metric == evaluate.SilenceMetric
		case a.Metric != b.Metric:
			return a.Metric < b.Metric
		}
		return storage.LabelKey(a.Labels) < storage.LabelKey(b.Labels)
	})
}
