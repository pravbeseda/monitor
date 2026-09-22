package systemd_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor/systemd"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

type fake struct {
	booted bool
	failed int
	err    error
}

func (f fake) Booted() bool { return f.booted }

func (f fake) FailedUnits(context.Context) (int, error) { return f.failed, f.err }

func collect(t *testing.T, source systemd.Source) []float64 {
	t.Helper()
	got, err := systemd.New(source, func() time.Time { return collected }).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var values []float64
	for _, m := range got {
		if m.Metric != "systemd.failed_units" || len(m.Labels) != 0 || !m.TS.Equal(collected) {
			t.Fatalf("unexpected measurement %+v", m)
		}
		values = append(values, m.Value)
	}
	return values
}

// spec: host-sensors.md#failed-units — nothing failed is a reading of zero.
func TestCollectReportsZeroWhenNothingFailed(t *testing.T) {
	if got := collect(t, fake{booted: true}); len(got) != 1 || got[0] != 0 {
		t.Fatalf("failed units = %v, want [0]", got)
	}
}

// spec: host-sensors.md#failed-units — the count of failed units.
func TestCollectCountsFailedUnits(t *testing.T) {
	if got := collect(t, fake{booted: true, failed: 3}); len(got) != 1 || got[0] != 3 {
		t.Fatalf("failed units = %v, want [3]", got)
	}
}

// spec: host-sensors.md#failed-units — systemd that does not answer is an error, never a zero.
func TestCollectFailsWhenSystemdDoesNotAnswer(t *testing.T) {
	s := systemd.New(fake{booted: true, err: context.DeadlineExceeded}, time.Now)
	got, err := s.Collect(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and the deadline error", got, err)
	}
}

// spec: host-sensors.md#failed-units — enabled where systemd is not running: quiet.
func TestCollectIsQuietWithoutSystemd(t *testing.T) {
	if got := collect(t, fake{booted: false, err: errors.New("must not be asked")}); len(got) != 0 {
		t.Fatalf("failed units = %v, want nothing", got)
	}
}

// spec: host-sensors.md#applicability — applicable exactly where systemd booted the machine.
func TestManifestEntry(t *testing.T) {
	for _, booted := range []bool{true, false} {
		s := systemd.New(fake{booted: booted}, time.Now)
		if s.Name() != "systemd" || s.Applicable() != booted {
			t.Errorf("booted %v: manifest = %q/%v", booted, s.Name(), s.Applicable())
		}
	}
}
