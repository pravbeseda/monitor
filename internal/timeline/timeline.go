// Package timeline summarises a node's last 24 hours into hourly cells from what the hub
// already keeps: the levels evaluation stored, the log of their changes, and the stamps of
// the points each series sent (ADR 0038, docs/specs/timeline.md#model).
package timeline

import (
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Hours is how many cells a lane has.
const Hours = 24

// Cell is what one hour of a node looked like.
type Cell int

// The cell states, the most urgent first: a cell takes the first one that applies.
const (
	Silent Cell = iota
	NoFreshData
	Critical
	Warning
	OK
	Reporting
)

// Interval is the stretch [From, To).
type Interval struct {
	From, To time.Time
}

// closed is the stretch from one instant to another, both included: it ends a nanosecond
// after to, so an instant is one nanosecond long and still lands in the hour it fell in.
func closed(from, to time.Time) Interval { return Interval{From: from, To: to.Add(time.Nanosecond)} }

func (i Interval) overlaps(other Interval) bool {
	return i.From.Before(other.To) && other.From.Before(i.To)
}

func (i Interval) intersect(other Interval) (Interval, bool) {
	out := Interval{From: later(i.From, other.From), To: earlier(i.To, other.To)}
	return out, out.From.Before(out.To)
}

// Span is a level held over an interval.
type Span struct {
	Interval
	Level evaluate.Level
}

// Series is one series of a node as a lane reads it: when it was fresh, and the levels it
// held — none for a series nothing ever watched.
type Series struct {
	Fresh []Interval
	Spans []Span
}

// Node is one lane's input. Silent holds the stretches its silence subject stood critical.
type Node struct {
	Silent []Interval
	Series []Series
}

// Starts are the instants the 24 cells begin at: each on the hour of the reader's clock, the
// last one the hour in progress at now. A clock that shifts by half an hour has no hour
// start in the cell it cuts, so that cell begins at the shift, half an hour or an hour and a
// half long, and every other cell stays on the hour.
func Starts(now time.Time, zone *time.Location) []time.Time {
	out := make([]time.Time, Hours)
	for i := range out {
		out[i] = onTheHour(now.Add(-time.Duration(Hours-1-i)*time.Hour), zone)
	}
	return out
}

func onTheHour(at time.Time, zone *time.Location) time.Time {
	local := at.In(zone)
	start := local.Add(-time.Duration(local.Minute())*time.Minute -
		time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
	if shift, _ := local.ZoneBounds(); start.Before(shift) {
		return shift
	}
	return start
}

// Cells is the lane of one node over the cells that begin at starts, the last one running
// up to and including now. Each cell takes the first state that applies to some moment of
// it (docs/specs/timeline.md#model).
func Cells(node Node, starts []time.Time, now time.Time) []Cell {
	out := make([]Cell, len(starts))
	for i, start := range starts {
		cell := closed(start, now)
		if i+1 < len(starts) {
			cell.To = starts[i+1]
		}
		out[i] = cellOf(node, cell)
	}
	return out
}

func cellOf(node Node, cell Interval) Cell {
	for _, silent := range node.Silent {
		if silent.overlaps(cell) {
			return Silent
		}
	}
	fresh, worst := false, Reporting
	for _, series := range node.Series {
		for _, window := range series.Fresh {
			if !window.overlaps(cell) {
				continue
			}
			fresh = true
			for _, span := range series.Spans {
				if held, ok := span.intersect(window); ok && held.overlaps(cell) {
					worst = min(worst, cellFor(span.Level))
				}
			}
		}
	}
	if !fresh {
		return NoFreshData
	}
	return worst
}

func cellFor(level evaluate.Level) Cell {
	switch level {
	case evaluate.Critical:
		return Critical
	case evaluate.Warning:
		return Warning
	}
	return OK
}

// Fresh is when a series whose points carry these stamps was fresh: from each stamp for as
// long as bound, inclusive, as the state judges staleness (docs/specs/state.md#staleness).
// A series with no bound is never fresh.
func Fresh(stamps []time.Time, bound time.Duration) []Interval {
	if bound <= 0 {
		return nil
	}
	sorted := append([]time.Time(nil), stamps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	var out []Interval
	for _, ts := range sorted {
		next := closed(ts, ts.Add(bound))
		if n := len(out); n > 0 && !next.From.After(out[n-1].To) {
			out[n-1].To = later(out[n-1].To, next.To)
			continue
		}
		out = append(out, next)
	}
	return out
}

// Spans turns the stored levels and the log of their changes into each subject's spans,
// keyed as storage keys subjects. A level runs from the `since` it was entered at to the
// change that left it, or to now for the level held now. A level whose end was never
// recorded — its threshold was removed — is kept only as the instant it began: every later
// end would be a guess (ADR 0038).
func Spans(states []storage.State, events []storage.Transition, now time.Time) (map[string][]Span, error) {
	// begun holds the instants a later record says a level began at, so a change whose
	// level nothing continued is known to have been forgotten.
	type begin struct {
		key string
		at  int64
	}
	out, begun := map[string][]Span{}, map[begin]bool{}

	for _, state := range states {
		key, err := state.Key()
		if err != nil {
			return nil, err
		}
		if level, known := evaluate.ParseLevel(state.Level); known {
			out[key] = append(out[key], Span{Interval: closed(state.Since, now), Level: level})
			begun[begin{key, state.Since.UnixNano()}] = true
		}
	}
	keys := make([]string, len(events))
	for i, event := range events {
		key, err := event.Key()
		if err != nil {
			return nil, err
		}
		keys[i] = key
		// A first evaluation records a change from ok that began where it ended: it
		// continues nothing.
		if !event.FromSince.Before(event.At) {
			continue
		}
		if from, known := evaluate.ParseLevel(event.From); known {
			out[key] = append(out[key], Span{Interval: Interval{From: event.FromSince, To: event.At}, Level: from})
		}
		begun[begin{key, event.FromSince.UnixNano()}] = true
	}
	for i, event := range events {
		if to, known := evaluate.ParseLevel(event.To); known && !begun[begin{keys[i], event.At.UnixNano()}] {
			out[keys[i]] = append(out[keys[i]], Span{Interval: closed(event.At, event.At), Level: to})
		}
	}
	return out, nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
