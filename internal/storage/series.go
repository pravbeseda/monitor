package storage

import (
	"context"
	"encoding/json"
	"fmt"
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

// SeriesPoints is one series with the points a window holds, oldest first.
type SeriesPoints struct {
	SeriesRef
	Points []Point
}

// Series lists every stored series of a metric, whatever the age of its last point.
func (s *SQLite) Series(ctx context.Context, sel Selection) ([]SeriesRef, error) {
	where, args := sel.where()
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT node, labels FROM measurements WHERE `+where, args...)
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

// Points reads the stored points of every selected series from `from` onwards. It takes no
// upper bound: a measurement stamped ahead of the hub's clock is the newest value of its
// series and the window ends at it (docs/specs/history.md#window).
func (s *SQLite) Points(ctx context.Context, sel Selection, from time.Time) ([]SeriesPoints, error) {
	where, args := sel.where()
	rows, err := s.db.QueryContext(ctx,
		`SELECT node, labels, ts, value FROM measurements WHERE `+where+` AND ts >= ? ORDER BY node, labels, ts`,
		append(args, formatTime(from))...)
	if err != nil {
		return nil, fmt.Errorf("read history of %s: %w", sel.Metric, err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeriesPoints
	var current string
	for rows.Next() {
		var node, labels, ts string
		var point Point
		if err := rows.Scan(&node, &labels, &ts, &point.Value); err != nil {
			return nil, fmt.Errorf("read history of %s: %w", sel.Metric, err)
		}
		if point.TS, err = parseTime(ts); err != nil {
			return nil, fmt.Errorf("series %s of %s: %w", labels, node, err)
		}
		if key := node + "\x00" + labels; key != current {
			current = key
			ref, err := seriesRef(node, sel.Metric, labels)
			if err != nil {
				return nil, err
			}
			out = append(out, SeriesPoints{SeriesRef: ref})
		}
		last := &out[len(out)-1]
		last.Points = append(last.Points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read history of %s: %w", sel.Metric, err)
	}
	sortSeries(out, func(s SeriesPoints) SeriesRef { return s.SeriesRef })
	return out, nil
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
