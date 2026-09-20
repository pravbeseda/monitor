package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sort"
	"strings"
	"time"
)

// Selection narrows a history read to one metric and, when it is named, one node. Label
// filters are not part of it: labels live in one JSON column, so no index can serve them
// and the caller applies them to the small set a metric returns
// (docs/specs/history.md#selection).
type Selection struct {
	Metric string
	Node   string
}

// SeriesRef identifies one series — the triple a history query selects.
type SeriesRef struct {
	Node   string
	Metric string
	Labels map[string]string
}

// Point is one stored reading.
type Point struct {
	TS    time.Time
	Value float64
}

// SeriesNewest is one series a window holds, with the timestamp of its newest stored
// point — the instant the window may end at (docs/specs/history.md#window).
type SeriesNewest struct {
	SeriesRef
	Newest time.Time
}

// Series lists every stored series of a metric, whatever the age of its last point. It
// reads the series table rather than ranking points, so it costs what it answers (ADR 0031).
func (s *SQLite) Series(ctx context.Context, sel Selection) ([]SeriesRef, error) {
	query, args := seriesStatement(sel)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read series of %s: %w", sel.Metric, err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeriesRef
	for rows.Next() {
		var node, labels string
		if err := rows.Scan(&node, &labels); err != nil {
			return nil, fmt.Errorf("read series of %s: %w", sel.Metric, err)
		}
		ref, err := seriesRef(node, sel.Metric, labels)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read series of %s: %w", sel.Metric, err)
	}
	sortSeries(out, func(ref SeriesRef) SeriesRef { return ref })
	return out, nil
}

// Newest lists every selected series holding a point at or after `from`, each with the
// timestamp of its newest stored point. It takes no upper bound: a measurement stamped
// ahead of the hub's clock is the newest value of its series and the window may end at it
// (docs/specs/history.md#window). One row per series is what lets a read settle its window
// before it touches a single point, and the series table is what makes that row cheap.
func (s *SQLite) Newest(ctx context.Context, sel Selection, from time.Time) ([]SeriesNewest, error) {
	query, args := newestStatement(sel, from)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the series of %s in the window: %w", sel.Metric, err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeriesNewest
	for rows.Next() {
		var node, labels, ts string
		if err := rows.Scan(&node, &labels, &ts); err != nil {
			return nil, fmt.Errorf("read the series of %s in the window: %w", sel.Metric, err)
		}
		ref, err := seriesRef(node, sel.Metric, labels)
		if err != nil {
			return nil, err
		}
		at, err := parseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("series %s of %s: %w", labels, node, err)
		}
		out = append(out, SeriesNewest{SeriesRef: ref, Newest: at})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the series of %s in the window: %w", sel.Metric, err)
	}
	sortSeries(out, func(s SeriesNewest) SeriesRef { return s.SeriesRef })
	return out, nil
}

// Points streams one series' stored points inside [from, to], oldest first. Stored
// timestamps have millisecond resolution and so do the bounds, so a `from` falling between
// two milliseconds reaches one point further back than it says; the exact window belongs to
// the caller that set it. It yields
// rather than returns them so that the memory a read needs follows the size of its answer
// and not the length of its window: the reduction consumes the stream point by point
// (docs/specs/history.md#reduction). A failed read yields one zero point with the error and
// stops.
func (s *SQLite) Points(ctx context.Context, ref SeriesRef, from, to time.Time) iter.Seq2[Point, error] {
	return func(yield func(Point, error) bool) {
		labels, err := encodeLabels(ref.Labels)
		if err != nil {
			yield(Point{}, fmt.Errorf("read history of %s of %s: %w", ref.Metric, ref.Node, err))
			return
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT ts, value FROM measurements
			 WHERE metric = ? AND node = ? AND labels = ? AND ts >= ? AND ts <= ? ORDER BY ts`,
			ref.Metric, ref.Node, labels, formatTime(from), formatTime(to))
		if err != nil {
			yield(Point{}, fmt.Errorf("read history of %s %s of %s: %w", ref.Metric, labels, ref.Node, err))
			return
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var ts string
			var point Point
			if err := rows.Scan(&ts, &point.Value); err != nil {
				yield(Point{}, fmt.Errorf("read history of %s %s of %s: %w", ref.Metric, labels, ref.Node, err))
				return
			}
			if point.TS, err = parseTime(ts); err != nil {
				yield(Point{}, fmt.Errorf("series %s of %s: %w", labels, ref.Node, err))
				return
			}
			if !yield(point, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(Point{}, fmt.Errorf("read history of %s %s of %s: %w", ref.Metric, labels, ref.Node, err))
		}
	}
}

// seriesStatement and newestStatement are single statements so that a test can EXPLAIN
// exactly what these reads run: one row per series, never a pass over the points (ADR 0031).
func seriesStatement(sel Selection) (string, []any) {
	where, args := sel.where()
	return `SELECT node, labels FROM series WHERE ` + where, args
}

func newestStatement(sel Selection, from time.Time) (string, []any) {
	where, args := sel.where()
	return `SELECT node, labels, last_ts FROM series WHERE ` + where + ` AND last_ts >= ?`,
		append(args, formatTime(from))
}

func (sel Selection) where() (string, []any) {
	if sel.Node == "" {
		return "metric = ?", []any{sel.Metric}
	}
	return "metric = ? AND node = ?", []any{sel.Metric, sel.Node}
}

func seriesRef(node, metric, labels string) (SeriesRef, error) {
	ref := SeriesRef{Node: node, Metric: metric}
	if err := json.Unmarshal([]byte(labels), &ref.Labels); err != nil {
		return SeriesRef{}, fmt.Errorf("series %s of %s: decode labels: %w", labels, node, err)
	}
	return ref, nil
}

// sortSeries puts series in the order the spec promises: by node, then by the labels
// rendered as sorted key=value pairs. Sorting here rather than in SQL is what keeps the
// order a statement about labels instead of about the encoding they are stored in.
func sortSeries[T any](series []T, ref func(T) SeriesRef) {
	sort.SliceStable(series, func(i, j int) bool {
		a, b := ref(series[i]), ref(series[j])
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return LabelKey(a.Labels) < LabelKey(b.Labels)
	})
}

// LabelKey renders a label set as the order the history and state contracts sort by.
func LabelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+labels[key])
	}
	return strings.Join(pairs, ",")
}
