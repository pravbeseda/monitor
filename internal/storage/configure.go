package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Excluded says whether the reader excluded a series from anomalies (ADR 0036).
func (s *SQLite) Excluded(ctx context.Context, ref SeriesRef) (bool, error) {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return false, fmt.Errorf("exclusion of %s: %w", ref.Metric, err)
	}
	var one int
	switch err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM anomaly_exclusions WHERE metric = ? AND node = ? AND labels = ?`,
		ref.Metric, ref.Node, labels).Scan(&one); {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read exclusion of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	return true, nil
}

// Configure stores everything the form says about one series in one transaction: what it
// is judged by — nil clears it — and whether it is excluded from anomalies. A form states
// the whole configuration, so neither half is ever stored without the other
// (docs/specs/thresholds.md#saving).
func (s *SQLite) Configure(ctx context.Context, ref SeriesRef, th *Threshold, exclude bool) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a save of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if th == nil {
		err = deleteThreshold(ctx, tx, ref)
	} else {
		err = saveThreshold(ctx, tx, *th)
	}
	if err != nil {
		return err
	}
	if err = saveExclusion(ctx, tx, ref, exclude); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit a save of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	return nil
}

func saveExclusion(ctx context.Context, to execer, ref SeriesRef, exclude bool) error {
	labels, err := encodeLabels(ref.Labels)
	if err != nil {
		return fmt.Errorf("exclusion of %s: %w", ref.Metric, err)
	}
	statement := `DELETE FROM anomaly_exclusions WHERE metric = ? AND node = ? AND labels = ?`
	if exclude {
		statement = `INSERT INTO anomaly_exclusions (metric, node, labels) VALUES (?, ?, ?)
			ON CONFLICT DO NOTHING`
	}
	if _, err := to.ExecContext(ctx, statement, ref.Metric, ref.Node, labels); err != nil {
		return fmt.Errorf("save exclusion of %s on %s: %w", ref.Metric, ref.Node, err)
	}
	return nil
}

// readExclusions reads every excluded series, ordered as subjects are
// (docs/specs/state.md#ordering).
func readExclusions(ctx context.Context, from querier) ([]SeriesRef, error) {
	rows, err := from.QueryContext(ctx, `
		SELECT node, metric, labels FROM anomaly_exclusions ORDER BY node, metric, labels`)
	if err != nil {
		return nil, fmt.Errorf("read exclusions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeriesRef
	for rows.Next() {
		var node, metric, labels string
		if err := rows.Scan(&node, &metric, &labels); err != nil {
			return nil, fmt.Errorf("read exclusions: %w", err)
		}
		ref, err := seriesRef(node, metric, labels)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read exclusions: %w", err)
	}
	return out, nil
}
