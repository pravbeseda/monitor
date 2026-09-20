package state_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// now is the instant every state in this file is read at.
var now = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

// A 10m silence window and, at three collections missed, a 45m staleness window.
const (
	silenceAfter = 10 * time.Minute
	interval     = 15 * time.Minute
	staleAfter   = evaluate.StaleFactor * interval
)

// watching configures every node it is asked about except the ones in forgotten: each runs
// the disk sensor at interval and falls silent after silenceAfter.
func watching(forgotten ...string) func(string) (evaluate.Target, bool) {
	return func(node string) (evaluate.Target, bool) {
		if slices.Contains(forgotten, node) {
			return evaluate.Target{}, false
		}
		return evaluate.Target{
			Node:         node,
			SilenceAfter: silenceAfter,
			Intervals:    map[string]time.Duration{"disk": interval},
		}, true
	}
}

func volume(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}
}

func removable(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "apfs", "removable": "true"}
}

// reported is the pair of series of one volume, both collected age ago by the disk sensor.
func reported(labels map[string]string, age time.Duration) []storage.Value {
	return []storage.Value{
		{Metric: "disk.free_bytes", Sensor: "disk", Labels: labels, Value: 40e9, TS: now.Add(-age)},
		{Metric: "disk.free_pct", Sensor: "disk", Labels: labels, Value: 31.25, TS: now.Add(-age)},
	}
}

// heard is a node last heard from age ago, carrying the given series.
func heard(node string, age time.Duration, values ...storage.Value) storage.NodeState {
	return storage.NodeState{Node: node, LastSeen: now.Add(-age), AgentVersion: "v1.0.0", Values: values}
}

// watch is the threshold that makes one series watched: 20 GB free is a warning, 10 GB a
// critical, and the volumes reported above sit comfortably above both.
func watch(node, metric string, labels map[string]string) storage.Threshold {
	warning, critical := 20e9, 10e9
	return storage.Threshold{
		Series:    storage.SeriesRef{Node: node, Metric: metric, Labels: labels},
		Direction: storage.Below,
		Warning:   &warning,
		Critical:  &critical,
	}
}

// recorded is a level a tick left behind for one series, under the direction watch stores.
func recorded(node, metric string, labels map[string]string, level evaluate.Level, since time.Time) storage.State {
	return storage.State{
		Subject:   storage.Subject{Node: node, Metric: metric, Labels: labels},
		Level:     level.String(),
		Direction: string(storage.Below),
		Since:     since,
	}
}

// recordedSilence is a level left behind for a node's own silence, which is judged under
// no direction at all.
func recordedSilence(node string, level evaluate.Level, since time.Time) storage.State {
	return storage.State{
		Subject: storage.Subject{Node: node, Metric: evaluate.SilenceMetric},
		Level:   level.String(),
		Since:   since,
	}
}

func build(t *testing.T, snap storage.Snapshot, forgotten ...string) state.State {
	t.Helper()
	return state.Build(watching(forgotten...), snap, now)
}

func subject(t *testing.T, s state.State, node, metric, mount string) state.Subject {
	t.Helper()
	for _, one := range s.Subjects {
		if one.Node == node && one.Metric == metric && one.Labels["mount"] == mount {
			return one
		}
	}
	t.Fatalf("no %s subject of %s at %q among %d", metric, node, mount, len(s.Subjects))
	return state.Subject{}
}

func node(t *testing.T, s state.State, name string) state.Node {
	t.Helper()
	for _, one := range s.Nodes {
		if one.Node == name {
			return one
		}
	}
	t.Fatalf("no node %s among %d", name, len(s.Nodes))
	return state.Node{}
}

func levelOf(level *evaluate.Level) string {
	if level == nil {
		return "null"
	}
	return level.String()
}

func staleOf(stale *bool) string {
	switch {
	case stale == nil:
		return "null"
	case *stale:
		return "true"
	default:
		return "false"
	}
}

