package uptime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor/uptime"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

func collect(t *testing.T, boot time.Time) (float64, error) {
	t.Helper()
	s := uptime.New(func() (time.Time, error) { return boot, nil }, func() time.Time { return collected })
	got, err := s.Collect(context.Background())
	if err != nil {
		return 0, err
	}
	if len(got) != 1 || got[0].Metric != "uptime.boot_seconds" || len(got[0].Labels) != 0 || !got[0].TS.Equal(collected) {
		t.Fatalf("unexpected measurements %+v", got)
	}
	return got[0].Value, nil
}

// spec: host-sensors.md#uptime — whole seconds since boot, truncated.
func TestCollectCountsWholeSecondsSinceBoot(t *testing.T) {
	boot := collected.Add(-(3*24*time.Hour + 2*time.Hour + 900*time.Millisecond))
	if got, err := collect(t, boot); err != nil || got != 266400 {
		t.Fatalf("uptime = %v, %v; want 266400", got, err)
	}
}

// spec: host-sensors.md#uptime — after a reboot the count starts from the new boot.
func TestCollectCountsFromTheNewBoot(t *testing.T) {
	if got, err := collect(t, collected.Add(-10*time.Minute)); err != nil || got != 600 {
		t.Fatalf("uptime = %v, %v; want 600", got, err)
	}
}

// spec: host-sensors.md#uptime — a boot time ahead of the clock is no reading.
func TestCollectRefusesABootInTheFuture(t *testing.T) {
	if got, err := collect(t, collected.Add(time.Minute)); err == nil {
		t.Fatalf("uptime = %v, want an error", got)
	}
}

// spec: host-sensors.md#uptime — an unreadable boot time is an error, never a zero.
func TestCollectFailsWhenTheBootTimeCannotBeRead(t *testing.T) {
	s := uptime.New(func() (time.Time, error) { return time.Time{}, errors.New("no boot time") }, time.Now)
	if got, err := s.Collect(context.Background()); err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and an error", got, err)
	}
}

// spec: host-sensors.md#applicability — uptime is applicable everywhere.
func TestManifestEntry(t *testing.T) {
	s := uptime.New(nil, time.Now)
	if s.Name() != "uptime" || !s.Applicable() {
		t.Fatalf("manifest = %q/%v, want uptime, applicable", s.Name(), s.Applicable())
	}
}
