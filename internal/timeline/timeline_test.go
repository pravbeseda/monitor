package timeline_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/timeline"
)

// at is a time of the day the tests look back from, in UTC, which is the reader's zone here.
func at(hour, minute int) time.Time {
	return time.Date(2026, time.September, 24, hour, minute, 0, 0, time.UTC)
}

var (
	now    = at(15, 40)
	volume = storage.Subject{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/data"}}
	bound  = 15 * time.Minute // three of a 5m interval
)

// every is a stamp every five minutes from one instant up to another, both included.
func every(from, to time.Time) []time.Time {
	var out []time.Time
	for ts := from; !ts.After(to); ts = ts.Add(5 * time.Minute) {
		out = append(out, ts)
	}
	return out
}

// throughout is a series fresh at every moment of the window and before it.
func throughout() []timeline.Interval {
	return timeline.Fresh(every(now.Add(-30*time.Hour), now), bound)
}

func state(level string, since time.Time) storage.State {
	return storage.State{Subject: volume, Level: level, Since: since}
}

func change(at time.Time, from, to string, fromSince time.Time) storage.Transition {
	return storage.Transition{Subject: volume, At: at, From: from, To: to, FromSince: fromSince}
}

func spansOf(t *testing.T, states []storage.State, events []storage.Transition) []timeline.Span {
	t.Helper()
	all, err := timeline.Spans(states, events, now)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	key, err := volume.Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	return all[key]
}

var letters = map[timeline.Cell]string{
	timeline.Silent: "S", timeline.NoFreshData: "_", timeline.Critical: "C",
	timeline.Warning: "W", timeline.OK: "o", timeline.Reporting: ".",
}

// lane writes a lane as one letter per cell, from 16:00 yesterday to 15:00 today.
func lane(node timeline.Node) string {
	var out strings.Builder
	for _, cell := range timeline.Cells(node, timeline.Starts(now, time.UTC), now) {
		out.WriteString(letters[cell])
	}
	return out.String()
}

// hours builds an expected lane: every hour given the letter of the last mark at or before
// it, marks keyed by the hour of today (a negative one is yesterday's).
func hours(marks map[int]string) string {
	var out strings.Builder
	for i := range timeline.Hours {
		hour := i - 8 // 16:00 yesterday is -8
		letter := ""
		for h := -8; h <= hour; h++ {
			if mark, ok := marks[h]; ok {
				letter = mark
			}
		}
		out.WriteString(letter)
	}
	return out.String()
}

func check(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("lane\n got %s\nwant %s\n     (16:00 yesterday … 15:00 today)", got, want)
	}
}

// spec: timeline.md#lanes — 24 cells from 16:00 yesterday to the hour in progress at 15:40.
func TestTheCellsStartOnTheReadersHours(t *testing.T) {
	starts := timeline.Starts(now, time.UTC)
	if len(starts) != 24 || !starts[0].Equal(at(-8, 0)) || !starts[23].Equal(at(15, 0)) {
		t.Fatalf("starts = %v … %v (%d), want 16:00 yesterday … 15:00", starts[0], starts[len(starts)-1], len(starts))
	}
}

// spec: timeline.md#lanes — a reader 30 minutes off UTC gets cells on the hour of their clock.
func TestTheCellsFollowAHalfHourZone(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	for _, start := range timeline.Starts(now, kolkata) {
		if local := start.In(kolkata); local.Minute() != 0 || local.Second() != 0 {
			t.Fatalf("a cell starts at %s", local.Format("15:04:05"))
		}
	}
	if last := timeline.Starts(now, kolkata)[23]; !last.Equal(at(15, 30)) {
		t.Errorf("last cell starts at %v, want 21:00 in Kolkata", last)
	}
}

// spec: timeline.md#lanes — reporting throughout with nothing watched is never green.
func TestANodeWithNothingWatchedIsReporting(t *testing.T) {
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout()}}}), hours(map[int]string{-8: "."}))
}

// spec: timeline.md#lanes — one series watched and ok all day.
func TestAWatchedSeriesAtOkIsGreen(t *testing.T) {
	spans := spansOf(t, []storage.State{state("ok", now.Add(-30*time.Hour))}, nil)
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), hours(map[int]string{-8: "o"}))
}

