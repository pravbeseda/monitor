package evaluate_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The rows of docs/specs/anomaly.md#record are read at 10:20 on the 23rd, so the norm
// period runs from 10:00 on the 15th to 10:00 on the 22nd.
var (
	judgedAt = time.Date(2026, time.September, 23, 10, 20, 0, 0, time.UTC)
	loadLine = storage.Subject{Node: "server-b", Metric: "load.avg_5m", Labels: map[string]string{}}
	loadNode = evaluate.Target{Node: "server-b", SilenceAfter: time.Hour, Intervals: map[string]time.Duration{"load": 5 * time.Minute, "disk": 15 * time.Minute}}
)

// noPoints is a store that holds no history, so nothing has a norm.
type noPoints struct{}

func (noPoints) Points(context.Context, storage.SeriesRef, time.Time, time.Time) iter.Seq2[storage.Point, error] {
	return func(func(storage.Point, error) bool) {}
}

// normsOf reads norms from the store a test evaluates, when it keeps points.
func normsOf(store evaluate.Store) *anomaly.Norms {
	if points, ok := store.(anomaly.Points); ok {
		return anomaly.NewNorms(points)
	}
	return anomaly.NewNorms(noPoints{})
}

// report stores one load value of server-b, stamped and received at at.
func report(t *testing.T, db *storage.SQLite, at time.Time, value float64) {
	t.Helper()
	if err := db.SaveIngest(context.Background(), storage.Ingest{
		Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: at,
		Measurements: []storage.Measurement{{Metric: loadLine.Metric, Sensor: "load", Labels: loadLine.Labels, Value: value, TS: at}},
	}); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
}

// usualWeek fills the norm period with the values 1, 2, …, 100, so the band runs from 1 to
// 99 around a norm of 50, each side 49 wide.
func usualWeek(t *testing.T, db *storage.SQLite) {
	t.Helper()
	from, _ := anomaly.Period(judgedAt)
	step := 7 * 24 * time.Hour / 100
	for i := range 100 {
		report(t, db, from.Add(time.Duration(i)*step), float64(i+1))
	}
}

func judge(t *testing.T, db *storage.SQLite, at time.Time, targets ...evaluate.Target) {
	t.Helper()
	pass(t, evaluator(db, at, targets...))
}

func anomalies(t *testing.T, db *storage.SQLite) []storage.Anomaly {
	t.Helper()
	records, err := db.AnomaliesSince(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("AnomaliesSince: %v", err)
	}
	return records
}

func opened(t *testing.T, db *storage.SQLite, at time.Time) {
	t.Helper()
	if err := db.OpenAnomaly(context.Background(), storage.Anomaly{Subject: loadLine, Began: at, Value: 148, Low: 1, High: 99}); err != nil {
		t.Fatalf("OpenAnomaly: %v", err)
	}
}

// spec: anomaly.md#record — a start is recorded with the tick's instant, the value and the
// band it was judged against; a value short of 2 records nothing.
func TestAnAnomalyStartsAtTwo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float64
		start bool
	}{
		{"148, score 2", 148, true},
		{"147, score 1.98", 147, false},
		{"−48, score −2", -48, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := open(t)
			usualWeek(t, db)
			report(t, db, judgedAt, tc.value)
			judge(t, db, judgedAt, loadNode)

			records := anomalies(t, db)
			if !tc.start {
				if len(records) != 0 {
					t.Fatalf("records = %+v, want none", records)
				}
				return
			}
			if len(records) != 1 {
				t.Fatalf("records = %+v, want one", records)
			}
			got := records[0]
			if !got.Began.Equal(judgedAt) || got.Value != tc.value || got.Low != 1 || got.High != 99 || !got.Ended.IsZero() {
				t.Errorf("record = %+v", got)
			}
		})
	}
}

// spec: anomaly.md#record — an open anomaly ends when its score comes back to 1.6, with the
// instant and the value, and holds above it, on either side.
func TestAnAnomalyEndsAt1Point6(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float64
		ends  bool
	}{
		{"140, score 1.84", 140, false},
		{"129, score 1.61", 129, false},
		{"128.4, score 1.6", 128.4, true},
		{"−28.4, score −1.6", -28.4, true},
		{"−48, the other side", -48, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := open(t)
			usualWeek(t, db)
			opened(t, db, judgedAt.Add(-time.Hour))
			report(t, db, judgedAt, tc.value)
			judge(t, db, judgedAt, loadNode)

			records := anomalies(t, db)
			if len(records) != 1 {
				t.Fatalf("records = %+v, want the one opened", records)
			}
			got := records[0]
			if !tc.ends {
				if !got.Ended.IsZero() {
					t.Fatalf("ended at %v, want still open", got.Ended)
				}
				return
			}
			if !got.Ended.Equal(judgedAt) || got.Back == nil || *got.Back != tc.value {
				t.Errorf("record = %+v, want ended at %v with %v", got, judgedAt, tc.value)
			}
		})
	}
}

