// Package storage persists what the hub receives and what it makes of it: measurements
// and node state, the level of every subject, the log of level changes, and when the last
// digest went out.
package storage

import (
	"context"
	"time"
)

// Measurement is one reading of one metric, as it was collected.
type Measurement struct {
	Metric string
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
	// Series lists every stored series of a metric, ordered by node then by labels.
	Series(ctx context.Context, sel Selection) ([]SeriesRef, error)
	// Points reads those series with the points stored from `from` onwards, oldest first.
	Points(ctx context.Context, sel Selection, from time.Time) ([]SeriesPoints, error)
	Close() error
}
