package evaluate_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Sizes are decimal, the way the specs and the interface write them.
func gb(n float64) float64 { return n * 1e9 }

func num(v float64) *float64 { return &v }

const (
	node   = "server-b"
	metric = "disk.free_bytes"
	sensor = "disk"
)

var mount = map[string]string{"mount": "/"}

// judged is the target of a node that runs the disk sensor every 15 minutes and is not
// silent, so nothing in these tables is frozen.
func judged() []evaluate.Target {
	return []evaluate.Target{{
		Node:         node,
		SilenceAfter: time.Hour,
		Intervals:    map[string]time.Duration{sensor: 15 * time.Minute},
	}}
}

// below is the running example of docs/specs/evaluation.md#levels: warning at 10 GB,
// critical at 4 GB, low is bad.
func below() storage.Threshold {
	return storage.Threshold{
		Series:    storage.SeriesRef{Node: node, Metric: metric, Labels: mount},
		Direction: storage.Below,
		Warning:   num(gb(10)),
		Critical:  num(gb(4)),
	}
}

func above(warning, critical *float64) storage.Threshold {
	return storage.Threshold{
		Series:    storage.SeriesRef{Node: node, Metric: metric, Labels: mount},
		Direction: storage.Above,
		Warning:   warning,
		Critical:  critical,
	}
}

// levelCase is one row of a behaviour table: what the subject was, what it is judged by,
// what it reports, and the level that follows.
type levelCase struct {
	name      string
	previous  evaluate.Level
	stored    storage.Direction
	threshold *storage.Threshold
	value     float64
	want      evaluate.Level
	unwatched bool
}

// run builds the one subject each row describes and asserts the level a tick gives it.
// The level is read through Subjects, because that is what the tick judges by: a row of
// the table is a statement about a subject, not about a helper.
func run(t *testing.T, cases []levelCase) {
	t.Helper()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			threshold := below()
			if c.threshold != nil {
				threshold = *c.threshold
			}
			snap := storage.Snapshot{
				Nodes: []storage.NodeState{{
					Node:     node,
					LastSeen: now,
					Values: []storage.Value{{
						Metric: metric, Sensor: sensor, Labels: mount, Value: c.value, TS: now,
					}},
				}},
			}
			if !c.unwatched {
				snap.Thresholds = []storage.Threshold{threshold}
			}
			if c.previous != evaluate.OK || c.stored != "" {
				direction := c.stored
				if direction == "" {
					direction = threshold.Direction
				}
				snap.States = []storage.State{{
					Subject:   storage.Subject{Node: node, Metric: metric, Labels: mount},
					Level:     c.previous.String(),
					Direction: string(direction),
					Since:     now.Add(-time.Hour),
				}}
			}

			subjects := evaluate.Subjects(judged(), snap, now)
			var found *evaluate.Subject
			for i, subject := range subjects {
				if subject.Metric == metric {
					found = &subjects[i]
				}
			}
			if c.unwatched {
				if found != nil {
					t.Fatalf("an unwatched series is a subject at %v, want none", found.Level)
				}
				return
			}
			if found == nil {
				t.Fatal("the watched series is no subject")
			}
			if found.Level != c.want {
				t.Fatalf("level = %v, want %v", found.Level, c.want)
			}
		})
	}
}

// spec: evaluation.md#levels
func TestLevels(t *testing.T) {
	run(t, []levelCase{
		{name: "neither comparison holds", value: gb(40), want: evaluate.OK},
		{name: "entry is strict", value: gb(10), want: evaluate.OK},
		{name: "below warning, above critical", value: gb(9.99), want: evaluate.Warning},
		{name: "the critical comparison is strict too", value: gb(4), want: evaluate.Warning},
		{name: "below the critical value", value: gb(3), want: evaluate.Critical},
		{name: "the more severe level is entered at once", previous: evaluate.Warning, value: gb(3), want: evaluate.Critical},
		{name: "an empty volume is a value", value: 0, want: evaluate.Critical},
		{
			name: "a level with no value is never entered", value: gb(9),
			threshold: withValues(storage.Below, nil, num(gb(4))), want: evaluate.OK,
		},
		{
			name: "with no critical value, warning is the worst it reaches", value: gb(1),
			threshold: withValues(storage.Below, num(gb(10)), nil), want: evaluate.Warning,
		},
		{name: "nothing configured, nothing judged", value: gb(1), unwatched: true},
		{name: "judged upwards, below both", threshold: ptr(above(num(4), num(8))), value: 3.5, want: evaluate.OK},
		{name: "entry is strict upwards as well", threshold: ptr(above(num(4), num(8))), value: 4, want: evaluate.OK},
		{name: "past the warning value", threshold: ptr(above(num(4), num(8))), value: 4.1, want: evaluate.Warning},
		{name: "exactly the critical value, upwards", threshold: ptr(above(num(4), num(8))), value: 8, want: evaluate.Warning},
		{name: "past the critical value", threshold: ptr(above(num(4), num(8))), value: 8.2, want: evaluate.Critical},
	})
}