// spec: anomaly.md#record — an anomaly that ended leaves the next start a new record.
func TestASecondStartIsANewRecord(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	opened(t, db, judgedAt.Add(-time.Hour))
	report(t, db, judgedAt, 50)
	judge(t, db, judgedAt, loadNode)
	report(t, db, judgedAt.Add(20*time.Minute), 148)
	judge(t, db, judgedAt.Add(20*time.Minute), loadNode)

	records := anomalies(t, db)
	if len(records) != 2 || records[0].Ended.IsZero() || !records[1].Began.Equal(judgedAt.Add(20*time.Minute)) {
		t.Errorf("records = %+v, want the first ended and a second begun at 10:40", records)
	}
}

// spec: anomaly.md#record — stale values judge nothing: an open anomaly is held while its
// series is stale and ends at the first tick that finds it fresh and back.
func TestAStaleSeriesHoldsItsAnomaly(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	opened(t, db, judgedAt.Add(-time.Hour))
	report(t, db, judgedAt.Add(-30*time.Minute), 50)
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Ended.IsZero() {
		t.Fatalf("records = %+v, want still open while stale", got)
	}

	fresh := judgedAt.Add(time.Hour)
	report(t, db, fresh, 50)
	judge(t, db, fresh, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Ended.Equal(fresh) || got[0].Back == nil {
		t.Errorf("records = %+v, want ended at %v", got, fresh)
	}
}

// spec: anomaly.md#record — a stale series starts nothing, and a silent node's series are
// stale.
func TestAStaleSeriesStartsNothing(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt.Add(-30*time.Minute), 1000)
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 0 {
		t.Errorf("records = %+v, want none", got)
	}

	// Silent: its last report an hour and a half ago, past its hour of silence_after.
	db = open(t)
	usualWeek(t, db)
	report(t, db, judgedAt.Add(-90*time.Minute), 1000)
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 0 {
		t.Errorf("records of a silent node = %+v, want none", got)
	}
}

// spec: anomaly.md#record — an exclusion withdraws an open anomaly, stale or not, and a
// fresh series without a norm has its anomaly withdrawn too.
func TestAnAnomalyIsWithdrawn(t *testing.T) {
	exclude := func(t *testing.T, db *storage.SQLite) {
		t.Helper()
		if err := db.Configure(context.Background(), storage.SeriesRef(loadLine), nil, true); err != nil {
			t.Fatalf("Configure: %v", err)
		}
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, db *storage.SQLite)
	}{
		{"excluded", func(t *testing.T, db *storage.SQLite) {
			usualWeek(t, db)
			report(t, db, judgedAt, 148)
			exclude(t, db)
		}},
		{"excluded while stale", func(t *testing.T, db *storage.SQLite) {
			usualWeek(t, db)
			report(t, db, judgedAt.Add(-time.Hour), 148)
			exclude(t, db)
		}},
		{"fresh without a norm", func(t *testing.T, db *storage.SQLite) {
			report(t, db, judgedAt, 148)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := open(t)
			tc.setup(t, db)
			opened(t, db, judgedAt.Add(-time.Hour))
			channel := &recorder{}
			pass(t, evaluatorWith(db, channel, judgedAt, loadNode))
			got := anomalies(t, db)
			if len(got) != 1 || !got[0].Ended.Equal(judgedAt) || got[0].Back != nil {
				t.Errorf("records = %+v, want withdrawn at %v", got, judgedAt)
			}
			if events := logged(t, db); len(events) != 0 || len(channel.messages) != 0 {
				t.Errorf("a withdrawal wrote %+v and sent %+v", events, channel.messages)
			}
		})
	}
}

// spec: anomaly.md#record — an excluded series starts nothing.
func TestAnExcludedSeriesStartsNothing(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	if err := db.Configure(context.Background(), storage.SeriesRef(loadLine), nil, true); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 0 {
		t.Errorf("records = %+v, want none", got)
	}
}

// spec: anomaly.md#record — a node the configuration no longer names is not judged, and
// its record is left as it is until it is named again.
func TestAnUnnamedNodeKeepsItsAnomaly(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	opened(t, db, judgedAt.Add(-time.Hour))
	report(t, db, judgedAt, 50)
	judge(t, db, judgedAt)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Ended.IsZero() {
		t.Fatalf("records = %+v, want still open", got)
	}
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Ended.Equal(judgedAt) {
		t.Errorf("records = %+v, want ended once the node is named again", got)
	}
}