// spec: state.md#endpoint — a hub no node has reported to.
func TestNothingReported(t *testing.T) {
	s := build(t, storage.Snapshot{})
	if s.Level != nil || len(s.Nodes) != 0 || len(s.Subjects) != 0 {
		t.Fatalf("an empty hub answered level=%s nodes=%d subjects=%d",
			levelOf(s.Level), len(s.Nodes), len(s.Subjects))
	}
	if s.Watched != 0 || s.Unwatched != 0 {
		t.Fatalf("watched %d unwatched %d, want nothing counted", s.Watched, s.Unwatched)
	}
	if !s.At.Equal(now) {
		t.Fatalf("at = %v, want the instant the state was read", s.At)
	}
}

// spec: state.md#listing
func TestListing(t *testing.T) {
	t.Run("a configured node that has reported", func(t *testing.T) {
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", time.Minute)}})
		got := node(t, s, "server-b")
		if !got.Configured || got.AgentVersion != "v1.0.0" || !got.LastSeen.Equal(now.Add(-time.Minute)) {
			t.Fatalf("node = %+v", got)
		}
		silence := subject(t, s, "server-b", evaluate.SilenceMetric, "")
		if len(silence.Labels) != 0 || silence.Value != nil || !silence.TS.IsZero() || silence.Unit != "" {
			t.Fatalf("silence subject = %+v, want no labels and no value of its own", silence)
		}
	})

	t.Run("a configured node that has never reported", func(t *testing.T) {
		// laptop-a is configured — watching names every node — and has sent nothing.
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0)}})
		for _, one := range s.Nodes {
			if one.Node != "server-b" {
				t.Errorf("node %s listed, but it never reported", one.Node)
			}
		}
		for _, one := range s.Subjects {
			if one.Node != "server-b" {
				t.Errorf("a subject of %s listed, but it never reported", one.Node)
			}
		}
	})

	t.Run("a node the configuration no longer names", func(t *testing.T) {
		values := append(reported(volume("/"), time.Hour),
			storage.Value{Metric: "load.one", Value: 0.4, TS: now})
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, values...)},
			Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
		}
		s := build(t, snap, "server-b")
		got := node(t, s, "server-b")
		if got.Configured || got.Level != nil {
			t.Fatalf("node = configured %v level %s, want false and null", got.Configured, levelOf(got.Level))
		}
		if len(s.Subjects) != 3 {
			t.Fatalf("%d subjects, want every series of the node and no silence", len(s.Subjects))
		}
		for _, one := range s.Subjects {
			if one.Metric == evaluate.SilenceMetric {
				t.Fatalf("a node nothing judges kept a silence subject: %+v", one)
			}
			if staleOf(one.Stale) != "true" {
				t.Fatalf("%s stale = %s, want true: nothing will refresh it",
					one.Metric, staleOf(one.Stale))
			}
			if one.Level != nil {
				t.Fatalf("%s carries level %s, want none: nothing judges this node",
					one.Metric, levelOf(one.Level))
			}
		}
	})

	t.Run("a volume both of whose series are stored", func(t *testing.T) {
		values := reported(volume("/"), time.Minute)
		values[1].TS = now.Add(-2 * time.Minute)
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, values...)}})

		bytes := subject(t, s, "server-b", "disk.free_bytes", "/")
		percent := subject(t, s, "server-b", "disk.free_pct", "/")
		if bytes.Unit != history.Bytes || percent.Unit != history.Percent {
			t.Fatalf("units = %s, %s, want the ones the metric ids declare", bytes.Unit, percent.Unit)
		}
		if bytes.Value == nil || *bytes.Value != 40e9 || percent.Value == nil || *percent.Value != 31.25 {
			t.Fatalf("values = %+v, %+v, want each series its own newest value", bytes.Value, percent.Value)
		}
		if !bytes.TS.Equal(values[0].TS) || !percent.TS.Equal(values[1].TS) {
			t.Fatalf("collected %v and %v, want each series its own time", bytes.TS, percent.TS)
		}
	})

	t.Run("a series with a threshold stored for it", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
		}
		if got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/"); !got.Watched {
			t.Fatal("a series a threshold is stored for came out unwatched")
		}
	})

	t.Run("a series with no threshold", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)}}
		got := subject(t, build(t, snap), "server-b", "disk.free_pct", "/")
		if got.Watched || got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("subject = watched %v level %s since %v, want unwatched with no level",
				got.Watched, levelOf(got.Level), got.Since)
		}
	})

	t.Run("a node's silence subject", func(t *testing.T) {
		// Nothing is stored for it and nothing can be: it is judged on the node's class.
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0)}})
		if got := subject(t, s, "server-b", evaluate.SilenceMetric, ""); !got.Watched {
			t.Fatal("the silence subject came out unwatched")
		}
	})

	t.Run("a series whose threshold was removed while it held a level", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			States: []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Critical, now.Add(-time.Hour))},
		}
		got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/")
		if got.Watched || got.Level != nil {
			t.Fatalf("subject = watched %v level %s, want the level forgotten with the threshold",
				got.Watched, levelOf(got.Level))
		}
	})

	t.Run("a series whose stored threshold this build cannot read", func(t *testing.T) {
		unreadable := watch("server-b", "disk.free_bytes", volume("/"))
		unreadable.Direction = "sideways"
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			Thresholds: []storage.Threshold{unreadable},
			States:     []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, now.Add(-time.Hour))},
		}
		got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/")
		if !got.Watched || got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("subject = watched %v level %s since %v, want something set and nothing judged by it",
				got.Watched, levelOf(got.Level), got.Since)
		}
	})

	t.Run("a sensor the node resolves as enabled: false", func(t *testing.T) {
		disabled := func(node string) (evaluate.Target, bool) {
			target, known := watching()(node)
			target.Intervals = map[string]time.Duration{}
			return target, known
		}
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
			States:     []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Critical, now.Add(-time.Hour))},
		}
		s := state.Build(disabled, snap, now)
		got := subject(t, s, "server-b", "disk.free_bytes", "/")
		if staleOf(got.Stale) != "true" || levelOf(got.Level) != "critical" {
			t.Fatalf("subject = stale %s level %s, want it stale and keeping what was stored",
				staleOf(got.Stale), levelOf(got.Level))
		}
		if got := node(t, s, "server-b"); got.Level != nil {
			t.Fatalf("node level = %s: a level no tick maintains any more still counted", levelOf(got.Level))
		}
	})

	t.Run("a series with no labels", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", 0, storage.Value{Metric: "load.one", Value: 0.4, TS: now}),
		}}
		if got := subject(t, build(t, snap), "server-b", "load.one", ""); len(got.Labels) != 0 {
			t.Fatalf("labels = %v, want none", got.Labels)
		}
	})

	t.Run("agent_version of a node that never reported one", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{{Node: "server-b", LastSeen: now}}}
		if got := node(t, build(t, snap), "server-b"); got.AgentVersion != "" {
			t.Fatalf("agent version = %q, want none", got.AgentVersion)
		}
	})
}

