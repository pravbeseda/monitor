package ingest_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
)

// agentHandler is what the hub mounts under /api/v1/agent/, over a configuration whose
// laptop-a follows target; an empty target names none.
func agentHandler(t *testing.T, target string) http.Handler {
	t.Helper()
	t.Setenv(tokenEnv, token)
	body := configBody
	if target != "" {
		body += "    agent_target: " + target + "\n"
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return ingest.NewAgentHandler(cfg)
}

func ask(t *testing.T, h http.Handler, method, path, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// spec: ingest.md#the-agents-target — the token's node's target, as one line of text, and
// never a cached one.
func TestTheAgentTargetIsTheTokensNodes(t *testing.T) {
	for _, target := range []string{"latest", "1.4.0"} {
		t.Run(target, func(t *testing.T) {
			rec := ask(t, agentHandler(t, target), http.MethodGet, "/api/v1/agent/target", bearer())

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != target+"\n" {
				t.Errorf("body = %q, want %q", got, target+"\n")
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

// spec: ingest.md#the-agents-target — a node no layer names a target for gets 204.
func TestNoAgentTargetIsNoContent(t *testing.T) {
	rec := ask(t, agentHandler(t, ""), http.MethodGet, "/api/v1/agent/target", bearer())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want none", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// spec: ingest.md#the-agents-target — every path under the prefix is authenticated before it
// is routed, whether the hub serves it or not.
func TestTheAgentPrefixIsAuthenticatedBeforeItIsRouted(t *testing.T) {
	for _, path := range []string{"/api/v1/agent/target", "/api/v1/agent/nowhere", "/api/v1/agent/"} {
		for _, auth := range []string{"", "Bearer " + strings.Repeat("wrong-", 6), token} {
			t.Run(path+" "+auth, func(t *testing.T) {
				rec := ask(t, agentHandler(t, "latest"), http.MethodGet, path, auth)

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", rec.Code)
				}
				if got := rec.Header().Get("WWW-Authenticate"); got != "" {
					t.Errorf("WWW-Authenticate = %q; the installer reads that header as the proxy's refusal", got)
				}
				if _, ok := decode(t, rec)["error"]; !ok {
					t.Errorf("body = %q, want a JSON error", rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), "latest") {
					t.Errorf("an unauthenticated request learned the target: %q", rec.Body.String())
				}
			})
		}
	}
}

// spec: ingest.md#the-agents-target — with a valid token, a path the hub does not serve is
// 404 and a method it does not take is 405; HEAD is GET's.
func TestTheAgentPrefixRoutesAnAuthenticatedRequest(t *testing.T) {
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/agent/nowhere", http.StatusNotFound},
		{http.MethodPost, "/api/v1/agent/target", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/agent/target", http.StatusMethodNotAllowed},
		{http.MethodHead, "/api/v1/agent/target", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if rec := ask(t, agentHandler(t, "latest"), tc.method, tc.path, bearer()); rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// spec: ingest.md#the-agents-target — the answer is the token's node's alone, whatever the
// query string names.
func TestTheAgentTargetIgnoresTheQuery(t *testing.T) {
	t.Setenv("MONITOR_TOKEN_SERVER_B", strings.Repeat("server-b-", 4))
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := configBody + "    agent_target: 1.4.0\n" +
		"  server-b:\n    class: server\n    token_env: MONITOR_TOKEN_SERVER_B\n    agent_target: 9.9.9\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tokenEnv, token)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	rec := ask(t, ingest.NewAgentHandler(cfg), http.MethodGet, "/api/v1/agent/target?node=server-b", bearer())

	if got := rec.Body.String(); got != "1.4.0\n" {
		t.Errorf("body = %q, want laptop-a's own target", got)
	}
}
