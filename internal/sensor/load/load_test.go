package load_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor/load"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

// spec: host-sensors.md#load — three averages, rounded to two decimals.
func TestCollectReportsTheThreeAverages(t *testing.T) {
	s := load.New(func() ([3]float64, error) { return [3]float64{0.5, 1.25, 2.125}, nil }, func() time.Time { return collected })

	got, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	want := map[string]float64{"load.avg_1m": 0.5, "load.avg_5m": 1.25, "load.avg_15m": 2.13}
	if len(got) != len(want) {
		t.Fatalf("got %d measurements, want %d: %+v", len(got), len(want), got)
	}
	for _, m := range got {
		if v, ok := want[m.Metric]; !ok || m.Value != v {
			t.Errorf("%s = %v, want %v", m.Metric, m.Value, want[m.Metric])
		}
		if len(m.Labels) != 0 {
			t.Errorf("%s carries labels %v, want none", m.Metric, m.Labels)
		}
		if !m.TS.Equal(collected) {
			t.Errorf("%s ts = %v, want the agent's clock", m.Metric, m.TS)
		}
	}
}

// spec: host-sensors.md#load — an unreadable source is an error, never a zero.
func TestCollectFailsWhenTheAveragesCannotBeRead(t *testing.T) {
	s := load.New(func() ([3]float64, error) { return [3]float64{}, errors.New("no loadavg") }, time.Now)

	got, err := s.Collect(context.Background())
	if err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and an error", got, err)
	}
}

// spec: host-sensors.md#applicability — load is applicable everywhere.
func TestManifestEntry(t *testing.T) {
	s := load.New(nil, time.Now)
	if s.Name() != "load" || !s.Applicable() {
		t.Fatalf("manifest = %q/%v, want load, applicable", s.Name(), s.Applicable())
	}
}
