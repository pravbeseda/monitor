package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func started(subject Subject, at time.Time) Anomaly {
	return Anomaly{Subject: subject, Began: at, Value: 148, Low: 1, High: 99}
}

func back(value float64) *float64 { return &value }

func (s *SQLite) openAnomalies(t *testing.T) []Anomaly {
	t.Helper()
	snap, err := s.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snap.Unusual
}

// spec: anomaly.md#record — a start is kept with the value and the band it was judged
// against, and the snapshot a tick reads holds it open.
func TestAnOpenedAnomalyIsInTheSnapshot(t *testing.T) {
	db := open(t)
	if err := db.OpenAnomaly(context.Background(), started(volume("/"), tickOne)); err != nil {
		t.Fatalf("OpenAnomaly: %v", err)
	}
	got := db.openAnomalies(t)
	if len(got) != 1 {
		t.Fatalf("open anomalies = %+v, want one", got)
	}
	one := got[0]
	if !one.Began.Equal(tickOne) || one.Value != 148 || one.Low != 1 || one.High != 99 ||
		!one.Ended.IsZero() || one.Back != nil || one.Labels["mount"] != "/" {
		t.Errorf("open anomaly = %+v", one)
	}
}

// spec: anomaly.md#record — two ticks at one instant over the same data record one start,
// and a series has at most one open record.
func TestAnAnomalyOpensOnce(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	for _, at := range []time.Time{tickOne, tickOne, tickTwo} {
		if err := db.OpenAnomaly(ctx, started(volume("/"), at)); err != nil {
			t.Fatalf("OpenAnomaly: %v", err)
		}
	}
	if got := db.openAnomalies(t); len(got) != 1 || !got[0].Began.Equal(tickOne) {
		t.Errorf("open anomalies = %+v, want the one opened first", got)
	}
}

// spec: anomaly.md#record — an end closes the record with the instant and the value it came
// back at; a withdrawal closes it with no value; a new start after either is a new record.
func TestClosingAnAnomaly(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	for _, record := range []Anomaly{started(volume("/"), tickOne), started(volume("/data"), tickOne)} {
		if err := db.OpenAnomaly(ctx, record); err != nil {
			t.Fatalf("OpenAnomaly: %v", err)
		}
	}
	if err := db.CloseAnomaly(ctx, volume("/"), tickTwo, back(128.4)); err != nil {
		t.Fatalf("CloseAnomaly: %v", err)
	}
	if err := db.CloseAnomaly(ctx, volume("/data"), tickTwo, nil); err != nil {
		t.Fatalf("CloseAnomaly: %v", err)
	}
	if got := db.openAnomalies(t); len(got) != 0 {
		t.Fatalf("open anomalies = %+v, want none", got)
	}

	later := tickTwo.Add(time.Minute)
	if err := db.OpenAnomaly(ctx, started(volume("/"), later)); err != nil {
		t.Fatalf("OpenAnomaly: %v", err)
	}
	records, err := db.AnomaliesSince(ctx, tickOne)
	if err != nil {
		t.Fatalf("AnomaliesSince: %v", err)
	}
	var got []string
	for _, record := range records {
		got = append(got, describeAnomaly(record))
	}
	want := []string{
		"/ 10:00 → 10:01 at 128.4",
		"/data 10:00 → 10:01 withdrawn",
		"/ 10:02 → open",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

func describeAnomaly(record Anomaly) string {
	head := fmt.Sprintf("%s %s", record.Labels["mount"], record.Began.Format("15:04"))
	switch {
	case record.Ended.IsZero():
		return head + " → open"
	case record.Back == nil:
		return head + " → " + record.Ended.Format("15:04") + " withdrawn"
	}
	return fmt.Sprintf("%s → %s at %v", head, record.Ended.Format("15:04"), *record.Back)
}

// spec: timeline.md#lanes — a lane reads every record still open or ended inside its
// window, and none that ended before it.
func TestAnomaliesSinceTheWindowBegan(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	early := tickOne.Add(-3 * time.Hour)
	for _, step := range []struct {
		mount string
		began time.Time
		ended time.Time
	}{
		{"/old", early, early.Add(time.Hour)},
		{"/edge", early, tickOne},
		{"/late", early, tickTwo},
		{"/open", early, time.Time{}},
	} {
		if err := db.OpenAnomaly(ctx, started(volume(step.mount), step.began)); err != nil {
			t.Fatalf("OpenAnomaly: %v", err)
		}
		if !step.ended.IsZero() {
			if err := db.CloseAnomaly(ctx, volume(step.mount), step.ended, back(50)); err != nil {
				t.Fatalf("CloseAnomaly: %v", err)
			}
		}
	}
	records, err := db.AnomaliesSince(ctx, tickOne)
	if err != nil {
		t.Fatalf("AnomaliesSince: %v", err)
	}
	var got []string
	for _, record := range records {
		got = append(got, record.Labels["mount"])
	}
	// An anomaly ending exactly where the window begins leaves nothing inside it.
	if want := []string{"/late", "/open"}; !reflect.DeepEqual(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

// spec: timeline.md#changes — the newest records of the named nodes, by their latest
// entry: an end where one is listed, the start otherwise.
func TestRecentAnomaliesOfTheNamedNodes(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	other := Subject{Node: "server-z", Metric: "disk", Labels: map[string]string{"mount": "/"}}
	at := func(minutes int) time.Time { return tickOne.Add(time.Duration(minutes) * time.Minute) }

	steps := []struct {
		subject     Subject
		began       int
		ended       int
		withdrawn   bool
		stillOpened bool
	}{
		{other, 50, 0, false, true},
		{volume("/a"), 0, 40, false, false}, // latest entry 40
		{volume("/b"), 30, 0, false, true},  // latest entry 30
		{volume("/c"), 35, 45, true, false}, // withdrawn: latest entry 35
		{volume("/d"), 10, 20, false, false},
	}
	for _, step := range steps {
		if err := db.OpenAnomaly(ctx, started(step.subject, at(step.began))); err != nil {
			t.Fatalf("OpenAnomaly: %v", err)
		}
		if step.stillOpened {
			continue
		}
		var value *float64
		if !step.withdrawn {
			value = back(50)
		}
		if err := db.CloseAnomaly(ctx, step.subject, at(step.ended), value); err != nil {
			t.Fatalf("CloseAnomaly: %v", err)
		}
	}

	records, err := db.RecentAnomalies(ctx, []string{"server-b"}, 3)
	if err != nil {
		t.Fatalf("RecentAnomalies: %v", err)
	}
	var got []string
	for _, record := range records {
		got = append(got, record.Labels["mount"])
	}
	if want := []string{"/a", "/c", "/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("recent anomalies = %q, want %q", got, want)
	}
	if none, err := db.RecentAnomalies(ctx, nil, 50); err != nil || len(none) != 0 {
		t.Errorf("RecentAnomalies of no node = %v, %v; want nothing", none, err)
	}
}

// spec: anomaly.md#rank — an open record survives a restart.
func TestAnOpenAnomalySurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	first, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := first.OpenAnomaly(context.Background(), started(volume("/"), tickOne)); err != nil {
		t.Fatalf("OpenAnomaly: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.Close() }()
	if got := second.openAnomalies(t); len(got) != 1 || !got[0].Began.Equal(tickOne) {
		t.Errorf("open anomalies after a reopen = %+v", got)
	}
}