// spec: state.md#listing — watched and unwatched count the series listed, per node and in
// total; a node's silence is judged without a threshold and counted in neither.
func TestCounts(t *testing.T) {
	snap := storage.Snapshot{
		Nodes: []storage.NodeState{
			heard("laptop-a", 0, reported(volume("/"), time.Minute)...),
			heard("server-b", 0, reported(volume("/data"), time.Minute)...),
		},
		Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/data"))},
	}
	s := build(t, snap)

	if got := node(t, s, "laptop-a"); got.Watched != 0 || got.Unwatched != 2 {
		t.Errorf("laptop-a watched %d unwatched %d, want both its series unwatched", got.Watched, got.Unwatched)
	}
	if got := node(t, s, "server-b"); got.Watched != 1 || got.Unwatched != 1 {
		t.Errorf("server-b watched %d unwatched %d, want one series of the volume watched", got.Watched, got.Unwatched)
	}
	if s.Watched != 1 || s.Unwatched != 3 {
		t.Errorf("watched %d unwatched %d, want every node's series counted", s.Watched, s.Unwatched)
	}
	// Two silence subjects are listed and counted in neither.
	if s.Watched+s.Unwatched+2 != len(s.Subjects) {
		t.Errorf("%d + %d series counted, %d subjects listed", s.Watched, s.Unwatched, len(s.Subjects))
	}
}

