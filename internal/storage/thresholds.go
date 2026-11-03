package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
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

// Readable reports whether a stored threshold is one this build can judge by: a direction
// it knows, and values that are finite. Anything else is skipped rather than guessed at —
// an unknown direction read as its opposite would alert upside down, and a NaN is never
// entered and never left, so it would latch a subject at its level for good
// (docs/specs/evaluation.md#configuration-changes).
func (th Threshold) Readable() bool {
	if th.Direction != Below && th.Direction != Above {
		return false
	}
	for _, value := range []*float64{th.Warning, th.Critical} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return false
		}
	}
	return true
}

// Ordered reports whether critical is strictly beyond warning in the direction. Equal values
// would leave one of the two levels unreachable.
func Ordered(direction Direction, warning, critical *float64) bool {
	if warning == nil || critical == nil {
		return true
	}
	if direction == Above {
		return *critical > *warning
	}
	return *critical < *warning
}

// Storable reports whether the store may hold th: readable, with a value, and its critical
// strictly beyond its warning (docs/specs/thresholds.md#invariants).
func (th Threshold) Storable() bool {
	return th.Readable() && (th.Warning != nil || th.Critical != nil) && Ordered(th.Direction, th.Warning, th.Critical)
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

const insertThreshold = `
	INSERT INTO thresholds (metric, node, labels, direction, warning, critical)
	VALUES (?, ?, ?, ?, ?, ?)`

// saveThreshold stores what a series is judged by, replacing whatever was there: a save
// states the whole configuration (docs/specs/thresholds.md#saving).
func saveThreshold(ctx context.Context, to execer, th Threshold) error {
	return writeThreshold(ctx, to, th, `
		ON CONFLICT (metric, node, labels) DO UPDATE SET
			direction = excluded.direction,
			warning   = excluded.warning,
			critical  = excluded.critical`)
}

func writeThreshold(ctx context.Context, to execer, th Threshold, onConflict string) error {
	labels, err := encodeLabels(th.Series.Labels)
	if err != nil {
		return fmt.Errorf("threshold of %s: %w", th.Series.Metric, err)
	}
	_, err = to.ExecContext(ctx, insertThreshold+onConflict,
		th.Series.Metric, th.Series.Node, labels, string(th.Direction),
		optional(th.Warning), optional(th.Critical))
	if err != nil {
		return fmt.Errorf("save threshold of %s on %s: %w", th.Series.Metric, th.Series.Node, err)
	}
	return nil
}

// seed writes a sensor's default into its series once: never over a stored threshold, and
// never into a series seeded before or whose threshold a save changed
// (docs/specs/thresholds.md#defaults).
func seed(ctx context.Context, to execer, th Threshold) error {
	first, err := markSeeded(ctx, to, th.Series)
	if err != nil || !first {
		return err
	}
	return writeThreshold(ctx, to, th, ` ON CONFLICT DO NOTHING`)
}

// markSeeded records that a series is not to be seeded again, and whether it was the first
// to say so.
func markSeeded(ctx context.Context, to execer, ref SeriesRef) (bool, error) {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return false, fmt.Errorf("seeding of %s: %w", ref.Metric, err)
	}
	result, err := to.ExecContext(ctx, `
		INSERT INTO seeded (metric, node, labels) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		ref.Metric, ref.Node, labels)
	if err != nil {
		return false, fmt.Errorf("mark %s on %s seeded: %w", ref.Metric, ref.Node, err)
	}
	marked, err := result.RowsAffected()
	return marked == 1, err
}

// deleteThreshold stops a series being judged, and says whether anything was stored.
// Removing what is not there is not an error: the form clears a configuration by saving it
// empty, whatever was stored before.
func deleteThreshold(ctx context.Context, to execer, ref SeriesRef) (bool, error) {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return false, fmt.Errorf("threshold of %s: %w", ref.Metric, err)
	}
	result, err := to.ExecContext(ctx, `
		DELETE FROM thresholds WHERE metric = ? AND node = ? AND labels = ?`,
		ref.Metric, ref.Node, labels)
	if err != nil {
		return false, fmt.Errorf("delete threshold of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	deleted, err := result.RowsAffected()
	return deleted > 0, err
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
