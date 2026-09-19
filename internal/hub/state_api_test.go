package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

func getState(t *testing.T, store hub.Store, target string, now func() time.Time) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	routesWith(t, store, now).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// spec: state.md#endpoint — a hub no node has reported to.
func TestStateOfAnEmptyHub(t *testing.T) {
	rec := getState(t, stored{}, "/api/v1/state", at)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	want := `{"at":"2026-08-31T12:00:00.000Z","level":null,"nodes":[],"subjects":[],"readings":[]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
}

// spec: state.md#endpoint — refused requests.
func TestStateRefusals(t *testing.T) {
	for _, target := range []string{
		"/api/v1/state?at=2026-09-01T00:00:00Z",
		"/api/v1/state?at=",
		"/api/v1/state?lang=ru",
		"/api/v1/state?%zz=1",
	} {
		rec := getState(t, stored{}, target, at)
		var body struct{ Error string }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusBadRequest || body.Error == "" {
			t.Errorf("%s answered %d %s, want 400 with an error", target, rec.Code, rec.Body)
		}
	}

	rec := httptest.NewRecorder()
	routesWith(t, stored{}, at).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/state", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST answered %d, want 405", rec.Code)
	}
}

// spec: state.md#endpoint — the stored state cannot be read.
func TestStateReadFailure(t *testing.T) {
	rec := getState(t, stored{err: errFailed}, "/api/v1/state", at)
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusInternalServerError || body.Error == "" {
		t.Fatalf("answered %d %s, want 500 with an error", rec.Code, rec.Body)
	}
	if strings.Contains(body.Error, errFailed.Error()) {
		t.Errorf("the error leaks the storage failure: %q", body.Error)
	}
}

// reporting is server-b with one warning volume and a metric no rule reads, as a tick left it.
func reporting() stored {
	labels := map[string]string{"mount": "/data", "fs": "ext4", "removable": "false"}
	return stored{
		states: []storage.NodeState{{
			Node:     "server-b",
			LastSeen: collected.Add(-time.Minute),
			Values: []storage.Value{
				{Metric: "disk.free_bytes", Labels: labels, Value: 12e9, TS: collected.Add(-15 * time.Minute)},
				{Metric: "disk.free_pct", Labels: labels, Value: 12, TS: collected.Add(-15 * time.Minute)},
				{Metric: "load.one", Value: 0.4, TS: collected.Add(-5 * time.Minute)},
			},
		}},
		levels: []storage.State{
			{Subject: storage.Subject{Node: "server-b", Rule: "disk", Labels: labels}, Level: "warning", Since: collected.Add(-14 * time.Hour)},
		},
	}
}

// spec: state.md#wire-format — every field as the example spells it, null where a value is
// absent.
func TestStateWireFormat(t *testing.T) {
	rec := getState(t, reporting(), "/api/v1/state", at)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	want := `{"at":"2026-08-31T12:00:00.000Z","level":"warning",` +
		`"nodes":[{"node":"server-b","configured":true,"agent_version":null,"last_seen":"2026-08-31T11:59:00.000Z","level":"warning"}],` +
		`"subjects":[` +
		`{"node":"server-b","rule":"silence","labels":{},"level":null,"since":null,"stale":false,"values":[]},` +
		`{"node":"server-b","rule":"disk","labels":{"fs":"ext4","mount":"/data","removable":"false"},` +
		`"level":"warning","since":"2026-08-30T22:00:00.000Z","stale":false,"values":[` +
		`{"metric":"disk.free_bytes","unit":"bytes","value":12000000000,"ts":"2026-08-31T11:45:00.000Z"},` +
		`{"metric":"disk.free_pct","unit":"percent","value":12,"ts":"2026-08-31T11:45:00.000Z"}]}],` +
		`"readings":[{"node":"server-b","metric":"load.one","labels":{},"unit":"number","value":0.4,"ts":"2026-08-31T11:55:00.000Z","stale":null}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
}

// spec: state.md#endpoint — two requests with nothing changed between them differ only by
// `at`.
func TestStateIsStable(t *testing.T) {
	later := func() time.Time { return collected.Add(time.Second) }
	first := getState(t, reporting(), "/api/v1/state", at).Body.String()
	second := getState(t, reporting(), "/api/v1/state", later).Body.String()
	strip := func(body string) string {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		delete(decoded, "at")
		encoded, _ := json.Marshal(decoded)
		return string(encoded)
	}
	if strip(first) != strip(second) {
		t.Fatalf("bodies differ beyond at:\n%s\n%s", first, second)
	}
}