// spec: state.md#listing — a hub where nothing is watched: no threshold is stored, so no
// series is judged anywhere.
func TestNothingIsWatched(t *testing.T) {
	snap := storage.Snapshot{Nodes: []storage.NodeState{
		heard("laptop-a", 0, reported(volume("/"), time.Minute)...),
		heard("server-b", 0, reported(volume("/data"), time.Minute)...),
	}}
	s := build(t, snap)

	for _, one := range s.Subjects {
		if one.Metric == evaluate.SilenceMetric {
			continue
		}
		if one.Watched {
			t.Errorf("%s of %s is watched on a hub with no thresholds", one.Metric, one.Node)
		}
	}
	for _, one := range s.Nodes {
		if one.Unwatched != 2 {
			t.Errorf("%s unwatched = %d, want both its series", one.Node, one.Unwatched)
		}
	}
	if s.Unwatched != 4 {
		t.Errorf("unwatched = %d, want every series of every node", s.Unwatched)
	}
}

// spec: state.md#levels
func TestLevels(t *testing.T) {
	since := now.Add(-12 * time.Hour)
	// watched is one volume whose bytes series is judged, as most of these cases need it.
	watched := func(values []storage.Value, states ...storage.State) storage.Snapshot {
		return storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, values...)},
			Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
			States:     states,
		}
	}

	t.Run("a subject evaluation stored as warning", func(t *testing.T) {
		snap := watched(reported(volume("/"), time.Minute),
			recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, since))
		got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/")
		if levelOf(got.Level) != "warning" || !got.Since.Equal(since) {
			t.Fatalf("level %s since %v, want the stored warning since %v", levelOf(got.Level), got.Since, since)
		}
	})

	t.Run("a value that crossed a threshold after the last tick", func(t *testing.T) {
		values := reported(volume("/"), time.Minute)
		values[0].Value = 1e9 // well past the critical threshold, and not yet judged
		snap := watched(values, recorded("server-b", "disk.free_bytes", volume("/"), evaluate.OK, since))
		if got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/"); levelOf(got.Level) != "ok" {
			t.Fatalf("level = %s: reading the state judged the value itself", levelOf(got.Level))
		}
	})

	t.Run("a subject given a threshold after the last tick", func(t *testing.T) {
		got := subject(t, build(t, watched(reported(volume("/"), time.Minute))), "server-b", "disk.free_bytes", "/")
		if !got.Watched || got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("watched %v level %s since %v, want it watched and not yet judged",
				got.Watched, levelOf(got.Level), got.Since)
		}
	})

	t.Run("a subject stale since it first appeared", func(t *testing.T) {
		got := subject(t, build(t, watched(reported(volume("/"), staleAfter+time.Minute))), "server-b", "disk.free_bytes", "/")
		if got.Level != nil || staleOf(got.Stale) != "true" {
			t.Fatalf("level %s stale %s, want null and stale", levelOf(got.Level), staleOf(got.Stale))
		}
	})

	t.Run("a subject whose stored level this build does not know", func(t *testing.T) {
		corrupt := recorded("server-b", "disk.free_bytes", volume("/"), evaluate.OK, since)
		corrupt.Level = "bogus"
		got := subject(t, build(t, watched(reported(volume("/"), time.Minute), corrupt)), "server-b", "disk.free_bytes", "/")
		if got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("level %s since %v, want both null", levelOf(got.Level), got.Since)
		}
	})

	t.Run("a node whose only critical subject names no sensor", func(t *testing.T) {
		loose := storage.Value{Metric: "load.one", Value: 99, TS: now.Add(-24 * time.Hour)}
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", 0, loose)},
			Thresholds: []storage.Threshold{watch("server-b", "load.one", nil)},
			States:     []storage.State{recorded("server-b", "load.one", nil, evaluate.Critical, since)},
		}
		s := build(t, snap)
		if got := subject(t, s, "server-b", "load.one", ""); staleOf(got.Stale) != "null" {
			t.Fatalf("stale = %s: no freshness rule applies to a series naming no sensor", staleOf(got.Stale))
		}
		if got := node(t, s, "server-b"); levelOf(got.Level) != "critical" {
			t.Fatalf("node level = %s, want critical: that subject is not stale", levelOf(got.Level))
		}
	})

	t.Run("a node whose watched subjects are ok and whose silence is critical", func(t *testing.T) {
		snap := watched(reported(volume("/"), time.Minute),
			recorded("server-b", "disk.free_bytes", volume("/"), evaluate.OK, since),
			recordedSilence("server-b", evaluate.Critical, since))
		if got := node(t, build(t, snap), "server-b"); levelOf(got.Level) != "critical" {
			t.Fatalf("node level = %s, want critical", levelOf(got.Level))
		}
	})

	t.Run("a node with one warning subject and the rest ok", func(t *testing.T) {
		values := append(reported(volume("/"), time.Minute), reported(volume("/data"), time.Minute)...)
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", 0, values...)},
			Thresholds: []storage.Threshold{
				watch("server-b", "disk.free_bytes", volume("/")),
				watch("server-b", "disk.free_bytes", volume("/data")),
			},
			States: []storage.State{
				recorded("server-b", "disk.free_bytes", volume("/"), evaluate.OK, since),
				recorded("server-b", "disk.free_bytes", volume("/data"), evaluate.Warning, since),
				recordedSilence("server-b", evaluate.OK, since),
			},
		}
		if got := node(t, build(t, snap), "server-b"); levelOf(got.Level) != "warning" {
			t.Fatalf("node level = %s, want warning", levelOf(got.Level))
		}
	})

	t.Run("a node none of whose subjects has a level", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)}}
		if got := node(t, build(t, snap), "server-b"); got.Level != nil {
			t.Fatalf("node level = %s, want null", levelOf(got.Level))
		}
	})

	t.Run("a node whose only critical subject is stale", func(t *testing.T) {
		values := append(reported(removable("/Volumes/b"), staleAfter+time.Minute),
			reported(volume("/"), time.Minute)...)
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", 0, values...)},
			Thresholds: []storage.Threshold{
				watch("server-b", "disk.free_bytes", removable("/Volumes/b")),
				watch("server-b", "disk.free_bytes", volume("/")),
			},
			States: []storage.State{
				recorded("server-b", "disk.free_bytes", removable("/Volumes/b"), evaluate.Critical, since),
				recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, since),
				recordedSilence("server-b", evaluate.OK, since),
			},
		}
		s := build(t, snap)
		if got := node(t, s, "server-b"); levelOf(got.Level) != "warning" {
			t.Fatalf("node level = %s, want warning: a stale subject raised it", levelOf(got.Level))
		}
		got := subject(t, s, "server-b", "disk.free_bytes", "/Volumes/b")
		if levelOf(got.Level) != "critical" || staleOf(got.Stale) != "true" {
			t.Fatalf("stale subject level %s stale %s, want critical and stale", levelOf(got.Level), staleOf(got.Stale))
		}
	})

	t.Run("a node silent past its silence_after whose subjects were warning", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard("server-b", silenceAfter+time.Minute, reported(volume("/"), time.Minute)...)},
			Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
			States: []storage.State{
				recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, since),
				recordedSilence("server-b", evaluate.Critical, since),
			},
		}
		if got := node(t, build(t, snap), "server-b"); levelOf(got.Level) != "critical" {
			t.Fatalf("node level = %s, want critical, from its silence subject alone", levelOf(got.Level))
		}
	})

	t.Run("one node critical, another ok", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("laptop-a", 0), heard("server-b", 0)},
			States: []storage.State{
				recordedSilence("laptop-a", evaluate.OK, since),
				recordedSilence("server-b", evaluate.Critical, since),
			},
		}
		if got := build(t, snap); levelOf(got.Level) != "critical" {
			t.Fatalf("response level = %s, want critical", levelOf(got.Level))
		}
	})
}

