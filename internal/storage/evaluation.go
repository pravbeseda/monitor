package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// lastDigestKey is the one row the meta table holds today: the boundary of the digest
// window. It is opened at the instant a hub first evaluates a database and moved forward
// by every digest that goes out, so a reader is never told twice about one transition.
const lastDigestKey = "last_digest_at"

// Subject is what has a level: one series — a node, a metric and the labels that pick
// one of its instances (ADR 0033). A node's own silence is the subject whose metric is
// `silence` and whose labels are empty.
type Subject struct {
	Node   string
	Metric string
	Labels map[string]string
}

// Key identifies a subject in a map. Labels make Subject uncomparable, and the encoded
// form is the same one the database keys on, so the two cannot drift apart.
func (s Subject) Key() (string, error) {
	labels, err := encodeLabels(s.Labels)
	if err != nil {
		return "", fmt.Errorf("subject %s %s: %w", s.Node, s.Metric, err)
	}
	return s.Node + "\x00" + s.Metric + "\x00" + labels, nil
}

// describe names a subject in an error: a node has many volumes, and "state of server-b"
// does not say which one.
func (s Subject) describe() string {
	labels, err := encodeLabels(s.Labels)
	if err != nil {
		labels = "{}"
	}
	return fmt.Sprintf("%s %s %s", s.Node, s.Metric, labels)
}

// State is what a subject looks like between ticks.
type State struct {
	Subject
	Level string
	// Direction is the direction the level was judged under. Hysteresis holds a level
	// against the comparison that created it, so a level earned under another direction
	// is dropped rather than held (docs/specs/evaluation.md#hysteresis).
	Direction string
	Since     time.Time
	// LastNotifiedAt is zero until a message about this subject has been delivered.
	LastNotifiedAt time.Time
}

// Transition is one change of level, kept as it happened: the levels it went between and
// the values that produced it.
type Transition struct {
	Subject
	At   time.Time
	From string
	To   string
	// FromSince is when the subject entered the level it is leaving, which is what a
	// message reports as how long it had been there.
	FromSince time.Time
	// Direction is what the subject was judged under when it changed.
	Direction string
	// Readings are the values that produced the change, keyed by metric id. A subject is
	// one series, so this holds one entry; the shape outlives that.
	Readings map[string]float64
	// ID orders the log. It is set on read and ignored on write.
	ID int64
}

// Snapshot is everything one tick reads, taken as one view of the database: a measurement
// that arrives while a tick is running belongs to the next tick, never to half of this one.
type Snapshot struct {
	// Nodes carries each node's last-seen and the newest value of each of its series.
	Nodes []NodeState
	// States is the level every subject held when the tick began.
	States []State
	// Thresholds is what every watched subject is judged by, read in the same view: an
	// edit saved while a tick runs belongs to the next tick (ADR 0032).
	Thresholds []Threshold
	// Excluded is every series the reader excluded from anomalies (ADR 0036), ordered as
	// subjects are.
	Excluded []SeriesRef
	// Newest is the latest transition of every subject that entered or left one of the
	// levels the caller named as owed. A later transition of a quieter kind must not hide
	// it: a send that failed is owed whatever the subject has become since.
	Newest []Transition
}

