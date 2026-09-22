package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor/memory"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

// spec: host-sensors.md#memory — both metrics, the share rounded to two decimals.
func TestCollectReportsBytesAndShare(t *testing.T) {
	source := func() (memory.Available, error) {
		return memory.Available{Bytes: 8589934592, Pct: 49.996}, nil
	}
	s := memory.New(source, func() time.Time { return collected })

	got, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	want := map[string]float64{"memory.available_bytes": 8589934592, "memory.available_pct": 50}
	if len(got) != len(want) {
		t.Fatalf("got %d measurements, want %d: %+v", len(got), len(want), got)
	}
	for _, m := range got {
		if v, ok := want[m.Metric]; !ok || m.Value != v {
			t.Errorf("%s = %v, want %v", m.Metric, m.Value, want[m.Metric])
		}
		if len(m.Labels) != 0 || !m.TS.Equal(collected) {
			t.Errorf("%s = %+v, want no labels and the agent's clock", m.Metric, m)
		}
	}
}

// spec: host-sensors.md#memory — unreadable figures are an error, never a zero.
func TestCollectFailsWhenTheFiguresCannotBeRead(t *testing.T) {
	s := memory.New(func() (memory.Available, error) { return memory.Available{}, errors.New("no meminfo") }, time.Now)

	got, err := s.Collect(context.Background())
	if err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and an error", got, err)
	}
}

// spec: host-sensors.md#applicability — memory is applicable everywhere.
func TestManifestEntry(t *testing.T) {
	s := memory.New(nil, time.Now)
	if s.Name() != "memory" || !s.Applicable() {
		t.Fatalf("manifest = %q/%v, want memory, applicable", s.Name(), s.Applicable())
	}
}
