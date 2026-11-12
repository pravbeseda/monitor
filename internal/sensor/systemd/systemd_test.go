package systemd_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pravbeseda/monitor/internal/note"
	"github.com/pravbeseda/monitor/internal/sensor/systemd"
)

var collected = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

type fake struct {
	booted bool
	failed int
	err    error
	// units and listErr are what listing the failed units answers.
	units   []systemd.Unit
	listErr error
	// cancel makes listing the units outlast the collection's deadline.
	cancel context.CancelFunc
}

func (f fake) Booted() bool { return f.booted }

func (f fake) FailedUnits(context.Context) (int, error) { return f.failed, f.err }

func (f fake) ListFailed(ctx context.Context) ([]systemd.Unit, error) {
	if f.cancel != nil {
		f.cancel()
		return nil, ctx.Err()
	}
	return f.units, f.listErr
}

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

// notesOf collects from source and returns the count and the notes it came with.
func notesOf(t *testing.T, source systemd.Source) (float64, []string, error) {
	t.Helper()
	got, err := systemd.New(source, func() time.Time { return collected }).Collect(context.Background())
	if len(got) != 1 {
		t.Fatalf("Collect = %+v, %v; want the count", got, err)
	}
	return got[0].Value, got[0].Notes, err
}

// spec: host-sensors.md#failed-units — each failed unit is a note of its name and its Result,
// ordered by name byte by byte; a unit with no Result is named alone.
func TestCollectNamesTheFailedUnits(t *testing.T) {
	for _, c := range []struct {
		name  string
		units []systemd.Unit
		want  []string
	}{
		{"by name", []systemd.Unit{{Name: "b.service", Result: "exit-code"}, {Name: "a.timer", Result: "resources"}},
			[]string{"a.timer (resources)", "b.service (exit-code)"}},
		{"byte by byte", []systemd.Unit{{Name: "a.service", Result: "timeout"}, {Name: "B.service", Result: "timeout"}},
			[]string{"B.service (timeout)", "a.service (timeout)"}},
		{"no Result", []systemd.Unit{{Name: "a.service"}}, []string{"a.service"}},
		{"the root mount", []systemd.Unit{{Name: "-.mount", Result: "exit-code"}}, []string{"-.mount (exit-code)"}},
	} {
		value, notes, err := notesOf(t, fake{booted: true, failed: len(c.units), units: c.units})
		if err != nil || value != float64(len(c.units)) || !slices.Equal(notes, c.want) {
			t.Errorf("%s: %v, %q, %v; want %d, %q", c.name, value, notes, err, len(c.units), c.want)
		}
	}
}

// spec: host-sensors.md#failed-units — 25 failed units: the count, and the notes of the first
// 20 by name.
func TestCollectNamesTheFirstTwentyUnits(t *testing.T) {
	var units []systemd.Unit
	for i := 25; i > 0; i-- {
		units = append(units, systemd.Unit{Name: fmt.Sprintf("unit-%02d.service", i), Result: "exit-code"})
	}
	value, notes, err := notesOf(t, fake{booted: true, failed: 25, units: units})
	if err != nil || value != 25 || len(notes) != note.MaxCount || notes[0] != "unit-01.service (exit-code)" || notes[19] != "unit-20.service (exit-code)" {
		t.Fatalf("got %v, %q, %v; want 25 and the first 20 by name", value, notes, err)
	}
}

// spec: host-sensors.md#failed-units — a name too long for a note is cut to fit, ending in …,
// its result kept.
func TestCollectCutsANameTooLongForANote(t *testing.T) {
	long := strings.Repeat("x", 200) + ".service"
	_, notes, _ := notesOf(t, fake{booted: true, failed: 1, units: []systemd.Unit{{Name: long, Result: "exit-code"}}})
	if len(notes) != 1 || utf8.RuneCountInString(notes[0]) != note.MaxLength || !strings.HasSuffix(notes[0], "… (exit-code)") ||
		!strings.HasPrefix(notes[0], "xxxx") {
		t.Fatalf("notes = %q, want the name cut to a note of %d ending in … (exit-code)", notes, note.MaxLength)
	}
}

// spec: host-sensors.md#failed-units — nothing failed: no units are asked for, and the count
// carries no notes.
func TestCollectNamesNothingWhenNothingFailed(t *testing.T) {
	value, notes, err := notesOf(t, fake{booted: true, units: []systemd.Unit{{Name: "stray.service"}}})
	if err != nil || value != 0 || notes != nil {
		t.Fatalf("got %v, %q, %v; want 0 with no notes", value, notes, err)
	}
}

// spec: host-sensors.md#failed-units — counted one, then listed none: the count with no notes
// and no error.
func TestCollectKeepsACountThatListsNone(t *testing.T) {
	value, notes, err := notesOf(t, fake{booted: true, failed: 1})
	if err != nil || value != 1 || notes != nil {
		t.Fatalf("got %v, %q, %v; want 1 with no notes and no error", value, notes, err)
	}
}

// spec: host-sensors.md#failed-units — the list or the results fail: the count stands
// without notes, and the error is logged.
func TestCollectKeepsTheCountWhenTheUnitsCannotBeRead(t *testing.T) {
	broken := errors.New("systemctl show: exit status 1")
	value, notes, err := notesOf(t, fake{booted: true, failed: 2, listErr: broken})
	if !errors.Is(err, broken) || value != 2 || notes != nil {
		t.Fatalf("got %v, %q, %v; want 2 with no notes and the error", value, notes, err)
	}
}

// spec: host-sensors.md#failed-units — the units not read before the collection is cancelled:
// no measurements and an error.
func TestCollectFailsWhenTheUnitsOutlastTheCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := systemd.New(fake{booted: true, failed: 2, cancel: cancel}, time.Now).Collect(ctx)
	if !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatalf("got %v, %v; want no measurements and the cancellation", got, err)
	}
}
