package evaluate

import (
	"log/slog"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// SilenceRule is the rule of the subject that carries a node's own silence. It is not a
// configurable rule: it has no thresholds, only the window its class resolves to, and its
// input is hub receipt time, which is always fresh.
const SilenceRule = "silence"

// StaleFactor turns a sensor's interval into the age at which its values stop being
// evidence: three collections missed is no longer a hiccup. It is exported because a
// history chart breaks its line at the same age (docs/specs/history.md#gaps), and two
// copies of the number would let the two drift apart.
const StaleFactor = 3

// Target is one configured node as evaluation reads it. The hub resolves the layers of
// ADR 0010 and hands over what a tick needs; none of it ever reaches an agent.
type Target struct {
	Node         string
	SilenceAfter time.Duration
	// Intervals is the interval each sensor this node runs resolves to. A rule whose
	// sensor is absent — switched off, or named by no layer — has no subjects here,
	// because nothing collects for it.
	Intervals map[string]time.Duration
	// Rules judge a volume the file says nothing about; Volumes carries the rules of the
	// mounts it names, keyed by mount and then by rule name.
	Rules   map[string]Rule
	Volumes map[string]map[string]Rule
}

// Rule returns the thresholds that judge one subject of this node: the rules of the volume
// at that mount when the file names it, and the node's own otherwise. A mount is matched
// byte for byte, exactly as the sensor reports it, so a trailing slash is another volume.
func (t Target) Rule(name, mount string) (Rule, bool) {
	if volume, named := t.Volumes[mount]; named {
		if found, ok := volume[name]; ok {
			return found, true
		}
	}
	found, ok := t.Rules[name]
	return found, ok
}

// Frozen reports whether a value of a sensor, stamped at ts, is past judging at now: its
// node has been silent longer than its class allows, or the value is older than three of the
// sensor's intervals. A sensor the node does not run has no interval and never freezes. It is
// exported because the index page hides and marks rows by the same rule
// (docs/specs/history.md#page).
func (t Target) Frozen(sensor string, lastSeen, ts, now time.Time) bool {
	interval, runs := t.Intervals[sensor]
	if !runs {
		return false
	}
	return t.silent(lastSeen, now) || now.Sub(ts) > StaleFactor*interval
}

func (t Target) silent(lastSeen, now time.Time) bool {
	return now.Sub(lastSeen) > t.SilenceAfter
}

// Subject is one thing that has a level, as one tick sees it: the triple (node, rule,
// labels), what it was, what it is now, and the values that decided it.
type Subject struct {
	storage.Subject
	// Previous is the level the subject held when the tick began, and Since is when it
	// reached that level. A subject with no stored state was previously ok.
	Previous Level
	Since    time.Time
	// Level is what the subject is at now.
	Level Level
	// LastNotifiedAt is zero until a message about this subject has been delivered.
	LastNotifiedAt time.Time
	// Readings are the values that produced Level, keyed by metric id: the event log
	// outlives any rule's own names for them.
	Readings map[string]float64
	// Frozen says the values behind the subject are stale, so none of them was judged: it
	// keeps its level and its Since, writes no event, sends no repeat, and is left out of
	// the digest.
	Frozen bool
}

// Changed reports whether this tick moved the subject, which is what writes an event.
func (s Subject) Changed() bool { return !s.Frozen && s.Level != s.Previous }

// Mount is the volume a subject speaks for, and what messages are ordered by within a
// node. The silence subject has none.
func (s Subject) Mount() string { return s.Labels["mount"] }

// Subjects is what one tick evaluates: every subject of every configured node that has
// reported, in the order messages leave in — by node name, then by mount.
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

	var out []Subject
	for _, target := range targets {
		node, ever := reported[target.Node]
		if !ever {
			continue // a node the file lists and no agent has installed is not an incident.
		}
		// Silence and freezing read one clock, so a node that has just fallen silent freezes
		// its other subjects in this tick rather than the next.
		out = append(out, silenceSubject(target, target.silent(node.LastSeen, now), stored, now))
		out = append(out, volumeSubjects(target, node, stored, now)...)
	}
	sortSubjects(out)
	return out
}

