package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the CGO-free driver of ADR 0005
)

// timeLayout is fixed-width, so stored timestamps sort lexicographically. It also sets
// the resolution of the uniqueness key: one millisecond (docs/specs/ingest.md).
const timeLayout = "2006-01-02T15:04:05.000Z"

// migrations are applied in order and recorded in PRAGMA user_version. What that buys is
// the first ALTER TABLE: CREATE TABLE IF NOT EXISTS cannot add a column to a database that
// already exists, and the measurements such a database holds are the only copy anyone has.
// An entry that has shipped is never edited — the edit would be a silent no-op on every
// database that already applied it — so a change to a released schema is a new entry.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS nodes (
		node           TEXT PRIMARY KEY,
		last_seen      TEXT NOT NULL,
		agent_version  TEXT NOT NULL,
		config_version TEXT NOT NULL,
		manifest       TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS measurements (
		node   TEXT NOT NULL,
		metric TEXT NOT NULL,
		labels TEXT NOT NULL,
		ts     TEXT NOT NULL,
		value  REAL NOT NULL,
		PRIMARY KEY (node, metric, labels, ts)
	) WITHOUT ROWID;`,

	`CREATE TABLE IF NOT EXISTS states (
		node             TEXT NOT NULL,
		rule             TEXT NOT NULL,
		labels           TEXT NOT NULL,
		level            TEXT NOT NULL,
		since            TEXT NOT NULL,
		last_notified_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (node, rule, labels)
	) WITHOUT ROWID;

	CREATE TABLE IF NOT EXISTS events (
		id         INTEGER PRIMARY KEY,
		at         TEXT NOT NULL,
		node       TEXT NOT NULL,
		rule       TEXT NOT NULL,
		labels     TEXT NOT NULL,
		from_level TEXT NOT NULL,
		to_level   TEXT NOT NULL,
		from_since TEXT NOT NULL,
		readings   TEXT NOT NULL,
		UNIQUE (node, rule, labels, at)
	);

	CREATE INDEX IF NOT EXISTS events_at ON events (at);
	CREATE INDEX IF NOT EXISTS events_subject ON events (node, rule, labels, at DESC);

	CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) WITHOUT ROWID;`,

	// History selects by metric across every node, which the primary key cannot serve:
	// its leading column is the node (docs/specs/history.md#selection).
	`CREATE INDEX IF NOT EXISTS measurements_series ON measurements (metric, node, labels, ts);`,

	// Every question the hub asks first is about series — which exist, when each last
	// reported, what each holds now — and `measurements_series` could only answer them by
	// ranking points, so the cost followed the stored history rather than the answer. An
	// index cannot fix that shape, so the series are a table of their own (ADR 0031),
	// backfilled from the measurements that imply them. No read scans `measurements` any
	// more, so it keeps its primary key alone.
	`DROP INDEX IF EXISTS measurements_series;

	CREATE TABLE IF NOT EXISTS series (
		metric  TEXT NOT NULL,
		node    TEXT NOT NULL,
		labels  TEXT NOT NULL,
		last_ts TEXT NOT NULL,
		PRIMARY KEY (metric, node, labels)
	) WITHOUT ROWID;

	-- Raising rather than ignoring on conflict makes the backfill repeatable: a database
	-- whose series table is already there, or behind what the measurements say, ends up
	-- with the same rows as one that is built from scratch.
	INSERT INTO series (metric, node, labels, last_ts)
	SELECT metric, node, labels, MAX(ts) FROM measurements GROUP BY metric, node, labels
	ON CONFLICT (metric, node, labels) DO UPDATE SET
		last_ts = MAX(last_ts, excluded.last_ts);`,

	// A threshold is what one series is judged by, and it is stored beside the
	// measurements rather than written in the file (ADR 0032). The sensor comes with the
	// measurements that wrote the series (ADR 0033): staleness is three times that
	// sensor's interval, and nothing else says which sensor a metric comes from. A series
	// stored before this migration has none, and is aged by the longest interval among the
	// sensors its node runs until its agent reports again (ADR 0034).
	//
	// The series table is rebuilt rather than altered: it is derived from the
	// measurements, so rebuilding costs nothing, and unlike ADD COLUMN it can be applied
	// twice — which a database whose schema version was rewound is entitled to. A replay
	// does cost the sensor names, since the measurements do not carry them; the series
	// that lose one are aged by a coarser bound than their own until their agent reports
	// again, which is one collection interval away (docs/specs/evaluation.md#freezing).
	`DROP TABLE IF EXISTS series;

	CREATE TABLE series (
		metric  TEXT NOT NULL,
		node    TEXT NOT NULL,
		labels  TEXT NOT NULL,
		last_ts TEXT NOT NULL,
		sensor  TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (metric, node, labels)
	) WITHOUT ROWID;

	INSERT INTO series (metric, node, labels, last_ts)
	SELECT metric, node, labels, MAX(ts) FROM measurements GROUP BY metric, node, labels;

	CREATE TABLE IF NOT EXISTS thresholds (
		metric    TEXT NOT NULL,
		node      TEXT NOT NULL,
		labels    TEXT NOT NULL,
		direction TEXT NOT NULL,
		warning   REAL,
		critical  REAL,
		PRIMARY KEY (metric, node, labels)
	) WITHOUT ROWID;`,

	// A subject is a series now, not a rule over two of them (ADR 0033), so what a level
	// belongs to changes shape. The stored levels and the event log named a rule and
	// cannot be translated into the new key, and nothing is judged until a threshold is
	// set anyway, so both tables are rebuilt empty rather than migrated. A level also
	// records the direction it was judged under, so that flipping the direction drops it
	// instead of holding it in the mirrored band.
	`DROP TABLE IF EXISTS states;
	DROP TABLE IF EXISTS events;

	CREATE TABLE states (
		node             TEXT NOT NULL,
		metric           TEXT NOT NULL,
		labels           TEXT NOT NULL,
		level            TEXT NOT NULL,
		direction        TEXT NOT NULL DEFAULT '',
		since            TEXT NOT NULL,
		last_notified_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (node, metric, labels)
	) WITHOUT ROWID;

	CREATE TABLE events (
		id         INTEGER PRIMARY KEY,
		at         TEXT NOT NULL,
		node       TEXT NOT NULL,
		metric     TEXT NOT NULL,
		labels     TEXT NOT NULL,
		from_level TEXT NOT NULL,
		to_level   TEXT NOT NULL,
		from_since TEXT NOT NULL,
		readings   TEXT NOT NULL,
		UNIQUE (node, metric, labels, at)
	);

	CREATE INDEX events_at ON events (at);
	CREATE INDEX events_subject ON events (node, metric, labels, at DESC);`,

	// A series the reader excluded from anomalies (ADR 0036). It is kept apart from its
	// threshold: a series may be excluded with none, and keeps the exclusion when its
	// threshold is cleared.
	`CREATE TABLE IF NOT EXISTS anomaly_exclusions (
		metric TEXT NOT NULL,
		node   TEXT NOT NULL,
		labels TEXT NOT NULL,
		PRIMARY KEY (metric, node, labels)
	) WITHOUT ROWID;`,
}

// querier is what a database handle and a transaction both offer, so one read runs either
// on its own or inside the snapshot a tick takes.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// execer is what a transaction offers for a write, so a save that states several things at
// once runs each of them inside it.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// SQLite is the Storage implementation the hub runs on.
type SQLite struct {
	db *sql.DB
}

var _ Storage = (*SQLite)(nil)

// OpenSQLite opens the database at path, creating it and its schema when absent.
func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &SQLite{db: db}, nil
}

// dsn builds the driver's URI. The path is escaped because SQLite reads one and decodes
// it: a '#' truncates the path into a fragment and a '%2F' becomes a slash, either way
// opening a different file without an error. Transactions are immediate, so one that reads
// before it writes waits out the busy timeout instead of failing at once — a deferred
// transaction is refused its upgrade without the busy handler ever running. A read-only
// transaction is exempt and takes no write lock, which is what lets a tick read a
// consistent snapshot without serialising ingest against itself.
func dsn(path string) string {
	// The replacer does not rescan its own output, so escaping '%' first is safe.
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	return "file:" + escaped + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
}

// migrate brings a database up to the schema this build expects, whatever it was written
// by, and records how far it got.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("the database is at schema version %d, which this build does not know (it knows %d): a hub is being started older than the one that wrote its database",
			version, len(migrations))
	}
	for step := version; step < len(migrations); step++ {
		if err := applyMigration(db, step); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one step and records it in the same transaction, so a crash between
// the two cannot leave a half-migrated database that refuses to start ever after. SQLite
// runs DDL transactionally, and the version is re-read inside the transaction because two
// hubs may open one file at once.
func applyMigration(db *sql.DB, step int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", step+1, err)
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}
	if version != step {
		return nil // another process applied it while this one waited for the lock.
	}
	if _, err := tx.Exec(migrations[step]); err != nil {
		return fmt.Errorf("apply migration %d: %w", step+1, err)
	}
	// PRAGMA takes no bound parameters; the value is this loop's counter.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", step+1)); err != nil {
		return fmt.Errorf("record migration %d: %w", step+1, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", step+1, err)
	}
	return nil
}

// SaveIngest stores one request in a single transaction; a failure stores nothing.
func (s *SQLite) SaveIngest(ctx context.Context, in Ingest) (err error) {
	manifest, err := json.Marshal(in.Manifest)
	if err != nil {
		return fmt.Errorf("encode manifest of %s: %w", in.Node, err)
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

	_, err = tx.ExecContext(ctx, `
		INSERT INTO nodes (node, last_seen, agent_version, config_version, manifest)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(node) DO UPDATE SET
			last_seen      = excluded.last_seen,
			agent_version  = excluded.agent_version,
			config_version = excluded.config_version,
			manifest       = excluded.manifest`,
		in.Node, formatTime(in.ReceivedAt), in.AgentVersion, in.ConfigVersion, string(manifest))
	if err != nil {
		return fmt.Errorf("save node %s: %w", in.Node, err)
	}

	for _, m := range in.Measurements {
		var labels string
		if labels, err = encodeLabels(m.Labels); err != nil {
			return fmt.Errorf("measurement %s of %s: %w", m.Metric, in.Node, err)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO measurements (node, metric, labels, ts, value)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			in.Node, m.Metric, labels, formatTime(m.TS), m.Value)
		if err != nil {
			return fmt.Errorf("save measurement %s of %s: %w", m.Metric, in.Node, err)
		}
		// The series carries the newest timestamp it holds, and only ever forwards: a
		// measurement arriving late is stored, but it is not what the series last
		// reported (ADR 0031).
		// The sensor follows the newest value, not the latest request: a measurement
		// arriving late does not rename the series (docs/specs/ingest.md#storage).
		_, err = tx.ExecContext(ctx, `
			INSERT INTO series (metric, node, labels, last_ts, sensor)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (metric, node, labels) DO UPDATE SET
				sensor  = CASE WHEN excluded.last_ts > last_ts THEN excluded.sensor ELSE sensor END,
				last_ts = MAX(last_ts, excluded.last_ts)`,
			m.Metric, in.Node, labels, formatTime(m.TS), m.Sensor)
		if err != nil {
			return fmt.Errorf("save series %s of %s: %w", m.Metric, in.Node, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit ingest of %s: %w", in.Node, err)
	}
	return nil
}

func states(ctx context.Context, from querier) ([]NodeState, error) {
	states, order, err := nodeStates(ctx, from)
	if err != nil {
		return nil, err
	}
	if err := attachLatestValues(ctx, from, states); err != nil {
		return nil, err
	}

	out := make([]NodeState, 0, len(order))
	for _, node := range order {
		out = append(out, *states[node])
	}
	return out, nil
}

func nodeStates(ctx context.Context, from querier) (map[string]*NodeState, []string, error) {
	rows, err := from.QueryContext(ctx, `SELECT node, last_seen, agent_version FROM nodes ORDER BY node`)
	if err != nil {
		return nil, nil, fmt.Errorf("read nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	states := map[string]*NodeState{}
	var order []string
	for rows.Next() {
		var node, lastSeen, agentVersion string
		if err := rows.Scan(&node, &lastSeen, &agentVersion); err != nil {
			return nil, nil, fmt.Errorf("read nodes: %w", err)
		}
		seen, err := parseTime(lastSeen)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", node, err)
		}
		states[node] = &NodeState{Node: node, LastSeen: seen, AgentVersion: agentVersion}
		order = append(order, node)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read nodes: %w", err)
	}
	return states, order, nil
}

const latestValuesQuery = `
	SELECT series.node, series.metric, series.labels, series.last_ts, series.sensor,
	       measurements.value
	FROM series
	-- CROSS fixes the order: the series are the small side and must drive the join, or
	-- SQLite is free to scan every measurement instead (ADR 0031).
	CROSS JOIN measurements
	  ON measurements.node = series.node
	 AND measurements.metric = series.metric
	 AND measurements.labels = series.labels
	 AND measurements.ts = series.last_ts
	ORDER BY series.node, series.metric, series.labels`

// attachLatestValues keeps one row per series: the point the series says is its newest. The
// series table names it, so this reads one point per series instead of ranking every point
// ever stored (ADR 0031).
func attachLatestValues(ctx context.Context, from querier, states map[string]*NodeState) error {
	rows, err := from.QueryContext(ctx, latestValuesQuery)
	if err != nil {
		return fmt.Errorf("read measurements: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var node, metric, labels, ts string
		var value Value
		if err := rows.Scan(&node, &metric, &labels, &ts, &value.Sensor, &value.Value); err != nil {
			return fmt.Errorf("read measurements: %w", err)
		}
		state, known := states[node]
		if !known {
			continue
		}
		value.Metric = metric
		if err := json.Unmarshal([]byte(labels), &value.Labels); err != nil {
			return fmt.Errorf("node %s: decode labels: %w", node, err)
		}
		if value.TS, err = parseTime(ts); err != nil {
			return fmt.Errorf("node %s: %w", node, err)
		}
		state.Values = append(state.Values, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read measurements: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}

// encodeLabels is the uniqueness key's label part: json.Marshal sorts map keys, so the
// same labels always encode to the same string.
func encodeLabels(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", fmt.Errorf("encode labels: %w", err)
	}
	return string(b), nil
}
