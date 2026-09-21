package evaluate_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// tick is the instant every subject in this file is evaluated at.
var tick = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

// silenceAfter and interval give the node a 10m silence window and, at three collections
// missed, a 45m staleness window.
const (
	silenceAfter = 10 * time.Minute
	interval     = 15 * time.Minute
	staleAfter   = 3 * interval
)

// watching is a node the file lists, running the disk sensor.
func watching(t *testing.T) evaluate.Target {
	t.Helper()
	return evaluate.Target{
		Node:         "server-b",
		SilenceAfter: silenceAfter,
		Intervals:    map[string]time.Duration{"disk": interval},
	}
}

// volume is the label set of one mounted volume, as the disk sensor reports it.
func volume(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}
}

// reported is one volume's free-space series, collected age ago and naming its sensor.
func reported(labels map[string]string, free float64, age time.Duration) []storage.Value {
	return []storage.Value{
		{Metric: "disk.free_bytes", Sensor: "disk", Labels: labels, Value: free, TS: tick.Add(-age)},
	}
}

// heard is a node last heard from age ago, carrying the given series.
func heard(age time.Duration, values ...storage.Value) storage.NodeState {
	return storage.NodeState{Node: "server-b", LastSeen: tick.Add(-age), Values: values}
}

// watched is the threshold that makes a series a subject: warning at 10 GB, critical at
// 4 GB, low is bad.
func watched(labels map[string]string) storage.Threshold {
	return storage.Threshold{
		Series:    storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: labels},
		Direction: storage.Below,
		Warning:   num(gb(10)),
		Critical:  num(gb(4)),
	}
}

func stored(metric string, labels map[string]string, level evaluate.Level, since time.Time) storage.State {
	return storage.State{
		Subject:   storage.Subject{Node: "server-b", Metric: metric, Labels: labels},
		Level:     level.String(),
		Direction: string(storage.Below),
		Since:     since,
	}
}

// subjectsOf evaluates one node against one snapshot.
func subjectsOf(t *testing.T, target evaluate.Target, snap storage.Snapshot) []evaluate.Subject {
	t.Helper()
	return evaluate.Subjects([]evaluate.Target{target}, snap, tick)
}

// find returns the subject of one metric and one mount, or fails: an absent subject is a
// different assertion, made with count.
func find(t *testing.T, subjects []evaluate.Subject, metric, mount string) evaluate.Subject {
	t.Helper()
	for _, subject := range subjects {
		if subject.Metric == metric && subject.Labels["mount"] == mount {
			return subject
		}
	}
	t.Fatalf("no %s subject for mount %q among %d subjects", metric, mount, len(subjects))
	return evaluate.Subject{}
}

func count(t *testing.T, subjects []evaluate.Subject, metric string) int {
	t.Helper()
	n := 0
	for _, subject := range subjects {
		if subject.Metric == metric {
			n++
		}
	}
	return n
}