// spec: timeline.md#lanes — ok from before the window, warning at 14:20, critical at 16:05.
func TestAVolumeWorseningThroughTheDay(t *testing.T) {
	late := at(17, 30)
	fresh := timeline.Fresh(every(late.Add(-30*time.Hour), late), bound)
	all, err := timeline.Spans(
		[]storage.State{state("critical", at(16, 5))},
		[]storage.Transition{change(at(14, 20), "ok", "warning", late.Add(-30*time.Hour)), change(at(16, 5), "warning", "critical", at(14, 20))},
		late)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	key, _ := volume.Key()
	var got strings.Builder
	for _, cell := range timeline.Cells(timeline.Node{Series: []timeline.Series{{Fresh: fresh, Spans: all[key]}}}, timeline.Starts(late, time.UTC), late) {
		got.WriteString(letters[cell])
	}
	// 18:00 yesterday … 17:00 today: 20 hours ok, 14 and 15 warning, 16 and 17 critical.
	check(t, got.String(), strings.Repeat("o", 20)+"WWCC")
}

// spec: timeline.md#lanes — a warning that recovered at 09:10.
func TestARecoveryTurnsTheNextHourGreen(t *testing.T) {
	spans := spansOf(t, []storage.State{state("ok", at(9, 10))},
		[]storage.Transition{change(at(6, 0), "ok", "warning", now.Add(-30*time.Hour)), change(at(9, 10), "warning", "ok", at(6, 0))})
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), hours(map[int]string{-8: "o", 6: "W", 10: "o"}))
}

// spec: timeline.md#lanes — critical since 30 hours ago and still.
func TestALongCriticalFillsTheLane(t *testing.T) {
	spans := spansOf(t, []storage.State{state("critical", now.Add(-30*time.Hour))}, nil)
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), hours(map[int]string{-8: "C"}))
}

// spec: timeline.md#lanes — a first evaluation that found critical at 11:30.
func TestAFirstEvaluationStartsTheLevel(t *testing.T) {
	spans := spansOf(t, []storage.State{state("critical", at(11, 30))}, []storage.Transition{change(at(11, 30), "ok", "critical", at(11, 30))})
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), hours(map[int]string{-8: ".", 11: "C"}))
}

// spec: timeline.md#lanes — a laptop last fresh at 01:50 and fresh again from 07:40.
func TestASleepingLaptopHasNoFreshData(t *testing.T) {
	stamps := append(every(now.Add(-30*time.Hour), at(1, 35)), every(at(7, 40), now)...)
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: timeline.Fresh(stamps, bound)}}}), hours(map[int]string{-8: ".", 2: "_", 7: "."}))
}

// spec: timeline.md#lanes — a node silent from 09:10, and one whose series was critical
// from before.
func TestSilenceWinsFromTheHourItWasNoticed(t *testing.T) {
	silent := []timeline.Interval{{From: at(9, 10), To: now.Add(time.Nanosecond)}}
	fresh := timeline.Fresh(every(now.Add(-30*time.Hour), at(8, 55)), bound)
	check(t, lane(timeline.Node{Silent: silent, Series: []timeline.Series{{Fresh: fresh}}}), hours(map[int]string{-8: ".", 9: "S"}))

	spans := spansOf(t, []storage.State{state("critical", now.Add(-30*time.Hour))}, nil)
	check(t, lane(timeline.Node{Silent: silent, Series: []timeline.Series{{Fresh: fresh, Spans: spans}}}), hours(map[int]string{-8: "C", 9: "S"}))
}

// spec: timeline.md#lanes — a critical volume whose last point is at 12:00, other series
// fresh and unwatched.
func TestALevelCountsOnlyWhileItsSeriesIsFresh(t *testing.T) {
	spans := spansOf(t, []storage.State{state("critical", now.Add(-30*time.Hour))}, nil)
	unplugged := timeline.Fresh(every(now.Add(-30*time.Hour), at(12, 0)), bound)
	node := timeline.Node{Series: []timeline.Series{{Fresh: unplugged, Spans: spans}, {Fresh: throughout()}}}
	check(t, lane(node), hours(map[int]string{-8: "C", 13: "."}))
}

