package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
	"github.com/pravbeseda/monitor/internal/storage"
)

const tokenEnv = "MONITOR_TOKEN_LAPTOP_A"

var token = strings.Repeat("synthetic-", 4)

// received is the hub clock; the agent's own timestamps differ from it on purpose.
var received = time.Date(2026, 8, 28, 10, 0, 5, 0, time.UTC)

const configBody = `
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
`

const validBody = `{
  "node": "laptop-a",
  "agent_version": "0.1.0",
  "config_version": "%s",
  "ts": "2026-08-28T10:00:00Z",
  "manifest": [{"sensor": "disk", "applicable": true}],
  "measurements": [
    {"metric": "disk.free_bytes", "labels": {"mount": "/"}, "value": 123456789}
  ]
}`

// spy records what ingest asked storage to do, so a test can assert that nothing was.
type spy struct {
	saved []storage.Ingest
}

func (s *spy) SaveIngest(_ context.Context, in storage.Ingest) error {
	s.saved = append(s.saved, in)
	return nil
}

func (s *spy) Series(context.Context, storage.Selection) ([]storage.SeriesRef, error) {
	return nil, nil
}

func (s *spy) Newest(context.Context, storage.Selection, time.Time) ([]storage.SeriesNewest, error) {
	return nil, nil
}

func (s *spy) Points(context.Context, storage.SeriesRef, time.Time, time.Time) iter.Seq2[storage.Point, error] {
	return func(func(storage.Point, error) bool) {}
}

func (s *spy) Close() error { return nil }

func newHandler(t *testing.T) (http.Handler, *spy, config.Node) {
	t.Helper()
	t.Setenv(tokenEnv, token)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	node, ok := cfg.Node("laptop-a")
	if !ok {
		t.Fatal("laptop-a is missing from the configuration")
	}

	store := &spy{}
	clock := func() time.Time { return received }
	return ingest.NewHandler(cfg, store, clock), store, node
}

// post sends body with the given authorization header value ("" sends none).
func post(t *testing.T, h http.Handler, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bearer() string { return "Bearer " + token }

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

// spec: ingest.md#authentication
func TestAuthentication(t *testing.T) {
	tests := []struct {
		name string
		auth string
		body string
		want int
	}{
		{"no Authorization header", "", validBody, http.StatusUnauthorized},
		{"a token unknown to the hub", "Bearer " + strings.Repeat("wrong-", 6), validBody, http.StatusUnauthorized},
		{"a header that is not a bearer token", token, validBody, http.StatusUnauthorized},
		{"a node that is not the token's node", bearer(),
			strings.Replace(validBody, "laptop-a", "server-b", 1), http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, store, _ := newHandler(t)

			rec := post(t, h, tc.auth, strings.ReplaceAll(tc.body, "%s", ""))

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if len(store.saved) != 0 {
				t.Errorf("stored %+v, want nothing", store.saved)
			}
		})
	}
}

// spec: ingest.md#validation
func TestValidation(t *testing.T) {
	valid := strings.ReplaceAll(validBody, "%s", "")
	tests := []struct {
		name string
		body string
		want int
	}{
		{"a body that is not JSON", "{", http.StatusBadRequest},
		{"node missing", strings.Replace(valid, `"node": "laptop-a",`, "", 1), http.StatusBadRequest},
		{"ts missing", strings.Replace(valid, `"ts": "2026-08-28T10:00:00Z",`, "", 1), http.StatusBadRequest},
		{"measurements missing", strings.Replace(valid,
			`"measurements": [
    {"metric": "disk.free_bytes", "labels": {"mount": "/"}, "value": 123456789}
  ]`, `"agent_version": "0.1.0"`, 1), http.StatusBadRequest},
		{"a ts that is not RFC 3339", strings.Replace(valid, "2026-08-28T10:00:00Z", "yesterday", 1), http.StatusBadRequest},
		{"a measurement ts that is not RFC 3339", strings.Replace(valid,
			`"value": 123456789`, `"value": 1, "ts": "yesterday"`, 1), http.StatusBadRequest},
		{"a measurement without a metric", strings.Replace(valid, `"metric": "disk.free_bytes",`, "", 1), http.StatusBadRequest},
		{"a measurement without a value", strings.Replace(valid, `, "value": 123456789`, "", 1), http.StatusBadRequest},
		{"a value that is not finite", strings.Replace(valid, "123456789", "1e999", 1), http.StatusBadRequest},
		{"a metric id with forbidden characters", strings.Replace(valid, "disk.free_bytes", "Disk Free!", 1), http.StatusBadRequest},
		{"a second document behind the first", valid + " garbage", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, store, _ := newHandler(t)

			rec := post(t, h, bearer(), tc.body)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body %q", rec.Code, tc.want, rec.Body.String())
			}
			if len(store.saved) != 0 {
				t.Errorf("stored %+v, want nothing", store.saved)
			}
			if msg, ok := decode(t, rec)["error"].(string); !ok || msg == "" {
				t.Error("response carries no error message")
			}
		})
	}
}

