package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Direction is which side of a threshold is bad (docs/specs/thresholds.md#model).
type Direction string

// The two directions a threshold can be read in: a value below it is bad, or a value
// above it is (docs/specs/thresholds.md#model).
const (
	Below Direction = "below"
	Above Direction = "above"
)

// Threshold is what one series is judged by: a direction, and up to two values in the
// unit of the series. A nil value is a level nothing can enter or hold.
type Threshold struct {
	Series SeriesRef
	Direction
	Warning  *float64
	Critical *float64
}

// ThresholdOf reads what one series is judged by. The second result is false when
// nothing is stored for it, which is how an unwatched series differs from a watched one
// that no tick has judged yet (docs/specs/state.md#listing).
func (s *SQLite) ThresholdOf(ctx context.Context, ref SeriesRef) (Threshold, bool, error) {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return Threshold{}, false, fmt.Errorf("threshold of %s: %w", ref.Metric, err)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT direction, warning, critical FROM thresholds
		WHERE metric = ? AND node = ? AND labels = ?`,
		ref.Metric, ref.Node, labels)

	th := Threshold{Series: ref}
	var warning, critical sql.NullFloat64
	switch err := row.Scan(&th.Direction, &warning, &critical); {
	case errors.Is(err, sql.ErrNoRows):
		return Threshold{}, false, nil
	case err != nil:
		return Threshold{}, false, fmt.Errorf("read threshold of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	th.Warning, th.Critical = nullable(warning), nullable(critical)
	return th, true, nil
}

// readThresholds reads every stored threshold, ordered as subjects are
// (docs/specs/state.md#ordering). A tick reads them all through its snapshot, so an edit
// needs no restart (docs/specs/evaluation.md#configuration).
func readThresholds(ctx context.Context, from querier) ([]Threshold, error) {
	rows, err := from.QueryContext(ctx, `
		SELECT node, metric, labels, direction, warning, critical FROM thresholds
		ORDER BY node, metric, labels`)
	if err != nil {
		return nil, fmt.Errorf("read thresholds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Threshold
	for rows.Next() {
		var node, metric, labels string
		var warning, critical sql.NullFloat64
		th := Threshold{}
		if err := rows.Scan(&node, &metric, &labels, &th.Direction, &warning, &critical); err != nil {
			return nil, fmt.Errorf("read thresholds: %w", err)
		}
		if th.Series, err = seriesRef(node, metric, labels); err != nil {
			return nil, err
		}
		th.Warning, th.Critical = nullable(warning), nullable(critical)
		out = append(out, th)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read thresholds: %w", err)
	}
	return out, nil
}

// SaveThreshold stores what a series is judged by, replacing whatever was there: a save
// states the whole configuration (docs/specs/thresholds.md#saving).
func (s *SQLite) SaveThreshold(ctx context.Context, th Threshold) error {
	labels, err := encodeLabels(th.Series.Labels)
	if err != nil {
		return fmt.Errorf("threshold of %s: %w", th.Series.Metric, err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO thresholds (metric, node, labels, direction, warning, critical)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (metric, node, labels) DO UPDATE SET
			direction = excluded.direction,
			warning   = excluded.warning,
			critical  = excluded.critical`,
		th.Series.Metric, th.Series.Node, labels, string(th.Direction),
		optional(th.Warning), optional(th.Critical))
	if err != nil {
		return fmt.Errorf("save threshold of %s on %s: %w", th.Series.Metric, th.Series.Node, err)
	}
	return nil
}

// DeleteThreshold stops a series being judged. Removing what is not there is not an
// error: the form clears a configuration by saving it empty, whatever was stored before.
func (s *SQLite) DeleteThreshold(ctx context.Context, ref SeriesRef) error {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return fmt.Errorf("threshold of %s: %w", ref.Metric, err)
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM thresholds WHERE metric = ? AND node = ? AND labels = ?`,
		ref.Metric, ref.Node, labels)
	if err != nil {
		return fmt.Errorf("delete threshold of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	return nil
}

func nullable(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func optional(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
