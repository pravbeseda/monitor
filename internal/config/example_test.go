package config_test

import (
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/config"
)

// The shipped example is what a reader copies first, so it has to start on the hub as it
// is now: a key the example demonstrates before the code accepts it is a broken promise.
func TestExampleFileResolves(t *testing.T) {
	t.Setenv("MONITOR_TOKEN_LAPTOP_A", token)
	t.Setenv("MONITOR_TOKEN_SERVER_B", strings.Repeat("server-b-", 4))

	cfg, err := config.Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
	if len(cfg.Nodes()) != 2 {
		t.Fatalf("the example resolved %d nodes, want 2", len(cfg.Nodes()))
	}
}
