package state_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// usualWeek is a norm under which a series usually sits between 30e9 and 50e9 — 40e9 is
// the norm, and each side of the band is 9.8e9 wide — so 40e9 free is ordinary and 5e9 is
// unusual.
func usualWeek() anomaly.Norm {
	var points []storage.Point
	for i := range 101 {
		points = append(points, storage.Point{
			TS:    now.Add(-8 * 24 * time.Hour).Add(time.Duration(i) * time.Hour),
			Value: 30e9 + float64(i)*0.2e9,
		})
	}
	norm, _ := anomaly.NormOf(points)
	return norm
}

// usually gives every series the snapshot reports the usual week, keyed as the state
// keys a series.
func usually(snap storage.Snapshot) map[string]anomaly.Norm {
	out := map[string]anomaly.Norm{}
	for _, node := range snap.Nodes {
		for _, value := range node.Values {
			key, err := storage.Subject{Node: node.Node, Metric: value.Metric, Labels: value.Labels}.Key()
			if err != nil {
				panic(err)
			}
			out[key] = usualWeek()
		}
	}
	return out
}

func noNorm(storage.Snapshot) map[string]anomaly.Norm { return map[string]anomaly.Norm{} }

func unreadNorms(storage.Snapshot) map[string]anomaly.Norm { return nil }

func withNorms(t *testing.T, snap storage.Snapshot, norms func(storage.Snapshot) map[string]anomaly.Norm, forgotten ...string) state.State {
	t.Helper()
	return state.Build(watching(forgotten...), snap, now, norms(snap))
}

func unusual(labels map[string]string, value float64) storage.Value {
	return storage.Value{Metric: "disk.free_bytes", Sensor: "disk", Labels: labels, Value: value, TS: now}
}

// spec: anomaly.md#subjects
func TestWhichSubjectsCarryAnAnomaly(t *testing.T) {
	root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/")}
	for _, tc := range []struct {
		name      string
		snap      storage.Snapshot
		forgotten []string
		norms     func(storage.Snapshot) map[string]anomaly.Norm
		want      string // "rank", "object" or "null"
	}{
		{
			name: "a watched series at critical, also outside its band",
			snap: storage.Snapshot{
				Nodes:      []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))},
				Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
				States:     []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Critical, now)},
			},
			norms: usually, want: "rank",
		},
		{
			name:  "an unwatched series with a norm",
			snap:  storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 40e9))}},
			norms: usually, want: "object",
		},
		{
			name:  "a stale series",
			snap:  storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, storage.Value{Metric: "disk.free_bytes", Sensor: "disk", Labels: volume("/"), Value: 5e9, TS: now.Add(-staleAfter - time.Millisecond)})}},
			norms: usually, want: "null",
		},
		{
			name: "a series excluded on its page",
			snap: storage.Snapshot{
				Nodes:    []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))},
				Excluded: []storage.SeriesRef{root},
			},
			norms: usually, want: "null",
		},
		{
			name:      "a series of a node the configuration no longer names",
			snap:      storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))}},
			forgotten: []string{"server-b"},
			norms:     usually, want: "null",
		},
		{
			name:  "a series with no norm",
			snap:  storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))}},
			norms: noNorm, want: "null",
		},
		{
			name:  "norms that could not be read",
			snap:  storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))}},
			norms: unreadNorms, want: "null",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := subject(t, withNorms(t, tc.snap, tc.norms, tc.forgotten...), "server-b", "disk.free_bytes", "/").Anomaly
			switch {
			case tc.want == "null" && got != nil:
				t.Fatalf("anomaly = %+v, want null", *got)
			case tc.want != "null" && got == nil:
				t.Fatal("anomaly = null")
			case tc.want == "rank" && got.Rank == 0:
				t.Fatalf("anomaly = %+v, want a rank", *got)
			case tc.want == "object" && got.Rank != 0:
				t.Fatalf("anomaly = %+v, want no rank", *got)
			}
		})
	}
}

// spec: anomaly.md#subjects — a node's silence subject carries no anomaly.
func TestSilenceCarriesNoAnomaly(t *testing.T) {
	snap := storage.Snapshot{
		Nodes:  []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))},
		States: []storage.State{recordedSilence("server-b", evaluate.OK, now)},
	}
	for _, one := range withNorms(t, snap, usually).Subjects {
		if one.Metric == evaluate.SilenceMetric && one.Anomaly != nil {
			t.Fatalf("silence carries %+v", *one.Anomaly)
		}
	}
}

