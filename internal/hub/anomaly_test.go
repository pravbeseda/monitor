package hub_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// spec: anomaly.md#wire-format — an anomaly with no rank, and one with no score.
func TestAnAnomalyWithoutARankOrAScore(t *testing.T) {
	score := -0.8
	current := stateOf(
		free("server-b", "/a", 12),
		free("server-b", "/b", 1),
	)
	current.Subjects[0].Anomaly = &anomaly.Anomaly{Norm: 14, Score: &score}
	current.Subjects[1].Anomaly = &anomaly.Anomaly{Norm: 0, Rank: 1}
	rec := httptest.NewRecorder()
	hub.StateAPI(func(context.Context) (state.State, error) { return current, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/state", nil))
	for _, want := range []string{
		`"anomaly":{"norm":14,"score":-0.8,"rank":null}`,
		`"anomaly":{"norm":0,"score":null,"rank":1}`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body does not carry %s: %s", want, rec.Body)
		}
	}
}

// loadAt is server-b reporting its 5-minute load average at value, against a week of load
// whose norm is 0.4.
func loadAt(value float64) stored {
	store := reporting()
	store.states[0].Values[2].Value = value
	return store
}

func anomalyOf(t *testing.T, handler http.Handler, metric string) *json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/state", nil))
	var body struct {
		Subjects []struct {
			Metric  string
			Anomaly *json.RawMessage
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, one := range body.Subjects {
		if one.Metric == metric {
			return one.Anomaly
		}
	}
	t.Fatalf("no %s in %s", metric, rec.Body)
	return nil
}

// spec: anomaly.md#reading — two answers in one hour carry the same anomalies, and a new
// value is scored in the next answer against that hour's norm.
func TestOneHourOneNorm(t *testing.T) {
	store := loadAt(3.1)
	clock := collected
	routes := routesWith(t, store, func() time.Time { return clock })

	first := anomalyOf(t, routes, "load.avg_5m")
	clock = clock.Add(time.Minute)
	if second := anomalyOf(t, routes, "load.avg_5m"); string(*first) != string(*second) {
		t.Fatalf("first %s, then %s within one hour", *first, *second)
	}

	store.states[0].Values[2].Value = 0.4
	if got := anomalyOf(t, routes, "load.avg_5m"); !strings.Contains(string(*got), `"norm":0.4,"score":0,"rank":null`) {
		t.Fatalf("a new value scored %s, want 0 against the same norm", *got)
	}
}

// spec: mission-control.md#invariants — the board shows an anomaly the State API ranks, read
// from stored points through the same state.
func TestTheBoardShowsWhatTheStateRanks(t *testing.T) {
	rec := getState(t, loadAt(3.1), "/", at)
	containsAll(t, "the anomaly item", oneOf(t, rec.Body.String(), "load.avg_5m"), "3.10", "usually 0.40")
}

func oneOf(t *testing.T, body, mark string) string {
	t.Helper()
	for _, one := range items(body) {
		if strings.Contains(one, mark) {
			return one
		}
	}
	t.Fatalf("no item carries %q: %v", mark, items(body))
	return ""
}

// spec: thresholds.md#saving — the switch turned off: from the next answer the series
// carries no anomaly, and mission control shows it as unusual no more; turned on again, it
// ranks again.
func TestExcludingASeriesTakesItOffTheBoard(t *testing.T) {
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	load := storage.SeriesRef{Node: "server-b", Metric: "load.avg_5m", Labels: map[string]string{}}
	var measurements []storage.Measurement
	for _, p := range usualLoad() {
		measurements = append(measurements, storage.Measurement{Metric: load.Metric, Sensor: "load", Labels: load.Labels, Value: p.Value, TS: p.TS})
	}
	measurements = append(measurements, storage.Measurement{Metric: load.Metric, Sensor: "load", Labels: load.Labels, Value: 3.1, TS: collected.Add(-5 * time.Minute)})
	if err := db.SaveIngest(context.Background(), storage.Ingest{Node: "server-b", ReceivedAt: collected, Measurements: measurements}); err != nil {
		t.Fatal(err)
	}
	routes := routesWith(t, db, at)
	address := "/thresholds?metric=load.avg_5m&node=server-b"

	for _, step := range []struct {
		anomalies string
		onBoard   bool
	}{{"exclude", false}, {"show", true}} {
		form := url.Values{"direction": {"above"}, "warning": {""}, "critical": {""}, "anomalies": {step.anomalies}}
		req := httptest.NewRequest(http.MethodPost, address, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://"+req.Host)
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("save %s = %d: %s", step.anomalies, rec.Code, rec.Body)
		}
		if got := anomalyOf(t, routes, "load.avg_5m"); (got != nil && string(*got) != "null") != step.onBoard {
			t.Errorf("after %s the anomaly is %v", step.anomalies, got)
		}
		board := httptest.NewRecorder()
		routes.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
		if shown := strings.Contains(board.Body.String(), "load.avg_5m"); shown != step.onBoard {
			t.Errorf("after %s the board shows the series: %v", step.anomalies, shown)
		}
	}
}
