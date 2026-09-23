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
	want := `{"at":"2026-08-31T12:00:00.000Z","level":null,"watched":0,"unwatched":0,"nodes":[],"subjects":[]}`
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

// dataVolume is the volume both of whose series server-b reports below.
var dataVolume = map[string]string{"mount": "/data", "fs": "ext4", "removable": "false"}

// reporting is server-b with one watched volume series, one unwatched beside it and a
// series naming no sensor, as a tick left it.
func reporting() stored {
	warning, critical := 20e9, 10e9
	return stored{
		states: []storage.NodeState{{
			Node:     "server-b",
			LastSeen: collected.Add(-time.Minute),
			Values: []storage.Value{
				{Metric: "disk.free_bytes", Sensor: "disk", Labels: dataVolume, Value: 9e9, TS: collected.Add(-15 * time.Minute)},
				{Metric: "disk.free_pct", Sensor: "disk", Labels: dataVolume, Value: 12, TS: collected.Add(-15 * time.Minute)},
				{Metric: "load.avg_5m", Value: 3.1, TS: collected.Add(-5 * time.Minute)},
			},
		}},
		levels: []storage.State{
			{
				Subject:   storage.Subject{Node: "server-b", Metric: "disk.free_bytes", Labels: dataVolume},
				Level:     "warning",
				Direction: string(storage.Below),
				Since:     collected.Add(-14 * time.Hour),
			},
			{
				Subject: storage.Subject{Node: "server-b", Metric: "silence"},
				Level:   "ok",
				Since:   collected.Add(-30 * 24 * time.Hour),
			},
		},
		thresholds: []storage.Threshold{{
			Series:    storage.SeriesRef{Node: "server-b", Metric: "disk.free_bytes", Labels: dataVolume},
			Direction: storage.Below,
			Warning:   &warning,
			Critical:  &critical,
		}},
		points: map[string][]storage.Point{"load.avg_5m": usualLoad()},
	}
}

// usualLoad is a week of load whose norm is 0.4 and whose band reaches 0.76 above it, so
// the 3.1 reported above scores 7.5 (docs/specs/anomaly.md#wire-format).
func usualLoad() []storage.Point {
	values := append(make([]float64, 0, 100), 0.76, 0.9)
	for len(values) < 100 {
		values = append(values, 0.4)
	}
	first := collected.Add(-7 * 24 * time.Hour)
	out := make([]storage.Point, len(values))
	for i, v := range values {
		out[i] = storage.Point{TS: first.Add(time.Duration(i) * time.Hour), Value: v}
	}
	return out
}