// spec: evaluation.md#freezing — stale values are never re-evaluated: a frozen subject
// keeps the level and the `since` it had.
func TestFreezing(t *testing.T) {
	t.Run("the node is silent", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(silenceAfter+time.Minute, reported(volume("/"), gb(40), time.Minute)...)},
			Thresholds: []storage.Threshold{watched(volume("/"))},
		}
		subjects := subjectsOf(t, watching(t), snap)
		if got := find(t, subjects, "disk.free_bytes", "/"); !got.Frozen {
			t.Fatal("a silent node's series was evaluated, so stale values decided its level")
		}
		if got := find(t, subjects, evaluate.SilenceMetric, ""); got.Frozen {
			t.Fatal("the silence subject froze itself, so the node could never recover")
		}
	})

	t.Run("the newest value is older than stale_after", func(t *testing.T) {
		values := append(reported(volume("/data"), gb(5), staleAfter+time.Minute),
			reported(volume("/"), gb(40), time.Minute)...)
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0, values...)},
			Thresholds: []storage.Threshold{watched(volume("/data")), watched(volume("/"))},
		}
		subjects := subjectsOf(t, watching(t), snap)
		if got := find(t, subjects, "disk.free_bytes", "/data"); !got.Frozen {
			t.Fatal("a stale series was evaluated")
		}
		if got := find(t, subjects, "disk.free_bytes", "/"); got.Frozen || got.Level != evaluate.OK {
			t.Fatalf("the fresh series of the same node came out frozen=%v level=%v", got.Frozen, got.Level)
		}
	})

	t.Run("exactly stale_after old", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0, reported(volume("/"), gb(40), staleAfter)...)},
			Thresholds: []storage.Threshold{watched(volume("/"))},
		}
		if got := find(t, subjectsOf(t, watching(t), snap), "disk.free_bytes", "/"); got.Frozen {
			t.Fatal("a value exactly three intervals old froze: the bound is inclusive")
		}
	})

	t.Run("the series names no sensor", func(t *testing.T) {
		labels := volume("/")
		sensorless := func(age time.Duration) storage.Snapshot {
			return storage.Snapshot{
				Nodes: []storage.NodeState{heard(0, storage.Value{
					Metric: "disk.free_bytes", Labels: labels, Value: gb(5), TS: tick.Add(-age),
				})},
				Thresholds: []storage.Threshold{watched(labels)},
			}
		}

		got := find(t, subjectsOf(t, watching(t), sensorless(staleAfter)), "disk.free_bytes", "/")
		if got.Frozen {
			t.Fatal("a value exactly three of the node's longest interval old froze: the bound is inclusive")
		}
		if got.Level != evaluate.Warning {
			t.Fatalf("level = %v, want the fresh value judged as it stands", got.Level)
		}

		if got := find(t, subjectsOf(t, watching(t), sensorless(staleAfter+time.Second)), "disk.free_bytes", "/"); !got.Frozen {
			t.Fatal("a series with no sensor stayed evaluated past its node's longest interval")
		}
	})

	t.Run("a series nothing watches", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard(0, reported(volume("/"), gb(1), time.Minute)...)}}
		if got := count(t, subjectsOf(t, watching(t), snap), "disk.free_bytes"); got != 0 {
			t.Fatalf("an unwatched series produced %d subjects, want none", got)
		}
	})

	t.Run("a threshold whose series has never reported", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0)},
			Thresholds: []storage.Threshold{watched(volume("/"))},
		}
		if got := count(t, subjectsOf(t, watching(t), snap), "disk.free_bytes"); got != 0 {
			t.Fatalf("a threshold with no value produced %d subjects, want none", got)
		}
	})

	t.Run("a removable volume is unplugged", func(t *testing.T) {
		labels := volume("/mnt/usb")
		labels["removable"] = "true"
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0, reported(labels, gb(5), staleAfter+time.Minute)...)},
			Thresholds: []storage.Threshold{watched(labels)},
			States:     []storage.State{stored("disk.free_bytes", labels, evaluate.Critical, tick.Add(-time.Hour))},
		}
		got := find(t, subjectsOf(t, watching(t), snap), "disk.free_bytes", "/mnt/usb")
		if !got.Frozen || got.Level != evaluate.Critical || !got.Since.Equal(tick.Add(-time.Hour)) {
			t.Fatalf("an unplugged volume came out frozen=%v level=%v since=%v", got.Frozen, got.Level, got.Since)
		}
	})

	t.Run("the node does not run the sensor", func(t *testing.T) {
		target := watching(t)
		target.Intervals = map[string]time.Duration{}
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0, reported(volume("/"), gb(5), time.Minute)...)},
			Thresholds: []storage.Threshold{watched(volume("/"))},
		}
		subjects := subjectsOf(t, target, snap)
		if got := find(t, subjects, "disk.free_bytes", "/"); !got.Frozen {
			t.Fatal("a series whose sensor the node does not run was judged, though nothing will refresh it")
		}
		find(t, subjects, evaluate.SilenceMetric, "")
	})

	t.Run("a volume reappears under different labels", func(t *testing.T) {
		was, now := volume("/data"), volume("/data")
		now["fs"] = "xfs"
		values := append(reported(was, gb(5), staleAfter+time.Minute), reported(now, gb(5), time.Minute)...)
		snap := storage.Snapshot{
			Nodes:      []storage.NodeState{heard(0, values...)},
			Thresholds: []storage.Threshold{watched(was), watched(now)},
			States:     []storage.State{stored("disk.free_bytes", was, evaluate.Critical, tick.Add(-time.Hour))},
		}
		subjects := subjectsOf(t, watching(t), snap)
		if got := count(t, subjects, "disk.free_bytes"); got != 2 {
			t.Fatalf("relabelling gave %d subjects, want the old one and the new", got)
		}
		for _, subject := range subjects {
			if subject.Metric != "disk.free_bytes" {
				continue
			}
			if subject.Labels["fs"] == "ext4" && (!subject.Frozen || subject.Level != evaluate.Critical) {
				t.Fatalf("the old subject came out frozen=%v level=%v", subject.Frozen, subject.Level)
			}
			if subject.Labels["fs"] == "xfs" && (subject.Frozen || subject.Previous != evaluate.OK) {
				t.Fatalf("the new subject started at %v, want a new subject at ok", subject.Previous)
			}
		}
	})
}