// spec: ingest.md#validation — one invalid measurement rejects the whole request.
func TestValidationRejectsWholeBatch(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.Replace(strings.ReplaceAll(validBody, "%s", ""),
		`{"metric": "disk.free_bytes", "labels": {"mount": "/"}, "value": 123456789}`,
		`{"metric": "disk.free_bytes", "value": 1}, {"metric": "disk.free_pct"}`, 1)

	rec := post(t, h, bearer(), body)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(store.saved) != 0 {
		t.Errorf("stored %+v, want the valid measurement rejected with the batch", store.saved)
	}
}

// spec: ingest.md#validation — a megabyte of padding behind a valid document is still a
// body over the limit, even though the document itself is small.
func TestValidationRejectsOversizedTrailingBody(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.ReplaceAll(validBody, "%s", "") + strings.Repeat(" ", 1<<20)

	rec := post(t, h, bearer(), body)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413; body %q", rec.Code, rec.Body.String())
	}
	if len(store.saved) != 0 {
		t.Errorf("stored %+v, want nothing", store.saved)
	}
}

// spec: ingest.md#validation — a body over 1 MiB.
func TestValidationRejectsOversizedBody(t *testing.T) {
	h, store, _ := newHandler(t)
	padding := strings.Repeat("a", 1<<20)
	body := strings.Replace(strings.ReplaceAll(validBody, "%s", ""),
		`"agent_version": "0.1.0"`, `"agent_version": "`+padding+`"`, 1)

	rec := post(t, h, bearer(), body)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if len(store.saved) != 0 {
		t.Errorf("stored %+v, want nothing", store.saved)
	}
}