// spec: state.md#wire-format — every field the example names, null where a value is absent,
// and one list holding every series beside each node's silence. An anomaly object with no
// rank or no score is TestAnAnomalyWithoutARankOrAScore's.
func TestStateWireFormat(t *testing.T) {
	rec := getState(t, reporting(), "/api/v1/state", at)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	want := `{"at":"2026-08-31T12:00:00.000Z","level":"warning","watched":1,"unwatched":2,` +
		`"nodes":[{"node":"server-b","configured":true,"agent_version":null,` +
		`"last_seen":"2026-08-31T11:59:00.000Z","level":"warning","watched":1,"unwatched":2}],` +
		`"subjects":[` +
		`{"node":"server-b","metric":"silence","labels":{},"watched":true,"level":"ok",` +
		`"since":"2026-08-01T12:00:00.000Z","stale":false,"unit":null,"value":null,"ts":null,"anomaly":null},` +
		`{"node":"server-b","metric":"disk.free_bytes","labels":{"fs":"ext4","mount":"/data","removable":"false"},` +
		`"watched":true,"level":"warning","since":"2026-08-30T22:00:00.000Z","stale":false,` +
		`"unit":"bytes","value":9000000000,"ts":"2026-08-31T11:45:00.000Z","anomaly":null},` +
		`{"node":"server-b","metric":"disk.free_pct","labels":{"fs":"ext4","mount":"/data","removable":"false"},` +
		`"watched":false,"level":null,"since":null,"stale":false,` +
		`"unit":"percent","value":12,"ts":"2026-08-31T11:45:00.000Z","anomaly":null},` +
		`{"node":"server-b","metric":"load.avg_5m","labels":{},"watched":false,"level":null,"since":null,` +
		`"stale":false,"unit":"number","value":3.1,"ts":"2026-08-31T11:55:00.000Z",` +
		`"anomaly":{"norm":0.4,"score":7.5,"rank":1}}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
}

// subjectJSON is one entry of the one list the response carries.
type subjectJSON struct {
	Node    string
	Metric  string
	Labels  map[string]string
	Watched bool
	Level   *string
	Since   *string
	Stale   *bool
	Unit    *string
	Value   *float64
	TS      *string `json:"ts"`
}

type stateBody struct {
	Watched   int
	Unwatched int
	Nodes     []struct {
		Node               string
		Watched, Unwatched int
		Configured         bool
	}
	Subjects []subjectJSON
}

func decodeState(t *testing.T, rec *httptest.ResponseRecorder) stateBody {
	t.Helper()
	var body stateBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return body
}

// spec: state.md#listing — one list holds every series of every node plus each node's
// silence, and the counts add up to it, per node and in total.
func TestStateMergesEverySeriesIntoOneList(t *testing.T) {
	body := decodeState(t, getState(t, reporting(), "/api/v1/state", at))

	var listed []string
	for _, one := range body.Subjects {
		listed = append(listed, one.Metric)
	}
	want := []string{"silence", "disk.free_bytes", "disk.free_pct", "load.avg_5m"}
	if strings.Join(listed, ",") != strings.Join(want, ",") {
		t.Fatalf("subjects = %v, want %v", listed, want)
	}
	if body.Watched != 1 || body.Unwatched != 2 {
		t.Errorf("watched %d unwatched %d, want the one threshold watched and the rest not",
			body.Watched, body.Unwatched)
	}
	if len(body.Nodes) != 1 || body.Nodes[0].Watched != 1 || body.Nodes[0].Unwatched != 2 {
		t.Errorf("nodes = %+v, want the same counts on the node", body.Nodes)
	}
	// The counts are of series; the node's silence is listed and counted in neither.
	if body.Watched+body.Unwatched+1 != len(body.Subjects) {
		t.Errorf("%d + %d counted, %d subjects listed", body.Watched, body.Unwatched, len(body.Subjects))
	}
}

// spec: state.md#wire-format — the silence subject carries no value of its own: its input
// is the node's last-seen time, which `nodes` already states.
func TestStateSilenceSubjectCarriesNoValue(t *testing.T) {
	body := decodeState(t, getState(t, reporting(), "/api/v1/state", at))

	silence := body.Subjects[0]
	if silence.Metric != "silence" || !silence.Watched {
		t.Fatalf("first subject = %+v, want the node's silence, watched", silence)
	}
	if silence.Unit != nil || silence.Value != nil || silence.TS != nil {
		t.Errorf("silence unit %v value %v ts %v, want all three null", silence.Unit, silence.Value, silence.TS)
	}
	if len(silence.Labels) != 0 {
		t.Errorf("silence labels = %v, want an empty map and never null", silence.Labels)
	}
}

// spec: state.md#listing — a configured node that has never reported appears nowhere: the
// configuration names laptop-a and server-b, and only server-b has reported.
func TestStateLeavesOutANodeThatNeverReported(t *testing.T) {
	body := decodeState(t, getState(t, reporting(), "/api/v1/state", at))

	for _, node := range body.Nodes {
		if node.Node != "server-b" {
			t.Errorf("node %s listed, but it never reported", node.Node)
		}
	}
	for _, subject := range body.Subjects {
		if subject.Node != "server-b" {
			t.Errorf("a subject of %s listed, but it never reported", subject.Node)
		}
	}
}

// spec: state.md#endpoint — two requests inside one hour with nothing changed between them
// differ only by `at`.
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

// spec: anomaly.md#reading — the stored points cannot be read for a norm: the rest of the
// state answered as ever, every anomaly null.
func TestStateWithoutNormsStillAnswers(t *testing.T) {
	store := reporting()
	store.pointsErr = errFailed
	rec := getState(t, store, "/api/v1/state", at)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Subjects []struct {
			Metric  string
			Level   *string
			Anomaly *json.RawMessage
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, one := range body.Subjects {
		if one.Anomaly != nil {
			t.Errorf("%s anomaly = %s, want null", one.Metric, *one.Anomaly)
		}
		if one.Metric == "disk.free_bytes" && (one.Level == nil || *one.Level != "warning") {
			t.Errorf("disk.free_bytes level = %v, want warning", one.Level)
		}
	}
}
