package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/evaluate"
)

func target(t *testing.T, cfg *config.Config, name string) evaluate.Target {
	t.Helper()
	return node(t, cfg, name).Target()
}

// spec: evaluation.md#freezing — a sensor the node resolves as disabled collects nothing,
// so its series have no interval to be aged against on that node.
func TestASwitchedOffSensorLeavesNoInterval(t *testing.T) {
	got := target(t, load(t, `
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
    sensors: { disk: { enabled: false } }
`), "laptop-a")
	if interval, runs := got.Intervals["disk"]; runs {
		t.Fatalf("a disabled sensor resolved an interval of %v, so its rule would still have subjects", interval)
	}
}

// spec: evaluation.md#freezing — a sensor no layer delivers is not collected either, and
// its series have no interval for the same reason.
func TestAnUndeliveredSensorLeavesNoInterval(t *testing.T) {
	got := target(t, load(t, minimal), "laptop-a")
	if interval, runs := got.Intervals["nosuchsensor"]; runs {
		t.Fatalf("a sensor no layer mentions resolved an interval of %v", interval)
	}
	if _, runs := got.Intervals["disk"]; !runs {
		t.Fatal("the disk sensor of the laptop profile resolved no interval")
	}
}

// spec: evaluation.md#configuration — thresholds are not in the file, so a target carries
// only what the installation says about the node: its silence window and its intervals.
func TestATargetCarriesTheNodesSilenceWindow(t *testing.T) {
	cfg := load(t, `
classes:
  laptop: { silence_after: 12h }
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
`)
	got := target(t, cfg, "laptop-a")
	if got.Node != "laptop-a" || got.SilenceAfter != 12*time.Hour {
		t.Fatalf("target %s carries a silence window of %v, want 12h", got.Node, got.SilenceAfter)
	}
	if got.SilenceAfter != node(t, cfg, "laptop-a").SilenceAfter {
		t.Fatalf("the target's window %v is not the node's %v", got.SilenceAfter, node(t, cfg, "laptop-a").SilenceAfter)
	}
}

// spec: evaluation.md#the-tick — a tick judges every configured node, in the order messages
// leave in.
func TestTargetsCoverEveryNodeInOrder(t *testing.T) {
	t.Setenv("MONITOR_TOKEN_SERVER_B", strings.Repeat("b", 40))
	cfg := load(t, `
nodes:
  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
`)

	got := cfg.Targets()
	if len(got) != 2 || got[0].Node != "laptop-a" || got[1].Node != "server-b" {
		t.Fatalf("Targets() = %v, want both nodes ordered by name", got)
	}
}