// spec: evaluation.md#node-silence — the node is a subject too, with the window its class
// resolves to.
func TestNodeSilence(t *testing.T) {
	t.Run("silent past the window", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard(silenceAfter + time.Second)}}
		if got := find(t, subjectsOf(t, watching(t), snap), evaluate.SilenceMetric, ""); got.Level != evaluate.Critical {
			t.Fatalf("a node past its silence window is %v, want critical", got.Level)
		}
	})

	t.Run("exactly at the window", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard(silenceAfter)}}
		if got := find(t, subjectsOf(t, watching(t), snap), evaluate.SilenceMetric, ""); got.Level != evaluate.OK {
			t.Fatalf("a node exactly at its window is %v, want ok: only past it is silence", got.Level)
		}
	})

	t.Run("heard from inside the window", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard(silenceAfter - time.Second)},
			States: []storage.State{{
				Subject: storage.Subject{Node: "server-b", Metric: evaluate.SilenceMetric},
				Level:   evaluate.Critical.String(), Since: tick.Add(-time.Hour),
			}},
		}
		got := find(t, subjectsOf(t, watching(t), snap), evaluate.SilenceMetric, "")
		if got.Level != evaluate.OK || got.Previous != evaluate.Critical {
			t.Fatalf("a node heard from again is %v from %v, want ok from critical", got.Level, got.Previous)
		}
	})

	t.Run("still inside the window", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard(time.Minute)}}
		if got := find(t, subjectsOf(t, watching(t), snap), evaluate.SilenceMetric, ""); got.Level != evaluate.OK || got.Changed() {
			t.Fatalf("a healthy node changed to %v", got.Level)
		}
	})

	t.Run("a node that has never reported", func(t *testing.T) {
		if got := evaluate.Subjects([]evaluate.Target{watching(t)}, storage.Snapshot{}, tick); len(got) != 0 {
			t.Fatalf("an uninstalled agent produced %d subjects, want none", len(got))
		}
	})
}

// spec: evaluation.md#the-tick — messages and digest entries come out by node name, then
// metric, then labels, a node's silence first.
func TestSubjectsComeOutInAStableOrder(t *testing.T) {
	values := append(reported(volume("/data"), gb(40), time.Minute), reported(volume("/"), gb(40), time.Minute)...)
	first, second := watching(t), watching(t)
	second.Node = "laptop-a"
	snap := storage.Snapshot{
		Nodes: []storage.NodeState{
			heard(0, values...),
			{Node: "laptop-a", LastSeen: tick},
		},
		Thresholds: []storage.Threshold{watched(volume("/data")), watched(volume("/"))},
	}

	got := evaluate.Subjects([]evaluate.Target{first, second}, snap, tick)
	want := []string{
		"laptop-a/silence/",
		"server-b/silence/",
		"server-b/disk.free_bytes//",
		"server-b/disk.free_bytes//data",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d subjects, want %d", len(got), len(want))
	}
	for i, subject := range got {
		if key := subject.Node + "/" + subject.Metric + "/" + subject.Labels["mount"]; key != want[i] {
			t.Fatalf("subject %d is %s, want %s", i, key, want[i])
		}
	}
}

