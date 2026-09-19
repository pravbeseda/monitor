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
	staleAfter   = 3 * interval
)

// watching configures every node it is asked about except the ones in forgotten: each runs
// the disk sensor and judges its volumes by the product defaults.
func watching(t *testing.T, forgotten ...string) func(string) (evaluate.Target, bool) {
	t.Helper()
	disk, ok := evaluate.Lookup("disk")
	if !ok {
		t.Fatal("the hub implements no disk rule")
	}
	return func(node string) (evaluate.Target, bool) {
		for _, name := range forgotten {
			if name == node {
				return evaluate.Target{}, false
			}
		}
		return evaluate.Target{
			Node:         node,
			SilenceAfter: silenceAfter,
			Intervals:    map[string]time.Duration{"disk": interval},
			Rules:        map[string]evaluate.Rule{"disk": disk.Default},
		}, true
	}
}

func volume(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}
}

func removable(mount string) map[string]string {
	return map[string]string{"mount": mount, "fs": "apfs", "removable": "true"}
}

// reported is the pair of series of one volume, both collected age ago.
func reported(labels map[string]string, age time.Duration) []storage.Value {
	return []storage.Value{
		{Metric: "disk.free_bytes", Labels: labels, Value: 40e9, TS: now.Add(-age)},
		{Metric: "disk.free_pct", Labels: labels, Value: 31.25, TS: now.Add(-age)},
	}
}

// heard is a node last heard from age ago, carrying the given series.
func heard(node string, age time.Duration, values ...storage.Value) storage.NodeState {
	return storage.NodeState{Node: node, LastSeen: now.Add(-age), AgentVersion: "v1.0.0", Values: values}
}

func recorded(node, rule string, labels map[string]string, level evaluate.Level, since time.Time) storage.State {
	return storage.State{
		Subject: storage.Subject{Node: node, Rule: rule, Labels: labels},
		Level:   level.String(),
		Since:   since,
	}
}

func build(t *testing.T, snap storage.Snapshot, forgotten ...string) state.State {
	t.Helper()
	return state.Build(watching(t, forgotten...), snap, now)
}