func silenceSubject(target Target, silent bool, stored map[string]storage.State, now time.Time) Subject {
	subject := Subject{Subject: storage.Subject{Node: target.Node, Rule: SilenceRule}}
	restore(&subject, stored, now)
	subject.Level = OK
	if silent {
		subject.Level = Critical
	}
	return subject
}

// volumeSubjects builds one subject per complete join of every rule the node runs a sensor
// for. A rule whose sensor is not delivered has no subjects: nothing collects for it, so
// every value it could read would be stale by definition.
func volumeSubjects(target Target, node storage.NodeState, stored map[string]storage.State, now time.Time) []Subject {
	var out []Subject
	for _, name := range Names() {
		definition, _ := Lookup(name)
		if _, runs := target.Intervals[definition.Sensor]; !runs {
			continue
		}
		for _, joined := range join(definition, node.Values) {
			rule, judged := target.Rule(name, joined.labels["mount"])
			if !judged {
				continue
			}
			subject := Subject{
				Subject:  storage.Subject{Node: target.Node, Rule: name, Labels: joined.labels},
				Readings: map[string]float64{definition.Free: joined.free, definition.Pct: joined.pct},
				Frozen:   target.Frozen(definition.Sensor, node.LastSeen, joined.oldest, now),
			}
			restore(&subject, stored, now)
			subject.Level = subject.Previous
			if !subject.Frozen {
				subject.Level = rule.Level(subject.Previous, joined.free, joined.pct)
			}
			out = append(out, subject)
		}
	}
	return out
}

// restore fills in what a restart left behind. A level this build cannot read is treated
// as a new subject rather than guessed at: corrupt data must not stop the hub watching the
// rest.
func restore(s *Subject, stored map[string]storage.State, now time.Time) {
	s.Since = now
	key, err := s.Key()
	if err != nil {
		slog.Error("identify a subject", "node", s.Node, "rule", s.Rule, "error", err)
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
			"node", s.Node, "rule", s.Rule, "level", state.Level)
		return
	}
	s.Previous, s.Since = level, state.Since
}

// reading is the two series of one volume, joined on byte-identical labels.
type reading struct {
	labels    map[string]string
	free, pct float64
	// oldest is the collection time of the older half: a join is only as fresh as that.
	oldest          time.Time
	gotFree, gotPct bool
}

// join pairs the two series a rule reads. An incomplete join is not a level, so a volume
// that has reported only one of them yields no subject at all.
func join(definition Definition, values []storage.Value) []reading {
	pairs := map[string]*reading{}
	var order []string
	for _, value := range values {
		if value.Metric != definition.Free && value.Metric != definition.Pct {
			continue
		}
		// Both series belong to one node and one rule here, so the labels alone
		// identify the volume; Subject.Key encodes them the way the database keys on
		// them, which is what "byte-identical labels" means.
		key, err := storage.Subject{Labels: value.Labels}.Key()
		if err != nil {
			slog.Error("join a series", "metric", value.Metric, "error", err)
			continue
		}
		pair, seen := pairs[key]
		if !seen {
			pair = &reading{labels: value.Labels, oldest: value.TS}
			pairs[key] = pair
			order = append(order, key)
		}
		if value.TS.Before(pair.oldest) {
			pair.oldest = value.TS
		}
		if value.Metric == definition.Free {
			pair.free, pair.gotFree = value.Value, true
		} else {
			pair.pct, pair.gotPct = value.Value, true
		}
	}

	out := make([]reading, 0, len(order))
	for _, key := range order {
		if pair := pairs[key]; pair.gotFree && pair.gotPct {
			out = append(out, *pair)
		}
	}
	return out
}

// sortSubjects puts messages and digest entries in the order the spec names, and breaks
// the remaining ties on the encoded subject so that two volumes sharing a mount point
// still come out the same way on every tick.
func sortSubjects(subjects []Subject) {
	sort.Slice(subjects, func(i, j int) bool {
		a, b := subjects[i], subjects[j]
		switch {
		case a.Node != b.Node:
			return a.Node < b.Node
		case a.Mount() != b.Mount():
			return a.Mount() < b.Mount()
		case a.Rule != b.Rule:
			return a.Rule < b.Rule
		}
		first, _ := a.Key()
		second, _ := b.Key()
		return first < second
	})
}
