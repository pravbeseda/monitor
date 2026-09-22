// Package systemd reports how many units the system manager holds as failed
// (docs/specs/host-sensors.md).
package systemd

import (
	"context"
	"fmt"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const metricFailedUnits = "systemd.failed_units"

// Source is what the sensor asks of the system manager.
type Source interface {
	// Booted reports whether systemd booted this machine.
	Booted() bool
	FailedUnits(ctx context.Context) (int, error)
}

// Sensor reports the failed-unit count.
type Sensor struct {
	source Source
	now    func() time.Time
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over the system manager and the agent's clock.
func New(source Source, now func() time.Time) *Sensor {
	return &Sensor{source: source, now: now}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return "systemd" }

// Applicable is true where systemd booted the machine.
func (s *Sensor) Applicable() bool { return s.source.Booted() }

// Collect returns the count, or an error and nothing. Enabled where systemd does not run, it
// returns nothing and no error: the configuration, not the machine, is what is off.
func (s *Sensor) Collect(ctx context.Context) ([]sensor.Measurement, error) {
	if !s.source.Booted() {
		return nil, nil
	}
	failed, err := s.source.FailedUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("count failed units: %w", err)
	}
	return []sensor.Measurement{
		{Metric: metricFailedUnits, Value: float64(failed), TS: s.now()},
	}, nil
}