func subject(t *testing.T, s state.State, node, rule, mount string) state.Subject {
	t.Helper()
	for _, one := range s.Subjects {
		if one.Node == node && one.Rule == rule && one.Labels["mount"] == mount {
			return one
		}
	}
	t.Fatalf("no %s subject of %s at %q among %d", rule, node, mount, len(s.Subjects))
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
	if s.Level != nil || len(s.Nodes) != 0 || len(s.Subjects) != 0 || len(s.Readings) != 0 {
		t.Fatalf("an empty hub answered level=%s nodes=%d subjects=%d readings=%d",
			levelOf(s.Level), len(s.Nodes), len(s.Subjects), len(s.Readings))
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
		silence := subject(t, s, "server-b", evaluate.SilenceRule, "")
		if len(silence.Labels) != 0 || len(silence.Values) != 0 {
			t.Fatalf("silence subject = %+v, want no labels and no values", silence)
		}
	})

	t.Run("a configured node that has never reported", func(t *testing.T) {
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0)}})
		for _, one := range s.Nodes {
			if one.Node != "server-b" {
				t.Fatalf("node %s listed, but it never reported", one.Node)
			}
		}
		for _, one := range s.Subjects {
			if one.Node != "server-b" {
				t.Fatalf("subject of %s listed, but it never reported", one.Node)
			}
		}
	})

	t.Run("a node the configuration no longer names", func(t *testing.T) {
		values := append(reported(volume("/"), time.Hour), storage.Value{Metric: "load.one", Value: 0.4, TS: now})
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, values...)}}, "server-b")
		got := node(t, s, "server-b")
		if got.Configured || got.Level != nil {
			t.Fatalf("node = configured %v level %s, want false and null", got.Configured, levelOf(got.Level))
		}
		if len(s.Subjects) != 0 {
			t.Fatalf("a node nothing judges has %d subjects", len(s.Subjects))
		}
		if len(s.Readings) != 3 {
			t.Fatalf("%d readings, want every value of the node", len(s.Readings))
		}
		for _, reading := range s.Readings {
			if reading.Stale != nil {
				t.Fatalf("%s stale = %s, want null", reading.Metric, staleOf(reading.Stale))
			}
		}
	})

	t.Run("a volume both of whose series are stored", func(t *testing.T) {
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)}})
		got := subject(t, s, "server-b", "disk", "/")
		if len(got.Values) != 2 || got.Values[0].Metric != "disk.free_bytes" || got.Values[1].Metric != "disk.free_pct" {
			t.Fatalf("values = %+v, want both series ordered by metric", got.Values)
		}
		if got.Values[0].Unit != history.Bytes || got.Values[1].Unit != history.Percent {
			t.Fatalf("units = %s, %s", got.Values[0].Unit, got.Values[1].Unit)
		}
		if len(s.Readings) != 0 {
			t.Fatalf("a series a subject reads is also listed as a reading: %+v", s.Readings)
		}
	})

	t.Run("a volume only one of whose series is stored", func(t *testing.T) {
		half := reported(volume("/"), time.Minute)[:1]
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, half...)}})
		for _, one := range s.Subjects {
			if one.Rule == "disk" {
				t.Fatalf("an incomplete join became a subject: %+v", one)
			}
		}
		if len(s.Readings) != 1 || s.Readings[0].Metric != "disk.free_bytes" {
			t.Fatalf("readings = %+v, want the one stored half", s.Readings)
		}
	})

	t.Run("a metric no rule declares", func(t *testing.T) {
		s := build(t, storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", 0, storage.Value{Metric: "load.one", Value: 0.4, TS: now.Add(-24 * time.Hour)}),
		}})
		if len(s.Readings) != 1 {
			t.Fatalf("readings = %+v", s.Readings)
		}
		got := s.Readings[0]
		if got.Stale != nil || got.Unit != history.Number {
			t.Fatalf("reading = %+v, want stale null and unit number", got)
		}
	})

	t.Run("a sensor the node resolves as enabled: false", func(t *testing.T) {
		disabled := func(node string) (evaluate.Target, bool) {
			target, known := watching(t)(node)
			target.Intervals = map[string]time.Duration{}
			return target, known
		}
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			States: []storage.State{recorded("server-b", "disk", volume("/"), evaluate.Critical, now.Add(-time.Hour))},
		}
		s := state.Build(disabled, snap, now)
		if len(s.Readings) != 2 {
			t.Fatalf("readings = %+v, want both series", s.Readings)
		}
		for _, reading := range s.Readings {
			if reading.Stale != nil {
				t.Fatalf("stale = %s, want null", staleOf(reading.Stale))
			}
		}
		if got := node(t, s, "server-b"); got.Level != nil {
			t.Fatalf("node level = %s: a level no tick maintains any more still counted", levelOf(got.Level))
		}
	})
}

