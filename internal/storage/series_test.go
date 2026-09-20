package storage

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pct(mount string, at time.Time, value float64) Measurement {
	return Measurement{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": mount},
		Value:  value,
		TS:     at,
	}
}

func store(t *testing.T, db *SQLite, node string, ms ...Measurement) {
	t.Helper()
	if err := db.SaveIngest(context.Background(), ingest(node, collected, ms...)); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
}

func ref(node, mount string) SeriesRef {
	return SeriesRef{Node: node, Metric: "disk.free_pct", Labels: map[string]string{"mount": mount}}
}

func newest(t *testing.T, db *SQLite, sel Selection, from time.Time) []SeriesNewest {
	t.Helper()
	got, err := db.Newest(context.Background(), sel, from)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	return got
}

func collect(t *testing.T, points iter.Seq2[Point, error]) []Point {
	t.Helper()
	var out []Point
	for point, err := range points {
		if err != nil {
			t.Fatalf("Points: %v", err)
		}
		out = append(out, point)
	}
	return out
}

// spec: history.md#selection — series come ordered by node, then by labels.
func TestNewestOrdersSeriesByNodeThenLabels(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/data", collected, 50), pct("/", collected.Add(time.Minute), 41), pct("/", collected, 42))
	store(t, db, "laptop-a", pct("/", collected, 80))

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	if len(got) != 3 {
		t.Fatalf("series = %d, want 3", len(got))
	}
	if got[0].Node != "laptop-a" || got[1].Labels["mount"] != "/" || got[2].Labels["mount"] != "/data" {
		t.Fatalf("order = %+v, want laptop-a, then server-b / then server-b /data", got)
	}
}

// spec: history.md#window — a series reports the timestamp of its newest stored point,
// which is where the window may end.
func TestNewestReportsTheLastStoredPoint(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected, 42), pct("/", collected.Add(time.Minute), 41))

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	if len(got) != 1 || !got[0].Newest.Equal(collected.Add(time.Minute)) {
		t.Fatalf("newest = %+v, want the later of the two points", got)
	}
}

// spec: history.md#selection — node given: only that node's series.
func TestNewestSelectsOneNode(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected, 42))
	store(t, db, "laptop-a", pct("/", collected, 80))

	got := newest(t, db, Selection{Metric: "disk.free_pct", Node: "laptop-a"}, collected.Add(-time.Hour))
	if len(got) != 1 || got[0].Node != "laptop-a" {
		t.Fatalf("series = %+v, want laptop-a alone", got)
	}
}

// spec: history.md#selection — a series whose every stored point is outside the window is
// not returned.
func TestNewestSkipsSeriesOlderThanTheWindow(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected.Add(-2*time.Hour), 90), pct("/data", collected, 50))

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	if len(got) != 1 || got[0].Labels["mount"] != "/data" {
		t.Fatalf("series = %+v, want /data alone", got)
	}
}

// spec: history.md#selection — points of one series come oldest first, and no other
// series' points come with them.
func TestPointsStreamsOneSeriesOldestFirst(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected.Add(time.Minute), 41), pct("/", collected, 42), pct("/data", collected, 50))
	store(t, db, "laptop-a", pct("/", collected, 80))

	got := collect(t, db.Points(context.Background(), ref("server-b", "/"), collected.Add(-time.Hour), collected.Add(time.Hour)))
	if len(got) != 2 || got[0].Value != 42 || got[1].Value != 41 {
		t.Fatalf("points = %+v, want 42 then 41", got)
	}
}

// spec: history.md#window — both bounds are inclusive, and nothing outside them is read.
func TestPointsKeepsBothBoundsAndNothingBeyond(t *testing.T) {
	db := open(t)
	store(t, db, "server-b",
		pct("/", collected.Add(-time.Hour-time.Millisecond), 90),
		pct("/", collected.Add(-time.Hour), 80),
		pct("/", collected, 42),
		pct("/", collected.Add(time.Millisecond), 41),
	)

	got := collect(t, db.Points(context.Background(), ref("server-b", "/"), collected.Add(-time.Hour), collected))
	if len(got) != 2 || got[0].Value != 80 || got[1].Value != 42 {
		t.Fatalf("points = %+v, want the two inside the window", got)
	}
}

