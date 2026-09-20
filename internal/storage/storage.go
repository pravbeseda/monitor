// Package storage persists what the hub receives and what it makes of it: measurements
// and node state, the level of every subject, the log of level changes, and when the last
// digest went out.
package storage

import (
	"context"
	"iter"
	"time"
)

// Measurement is one reading of one metric, as it was collected. Sensor names what
// produced it, and is empty when the agent is too old to say (docs/specs/ingest.md).
type Measurement struct {
	Metric string
	Sensor string
	Labels map[string]string
	Value  float64
	TS     time.Time
}

// SensorStatus is one line of an agent's manifest: a sensor the build contains and
// whether it applies to that machine.
type SensorStatus struct {
	Sensor     string
	Applicable bool
}

// Ingest is one accepted request, ready to be stored.
type Ingest struct {
	Node          string
	AgentVersion  string
	ConfigVersion string
	// ReceivedAt is hub time: the agent's clock never sets last-seen.
	ReceivedAt   time.Time
	Manifest     []SensorStatus
	Measurements []Measurement
}

// NodeState is what a node looks like right now: when it was last heard from, which agent
// version it last reported, and the latest value of every series it reports.
type NodeState struct {
	Node         string
	LastSeen     time.Time
	AgentVersion string
	Values       []Value
}

// Value is the latest reading of one series.
type Value struct {
	Metric string
	// Sensor is what produced it, and what staleness is measured against; empty when no
	// measurement of the series ever named one (docs/specs/evaluation.md#freezing).
	Sensor string
	Labels map[string]string
	Value  float64
	TS     time.Time
}

// Storage is what ingest and the history pages need of persistence (ADR 0005): SQLite
// behind an interface. Evaluation and the state declare their own boundaries where they
// consume one, so adding to those costs these callers nothing.
type Storage interface {
	// SaveIngest stores one request atomically — measurements, manifest and last-seen —
	// skipping measurements already stored under the same node, metric, labels and ts.
	SaveIngest(ctx context.Context, in Ingest) error
	// Series lists every stored series of a metric, ordered by node then by labels, each
	// with its newest timestamp and the sensor that value named.
	Series(ctx context.Context, sel Selection) ([]SeriesNewest, error)
	// Newest lists the selected series holding a point from `from` onwards, in the same
	// order, each with the timestamp of its newest stored point.
	Newest(ctx context.Context, sel Selection, from time.Time) ([]SeriesNewest, error)
	// Points streams one series' points inside [from, to], oldest first. Both bounds are
	// honoured to the stored millisecond, so a caller that means an exact instant
	// between two of them applies it itself.
	Points(ctx context.Context, ref SeriesRef, from, to time.Time) iter.Seq2[Point, error]
	Close() error
}
