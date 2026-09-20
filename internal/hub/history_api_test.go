package hub_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

var collected = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func at() time.Time { return collected }

var errFailed = errors.New("database is locked")

// seriesPoints is one stored series as a test writes it down. Sensor is what its newest
// point named, and what a node resolves an interval — and a staleness bound — against.
type seriesPoints struct {
	storage.SeriesRef
	Sensor string
	Points []storage.Point
}

// served is a storage answering with the series a test put in it.
type served struct {
	stored
	series []seriesPoints
}

func (h served) Series(_ context.Context, sel storage.Selection) ([]storage.SeriesNewest, error) {
	out := make([]storage.SeriesNewest, 0, len(h.series))
	for _, series := range h.series {
		if sel.Node != "" && series.Node != sel.Node {
			continue
		}
		if sel.Metric != "" && series.Metric != sel.Metric {
			continue
		}
		out = append(out, storage.SeriesNewest{SeriesRef: series.SeriesRef, Sensor: series.Sensor})
	}
	return out, h.err
}

func (h served) Newest(_ context.Context, sel storage.Selection, from time.Time) ([]storage.SeriesNewest, error) {
	var out []storage.SeriesNewest
	for _, series := range h.series {
		if sel.Node != "" && series.Node != sel.Node {
			continue
		}
		var newest time.Time
		for _, point := range series.Points {
			if !point.TS.Before(from) && point.TS.After(newest) {
				newest = point.TS
			}
		}
		if newest.IsZero() {
			continue
		}
		out = append(out, storage.SeriesNewest{SeriesRef: series.SeriesRef, Newest: newest, Sensor: series.Sensor})
	}
	return out, h.err
}

func (h served) Points(_ context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error] {
	return func(yield func(storage.Point, error) bool) {
		for _, series := range h.series {
			if series.Node != ref.Node || storage.LabelKey(series.Labels) != storage.LabelKey(ref.Labels) {
				continue
			}
			for _, point := range series.Points {
				if point.TS.Before(from) || point.TS.After(to) {
					continue
				}
				if !yield(point, nil) {
					return
				}
			}
		}
	}
}

func volume() seriesPoints {
	return seriesPoints{
		SeriesRef: storage.SeriesRef{
			Node:   "server-b",
			Metric: "disk.free_pct",
			Labels: map[string]string{"mount": "/", "fs": "ext4"},
		},
		Sensor: "disk",
		Points: []storage.Point{
			{TS: collected.Add(-time.Hour), Value: 34.2},
			{TS: collected.Add(-30 * time.Minute), Value: 34.1},
		},
	}
}

func get(t *testing.T, store hub.Store, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// spec: history.md — the wire format a consumer reads.
func TestHistoryAPIAnswersSeriesWithPoints(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	recorder := get(t, store, "/api/v1/history?metric=disk.free_pct&node=server-b&label.mount=%2F")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content type = %q, want JSON", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q, want no-store", got)
	}

	var body struct {
		Window struct{ From, To string }
		Series []struct {
			Node, Metric, Unit, Interval string
			Labels                       map[string]string
			Reduced                      bool
			Stored                       int
			Points                       []struct {
				TS    string `json:"ts"`
				Value float64
			}
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body, err)
	}
	if len(body.Series) != 1 {
		t.Fatalf("series = %d, want 1", len(body.Series))
	}
	series := body.Series[0]
	if series.Node != "server-b" || series.Metric != "disk.free_pct" || series.Unit != "percent" {
		t.Errorf("series = %+v, want the server-b percent series", series)
	}
	if series.Stored != 2 || series.Reduced {
		t.Errorf("series stored=%d reduced=%v, want two raw points", series.Stored, series.Reduced)
	}
	if len(series.Points) != 2 || series.Points[0].TS != "2026-08-31T11:00:00.000Z" {
		t.Errorf("points = %+v, want millisecond timestamps oldest first", series.Points)
	}
}

