// Package systemd reports how many units the system manager holds as failed, and names them
// (docs/specs/host-sensors.md).
package systemd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/note"
	"github.com/pravbeseda/monitor/internal/sensor"
)

const metricFailedUnits = "systemd.failed_units"

// Source is what the sensor asks of the system manager.
type Source interface {
	// Booted reports whether systemd booted this machine.
	Booted() bool
	FailedUnits(ctx context.Context) (int, error)
	// ListFailed lists the failed units with the Result of each.
	ListFailed(ctx context.Context) ([]Unit, error)
}

// Unit is one failed unit: its name, and its Result as systemd gives it, empty when it gives
// none.
type Unit struct {
	Name, Result string
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

// Names are what this sensor calls its metrics (docs/specs/metric-names.md).
func (s *Sensor) Names() names.Book { return sensor.Catalogued(metricFailedUnits) }

// Applicable is true where systemd booted the machine.
func (s *Sensor) Applicable() bool { return s.source.Booted() }

// Collect returns the count with a note per failed unit, or an error and nothing. Units that
// cannot be named leave the count standing without notes, beside the error; the collection
// running out of time costs the count too, as the agent drops a late one anyway. Enabled
// where systemd does not run, it returns nothing and no error: the configuration, not the
// machine, is what is off.
func (s *Sensor) Collect(ctx context.Context) ([]sensor.Measurement, error) {
	if !s.source.Booted() {
		return nil, nil
	}
	failed, err := s.source.FailedUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("count failed units: %w", err)
	}
	count := sensor.Measurement{Metric: metricFailedUnits, Value: float64(failed), TS: s.now()}
	if failed == 0 {
		return []sensor.Measurement{count}, nil
	}
	units, err := s.source.ListFailed(ctx)
	if err != nil {
		err = fmt.Errorf("name failed units: %w", err)
		if ctx.Err() != nil {
			return nil, err
		}
		return []sensor.Measurement{count}, err
	}
	count.Notes = notes(units)
	return []sensor.Measurement{count}, nil
}

// notes are the first of the units by name, byte by byte, each its name and, in
// parentheses, its result: as many as a measurement keeps, each no longer than a note may
// be (docs/specs/host-sensors.md#failed-units).
func notes(units []Unit) []string {
	sorted := slices.SortedFunc(slices.Values(units), func(a, b Unit) int { return strings.Compare(a.Name, b.Name) })
	var out []string
	for _, unit := range sorted[:min(len(sorted), note.MaxCount)] {
		suffix := ""
		if unit.Result != "" {
			suffix = " (" + unit.Result + ")"
		}
		out = append(out, fit(unit.Name, suffix))
	}
	return out
}

// fit cuts name so that it and its suffix make a note no longer than note.MaxLength, the cut
// marked by an ellipsis.
func fit(name, suffix string) string {
	if utf8.RuneCountInString(name+suffix) <= note.MaxLength {
		return name + suffix
	}
	keep := max(note.MaxLength-utf8.RuneCountInString(suffix)-1, 0)
	return string([]rune(name)[:keep]) + "…" + suffix
}