// spec: state.md#staleness
func TestStaleness(t *testing.T) {
	aged := func(age time.Duration) storage.Snapshot {
		return storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), age)...)}}
	}

	t.Run("a series whose newest value is exactly three intervals old", func(t *testing.T) {
		got := subject(t, build(t, aged(staleAfter)), "server-b", "disk.free_bytes", "/")
		if staleOf(got.Stale) != "false" {
			t.Fatalf("stale = %s at the bound; the bound is inclusive", staleOf(got.Stale))
		}
	})

	t.Run("the same a moment later", func(t *testing.T) {
		since := now.Add(-time.Hour)
		snap := aged(staleAfter + time.Second)
		snap.Thresholds = []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))}
		snap.States = []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, since)}

		got := subject(t, build(t, snap), "server-b", "disk.free_bytes", "/")
		if staleOf(got.Stale) != "true" || levelOf(got.Level) != "warning" || !got.Since.Equal(since) {
			t.Fatalf("stale %s level %s since %v, want stale with the stored level",
				staleOf(got.Stale), levelOf(got.Level), got.Since)
		}
	})

	t.Run("a node silent past its silence_after", func(t *testing.T) {
		loose := storage.Value{Metric: "load.one", Value: 0.4, TS: now}
		snap := storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", silenceAfter+time.Minute, append(reported(volume("/"), time.Minute), loose)...),
		}}
		s := build(t, snap)
		for metric, mount := range map[string]string{
			"disk.free_bytes": "/", "disk.free_pct": "/", "load.one": "",
		} {
			if got := subject(t, s, "server-b", metric, mount); staleOf(got.Stale) != "true" {
				t.Errorf("%s stale = %s, want a silent node to age everything under it", metric, staleOf(got.Stale))
			}
		}
		if got := subject(t, s, "server-b", evaluate.SilenceMetric, ""); staleOf(got.Stale) != "false" {
			t.Errorf("the silence subject is stale = %s, want false", staleOf(got.Stale))
		}
	})

	t.Run("a series whose newest value names no sensor", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", 0, storage.Value{Metric: "load.one", Value: 0.4, TS: now.Add(-24 * time.Hour)}),
		}}
		if got := subject(t, build(t, snap), "server-b", "load.one", ""); staleOf(got.Stale) != "null" {
			t.Fatalf("stale = %s, want null: no freshness rule applies to it", staleOf(got.Stale))
		}
	})

	t.Run("a series whose node resolves no interval for its sensor", func(t *testing.T) {
		none := func(node string) (evaluate.Target, bool) {
			return evaluate.Target{Node: node, SilenceAfter: silenceAfter}, true
		}
		s := state.Build(none, aged(time.Minute), now)
		if got := subject(t, s, "server-b", "disk.free_bytes", "/"); staleOf(got.Stale) != "true" {
			t.Fatalf("stale = %s, want true: nothing will refresh it", staleOf(got.Stale))
		}
	})

	t.Run("a stale removable volume", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", 0, reported(removable("/Volumes/b"), staleAfter+time.Minute)...),
		}}
		if got := subject(t, build(t, snap), "server-b", "disk.free_pct", "/Volumes/b"); staleOf(got.Stale) != "true" {
			t.Fatalf("an unplugged drive is stale = %s, want true and still listed", staleOf(got.Stale))
		}
	})

	t.Run("values stamped an hour ahead of the hub's clock", func(t *testing.T) {
		if got := subject(t, build(t, aged(-time.Hour)), "server-b", "disk.free_bytes", "/"); staleOf(got.Stale) != "false" {
			t.Fatalf("stale = %s, want a value from a clock running ahead to count as fresh", staleOf(got.Stale))
		}
	})
}

