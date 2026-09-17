package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

func routes(t *testing.T) http.Handler {
	t.Helper()
	return routesWith(t, stored{}, time.Now)
}

func routesWith(t *testing.T, store storage.Storage, now func() time.Time) http.Handler {
	t.Helper()
	t.Setenv("MONITOR_TOKEN_LAPTOP_A", strings.Repeat("synthetic-", 4))
	t.Setenv("MONITOR_TOKEN_SERVER_B", strings.Repeat("server-b-", 4))

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "nodes:\n  laptop-a:\n    class: laptop\n    token_env: MONITOR_TOKEN_LAPTOP_A\n" +
		"  server-b:\n    class: server\n    token_env: MONITOR_TOKEN_SERVER_B\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return hub.Routes(cfg, store, now)
}

func TestIngestIsMountedOnItsVersionedPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	routes(t).ServeHTTP(rec, req)

	// No token, so ingest answers 401 — what matters here is that it answered at all.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want ingest to handle the path", rec.Code)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nowhere", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	routes(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// counting is a store that remembers how many batches ingest saved.
type counting struct {
	stored
	saved *int
}

func (c counting) SaveIngest(context.Context, storage.Ingest) error {
	*c.saved++
	return nil
}

// spec: ingest.md#the-agents-target — a target request is served under the versioned
// prefix, stores nothing, and leaves ingest's limit for the node untouched.
func TestTheAgentTargetIsMountedAndStoresNothing(t *testing.T) {
	saved := 0
	h := routesWith(t, counting{saved: &saved}, time.Now)
	token := "Bearer " + strings.Repeat("synthetic-", 4)

	for range 61 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/target", nil)
		req.Header.Set("Authorization", token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want the hub's 204 for a node with no target", rec.Code)
		}
	}
	if saved != 0 {
		t.Errorf("target requests stored %d batches", saved)
	}

	body := `{"node":"laptop-a","ts":"2026-08-28T10:00:00Z","measurements":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(body))
	req.Header.Set("Authorization", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("ingest after 61 target requests answered %d, want 200", rec.Code)
	}
}