// spec: anomaly.md#rank — ranks run over the whole answer in the order the state lists its
// subjects, and an excluded series takes no rank from the others.
func TestRanksRunOverTheWholeAnswer(t *testing.T) {
	snap := storage.Snapshot{
		Nodes: []storage.NodeState{
			heard("server-a", 0, unusual(volume("/"), 5e9)),
			heard("server-b", 0, unusual(volume("/"), 10e9), unusual(volume("/data"), 0)),
		},
		Excluded: []storage.SeriesRef{{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/data")}},
	}
	got := withNorms(t, snap, usually)
	// server-a scores −3.57 and server-b's root −3.06: the larger magnitude ranks first.
	for _, want := range []struct {
		node, mount string
		rank        int
	}{{"server-a", "/", 1}, {"server-b", "/", 2}} {
		one := subject(t, got, want.node, "disk.free_bytes", want.mount)
		if one.Anomaly == nil || one.Anomaly.Rank != want.rank {
			t.Fatalf("%s %s anomaly = %+v, want rank %d", want.node, want.mount, one.Anomaly, want.rank)
		}
	}
}

// spec: thresholds.md#effects — an exclusion changes no level and no count: the series stays
// watched or unwatched by its threshold alone.
func TestAnExclusionChangesNoLevelAndNoCount(t *testing.T) {
	root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/")}
	snap := storage.Snapshot{
		Nodes:      []storage.NodeState{heard("server-b", 0, reported(volume("/"), 0)...)},
		Thresholds: []storage.Threshold{watch("server-b", "disk.free_bytes", volume("/"))},
		States:     []storage.State{recorded("server-b", "disk.free_bytes", volume("/"), evaluate.Warning, now)},
	}
	before := withNorms(t, snap, usually)
	snap.Excluded = []storage.SeriesRef{root, {Node: "server-b", Metric: "disk.free_pct", Labels: volume("/")}}
	after := withNorms(t, snap, usually)

	if before.Watched != after.Watched || before.Unwatched != after.Unwatched {
		t.Errorf("counts %d/%d became %d/%d", before.Watched, before.Unwatched, after.Watched, after.Unwatched)
	}
	got := subject(t, after, "server-b", "disk.free_bytes", "/")
	if !got.Watched || levelOf(got.Level) != "warning" {
		t.Errorf("excluded subject = watched %v, level %s; want watched at warning", got.Watched, levelOf(got.Level))
	}
}

// spec: anomaly.md#subjects — a series that reports fresh values again, and one whose
// exclusion is removed, carry an anomaly again.
func TestAnAnomalyComesBack(t *testing.T) {
	root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: volume("/")}
	stale := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, storage.Value{Metric: "disk.free_bytes", Sensor: "disk", Labels: volume("/"), Value: 5e9, TS: now.Add(-staleAfter - time.Millisecond)})}}
	fresh := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, unusual(volume("/"), 5e9))}}
	excluded := fresh
	excluded.Excluded = []storage.SeriesRef{root}

	for name, snaps := range map[string][2]storage.Snapshot{
		"fresh values again":    {stale, fresh},
		"the exclusion removed": {excluded, fresh},
	} {
		t.Run(name, func(t *testing.T) {
			if got := subject(t, withNorms(t, snaps[0], usually), "server-b", "disk.free_bytes", "/").Anomaly; got != nil {
				t.Fatalf("before: anomaly = %+v, want null", *got)
			}
			got := subject(t, withNorms(t, snaps[1], usually), "server-b", "disk.free_bytes", "/").Anomaly
			if got == nil || got.Rank != 1 {
				t.Fatalf("after: anomaly = %+v, want it ranked again", got)
			}
		})
	}
}

// spec: anomaly.md#rank — two series scoring alike are ranked in the order the state lists
// them, whatever order they were reported in.
func TestTiedScoresRankInTheStatesOrder(t *testing.T) {
	snap := storage.Snapshot{Nodes: []storage.NodeState{
		heard("server-b", 0, unusual(volume("/"), 5e9)),
		heard("server-a", 0, unusual(volume("/"), 5e9)),
	}}
	got := withNorms(t, snap, usually)
	for node, rank := range map[string]int{"server-a": 1, "server-b": 2} {
		if one := subject(t, got, node, "disk.free_bytes", "/").Anomaly; one == nil || one.Rank != rank {
			t.Errorf("%s anomaly = %+v, want rank %d", node, one, rank)
		}
	}
}