// spec: state.md#levels
func TestLevels(t *testing.T) {
	since := now.Add(-12 * time.Hour)

	t.Run("a subject evaluation stored", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			States: []storage.State{recorded("server-b", "disk", volume("/"), evaluate.Warning, since)},
		}
		got := subject(t, build(t, snap), "server-b", "disk", "/")
		if levelOf(got.Level) != "warning" || !got.Since.Equal(since) {
			t.Fatalf("level %s since %v, want the stored warning since %v", levelOf(got.Level), got.Since, since)
		}
	})

	t.Run("a value that crossed a threshold after the last tick", func(t *testing.T) {
		values := []storage.Value{
			{Metric: "disk.free_bytes", Labels: volume("/"), Value: 1e9, TS: now},
			{Metric: "disk.free_pct", Labels: volume("/"), Value: 1, TS: now},
		}
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, values...)},
			States: []storage.State{recorded("server-b", "disk", volume("/"), evaluate.OK, since)},
		}
		if got := subject(t, build(t, snap), "server-b", "disk", "/"); levelOf(got.Level) != "ok" {
			t.Fatalf("level = %s: reading the state judged the value itself", levelOf(got.Level))
		}
	})

	t.Run("a subject that first became complete after the last tick", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)}}
		got := subject(t, build(t, snap), "server-b", "disk", "/")
		if got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("level %s since %v, want both null", levelOf(got.Level), got.Since)
		}
	})

	t.Run("a subject stale since it first appeared", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), staleAfter+time.Minute)...)}}
		got := subject(t, build(t, snap), "server-b", "disk", "/")
		if got.Level != nil || !got.Stale {
			t.Fatalf("level %s stale %v, want null and stale", levelOf(got.Level), got.Stale)
		}
	})

	t.Run("a stored level this build does not know", func(t *testing.T) {
		corrupt := recorded("server-b", "disk", volume("/"), evaluate.OK, since)
		corrupt.Level = "bogus"
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			States: []storage.State{corrupt},
		}
		got := subject(t, build(t, snap), "server-b", "disk", "/")
		if got.Level != nil || !got.Since.IsZero() {
			t.Fatalf("level %s since %v, want both null", levelOf(got.Level), got.Since)
		}
	})

	t.Run("volumes ok, silence critical", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), time.Minute)...)},
			States: []storage.State{
				recorded("server-b", "disk", volume("/"), evaluate.OK, since),
				recorded("server-b", evaluate.SilenceRule, nil, evaluate.Critical, since),
			},
		}
		if got := node(t, build(t, snap), "server-b"); levelOf(got.Level) != "critical" {
			t.Fatalf("node level = %s, want critical", levelOf(got.Level))
		}
	})

	t.Run("one warning volume, the rest ok", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", 0,
				append(reported(volume("/"), time.Minute), reported(volume("/data"), time.Minute)...)...)},
			States: []storage.State{
				recorded("server-b", "disk", volume("/"), evaluate.OK, since),
				recorded("server-b", "disk", volume("/data"), evaluate.Warning, since),
				recorded("server-b", evaluate.SilenceRule, nil, evaluate.OK, since),
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
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", 0,
				append(reported(removable("/Volumes/b"), staleAfter+time.Minute), reported(volume("/"), time.Minute)...)...)},
			States: []storage.State{
				recorded("server-b", "disk", removable("/Volumes/b"), evaluate.Critical, since),
				recorded("server-b", "disk", volume("/"), evaluate.Warning, since),
				recorded("server-b", evaluate.SilenceRule, nil, evaluate.OK, since),
			},
		}
		s := build(t, snap)
		if got := node(t, s, "server-b"); levelOf(got.Level) != "warning" {
			t.Fatalf("node level = %s, want warning: a stale subject raised it", levelOf(got.Level))
		}
		if got := subject(t, s, "server-b", "disk", "/Volumes/b"); levelOf(got.Level) != "critical" || !got.Stale {
			t.Fatalf("stale subject level %s stale %v, want critical and stale", levelOf(got.Level), got.Stale)
		}
	})

	t.Run("a silent node whose volumes were warning", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("server-b", silenceAfter+time.Minute, reported(volume("/"), time.Minute)...)},
			States: []storage.State{
				recorded("server-b", "disk", volume("/"), evaluate.Warning, since),
				recorded("server-b", evaluate.SilenceRule, nil, evaluate.Critical, since),
			},
		}
		if got := node(t, build(t, snap), "server-b"); levelOf(got.Level) != "critical" {
			t.Fatalf("node level = %s, want critical", levelOf(got.Level))
		}
	})

	t.Run("one node critical, another ok", func(t *testing.T) {
		snap := storage.Snapshot{
			Nodes: []storage.NodeState{heard("laptop-a", 0), heard("server-b", 0)},
			States: []storage.State{
				recorded("laptop-a", evaluate.SilenceRule, nil, evaluate.OK, since),
				recorded("server-b", evaluate.SilenceRule, nil, evaluate.Critical, since),
			},
		}
		if got := build(t, snap); levelOf(got.Level) != "critical" {
			t.Fatalf("response level = %s, want critical", levelOf(got.Level))
		}
	})
}