// Snapshot reads all three in one read-only transaction, which takes no write lock, so a
// tick never serialises ingest against itself. `owed` names the levels whose transitions a
// channel could still be told about on their own; which those are is the caller's rule, so
// storage filters by it rather than knowing it.
func (s *SQLite) Snapshot(ctx context.Context, owed []string) (Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var out Snapshot
	if out.Nodes, err = states(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	if out.States, err = loadStates(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	if out.Newest, err = newestEvents(ctx, tx, owed); err != nil {
		return Snapshot{}, err
	}
	if out.Thresholds, err = readThresholds(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	if out.Excluded, err = readExclusions(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

func loadStates(ctx context.Context, from querier) ([]State, error) {
	rows, err := from.QueryContext(ctx, `
		SELECT node, metric, labels, level, direction, since, last_notified_at
		FROM states ORDER BY node, metric, labels`)
	if err != nil {
		return nil, fmt.Errorf("read states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []State
	for rows.Next() {
		var state State
		var labels, since, notified string
		if err := rows.Scan(&state.Node, &state.Metric, &labels, &state.Level, &state.Direction, &since, &notified); err != nil {
			return nil, fmt.Errorf("read states: %w", err)
		}
		if err := json.Unmarshal([]byte(labels), &state.Labels); err != nil {
			return nil, fmt.Errorf("decode labels of %s: %w", state.Node, err)
		}
		if state.Since, err = parseTime(since); err != nil {
			return nil, fmt.Errorf("state of %s: %w", state.Node, err)
		}
		if state.LastNotifiedAt, err = parseOptionalTime(notified); err != nil {
			return nil, fmt.Errorf("state of %s: %w", state.Node, err)
		}
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read states: %w", err)
	}
	return out, nil
}

// SaveState sets a subject's level without an event: the subject was seen for the first
// time, or the level stored for it is one the caller cannot read. Which levels are readable
// is the caller's rule, so a stored row is overwritten rather than second-guessed here.
func (s *SQLite) SaveState(ctx context.Context, state State) error {
	labels, err := encodeLabels(state.Labels)
	if err != nil {
		return fmt.Errorf("state of %s: %w", state.describe(), err)
	}
	if _, err := s.db.ExecContext(ctx, upsertState,
		state.Node, state.Metric, labels, state.Level, state.Direction, formatTime(state.Since)); err != nil {
		return fmt.Errorf("save state of %s: %w", state.describe(), err)
	}
	return nil
}

// upsertState keeps last_notified_at as it was: what a subject was told about is not part
// of what it is.
const upsertState = `
	INSERT INTO states (node, metric, labels, level, direction, since, last_notified_at)
	VALUES (?, ?, ?, ?, ?, ?, '')
	ON CONFLICT(node, metric, labels) DO UPDATE SET
		level     = excluded.level,
		direction = excluded.direction,
		since     = excluded.since`

// ApplyTransition writes the new level and its event in one transaction, so a reader never
// sees one without the other.
func (s *SQLite) ApplyTransition(ctx context.Context, change Transition) (err error) {
	labels, err := encodeLabels(change.Labels)
	if err != nil {
		return fmt.Errorf("transition of %s: %w", change.describe(), err)
	}
	readings, err := json.Marshal(change.Readings)
	if err != nil {
		return fmt.Errorf("transition of %s: encode readings: %w", change.describe(), err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, upsertState,
		change.Node, change.Metric, labels, change.To, change.Direction, formatTime(change.At)); err != nil {
		return fmt.Errorf("save state of %s: %w", change.describe(), err)
	}
	// One subject changes level at most once per tick, so a second event at the same
	// instant is a retry of the same one and is dropped rather than duplicated.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO events (at, node, metric, labels, from_level, to_level, from_since, readings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		formatTime(change.At), change.Node, change.Metric, labels,
		change.From, change.To, formatTime(change.FromSince), string(readings)); err != nil {
		return fmt.Errorf("record transition of %s: %w", change.describe(), err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transition of %s: %w", change.describe(), err)
	}
	return nil
}

// DeleteState forgets a subject: its threshold was cleared, so nothing judges it any
// more and the level it held is not an answer to any question (ADR 0032). Its events stay
// where they are: the log records what happened, and that did happen.
func (s *SQLite) DeleteState(ctx context.Context, subject Subject) error {
	labels, err := encodeLabels(subject.Labels)
	if err != nil {
		return fmt.Errorf("state of %s: %w", subject.describe(), err)
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM states WHERE node = ? AND metric = ? AND labels = ?`,
		subject.Node, subject.Metric, labels); err != nil {
		return fmt.Errorf("forget state of %s: %w", subject.describe(), err)
	}
	return nil
}

// RecordNotified marks what a subject was last told about. It is written after the message
// is delivered, so a hub that dies mid-send delivers again rather than staying silent.
func (s *SQLite) RecordNotified(ctx context.Context, subject Subject, at time.Time) error {
	labels, err := encodeLabels(subject.Labels)
	if err != nil {
		return fmt.Errorf("delivery to %s: %w", subject.describe(), err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE states SET last_notified_at = ?
		WHERE node = ? AND metric = ? AND labels = ?`,
		formatTime(at), subject.Node, subject.Metric, labels)
	if err != nil {
		return fmt.Errorf("record delivery to %s: %w", subject.describe(), err)
	}
	// A delivery recorded against nothing would be no record at all, and the message would
	// go out again on every tick that follows.
	marked, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record delivery to %s: %w", subject.describe(), err)
	}
	if marked == 0 {
		return fmt.Errorf("record delivery to %s: the subject has no state", subject.describe())
	}
	return nil
}

// newestEvents keeps one row per subject: the newest transition touching a level the
// caller named. Naming none is not "every level" but "none", because a caller that owes
// nothing is asking for nothing.
func newestEvents(ctx context.Context, from querier, owed []string) ([]Transition, error) {
	if len(owed) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(owed)), ", ")
	args := make([]any, 0, 2*len(owed))
	for range 2 {
		for _, level := range owed {
			args = append(args, level)
		}
	}
	return readEvents(ctx, from, `
		SELECT id, at, node, metric, labels, from_level, to_level, from_since, readings FROM (
			SELECT id, at, node, metric, labels, from_level, to_level, from_since, readings,
			       -- One subject changes level at most once per instant, which the events
			       -- table enforces; id breaks a tie that therefore cannot arise.
			       ROW_NUMBER() OVER (PARTITION BY node, metric, labels ORDER BY at DESC, id DESC) AS recency
			FROM events
			WHERE from_level IN (`+placeholders+`) OR to_level IN (`+placeholders+`))
		WHERE recency = 1
		ORDER BY node, metric, labels`, args...)
}

// EventsBetween returns the transitions recorded after from and up to and including to,
// which is the window a digest covers.
func (s *SQLite) EventsBetween(ctx context.Context, from, to time.Time) ([]Transition, error) {
	return readEvents(ctx, s.db, `
		SELECT id, at, node, metric, labels, from_level, to_level, from_since, readings
		FROM events WHERE at > ? AND at <= ? ORDER BY at, id`,
		formatTime(from), formatTime(to))
}

// RecentEvents returns the newest transitions of the named nodes, at most limit of them,
// newest first; inside one instant, the one recorded last comes first.
func (s *SQLite) RecentEvents(ctx context.Context, nodes []string, limit int) ([]Transition, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(nodes)+1)
	for _, node := range nodes {
		args = append(args, node)
	}
	args = append(args, limit)
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(nodes)), ", ")
	return readEvents(ctx, s.db, `
		SELECT id, at, node, metric, labels, from_level, to_level, from_since, readings
		FROM events WHERE node IN (`+placeholders+`) ORDER BY at DESC, id DESC LIMIT ?`, args...)
}

func readEvents(ctx context.Context, from querier, query string, args ...any) ([]Transition, error) {
	rows, err := from.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Transition
	for rows.Next() {
		var event Transition
		var at, labels, since, readings string
		if err := rows.Scan(&event.ID, &at, &event.Node, &event.Metric, &labels,
			&event.From, &event.To, &since, &readings); err != nil {
			return nil, fmt.Errorf("read events: %w", err)
		}
		if event.At, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("event of %s: %w", event.Node, err)
		}
		if event.FromSince, err = parseOptionalTime(since); err != nil {
			return nil, fmt.Errorf("event of %s: %w", event.Node, err)
		}
		if err := json.Unmarshal([]byte(labels), &event.Labels); err != nil {
			return nil, fmt.Errorf("decode labels of %s: %w", event.Node, err)
		}
		if err := json.Unmarshal([]byte(readings), &event.Readings); err != nil {
			return nil, fmt.Errorf("decode readings of %s: %w", event.Node, err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return out, nil
}

// LastDigestAt is where the current digest window begins, and whether a window has been
// opened at all. A database nothing has evaluated yet has none, and its first pass is what
// opens one.
func (s *SQLite) LastDigestAt(ctx context.Context) (time.Time, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, lastDigestKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read the digest mark: %w", err)
	}
	at, err := parseTime(value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("digest mark: %w", err)
	}
	return at, true, nil
}

// SetLastDigestAt moves the digest window's boundary. Its caller writes the instant a pass
// opened the window at, and afterwards the instant each digest read up to: anything stamped
// earlier than what was already reported would fall inside the next window as well and be
// said twice (docs/specs/evaluation.md#digest).
func (s *SQLite) SetLastDigestAt(ctx context.Context, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		lastDigestKey, formatTime(at)); err != nil {
		return fmt.Errorf("record the digest mark: %w", err)
	}
	return nil
}

// parseOptionalTime reads a timestamp that may never have been written: a subject nobody
// has been told about yet.
func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTime(value)
}