// spec: state.md#ordering
func TestOrdering(t *testing.T) {
	values := append(reported(volume("/data"), time.Minute), reported(volume("/"), time.Minute)...)
	values = append(values,
		storage.Value{Metric: "load.one", Labels: map[string]string{"cpu": "b"}, Value: 1, TS: now},
		storage.Value{Metric: "load.one", Labels: map[string]string{"cpu": "a"}, Value: 1, TS: now},
	)
	s := build(t, storage.Snapshot{Nodes: []storage.NodeState{
		heard("server-b", 0, values...),
		heard("laptop-a", 0, storage.Value{Metric: "load.one", Value: 1, TS: now}),
	}})

	var nodes []string
	for _, one := range s.Nodes {
		nodes = append(nodes, one.Node)
	}
	if want := []string{"laptop-a", "server-b"}; !slices.Equal(nodes, want) {
		t.Fatalf("nodes = %v, want %v", nodes, want)
	}

	var subjects []string
	for _, one := range s.Subjects {
		subjects = append(subjects, one.Node+" "+one.Metric+" "+one.Labels["mount"]+one.Labels["cpu"])
	}
	want := []string{
		"laptop-a silence ", "laptop-a load.one ",
		"server-b silence ",
		"server-b disk.free_bytes /", "server-b disk.free_bytes /data",
		"server-b disk.free_pct /", "server-b disk.free_pct /data",
		"server-b load.one a", "server-b load.one b",
	}
	if !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q, want %q", subjects, want)
	}
}