// spec: anomaly.md#record — two ticks at one instant over the same data record one start.
func TestTwoTicksAtOneInstantStartOnce(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	judge(t, db, judgedAt, loadNode)
	judge(t, db, judgedAt, loadNode)
	if got := anomalies(t, db); len(got) != 1 {
		t.Errorf("records = %+v, want one", got)
	}
}

// failingNorms is a store of norms that cannot be read.
type failingNorms struct{}

func (failingNorms) For(context.Context, time.Time, []storage.SeriesRef) (map[string]anomaly.Norm, error) {
	return nil, errors.New("the disk is gone")
}

// spec: anomaly.md#record — a tick that cannot read the norms records no anomaly, and has
// judged its levels as ever.
func TestUnreadableNormsCostOnlyTheAnomalies(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	volume := map[string]string{"mount": "/"}
	collect(t, db, judgedAt, volume, gb(3))
	e := evaluate.New(evaluate.Options{
		Store: db, Notifier: &recorder{}, Targets: []evaluate.Target{loadNode}, Norms: failingNorms{},
		Digest: evaluate.Schedule{Hour: 9, Location: time.UTC}, Started: judgedAt, Now: func() time.Time { return judgedAt },
	})
	if err := e.Tick(context.Background()); err == nil {
		t.Error("Tick = nil, want the failure to read the norms")
	}
	if got := anomalies(t, db); len(got) != 0 {
		t.Errorf("records = %+v, want none", got)
	}
	if got := levelOf(t, db, "disk.free_bytes", "/"); got.Level != "critical" {
		t.Errorf("level = %q, want critical: the levels are judged as ever", got.Level)
	}
	judge(t, db, judgedAt.Add(time.Minute), loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Began.Equal(judgedAt.Add(time.Minute)) {
		t.Errorf("records = %+v, want a start at the first tick that reads the norms", got)
	}
}

// spec: anomaly.md#record — a level and an anomaly are independent, and an anomaly is
// never notified: neither a message nor the digest carries one.
func TestAnAnomalyIsNeverNotified(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	channel := &recorder{}
	// Started the day before, so the tick crosses 09:00 and a digest is due.
	e := evaluatorSince(db, channel, judgedAt.Add(-24*time.Hour), judgedAt, loadNode)
	pass(t, e)

	if got := anomalies(t, db); len(got) != 1 {
		t.Fatalf("records = %+v, want one start", got)
	}
	for _, m := range channel.messages {
		if m.Metric == loadLine.Metric {
			t.Errorf("a message about the anomaly: %+v", m)
		}
	}
	for _, digest := range channel.digests {
		for _, m := range digest {
			if m.Metric == loadLine.Metric {
				t.Errorf("a digest entry about the anomaly: %+v", m)
			}
		}
	}
}

// spec: anomaly.md#record — a new hour moves the norm with no new value, so a series can
// start or end on the hour's first tick; over the hour the week of 1 … 100 loses its 1, so
// its norm moves from 50 to 51.
func TestANewHourMovesTheNorm(t *testing.T) {
	hour := judgedAt.Truncate(time.Hour).Add(time.Hour)

	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, -47)
	judge(t, db, judgedAt, loadNode)
	report(t, db, hour.Add(-5*time.Minute), -47)
	judge(t, db, hour, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Began.Equal(hour) {
		t.Errorf("records = %+v, want a start at %v: −1.98, then −2", got, hour)
	}

	db = open(t)
	usualWeek(t, db)
	opened(t, db, judgedAt.Add(-time.Hour))
	report(t, db, judgedAt, 129)
	judge(t, db, judgedAt, loadNode)
	report(t, db, hour.Add(-5*time.Minute), 129)
	judge(t, db, hour, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Ended.Equal(hour) || got[0].Back == nil || *got[0].Back != 129 {
		t.Errorf("records = %+v, want an end at %v with 129: 1.61, then 1.59", got, hour)
	}
}

// refusing fails the first anomaly it is asked to open, once.
type refusing struct {
	evaluate.Store
	refused bool
}

func (r *refusing) OpenAnomaly(ctx context.Context, record storage.Anomaly) error {
	if !r.refused {
		r.refused = true
		return errors.New("the disk is full")
	}
	return r.Store.OpenAnomaly(ctx, record)
}

// spec: anomaly.md#record — a start that fails to be recorded stops the ones after it, not
// the levels, and the next tick records them all.
func TestAFailedStartStopsOnlyTheAnomaliesAfterIt(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	for _, metric := range []string{"load.avg_1m", "load.avg_5m"} {
		if err := db.SaveIngest(context.Background(), storage.Ingest{
			Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: judgedAt,
			Measurements: []storage.Measurement{{Metric: metric, Sensor: "load", Labels: map[string]string{}, Value: 148, TS: judgedAt}},
		}); err != nil {
			t.Fatalf("SaveIngest: %v", err)
		}
	}
	// load.avg_1m has no week of its own: give it the same one.
	from, _ := anomaly.Period(judgedAt)
	for i := range 100 {
		ts := from.Add(time.Duration(i) * 7 * 24 * time.Hour / 100)
		if err := db.SaveIngest(context.Background(), storage.Ingest{
			Node: "server-b", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: judgedAt,
			Measurements: []storage.Measurement{{Metric: "load.avg_1m", Sensor: "load", Labels: map[string]string{}, Value: float64(i + 1), TS: ts}},
		}); err != nil {
			t.Fatalf("SaveIngest: %v", err)
		}
	}
	collect(t, db, judgedAt, map[string]string{"mount": "/"}, gb(3))

	store := &refusing{Store: db}
	at := func(now time.Time) *evaluate.Evaluator {
		return evaluate.New(evaluate.Options{
			Store: store, Notifier: &recorder{}, Targets: []evaluate.Target{loadNode}, Norms: anomaly.NewNorms(db),
			Digest: evaluate.Schedule{Hour: 9, Location: time.UTC}, Started: now, Now: func() time.Time { return now },
		})
	}
	if err := at(judgedAt).Tick(context.Background()); err == nil {
		t.Error("Tick = nil, want the failed start")
	}
	if got := anomalies(t, db); len(got) != 0 {
		t.Errorf("records = %+v, want none after the failed one", got)
	}
	if got := levelOf(t, db, "disk.free_bytes", "/"); got.Level != "critical" {
		t.Errorf("level = %q, want critical: the levels are judged as ever", got.Level)
	}
	pass(t, at(judgedAt.Add(time.Minute)))
	if got := anomalies(t, db); len(got) != 2 {
		t.Errorf("records = %+v, want both at the next tick", got)
	}
}

// spec: anomaly.md#record — a series whose exclusion is removed is judged again, and starts
// a new record.
func TestAnIncludedSeriesStartsAgain(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	ref := storage.SeriesRef(loadLine)
	if err := db.Configure(context.Background(), ref, nil, true); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	judge(t, db, judgedAt, loadNode)
	if err := db.Configure(context.Background(), ref, nil, false); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	later := judgedAt.Add(11 * time.Minute)
	report(t, db, later, 148)
	judge(t, db, later, loadNode)
	if got := anomalies(t, db); len(got) != 1 || !got[0].Began.Equal(later) {
		t.Errorf("records = %+v, want one start at %v", got, later)
	}
}

// spec: anomaly.md#record — a watched series at critical starts an anomaly all the same.
func TestACriticalSeriesIsUnusualToo(t *testing.T) {
	db := open(t)
	usualWeek(t, db)
	report(t, db, judgedAt, 148)
	threshold := storage.Threshold{Series: storage.SeriesRef(loadLine), Direction: storage.Above, Critical: num(100)}
	if err := db.Configure(context.Background(), threshold.Series, &threshold, false); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	judge(t, db, judgedAt, loadNode)
	if got := levelOf(t, db, loadLine.Metric, ""); got.Level != "critical" {
		t.Errorf("level = %q, want critical", got.Level)
	}
	if got := anomalies(t, db); len(got) != 1 {
		t.Errorf("records = %+v, want a start beside the level", got)
	}
}

// spec: anomaly.md#record — a week of zeros: any move off it starts an anomaly, with the
// band 0 to 0, and only a return to zero ends it.
func TestAWeekOfZeros(t *testing.T) {
	db := open(t)
	from, _ := anomaly.Period(judgedAt)
	for i := range 100 {
		report(t, db, from.Add(time.Duration(i)*7*24*time.Hour/100), 0)
	}
	report(t, db, judgedAt, 1)
	judge(t, db, judgedAt, loadNode)
	got := anomalies(t, db)
	if len(got) != 1 || got[0].Low != 0 || got[0].High != 0 {
		t.Fatalf("records = %+v, want a start with the band 0 to 0", got)
	}

	next := judgedAt.Add(5 * time.Minute)
	report(t, db, next, 1)
	judge(t, db, next, loadNode)
	if got := anomalies(t, db); !got[0].Ended.IsZero() {
		t.Fatalf("ended at %v, want still open at 1", got[0].Ended)
	}
	last := next.Add(5 * time.Minute)
	report(t, db, last, 0)
	judge(t, db, last, loadNode)
	if got := anomalies(t, db); !got[0].Ended.Equal(last) {
		t.Errorf("records = %+v, want an end at %v", got, last)
	}
}