// spec: evaluation.md#hysteresis — the clearing values are 12 GB and 4.8 GB.
func TestHysteresis(t *testing.T) {
	run(t, []levelCase{
		{name: "past entry, below the clearing value", previous: evaluate.Warning, value: gb(11), want: evaluate.Warning},
		{name: "exactly the clearing value", previous: evaluate.Warning, value: gb(12), want: evaluate.OK},
		{name: "entry still holds", previous: evaluate.Warning, value: gb(9), want: evaluate.Warning},
		{name: "hysteresis never creates a level", previous: evaluate.OK, value: gb(11), want: evaluate.OK},
		{name: "hysteresis never raises one", previous: evaluate.Warning, value: gb(4.5), want: evaluate.Warning},
		{name: "critical is held inside its margin", previous: evaluate.Critical, value: gb(4.5), want: evaluate.Critical},
		{name: "exactly the critical value holds", previous: evaluate.Critical, value: gb(4), want: evaluate.Critical},
		{name: "clears critical, still under warning", previous: evaluate.Critical, value: gb(4.9), want: evaluate.Warning},
		{name: "a level steps down one band at a time", previous: evaluate.Critical, value: gb(11), want: evaluate.Warning},
		{name: "clears both levels in one tick", previous: evaluate.Critical, value: gb(12), want: evaluate.OK},
		{
			name: "above the clearing value, upwards", previous: evaluate.Warning,
			threshold: ptr(above(num(4), nil)), value: 3.5, want: evaluate.Warning,
		},
		{
			name: "exactly the clearing value, upwards", previous: evaluate.Warning,
			threshold: ptr(above(num(4), nil)), value: 3.2, want: evaluate.OK,
		},
		{
			name: "critical held inside its margin, upwards", previous: evaluate.Critical,
			threshold: ptr(above(num(4), num(8))), value: 7, want: evaluate.Critical,
		},
		{
			name: "the margin is 20% of the magnitude", previous: evaluate.Warning,
			threshold: withValues(storage.Below, num(-100), nil), value: -90, want: evaluate.Warning,
		},
		{
			name: "a negative threshold clears upwards", previous: evaluate.Warning,
			threshold: withValues(storage.Below, num(-100), nil), value: -80, want: evaluate.OK,
		},
		{
			name: "an above threshold clears downwards", previous: evaluate.Warning,
			threshold: ptr(above(num(-100), nil)), value: -120, want: evaluate.OK,
		},
		{
			name: "a zero threshold alerts below zero only", previous: evaluate.OK,
			threshold: withValues(storage.Below, num(0), nil), value: 0, want: evaluate.OK,
		},
		{
			name: "a margin of zero clears at the threshold", previous: evaluate.Warning,
			threshold: withValues(storage.Below, num(0), nil), value: 0, want: evaluate.OK,
		},
		{
			name: "critical cannot be held without a value", previous: evaluate.Critical,
			threshold: withValues(storage.Below, num(gb(10)), nil), value: gb(11), want: evaluate.Warning,
		},
		{
			name: "warning cannot be held without a value", previous: evaluate.Warning,
			threshold: withValues(storage.Below, nil, num(gb(4))), value: gb(9), want: evaluate.OK,
		},
		{
			name:   "a flipped direction discards the level it held",
			stored: storage.Below, previous: evaluate.Warning,
			threshold: ptr(above(num(gb(10)), nil)), value: gb(9), want: evaluate.OK,
		},
		{
			name: "a held level clears against the new clearing value", previous: evaluate.Warning,
			threshold: withValues(storage.Below, num(gb(5)), nil), value: gb(9), want: evaluate.OK,
		},
		{
			name: "an edited threshold re-enters outright", previous: evaluate.Warning,
			threshold: withValues(storage.Below, num(gb(20)), nil), value: gb(9), want: evaluate.Warning,
		},
	})
}

func withValues(direction storage.Direction, warning, critical *float64) *storage.Threshold {
	return &storage.Threshold{
		Series:    storage.SeriesRef{Node: node, Metric: metric, Labels: mount},
		Direction: direction,
		Warning:   warning,
		Critical:  critical,
	}
}

func ptr(th storage.Threshold) *storage.Threshold { return &th }