// spec: history.md#selection — a series the window reports can be read back by the
// reference it carries: labels travel from the stored encoding and into the next query.
func TestPointsReadsBackWhatNewestReported(t *testing.T) {
	db := open(t)
	labelled := Measurement{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/", "removable": "false"},
		Value:  50,
		TS:     collected,
	}
	bare := Measurement{Metric: "disk.free_pct", Value: 7, TS: collected}
	store(t, db, "server-b", labelled, bare)

	series := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	if len(series) != 2 {
		t.Fatalf("series = %+v, want the labelled one and the one without labels", series)
	}
	for _, one := range series {
		got := collect(t, db.Points(context.Background(), one.SeriesRef, collected.Add(-time.Hour), one.Newest))
		if len(got) != 1 {
			t.Fatalf("points of %+v = %+v, want the one it was stored with", one.Labels, got)
		}
	}
}

// spec: history.md#selection — /api/v1/series lists series whose last point is older than any window.
func TestSeriesListsWhatExistsWithoutPoints(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected.Add(-365*24*time.Hour), 90), pct("/data", collected, 50))
	store(t, db, "server-b", free("/", 123))

	got, err := db.Series(context.Background(), Selection{Metric: "disk.free_pct"})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("series = %+v, want both mounts of disk.free_pct", got)
	}
	if got[0].Labels["mount"] != "/" || got[1].Labels["mount"] != "/data" {
		t.Fatalf("order = %+v, want / before /data", got)
	}
}

// spec: history.md#selection — series are ordered by the labels rendered as sorted
// key=value pairs, which is not the order their stored encoding compares in.
func TestSeriesOrdersByRenderedLabelsNotTheirEncoding(t *testing.T) {
	db := open(t)
	both := Measurement{
		Metric: "disk.free_pct",
		Labels: map[string]string{"mount": "/", "removable": "false"},
		Value:  50,
		TS:     collected,
	}
	store(t, db, "server-b", pct("/", collected, 42), both)

	got, err := db.Series(context.Background(), Selection{Metric: "disk.free_pct"})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	// "mount=/" sorts before "mount=/,removable=false" as rendered, while their stored
	// JSON encodings compare the other way round: ',' beats '}' where they diverge.
	if len(got) != 2 || len(got[0].Labels) != 1 || len(got[1].Labels) != 2 {
		t.Fatalf("order = %+v, want the single-label series first", got)
	}
}

// explain reports how SQLite says it will run a statement.
func explain(t *testing.T, db *SQLite, query string, args ...any) string {
	t.Helper()
	rows, err := db.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("EXPLAIN: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	return strings.Join(plan, " / ")
}

// A read must cost what it answers, not what the history holds: every question about series
// is answered by the series table, and the points are touched only by the primary key. This
// is the query plan rather than a behaviour row — ADR 0031 says why the schema carries it.
func TestReadsOfSeriesNeverScanTheMeasurements(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected, 42))

	window, windowArgs := newestStatement(Selection{Metric: "disk.free_pct"}, collected.Add(-time.Hour))
	listing, listingArgs := seriesStatement(Selection{Metric: "disk.free_pct"})
	plans := map[string]string{
		"newest":   explain(t, db, window, windowArgs...),
		"listing":  explain(t, db, listing, listingArgs...),
		"snapshot": explain(t, db, latestValuesQuery),
	}
	for read, plan := range plans {
		if strings.Contains(plan, "SCAN measurements") {
			t.Errorf("the %s read scans the measurements: %q", read, plan)
		}
		if !strings.Contains(plan, "series") {
			t.Errorf("the %s read does not go through the series table: %q", read, plan)
		}
	}
}

// spec: history.md#window — a measurement that arrives late is stored, but the series still
// reports the newest timestamp it holds.
func TestALateMeasurementDoesNotMoveTheSeriesBack(t *testing.T) {
	db := open(t)
	store(t, db, "server-b", pct("/", collected, 42))
	store(t, db, "server-b", pct("/", collected.Add(-time.Hour), 90))

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-2*time.Hour))
	if len(got) != 1 || !got[0].Newest.Equal(collected) {
		t.Fatalf("newest = %+v, want the series still at %v", got, collected)
	}
}

