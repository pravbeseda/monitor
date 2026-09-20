package history

import (
	"context"
	"iter"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// Source is what history needs of persistence. Points streams one series at a time so that
// a read holds its answer rather than its window: a year of a busy metric is millions of
// stored points and at most 1001 returned ones.
type Source interface {
	Series(ctx context.Context, sel storage.Selection) ([]storage.SeriesRef, error)
	Newest(ctx context.Context, sel storage.Selection, from time.Time) ([]storage.SeriesNewest, error)
	Points(ctx context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error]
}

// Interval reports how often a node is expected to report a metric, and zero when the
// metric belongs to no rule.
type Interval func(node, metric string) time.Duration

// Reader answers history queries.
type Reader struct {
	Source   Source
	Interval Interval
	Now      func() time.Time
}

// maxSeries bounds one answer: a query wide enough to exceed it is refused rather than
// truncated, so no caller is quietly told less than it asked for.
const maxSeries = 50

// List answers what exists, with no window and no points.
func (r Reader) List(ctx context.Context, query Query) ([]Series, error) {
	refs, err := r.Source.Series(ctx, selection(query))
	if err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(refs))
	for _, ref := range refs {
		if matches(ref.Labels, query.Labels) {
			out = append(out, r.describe(ref))
		}
	}
	if len(out) > maxSeries {
		return nil, tooMany(len(out))
	}
	return out, nil
}

// Read answers one window of history. The window is settled from one row per series before
// any point is read, and each series is then reduced as its points stream past.
func (r Reader) Read(ctx context.Context, query Query) (Result, error) {
	now := r.Now()
	stored, err := r.Source.Newest(ctx, selection(query), now.Add(-query.Window))
	if err != nil {
		return Result{}, err
	}

	selected := make([]storage.SeriesNewest, 0, len(stored))
	for _, series := range stored {
		if matches(series.Labels, query.Labels) {
			selected = append(selected, series)
		}
	}
	if len(selected) > maxSeries {
		return Result{}, tooMany(len(selected))
	}

	window := Window{To: now}
	for _, series := range selected {
		// A clock running ahead moves the end of the window to the point it stamped, but
		// no further ahead than the window is long: one node stamped a century out would
		// otherwise carry every other series off the chart for good.
		if series.Newest.After(window.To) && series.Newest.Sub(now) <= query.Window {
			window.To = series.Newest
		}
	}
	window.From = window.To.Add(-query.Window)

	result := Result{Window: window, Series: make([]Series, 0, len(selected))}
	for _, series := range selected {
		held := newCollector(window)
		for point, err := range r.Source.Points(ctx, series.SeriesRef, window.From, window.To) {
			if err != nil {
				return Result{}, err
			}
			// Both bounds are the window's own, whatever a source hands back. The
			// lower one needs it: stored timestamps have millisecond resolution, so a
			// window starting between two of them reaches one point further back.
			if point.TS.Before(window.From) || point.TS.After(window.To) {
				continue
			}
			held.add(point)
		}
		if held.stored == 0 {
			continue
		}
		out := r.describe(series.SeriesRef)
		out.Stored = held.stored
		out.Points, out.Reduced = held.result()
		result.Series = append(result.Series, out)
	}
	return result, nil
}

func (r Reader) describe(ref storage.SeriesRef) Series {
	return Series{
		Node:     ref.Node,
		Metric:   ref.Metric,
		Labels:   ref.Labels,
		Unit:     UnitOf(ref.Metric),
		Interval: r.Interval(ref.Node, ref.Metric),
	}
}

func selection(query Query) storage.Selection {
	return storage.Selection{Metric: query.Metric, Node: query.Node}
}

func tooMany(count int) error {
	return refuse("too_many_series", "this query selects %d series, more than the %d one answer carries", count, maxSeries)
}

// matches is exact equality on every named label. A filter cannot demand that a series
// carry no other label, which is why a link meant for one series names them all.
func matches(labels, filters map[string]string) bool {
	for name, want := range filters {
		if labels[name] != want {
			return false
		}
	}
	return true
}