// spec: history.md#selection — /api/v1/series lists what exists, without points.
func TestSeriesAPIListsWhatExists(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	recorder := get(t, store, "/api/v1/series?metric=disk.free_pct")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"labels":{"fs":"ext4","mount":"/"}`) || strings.Contains(body, `"points"`) {
		t.Fatalf("body = %s, want the series identity and no points", body)
	}
}

// spec: history.md#refusals — a malformed query is refused with an English message.
func TestHistoryAPIRefusesAMalformedQuery(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	for _, target := range []string{
		"/api/v1/history",
		"/api/v1/history?metric=disk.free_pct&window=2w",
		"/api/v1/history?metric=disk.free_pct&at=2026-08-01T00:00:00Z",
		"/api/v1/series?metric=",
	} {
		recorder := get(t, store, target)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, recorder.Code)
		}
		var body struct{ Error string }
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error == "" {
			t.Errorf("GET %s answered %s, want an error message", target, recorder.Body)
		}
	}
}

// spec: history.md#refusals — a read that fails answers 500.
func TestHistoryAPIReportsAFailedRead(t *testing.T) {
	store := served{stored: stored{err: errFailed}}

	if recorder := get(t, store, "/api/v1/history?metric=disk.free_pct"); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

// spec: history.md — a method other than GET is not answered.
func TestHistoryAPIRefusesOtherMethods(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	recorder := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/history?metric=disk.free_pct", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

// spec: history.md#selection — a metric nothing reports is an empty answer, not an error.
func TestHistoryAPIAnswersAnEmptyListForAnUnknownMetric(t *testing.T) {
	recorder := get(t, served{}, "/api/v1/history?metric=nothing.reported")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := strings.TrimSpace(recorder.Body.String()); !strings.Contains(got, `"series":[]`) {
		t.Errorf("body = %s, want an empty series list", got)
	}
}

// spec: history.md#refusals — ?lang= belongs to the page; the API does not know it.
func TestHistoryAPIRefusesTheLanguageParameter(t *testing.T) {
	if recorder := get(t, served{}, "/api/v1/history?metric=disk.free_pct&lang=ru"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// spec: history.md#refusals — the listing reports a failed read the same way.
func TestSeriesAPIReportsAFailedRead(t *testing.T) {
	store := served{stored: stored{err: errFailed}}

	if recorder := get(t, store, "/api/v1/series?metric=disk.free_pct"); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

// spec: history.md — a series always says whether its points were reduced, false included.
func TestHistoryAPIAlwaysStatesWhetherASeriesWasReduced(t *testing.T) {
	recorder := get(t, served{series: []seriesPoints{volume()}}, "/api/v1/history?metric=disk.free_pct")

	var body struct {
		Series []struct {
			Reduced *bool `json:"reduced"`
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body, err)
	}
	if len(body.Series) != 1 || body.Series[0].Reduced == nil {
		t.Fatalf("body = %s, want every series to carry a reduced flag", recorder.Body)
	}
}

// spec: history.md — the window and the interval are on the wire, in the format the
// contract names.
func TestHistoryAPICarriesTheWindowAndTheInterval(t *testing.T) {
	recorder := get(t, served{series: []seriesPoints{volume()}}, "/api/v1/history?metric=disk.free_pct")

	var body struct {
		Window struct{ From, To string }
		Series []struct{ Interval string }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body, err)
	}
	if body.Window.From != "2026-08-30T12:00:00.000Z" || body.Window.To != "2026-08-31T12:00:00.000Z" {
		t.Errorf("window = %+v, want the 24 hours ending now, to the millisecond", body.Window)
	}
	if len(body.Series) != 1 || body.Series[0].Interval != "15m" {
		t.Errorf("series = %+v, want the interval the node resolves for the disk sensor", body.Series)
	}
}

// spec: history.md — a metric no rule declares carries no interval.
func TestHistoryAPILeavesAnUndeclaredMetricWithoutAnInterval(t *testing.T) {
	other := volume()
	other.Metric, other.Sensor = "coffee.level", ""
	recorder := get(t, served{series: []seriesPoints{other}}, "/api/v1/history?metric=coffee.level")

	var body struct {
		Series []struct{ Interval, Unit string }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body, err)
	}
	if len(body.Series) != 1 || body.Series[0].Interval != "" || body.Series[0].Unit != "number" {
		t.Errorf("series = %+v, want no interval and the unit its id declares", body.Series)
	}
}
