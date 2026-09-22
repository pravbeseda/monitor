// Package load reports the system's load averages (docs/specs/host-sensors.md).
package load

import (
	"context"
	"fmt"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

var metrics = [3]string{"load.avg_1m", "load.avg_5m", "load.avg_15m"}

// Source reads the 1, 5 and 15 minute load averages from the platform.
type Source func() ([3]float64, error)

// Sensor reports the load averages as the system computes them.
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
func (s *Sensor) Name() string { return "load" }

// Applicable is true everywhere: every supported system keeps load averages.
func (s *Sensor) Applicable() bool { return true }

// Collect returns the three averages, or an error and nothing: a zero load is a reading.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	averages, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("read the load averages: %w", err)
	}
	at := s.now()
	out := make([]sensor.Measurement, 0, len(metrics))
	for i, metric := range metrics {
		out = append(out, sensor.Measurement{Metric: metric, Value: sensor.Round2(averages[i]), TS: at})
	}
	return out, nil
}
