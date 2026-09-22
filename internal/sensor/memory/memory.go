// Package memory reports how much memory the system considers available
// (docs/specs/host-sensors.md).
package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const (
	metricAvailableBytes = "memory.available_bytes"
	metricAvailablePct   = "memory.available_pct"
)

// Available is the system's own judgement of the memory a new program can have without
// swapping, as a size and as a share of the total the system reports.
type Available struct {
	Bytes uint64
	Pct   float64
}

// Source reads the platform's figures.
type Source func() (Available, error)

// Sensor reports available memory.
type Sensor struct {
	source Source
	now    func() time.Time
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over a platform source and the agent's clock.
func New(source Source, now func() time.Time) *Sensor {
	return &Sensor{source: source, now: now}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return "memory" }

// Applicable is true everywhere: every supported system reports its memory.
func (s *Sensor) Applicable() bool { return true }

// Collect returns both figures, or an error and nothing: zero available is a reading.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	available, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("read available memory: %w", err)
	}
	at := s.now()
	return []sensor.Measurement{
		{Metric: metricAvailableBytes, Value: float64(available.Bytes), TS: at},
		{Metric: metricAvailablePct, Value: sensor.Round2(available.Pct), TS: at},
	}, nil
}
