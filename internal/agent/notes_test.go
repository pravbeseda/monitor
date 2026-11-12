package agent_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/sensor"
)

func failing(value float64, notes ...string) sensor.Measurement {
	return sensor.Measurement{Metric: "systemd.failed_units", Value: value, TS: start, Notes: notes}
}

// spec: agent.md#ticking — a measurement posts with its notes in their order, one with none
// with no notes; in a batch kept from earlier ticks only a series' newest value posts them.
func TestNotesRideOnlyTheNewestValueOfTheirSeries(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{
		{response: configure("v1", "5m", map[string]api.SensorConfig{"systemd": {Enabled: true, Interval: "5m"}})},
		{err: api.StatusError{Status: 503}},
		{},
	}
	units := &stub{name: "systemd", measurements: []sensor.Measurement{failing(2, "b.service (exit-code)", "a.service (timeout)")}}
	a := newAgent(t, h, c, units)

	_ = a.Tick(context.Background()) // configuration
	c.advance(5 * time.Minute)
	_ = a.Tick(context.Background()) // refused: the batch is kept
	if got := measurements(h.last()); len(got) != 1 || !slices.Equal(got[0].Notes, api.Notes{"b.service (exit-code)", "a.service (timeout)"}) {
		t.Fatalf("posted %+v, want the measurement with its notes in their order", got)
	}

	units.measurements = []sensor.Measurement{failing(1, "c.service (exit-code)"), {Metric: "load.avg_1m", Value: 1, TS: start}}
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got := measurements(h.last())
	if len(got) != 3 {
		t.Fatalf("posted %+v, want the kept value beside the two new ones", got)
	}
	if got[0].Notes != nil {
		t.Errorf("the kept older value posts notes %q, want none: the hub keeps the newest only", got[0].Notes)
	}
	if !slices.Equal(got[1].Notes, api.Notes{"c.service (exit-code)"}) {
		t.Errorf("the newest value posts %q", got[1].Notes)
	}
	if got[2].Notes != nil {
		t.Errorf("a measurement with no notes posts %q", got[2].Notes)
	}
}
