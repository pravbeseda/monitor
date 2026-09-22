// Package uptime reports how long the machine has been up since it booted
// (docs/specs/host-sensors.md).
package uptime

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const metricBootSeconds = "uptime.boot_seconds"

// Source reads the time the machine booted.
type Source func() (time.Time, error)

// Sensor reports the time since boot by the agent's clock.
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
func (s *Sensor) Name() string { return "uptime" }

// Applicable is true everywhere: every machine booted once.
func (s *Sensor) Applicable() bool { return true }

// Collect returns whole seconds since boot, or an error and nothing.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	boot, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("read the boot time: %w", err)
	}
	at := s.now()
	up := at.Sub(boot)
	if up < 0 {
		return nil, fmt.Errorf("boot time %s is later than the clock %s", boot.UTC(), at.UTC())
	}
	return []sensor.Measurement{
		{Metric: metricBootSeconds, Value: math.Floor(up.Seconds()), TS: at},
	}, nil
}