// spec: timeline.md#lanes — a node that first reported at 12:30.
func TestHoursBeforeTheFirstReportHaveNoFreshData(t *testing.T) {
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: timeline.Fresh(every(at(12, 30), now), bound)}}}), hours(map[int]string{-8: "_", 12: "."}))
}

// spec: timeline.md#lanes — a threshold removed, then set again: the forgotten level shows
// only in the hour it began, and nothing paints the hours nobody judged.
func TestARemovedThresholdLeavesOnlyTheHourItsLevelBegan(t *testing.T) {
	entered := change(at(10, 0), "ok", "critical", at(10, 0))
	for _, tc := range []struct {
		name   string
		states []storage.State
		events []storage.Transition
		want   string
	}{
		{"removed at 12:00", nil, []storage.Transition{entered}, hours(map[int]string{-8: ".", 10: "C", 11: "."})},
		{"set again at 14:00, ok", []storage.State{state("ok", at(14, 0))}, []storage.Transition{entered},
			hours(map[int]string{-8: ".", 10: "C", 11: ".", 14: "o"})},
		{"set again at 14:00, critical", []storage.State{state("critical", at(14, 0))},
			[]storage.Transition{entered, change(at(14, 0), "ok", "critical", at(14, 0))},
			hours(map[int]string{-8: ".", 10: "C", 11: ".", 14: "C"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spans := spansOf(t, tc.states, tc.events)
			check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), tc.want)
		})
	}
}

// spec: timeline.md#lanes — an ok level that began and ended with no recorded change
// leaves nothing once it is forgotten.
func TestAForgottenOkLevelLeavesNothing(t *testing.T) {
	// Another volume's records are all the log holds; the forgotten one left none.
	other := storage.Subject{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/"}}
	spans := spansOf(t,
		[]storage.State{{Subject: other, Level: "warning", Since: at(9, 0)}},
		[]storage.Transition{{Subject: other, At: at(9, 0), From: "ok", To: "warning", FromSince: at(8, 0)}})
	check(t, lane(timeline.Node{Series: []timeline.Series{{Fresh: throughout(), Spans: spans}}}), hours(map[int]string{-8: "."}))
}

// spec: timeline.md#model — a series with no interval to be aged by is never fresh.
func TestASeriesWithNoBoundIsNeverFresh(t *testing.T) {
	if got := timeline.Fresh(every(at(12, 0), now), 0); got != nil {
		t.Errorf("fresh = %v, want nothing", got)
	}
}

// spec: timeline.md#model — a point keeps its series fresh from its own stamp, never before.
func TestAPointIsNotFreshBeforeItsStamp(t *testing.T) {
	got := timeline.Fresh([]time.Time{at(12, 0)}, bound)
	if len(got) != 1 || !got[0].From.Equal(at(12, 0)) || !got[0].To.After(at(12, 15)) || got[0].To.After(at(12, 15).Add(time.Microsecond)) {
		t.Errorf("fresh = %v, want 12:00 to 12:15 inclusive", got)
	}
}

// spec: timeline.md#edge-cases — every cell starts on the hour of the reader's clock, but for
// the one a half-hour daylight-saving shift cuts, which starts at the shift; whatever minute
// the page is read at.
func TestTheCellsStayOnTheHourAcrossAHalfHourShift(t *testing.T) {
	lordHowe, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	// The clocks go forward half an hour at 02:00 on 5 October 2025, and back on 6 April.
	for _, day := range []time.Time{
		time.Date(2025, time.October, 5, 12, 0, 0, 0, lordHowe),
		time.Date(2025, time.April, 6, 12, 0, 0, 0, lordHowe),
	} {
		for minute := range 60 {
			now := day.Add(time.Duration(minute) * time.Minute)
			starts := timeline.Starts(now, lordHowe)
			for i, start := range starts {
				shift, _ := start.In(lordHowe).ZoneBounds()
				if local := start.In(lordHowe); local.Minute() != 0 && !start.Equal(shift) {
					t.Errorf("at %s cell %d starts at %s", now.Format("Jan 2 15:04"), i, local.Format("15:04 -0700"))
				}
				if i > 0 && !start.After(starts[i-1]) {
					t.Errorf("at %s cell %d does not follow the one before it", now.Format("Jan 2 15:04"), i)
				}
			}
		}
	}
}