// spec: state.md#staleness
func TestStaleness(t *testing.T) {
	// older is a volume whose free bytes are age old and whose percentage is fresh, so only
	// the older series can decide its staleness.
	older := func(age time.Duration) []storage.Value {
		values := reported(volume("/"), time.Minute)
		values[0].TS = now.Add(-age)
		return values
	}

	t.Run("the older series exactly three intervals old", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, older(staleAfter)...)}}
		if got := subject(t, build(t, snap), "server-b", "disk", "/"); got.Stale {
			t.Fatal("a volume exactly at the bound came out stale; the bound is inclusive")
		}
	})

	t.Run("the older series a moment past the bound", func(t *testing.T) {
		since := now.Add(-time.Hour)
		snap := storage.Snapshot{
			Nodes:  []storage.NodeState{heard("server-b", 0, older(staleAfter+time.Second)...)},
			States: []storage.State{recorded("server-b", "disk", volume("/"), evaluate.Warning, since)},
		}
		got := subject(t, build(t, snap), "server-b", "disk", "/")
		if !got.Stale || levelOf(got.Level) != "warning" || !got.Since.Equal(since) {
			t.Fatalf("stale %v level %s since %v, want stale with the stored level", got.Stale, levelOf(got.Level), got.Since)
		}
	})

	t.Run("a node silent past silence_after", func(t *testing.T) {
		half := storage.Value{Metric: "disk.free_bytes", Labels: volume("/half"), Value: 1e9, TS: now}
		snap := storage.Snapshot{Nodes: []storage.NodeState{
			heard("server-b", silenceAfter+time.Minute, append(reported(volume("/"), time.Minute), half)...),
		}}
		s := build(t, snap)
		if !subject(t, s, "server-b", "disk", "/").Stale {
			t.Fatal("a silent node's volume came out fresh")
		}
		if subject(t, s, "server-b", evaluate.SilenceRule, "").Stale {
			t.Fatal("the silence subject came out stale")
		}
		if len(s.Readings) != 1 || staleOf(s.Readings[0].Stale) != "true" {
			t.Fatalf("half-join reading = %+v, want stale", s.Readings)
		}
	})

	t.Run("a stale removable volume", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(removable("/Volumes/b"), staleAfter+time.Minute)...)}}
		if got := subject(t, build(t, snap), "server-b", "disk", "/Volumes/b"); !got.Stale {
			t.Fatal("an unplugged drive came out fresh")
		}
	})

	t.Run("values stamped an hour ahead", func(t *testing.T) {
		snap := storage.Snapshot{Nodes: []storage.NodeState{heard("server-b", 0, reported(volume("/"), -time.Hour)...)}}
		if got := subject(t, build(t, snap), "server-b", "disk", "/"); got.Stale {
			t.Fatal("a value from a clock running ahead came out stale")
		}
	})
}

// spec: state.md#ordering
func TestOrdering(t *testing.T) {
	values := append(reported(volume("/data"), time.Minute), reported(volume("/"), time.Minute)...)
	values = append(values, reported(map[string]string{"fs": "ext4"}, time.Minute)...)
	values = append(values,
		storage.Value{Metric: "load.one", Labels: map[string]string{"cpu": "b"}, Value: 1, TS: now},
		storage.Value{Metric: "load.one", Labels: map[string]string{"cpu": "a"}, Value: 1, TS: now},
		storage.Value{Metric: "disk.free_pct", Labels: volume("/half"), Value: 1, TS: now},
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
		subjects = append(subjects, one.Node+" "+one.Rule+" "+one.Labels["mount"])
	}
	want := []string{"laptop-a silence ", "server-b silence ", "server-b disk ", "server-b disk /", "server-b disk /data"}
	if !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q, want %q", subjects, want)
	}

	var readings []string
	for _, one := range s.Readings {
		readings = append(readings, one.Node+" "+one.Metric+" "+one.Labels["cpu"]+one.Labels["mount"])
	}
	if want := []string{"laptop-a load.one ", "server-b disk.free_pct /half", "server-b load.one a", "server-b load.one b"}; !slices.Equal(readings, want) {
		t.Fatalf("readings = %q, want %q", readings, want)
	}
}

// quiet takes every message and delivers none.
type quiet struct{}

func (quiet) Notify(context.Context, evaluate.Message) error              { return nil }
func (quiet) Digest(context.Context, time.Time, []evaluate.Message) error { return nil }

// spec: state.md#levels — a value that crossed a threshold keeps the stored level until the
// next tick, and reads the level and since that tick stored after it.
func TestLevelsFollowTheTick(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	full := []storage.Measurement{
		{Metric: "disk.free_bytes", Labels: volume("/"), Value: 1e9, TS: now},
		{Metric: "disk.free_pct", Labels: volume("/"), Value: 1, TS: now},
	}
	if err := db.SaveIngest(ctx, storage.Ingest{Node: "server-b", ReceivedAt: now, Measurements: full}); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	read := func() state.Subject {
		snap, err := db.Snapshot(ctx, nil)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		return subject(t, state.Build(watching(t), snap, now), "server-b", "disk", "/")
	}
	if got := read(); got.Level != nil {
		t.Fatalf("before any tick level = %s, want null", levelOf(got.Level))
	}

	target, _ := watching(t)("server-b")
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
