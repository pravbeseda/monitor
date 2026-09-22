// Package sensor defines what every sensor looks like from the agent's side. The
// interface must not reveal whether an implementation is built in or external
// (ADR 0003).
package sensor

import (
	"context"
	"math"
	"time"
)

// Measurement is one reading, as collected on the node.
type Measurement struct {
	Metric string
	Labels map[string]string
	Value  float64
	TS     time.Time
}

// Sensor takes one kind of reading. Name and Applicable make up the manifest the agent
// sends; Collect is called on the schedule the hub configures.
type Sensor interface {
	Name() string
	// Applicable reports whether this machine can produce the reading at all.
	Applicable() bool
	Collect(ctx context.Context) ([]Measurement, error)
}

// Round2 rounds a reading to the two decimals the sensors report ratios and averages in.
func Round2(value float64) float64 {
	return math.Round(value*100) / 100
}
