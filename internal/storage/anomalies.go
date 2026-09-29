package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Anomaly is one stretch a series was unusual (ADR 0042): when it began, the value and the
// band it was judged against, and when and at what value it came back.
type Anomaly struct {
	Subject
	Began time.Time
	Value float64
	// Low and High are the band the value was judged against: values the series reported.
	Low, High float64
	// Ended is zero while the anomaly is open.
	Ended time.Time
	// Back is the value it came back at: nil while open, and for an anomaly withdrawn —
	// its series excluded or left without a norm — which no value ended.
	Back *float64
}

// OpenAnomaly records that a series became unusual. A series has at most one open anomaly
// and one per instant, so a retry of the same start is dropped rather than duplicated.
func (s *SQLite) OpenAnomaly(ctx context.Context, record Anomaly) error {
	labels, err := encodeLabels(record.Labels)
	if err != nil {
		return fmt.Errorf("anomaly of %s: %w", record.describe(), err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO anomalies (node, metric, labels, began, value, low, high)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		record.Node, record.Metric, labels, formatTime(record.Began), record.Value, record.Low, record.High); err != nil {
		return fmt.Errorf("record the anomaly of %s: %w", record.describe(), err)
	}
	return nil
}

// CloseAnomaly ends a series' open anomaly at at: with the value it came back at, or with
// none when it is withdrawn.
func (s *SQLite) CloseAnomaly(ctx context.Context, subject Subject, at time.Time, back *float64) error {
	labels, err := encodeLabels(subject.Labels)
	if err != nil {
		return fmt.Errorf("anomaly of %s: %w", subject.describe(), err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE anomalies SET ended = ?, back = ?
		WHERE node = ? AND metric = ? AND labels = ? AND ended = ''`,
		formatTime(at), back, subject.Node, subject.Metric, labels); err != nil {
		return fmt.Errorf("end the anomaly of %s: %w", subject.describe(), err)
	}
	return nil
}

// AnomaliesSince returns every anomaly open, or ended after from, oldest first: what a
// window that begins at from can show.
func (s *SQLite) AnomaliesSince(ctx context.Context, from time.Time) ([]Anomaly, error) {
	return readAnomalies(ctx, s.db, `WHERE ended = '' OR ended > ? ORDER BY began, node, metric, labels`,
		formatTime(from))
}

// RecentAnomalies returns the anomalies of the named nodes whose latest entry is newest —
// the end where a value ended them, the start otherwise — at most limit of them, newest
// first. Every one of the newest limit starts and ends belongs to one of them.
func (s *SQLite) RecentAnomalies(ctx context.Context, nodes []string, limit int) ([]Anomaly, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(nodes)+1)
	for _, node := range nodes {
		args = append(args, node)
	}
	args = append(args, limit)
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(nodes)), ", ")
	return readAnomalies(ctx, s.db, `WHERE node IN (`+placeholders+`)
		ORDER BY CASE WHEN back IS NULL THEN began ELSE ended END DESC, node DESC, metric DESC, labels DESC
		LIMIT ?`, args...)
}

func readAnomalies(ctx context.Context, from querier, where string, args ...any) ([]Anomaly, error) {
	rows, err := from.QueryContext(ctx, `
		SELECT node, metric, labels, began, value, low, high, ended, back
		FROM anomalies `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("read anomalies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Anomaly
	for rows.Next() {
		var record Anomaly
		var labels, began, ended string
		var back sql.NullFloat64
		if err := rows.Scan(&record.Node, &record.Metric, &labels, &began,
			&record.Value, &record.Low, &record.High, &ended, &back); err != nil {
			return nil, fmt.Errorf("read anomalies: %w", err)
		}
		if err := json.Unmarshal([]byte(labels), &record.Labels); err != nil {
			return nil, fmt.Errorf("decode labels of %s: %w", record.Node, err)
		}
		if record.Began, err = parseTime(began); err != nil {
			return nil, fmt.Errorf("anomaly of %s: %w", record.Node, err)
		}
		if record.Ended, err = parseOptionalTime(ended); err != nil {
			return nil, fmt.Errorf("anomaly of %s: %w", record.Node, err)
		}
		if back.Valid {
			record.Back = &back.Float64
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read anomalies: %w", err)
	}
	return out, nil
}
