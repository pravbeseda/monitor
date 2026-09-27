package ingest_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
)

// withService is the laptop's configuration plus a service node the hub collects itself.
func withService(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv(tokenEnv, token)
	for _, name := range []string{"MONITOR_GDRIVE_CLIENT_ID", "MONITOR_GDRIVE_CLIENT_SECRET", "MONITOR_GDRIVE_REFRESH_TOKEN"} {
		t.Setenv(name, "synthetic")
	}
	body := configBody + "  cloud:\n    class: service\n    sensors:\n      gdrive: { enabled: true }\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func serviceNode(t *testing.T, cfg *config.Config) config.Node {
	t.Helper()
	node, ok := cfg.Node("cloud")
	if !ok {
		t.Fatal("cloud is missing from the configuration")
	}
	return node
}

// spec: services.md#collection — no request speaks for the service node: a bare bearer is no
// token, and another node's token does not own it.
func TestNoRequestSpeaksForTheServiceNode(t *testing.T) {
	body := strings.ReplaceAll(strings.Replace(validBody, "laptop-a", "cloud", 1), "%s", "")
	tests := []struct {
		name string
		auth string
		want int
	}{
		{"a bearer with nothing after it", "Bearer ", http.StatusUnauthorized},
		{"another node's token", bearer(), http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &spy{}
			h := ingest.NewHandler(withService(t), store, func() time.Time { return received })

			if rec := post(t, h, tc.auth, body); rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if len(store.saved) != 0 {
				t.Errorf("stored %+v, want nothing", store.saved)
			}
		})
	}
}

// spec: services.md#collection — nor does a bare bearer reach the agent's target.
func TestABareBearerIsNoTokenUnderTheAgentPrefix(t *testing.T) {
	rec := ask(t, ingest.NewAgentHandler(withService(t)), http.MethodGet, "/api/v1/agent/target", "Bearer ")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func report(node, metric string) api.Request {
	value := 42.0
	return api.Request{
		Node:         node,
		AgentVersion: "0.1.0",
		TS:           "2026-09-27T10:00:00Z",
		Measurements: &[]api.Measurement{{Metric: metric, Value: &value}},
	}
}

// spec: services.md#invariants — a service node's report passes the checks an agent's does,
// and is stored at the hub's clock.
func TestAcceptStoresTheServiceNodesReport(t *testing.T) {
	cfg := withService(t)
	store := &spy{}
	h := ingest.NewHandler(cfg, store, func() time.Time { return received })

	if _, err := h.Accept(context.Background(), serviceNode(t, cfg), report("cloud", "gdrive.free_bytes")); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if len(store.saved) != 1 || store.saved[0].Node != "cloud" || !store.saved[0].ReceivedAt.Equal(received) {
		t.Fatalf("stored %+v, want the report of cloud received at %v", store.saved, received)
	}
}

// spec: services.md#invariants — and is refused, with the status the endpoint would answer,
// where an agent's would be.
func TestAcceptRefusesWhatTheEndpointRefuses(t *testing.T) {
	tests := []struct {
		name string
		req  api.Request
		want int
	}{
		{"a metric that is not an id", report("cloud", "Free Space"), http.StatusBadRequest},
		{"another node's report", report("laptop-a", "gdrive.free_bytes"), http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := withService(t)
			store := &spy{}
			h := ingest.NewHandler(cfg, store, func() time.Time { return received })

			_, err := h.Accept(context.Background(), serviceNode(t, cfg), tc.req)

			var refused api.StatusError
			if !errors.As(err, &refused) || refused.Status != tc.want {
				t.Fatalf("error = %v, want a refusal with status %d", err, tc.want)
			}
			if len(store.saved) != 0 {
				t.Errorf("stored %+v, want nothing", store.saved)
			}
		})
	}
}

// spec: ingest.md#storage — a service node's report with nothing in it makes the node known,
// and does not otherwise count as a sign of life.
// spec: services.md#collection — so the node falls silent when its sensors stop answering.
func TestAcceptOnlyIntroducesAnEmptyServiceReport(t *testing.T) {
	cfg := withService(t)
	store := &spy{}
	h := ingest.NewHandler(cfg, store, func() time.Time { return received })
	empty := report("cloud", "gdrive.free_bytes")
	empty.Measurements = &[]api.Measurement{}

	if _, err := h.Accept(context.Background(), serviceNode(t, cfg), empty); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if len(store.saved) != 0 || len(store.introduced) != 1 || store.introduced[0].Node != "cloud" {
		t.Errorf("saved %+v and introduced %+v, want cloud introduced and nothing saved", store.saved, store.introduced)
	}
}

// spec: ingest.md#storage — an agent's heartbeat still counts, as it always did.
func TestAcceptSavesAnEmptyAgentReport(t *testing.T) {
	cfg := withService(t)
	store := &spy{}
	h := ingest.NewHandler(cfg, store, func() time.Time { return received })
	laptop, _ := cfg.Node("laptop-a")
	empty := report("laptop-a", "disk.free_bytes")
	empty.Measurements = &[]api.Measurement{}

	if _, err := h.Accept(context.Background(), laptop, empty); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if len(store.saved) != 1 || len(store.introduced) != 0 {
		t.Errorf("saved %+v and introduced %+v, want the heartbeat saved", store.saved, store.introduced)
	}
}