// spec: evaluation.md#freezing — a series reported again under a different sensor name
// ages by the newest value's sensor from that tick on, and keeps its level.
func TestASeriesAgesByTheSensorOfItsNewestValue(t *testing.T) {
	labels := volume("/")
	// Collected half an hour ago: stale for a sensor collecting every 5 minutes, fresh
	// for the one collecting every 15.
	value := storage.Value{
		Metric: "disk.free_bytes", Sensor: "fast", Labels: labels, Value: gb(40), TS: tick.Add(-30 * time.Minute),
	}
	target := watching(t)
	target.Intervals = map[string]time.Duration{"disk": interval, "fast": 5 * time.Minute}
	snap := storage.Snapshot{
		Nodes:      []storage.NodeState{heard(0, value)},
		Thresholds: []storage.Threshold{watched(labels)},
		States:     []storage.State{stored("disk.free_bytes", labels, evaluate.Warning, tick.Add(-time.Hour))},
	}

	frozen := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/")
	if !frozen.Frozen || frozen.Level != evaluate.Warning {
		t.Fatalf("under the fast sensor the subject is frozen=%v level=%v, want frozen at warning",
			frozen.Frozen, frozen.Level)
	}

	value.Sensor = "disk"
	snap.Nodes = []storage.NodeState{heard(0, value)}
	fresh := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/")
	if fresh.Frozen {
		t.Fatal("the same value under the slower sensor still froze: the newest value names what ages it")
	}
}

// spec: evaluation.md#freezing — a series that names no sensor is aged by the longest
// interval among the sensors its node runs, whichever sensor holds it; a node running none
// freezes it outright, and naming a sensor again ages it by that sensor from then on.
// spec: evaluation.md#configuration-changes — changing which interval is the longest
// freezes or thaws such a series, though nothing about the series itself changed.
func TestASeriesWithoutASensorAgesByItsNodesLongestInterval(t *testing.T) {
	labels := volume("/")
	// Collected half an hour ago: past three intervals of the five-minute sensor, inside
	// three of the fifteen-minute one, so only the longest interval keeps it evaluated.
	value := storage.Value{Metric: "disk.free_bytes", Labels: labels, Value: gb(40), TS: tick.Add(-30 * time.Minute)}
	snap := storage.Snapshot{
		Nodes:      []storage.NodeState{heard(0, value)},
		Thresholds: []storage.Threshold{watched(labels)},
		States:     []storage.State{stored("disk.free_bytes", labels, evaluate.Warning, tick.Add(-time.Hour))},
	}

	target := watching(t)
	target.Intervals = map[string]time.Duration{"disk": interval, "fast": 5 * time.Minute}
	if got := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/"); got.Frozen {
		t.Fatal("a series with no sensor was aged by the node's shortest interval, not its longest")
	}

	target.Intervals = map[string]time.Duration{"fast": 5 * time.Minute}
	if got := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/"); !got.Frozen {
		t.Fatal("the same series stayed evaluated when the node's longest interval shrank below its age")
	}

	target.Intervals = map[string]time.Duration{}
	if got := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/"); !got.Frozen {
		t.Fatal("a series on a node running no sensor at all stayed evaluated: nothing will refresh it")
	}

	value.Sensor = "disk"
	snap.Nodes = []storage.NodeState{heard(0, value)}
	target.Intervals = map[string]time.Duration{"disk": interval, "fast": 5 * time.Minute}
	if got := find(t, subjectsOf(t, target, snap), "disk.free_bytes", "/"); got.Frozen {
		t.Fatal("the series named its sensor again and still froze by another bound")
	}
}

// spec: evaluation.md#configuration-changes — a stored threshold this build cannot read
// leaves its series unjudged, and the rest of the tick runs.
func TestAnUnreadableThresholdIsNotJudged(t *testing.T) {
	sideways := watched(volume("/"))
	sideways.Direction = storage.Direction("sideways")
	snap := storage.Snapshot{
		Nodes: []storage.NodeState{heard(0,
			append(reported(volume("/"), gb(1), time.Minute), reported(volume("/data"), gb(1), time.Minute)...)...)},
		Thresholds: []storage.Threshold{sideways, watched(volume("/data"))},
	}

	subjects := subjectsOf(t, watching(t), snap)
	if got := count(t, subjects, "disk.free_bytes"); got != 1 {
		t.Fatalf("%d subjects, want only the readable one", got)
	}
	if got := find(t, subjects, "disk.free_bytes", "/data"); got.Level != evaluate.Critical {
		t.Fatalf("the readable subject is %v, want the rest of the tick to run", got.Level)
	}
}
