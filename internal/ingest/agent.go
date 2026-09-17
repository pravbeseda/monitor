package ingest

import (
	"context"
	"net/http"

	"github.com/pravbeseda/monitor/internal/config"
)

// AgentPrefix holds what a node asks the hub without a running agent (ADR 0028). The proxy
// lets the whole prefix through, so the hub authenticates all of it.
const AgentPrefix = "/api/v1/agent/"

// NewAgentHandler serves every path under /api/v1/agent/, authenticating a request before it
// is routed: a path added here later cannot be reached without a token by forgetting to wrap
// it (docs/specs/ingest.md#the-agents-target).
func NewAgentHandler(cfg *config.Config) http.Handler {
	routes := http.NewServeMux()
	routes.HandleFunc("GET "+AgentPrefix+"target", agentTarget)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := authenticate(cfg, r)
		if !ok {
			fail(w, http.StatusUnauthorized, "unknown or missing token")
			return
		}
		routes.ServeHTTP(w, r.WithContext(withNode(r.Context(), node)))
	})
}

// agentTarget answers in the grammar of hub.target, which the installer already reads; an
// empty target is the hub naming none.
func agentTarget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	target := nodeOf(r.Context()).AgentTarget
	if target == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// The status line is already sent, so a failed write can only be lost.
	_, _ = w.Write([]byte(target + "\n"))
}

type nodeKey struct{}

func withNode(ctx context.Context, node config.Node) context.Context {
	return context.WithValue(ctx, nodeKey{}, node)
}

// nodeOf is the node NewAgentHandler authenticated; every route behind it has one.
func nodeOf(ctx context.Context) config.Node {
	node, _ := ctx.Value(nodeKey{}).(config.Node)
	return node
}