// spec: ingest.md#storage
func TestStoresMeasurementsAndReceiptTime(t *testing.T) {
	h, store, _ := newHandler(t)

	rec := post(t, h, bearer(), strings.ReplaceAll(validBody, "%s", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved %d requests, want 1", len(store.saved))
	}
	saved := store.saved[0]
	if !saved.ReceivedAt.Equal(received) {
		t.Errorf("last-seen = %v, want the hub receipt time %v", saved.ReceivedAt, received)
	}
	if len(saved.Measurements) != 1 || saved.Measurements[0].Metric != "disk.free_bytes" {
		t.Fatalf("measurements = %+v, want the one that was sent", saved.Measurements)
	}
	if got := saved.Measurements[0].TS; !got.Equal(time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("measurement ts = %v, want the request ts it inherits", got)
	}
	if len(saved.Manifest) != 1 || saved.Manifest[0].Sensor != "disk" || !saved.Manifest[0].Applicable {
		t.Errorf("manifest = %+v, want the one that was sent", saved.Manifest)
	}
}

// spec: ingest.md#storage — a measurement carrying its own collection time keeps it.
func TestKeepsMeasurementCollectionTime(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.Replace(strings.ReplaceAll(validBody, "%s", ""),
		`"value": 123456789`, `"value": 1, "ts": "2026-08-28T09:55:00Z"`, 1)

	if rec := post(t, h, bearer(), body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	want := time.Date(2026, 8, 28, 9, 55, 0, 0, time.UTC)
	if got := store.saved[0].Measurements[0].TS; !got.Equal(want) {
		t.Errorf("measurement ts = %v, want %v", got, want)
	}
}

// spec: ingest.md#storage — an empty batch is a valid request.
func TestAcceptsEmptyBatch(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.Replace(strings.ReplaceAll(validBody, "%s", ""),
		`[
    {"metric": "disk.free_bytes", "labels": {"mount": "/"}, "value": 123456789}
  ]`, "[]", 1)

	rec := post(t, h, bearer(), body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	if len(store.saved) != 1 || len(store.saved[0].Measurements) != 0 {
		t.Errorf("saved %+v, want last-seen advanced and nothing else", store.saved)
	}
}

// spec: ingest.md#storage — a metric the hub's configuration does not declare is stored.
func TestStoresUndeclaredMetric(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.Replace(strings.ReplaceAll(validBody, "%s", ""), "disk.free_bytes", "coffee.level", 1)

	if rec := post(t, h, bearer(), body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.saved[0].Measurements[0].Metric != "coffee.level" {
		t.Errorf("measurements = %+v, want the undeclared metric stored", store.saved[0].Measurements)
	}
}

// captureLog redirects the hub's log for one test and hands back what was written.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(before) })
	return &out
}

// spec: ingest.md#configuration-delivery — delivering a configuration logs one line naming
// the node, the version the agent held and the version it was given; holding the hub's own
// version delivers nothing and logs nothing.
func TestConfigurationDeliveryIsLogged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		want    string
	}{
		{"the agent holds an older version", "0000deadbeef", "from=0000deadbeef"},
		{"the agent holds no version yet", "", `from=""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, node := newHandler(t)
			logged := captureLog(t)

			post(t, h, bearer(), strings.ReplaceAll(validBody, "%s", tc.version))

			line := logged.String()
			if count := strings.Count(line, "\n"); count != 1 {
				t.Fatalf("the delivery wrote %d log lines: %q", count, line)
			}
			for _, want := range []string{"node=laptop-a", tc.want, "to=" + node.Version} {
				if !strings.Contains(line, want) {
					t.Errorf("log line = %q, want %q in it", line, want)
				}
			}
		})
	}

	t.Run("the agent already holds the hub's version", func(t *testing.T) {
		h, _, node := newHandler(t)
		logged := captureLog(t)

		post(t, h, bearer(), strings.ReplaceAll(validBody, "%s", node.Version))

		if line := logged.String(); line != "" {
			t.Errorf("a request that delivered nothing logged %q", line)
		}
	})
}

// spec: ingest.md#configuration-delivery
func TestConfigurationDelivery(t *testing.T) {
	t.Run("the agent already holds the hub's version", func(t *testing.T) {
		h, _, node := newHandler(t)

		rec := post(t, h, bearer(), strings.ReplaceAll(validBody, "%s", node.Version))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if body := decode(t, rec); len(body) != 0 {
			t.Errorf("body = %v, want it empty", body)
		}
	})

	for _, tc := range []struct {
		name    string
		version string
	}{
		{"the agent holds an older version", "0000deadbeef"},
		{"the agent holds no version yet", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, node := newHandler(t)

			rec := post(t, h, bearer(), strings.ReplaceAll(validBody, "%s", tc.version))

			body := decode(t, rec)
			if body["config_version"] != node.Version {
				t.Errorf("config_version = %v, want %q", body["config_version"], node.Version)
			}
			delivered, ok := body["config"].(map[string]any)
			if !ok {
				t.Fatalf("body = %v, want a config", body)
			}
			if delivered["base_tick"] != "5m" {
				t.Errorf("base_tick = %v, want the resolved 5m", delivered["base_tick"])
			}
			sensors, ok := delivered["sensors"].(map[string]any)
			if !ok {
				t.Fatalf("config = %v, want sensors", delivered)
			}
			disk, ok := sensors["disk"].(map[string]any)
			if !ok || disk["enabled"] != true || disk["interval"] != "1h" {
				t.Errorf("disk = %v, want it enabled every 1h", sensors["disk"])
			}
			if _, ok := delivered["filesystems"].([]any); !ok {
				t.Errorf("config = %v, want the filesystem allow-list", delivered)
			}
			if _, ok := delivered["skip_mounts"].([]any); !ok {
				t.Errorf("config = %v, want the mount skip list", delivered)
			}
		})
	}
}

// spec: ingest.md#limits
func TestRateLimit(t *testing.T) {
	h, store, _ := newHandler(t)
	body := strings.ReplaceAll(validBody, "%s", "")

	for i := range 60 {
		if rec := post(t, h, bearer(), body); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, rec.Code)
		}
	}

	rec := post(t, h, bearer(), body)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if len(store.saved) != 60 {
		t.Errorf("saved %d requests, want the refused one stored nothing", len(store.saved))
	}
}
