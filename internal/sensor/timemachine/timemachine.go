// Package timemachine reports how long ago each Time Machine destination last received a
// backup (docs/specs/timemachine-sensor.md).
package timemachine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const metricBackupAge = "timemachine.backup_age_seconds"

// Destination is one place Time Machine backs up to: the name of its volume as Time Machine
// last saw it, and the dates of the backups it holds.
type Destination struct {
	Name    string
	Backups []time.Time
}

// Source reads Time Machine's destinations.
type Source func(ctx context.Context) ([]Destination, error)

// Sensor reports each destination's backup age by the agent's clock.
type Sensor struct {
	source Source
	now    func() time.Time
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over a platform source, nil where Time Machine does not exist, and
// the agent's clock.
func New(source Source, now func() time.Time) *Sensor {
	return &Sensor{source: source, now: now}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return "timemachine" }

// Applicable is true where Time Machine exists: on macOS.
func (s *Sensor) Applicable() bool { return s.source != nil }

// Collect returns one age per destination that can be dated, or an error and nothing when
// the destinations cannot be read. Enabled where Time Machine does not exist, it returns
// nothing and no error.
func (s *Sensor) Collect(ctx context.Context) ([]sensor.Measurement, error) {
	if s.source == nil {
		return nil, nil
	}
	destinations, err := s.source(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the Time Machine destinations: %w", err)
	}
	named := map[string]int{}
	for _, d := range destinations {
		named[d.Name]++
	}
	at := s.now()
	var out []sensor.Measurement
	for _, d := range destinations {
		if d.Name == "" || named[d.Name] > 1 || len(d.Backups) == 0 {
			continue
		}
		age := at.Sub(latest(d.Backups))
		if age < 0 {
			continue
		}
		out = append(out, sensor.Measurement{
			Metric: metricBackupAge,
			Labels: map[string]string{"destination": d.Name},
			Value:  math.Floor(age.Seconds()),
			TS:     at,
		})
	}
	return out, nil
}

func latest(dates []time.Time) time.Time {
	newest := dates[0]
	for _, d := range dates[1:] {
		if d.After(newest) {
			newest = d
		}
	}
	return newest
}
