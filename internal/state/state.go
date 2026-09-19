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
type State struct {
	At       time.Time
	Level    *evaluate.Level
	Nodes    []Node
	Subjects []Subject
	Readings []Reading
}

// Node is one node that has reported. Its Level counts only subjects that are not stale:
// what is wrong now by fresh values.
type Node struct {
	Node         string
	Configured   bool
	AgentVersion string
	LastSeen     time.Time
	Level        *evaluate.Level
}

// Subject is what has a level. Level is nil, and Since zero, when evaluation holds no level
// for it that this build can read.
type Subject struct {
	Node   string
	Rule   string
	Labels map[string]string
	Level  *evaluate.Level
	Since  time.Time
	Stale  bool
	Values []Value
}

// Value is the newest reading of one series.
type Value struct {
	Metric string
	Unit   history.Unit
	Value  float64
	TS     time.Time
}

// Reading is the newest value of a series no subject reads. Stale is nil when no freshness
// rule applies to it: no rule reads its metric, or its node runs no sensor for it.
type Reading struct {
	Node   string
	Labels map[string]string
	Value
	Stale *bool
}

// Build reads the state out of one snapshot at now. targets resolves a node as evaluation
// reads it, and false for a node the configuration does not name.
func Build(targets func(node string) (evaluate.Target, bool), snap storage.Snapshot, now time.Time) State {
	out := State{
		At:       now,
		Nodes:    make([]Node, 0, len(snap.Nodes)),
		Subjects: []Subject{},
		Readings: []Reading{},
	}
	resolved := map[string]evaluate.Target{}
	var configured []evaluate.Target
	series := map[string]storage.Value{}
	for _, reported := range snap.Nodes {
		if target, known := targets(reported.Node); known {
			resolved[reported.Node] = target
			configured = append(configured, target)
		}
		for _, value := range reported.Values {
			series[seriesKey(reported.Node, value.Metric, value.Labels)] = value
		}
	}

	// Evaluation's own subjects, so the state lists and freezes exactly what a tick would.
	// The level a tick would decide now is not reported: the stored one is.
	read := map[string]bool{}
	fresh := map[string]*evaluate.Level{}
	for _, subject := range evaluate.Subjects(configured, snap, now) {
		one := subjectOf(subject, series, read)
		if !one.Stale {
			fresh[one.Node] = worse(fresh[one.Node], one.Level)
		}
		out.Subjects = append(out.Subjects, one)
	}

	for _, reported := range snap.Nodes {
		target, known := resolved[reported.Node]
		node := Node{
			Node:         reported.Node,
			Configured:   known,
			AgentVersion: reported.AgentVersion,
			LastSeen:     reported.LastSeen,
			Level:        fresh[reported.Node],
		}
		out.Level = worse(out.Level, node.Level)
		out.Nodes = append(out.Nodes, node)

		for _, value := range reported.Values {
			if read[seriesKey(reported.Node, value.Metric, value.Labels)] {
				continue
			}
			out.Readings = append(out.Readings, Reading{
				Node:   reported.Node,
				Labels: value.Labels,
				Value:  valueOf(value),
				Stale:  staleOf(target, known, reported.LastSeen, value, now),
			})
		}
	}

	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Node < out.Nodes[j].Node })
	sortSubjects(out.Subjects)
	sortReadings(out.Readings)
	return out
}

// subjectOf reports one subject with the level stored for it and the values it reads,
// marking those values as read so they are not listed again as readings.
func subjectOf(subject evaluate.Subject, series map[string]storage.Value, read map[string]bool) Subject {
	out := Subject{
		Node:   subject.Node,
		Rule:   subject.Rule,
		Labels: subject.Labels,
		Stale:  subject.Frozen,
		Values: []Value{},
	}
	if subject.Restored {
		level := subject.Previous
		out.Level, out.Since = &level, subject.Since
	}
	definition, judged := evaluate.Lookup(subject.Rule)
	if !judged {
		return out // the silence subject reads no series.
	}
	for _, metric := range []string{definition.Free, definition.Pct} {
		key := seriesKey(subject.Node, metric, subject.Labels)
		if value, stored := series[key]; stored {
			out.Values = append(out.Values, valueOf(value))
			read[key] = true
		}
	}
	sort.Slice(out.Values, func(i, j int) bool { return out.Values[i].Metric < out.Values[j].Metric })
	return out
}

// staleOf ages a reading by evaluation's freezing rule when that rule applies to it at all.
func staleOf(target evaluate.Target, configured bool, lastSeen time.Time, value storage.Value, now time.Time) *bool {
	sensor, declared := evaluate.SensorOf(value.Metric)
	if !configured || !declared {
		return nil
	}
	if _, runs := target.Intervals[sensor]; !runs {
		return nil
	}
	stale := target.Frozen(sensor, lastSeen, value.TS, now)
	return &stale
}

func valueOf(value storage.Value) Value {
	return Value{Metric: value.Metric, Unit: history.UnitOf(value.Metric), Value: value.Value, TS: value.TS}
}

// seriesKey identifies a series by the encoding evaluation joins on, so a subject reads
// exactly the series evaluation joined for it.
func seriesKey(node, metric string, labels map[string]string) string {
	key, err := storage.Subject{Node: node, Rule: metric, Labels: labels}.Key()
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

// sortSubjects orders by node, then mount, then rule, then labels, a node's silence subject
// first even beside a volume that reports no mount (docs/specs/state.md#ordering).
func sortSubjects(subjects []Subject) {
	sort.Slice(subjects, func(i, j int) bool {
		a, b := subjects[i], subjects[j]
		switch {
		case a.Node != b.Node:
			return a.Node < b.Node
		case (a.Rule == evaluate.SilenceRule) != (b.Rule == evaluate.SilenceRule):
			return a.Rule == evaluate.SilenceRule
		case a.Labels["mount"] != b.Labels["mount"]:
			return a.Labels["mount"] < b.Labels["mount"]
		case a.Rule != b.Rule:
			return a.Rule < b.Rule
		}
		return storage.LabelKey(a.Labels) < storage.LabelKey(b.Labels)
	})
}

func sortReadings(readings []Reading) {
	sort.Slice(readings, func(i, j int) bool {
		a, b := readings[i], readings[j]
		switch {
		case a.Node != b.Node:
			return a.Node < b.Node
		case a.Metric != b.Metric:
			return a.Metric < b.Metric
		}
		return storage.LabelKey(a.Labels) < storage.LabelKey(b.Labels)
	})
}