// quiet takes every message and delivers none.
type quiet struct{}

func (quiet) Notify(context.Context, evaluate.Message) error { return nil }

func (quiet) Digest(context.Context, time.Time, []evaluate.Message, int) error { return nil }

// spec: state.md#levels — a value that crossed a threshold keeps the stored level until the
// next tick, and reads the level and since that tick stored after it.
func TestLevelsFollowTheTick(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	measured := []storage.Measurement{
		{Metric: "disk.free_bytes", Sensor: "disk", Labels: volume("/"), Value: 1e9, TS: now},
	}
	if err := db.SaveIngest(ctx, storage.Ingest{Node: "server-b", ReceivedAt: now, Measurements: measured}); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if err := db.SaveThreshold(ctx, watch("server-b", "disk.free_bytes", volume("/"))); err != nil {
		t.Fatalf("SaveThreshold: %v", err)
	}
	read := func() state.Subject {
		snap, err := db.Snapshot(ctx, nil)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		return subject(t, state.Build(watching(), snap, now), "server-b", "disk.free_bytes", "/")
	}
	if got := read(); !got.Watched || got.Level != nil {
		t.Fatalf("before any tick watched %v level %s, want it watched and unjudged", got.Watched, levelOf(got.Level))
	}

	target, _ := watching()("server-b")
	evaluator := evaluate.New(evaluate.Options{
		Store:    db,
		Notifier: quiet{},
		Targets:  []evaluate.Target{target},
		Digest:   evaluate.Schedule{Hour: 9, Location: time.UTC},
		Started:  now,
		Now:      func() time.Time { return now },
	})
	if err := evaluator.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := read(); levelOf(got.Level) != "critical" || !got.Since.Equal(now) {
		t.Fatalf("after the tick level %s since %v, want critical since %v", levelOf(got.Level), got.Since, now)
	}
}