// A database written before the series table keeps its measurements, and comes up with the
// table carrying what they imply: one row per series, at its newest timestamp, and no index
// left over the measurements.
func TestOpeningADatabaseWrittenBeforeTheSeriesTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	old, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	store(t, old, "server-b",
		pct("/", collected.Add(-time.Hour), 90),
		pct("/", collected, 42),
		pct("/data", collected, 50),
	)
	// The shape a database had when the history index was the answer: version 3.
	for _, statement := range []string{
		`DROP TABLE series`,
		`CREATE INDEX IF NOT EXISTS measurements_series ON measurements (metric, node, labels, ts)`,
		`PRAGMA user_version = 3`,
	} {
		if _, err := old.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	got := newest(t, db, Selection{Metric: "disk.free_pct"}, collected.Add(-2*time.Hour))
	if len(got) != 2 {
		t.Fatalf("series = %+v, want the two the measurements imply", got)
	}
	if got[0].Labels["mount"] != "/" || !got[0].Newest.Equal(collected) {
		t.Fatalf("series = %+v, want / at its newest point", got[0])
	}
	points := collect(t, db.Points(context.Background(), ref("server-b", "/"), collected.Add(-2*time.Hour), collected))
	if len(points) != 2 {
		t.Fatalf("points = %+v, want both measurements of the migrated database", points)
	}
	var indexes int
	if err := db.db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'measurements' AND sql IS NOT NULL`).Scan(&indexes); err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	if indexes != 0 {
		t.Fatalf("measurements carries %d index(es), want none: every read goes by its primary key", indexes)
	}
}

// Whatever version a database stopped at, opening it ends with exactly the series its
// measurements imply, and with no index over them. The stored rows are written by hand here,
// because the code that would write them is what the migrations are being tested for.
func TestEveryUpgradePathEndsWithTheSeriesItsMeasurementsImply(t *testing.T) {
	for stopped := range len(migrations) {
		t.Run(fmt.Sprintf("version-%d", stopped), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "monitor.db")
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			for step := range stopped {
				if _, err := raw.Exec(migrations[step]); err != nil {
					t.Fatalf("apply migration %d: %v", step+1, err)
				}
			}
			if _, err := raw.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, stopped)); err != nil {
				t.Fatalf("record the version: %v", err)
			}
			if stopped > 0 {
				for _, point := range []struct {
					labels string
					ts     time.Time
				}{
					{`{"mount":"/"}`, collected.Add(-time.Hour)},
					{`{"mount":"/"}`, collected},
					{`{}`, collected.Add(-time.Minute)},
				} {
					if _, err := raw.Exec(
						`INSERT INTO measurements (node, metric, labels, ts, value) VALUES (?, ?, ?, ?, ?)`,
						"server-b", "disk.free_pct", point.labels, formatTime(point.ts), 42.0); err != nil {
						t.Fatalf("seed a measurement: %v", err)
					}
				}
			}
			if err := raw.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			db, err := OpenSQLite(path)
			if err != nil {
				t.Fatalf("OpenSQLite: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			assertSeriesMatchTheMeasurements(t, db)
			var indexes int
			if err := db.db.QueryRow(
				`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'measurements' AND sql IS NOT NULL`).Scan(&indexes); err != nil {
				t.Fatalf("read the schema: %v", err)
			}
			if indexes != 0 {
				t.Errorf("measurements carries %d index(es), want none: every read goes by its primary key", indexes)
			}
		})
	}
}

// assertSeriesMatchTheMeasurements compares the two in both directions: neither a series the
// measurements do not imply, nor a measured series the table is missing.
func assertSeriesMatchTheMeasurements(t *testing.T, db *SQLite) {
	t.Helper()
	const implied = `SELECT metric, node, labels, MAX(ts) FROM measurements GROUP BY metric, node, labels`
	for _, difference := range []string{
		`SELECT metric, node, labels, last_ts FROM series EXCEPT ` + implied,
		implied + ` EXCEPT SELECT metric, node, labels, last_ts FROM series`,
	} {
		var rows int
		if err := db.db.QueryRow(`SELECT count(*) FROM (` + difference + `)`).Scan(&rows); err != nil {
			t.Fatalf("compare the series with the measurements: %v", err)
		}
		if rows != 0 {
			t.Errorf("%d row(s) of difference between the series and the measurements they describe", rows)
		}
	}
}

// 1: the conflict clause of the backfill — a table left behind what the measurements say is
// raised to them, so a repeated migration lands where a fresh one does.
func TestTheBackfillRaisesASeriesLeftBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	old, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	store(t, old, "server-b", pct("/", collected.Add(-time.Hour), 90), pct("/", collected, 42))
	for _, statement := range []string{
		`UPDATE series SET last_ts = ?`,
		fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)-1),
	} {
		if _, err := old.db.Exec(statement, formatTime(collected.Add(-time.Hour))); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	assertSeriesMatchTheMeasurements(t, db)
}
