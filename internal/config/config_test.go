package config_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
)

// Synthetic throughout, per ADR 0007: invented node names, and a token built at runtime
// so that no fixture ever looks like a real secret to the scanner.
const tokenEnv = "MONITOR_TOKEN_LAPTOP_A"

var token = strings.Repeat("synthetic-", 4)

// The product default the hub ships with, restated here so a change to it is deliberate.
var defaultSkipMountsForTest = []string{"/System/Volumes/", "/Library/Developer/CoreSimulator/"}

const minimal = `
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
`

// write puts a configuration file in a temp directory and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func load(t *testing.T, body string) *config.Config {
	t.Helper()
	t.Setenv(tokenEnv, token)
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func node(t *testing.T, cfg *config.Config, name string) config.Node {
	t.Helper()
	n, ok := cfg.Node(name)
	if !ok {
		t.Fatalf("node %s is not in the configuration", name)
	}
	return n
}

// spec: hub-config.md#startup
func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // a fragment the error must name
	}{
		{"not valid YAML", "nodes: [", "config.yaml"},
		{"a key the hub does not know", minimal + "\ncolour: blue\n", "colour"},
		{"nodes missing", "base_tick: 5m\n", "nodes"},
		{"nodes empty", "nodes: {}\n", "nodes"},
		{"a node without token_env", "nodes:\n  laptop-a:\n    class: laptop\n", "laptop-a"},
		{"an unknown class", strings.Replace(minimal, "class: laptop", "class: toaster", 1), "toaster"},
		{"a duration Go cannot parse", minimal + "\nbase_tick: soon\n", "base_tick"},
		{"a zero duration", minimal + "\nbase_tick: 0s\n", "base_tick"},
		{"a sensor interval below the base tick", minimal + "\nsensors:\n  disk: { interval: 1m }\n", "disk"},
		{"filesystems present and empty", minimal + "\nfilesystems: []\n", "filesystems"},
		{"a sensor in a profile with no interval", minimal +
			"\nclasses:\n  laptop:\n    profile: [disk, battery]\n", "battery"},
		{"a class the file introduces without silence_after", strings.Replace(minimal, "class: laptop", "class: cluster", 1) +
			"\nclasses:\n  cluster:\n    profile: [disk]\n", "cluster"},
		{"a class no node uses, with a broken duration", minimal +
			"\nclasses:\n  cluster:\n    silence_after: 10m\n    base_tick: soon\n", "cluster"},
		{"a class no node uses, without silence_after", minimal +
			"\nclasses:\n  cluster:\n    profile: [disk]\n", "cluster"},
		{"a profile naming a sensor with no interval, on a class no node uses", minimal +
			"\nclasses:\n  cluster:\n    silence_after: 10m\n    profile: [batery]\n", "batery"},
		{"a sensor default no profile delivers", minimal +
			"\nsensors:\n  battery: { interval: 5x }\n", "battery"},
		{"an empty allow-list on a class no node uses", minimal +
			"\nclasses:\n  cluster:\n    silence_after: 10m\n    filesystems: []\n", "filesystems"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenEnv, token)
			_, err := config.Load(write(t, tc.body))
			if err == nil {
				t.Fatalf("Load accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

// spec: hub-config.md#startup — thresholds left the file (ADR 0032), but a hub that
// upgrades itself unattended has to start on the file already on its disk: `rules` and
// `volumes` are accepted wherever they were written, and no number inside one is used.
func TestLoadStartsOnAFileStillCarryingRulesOrVolumes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"rules at the top level", minimal + `
rules:
  disk:
    warning:  { floor: 10GB, ratio: 15, ceiling: 100GB }
    critical: { floor: 4GB,  ratio: 7,  ceiling: 40GB }
`},
		{"rules on a class", minimal + `
classes:
  laptop:
    rules:
      disk:
        warning: { floor: 1GB }
`},
		{"rules on a node", strings.TrimSuffix(minimal, "\n") + `
    rules:
      disk:
        warning: { floor: 1GB }
`},
		{"volumes on a node", strings.TrimSuffix(minimal, "\n") + `
    volumes:
      "/data/backup": { role: backup }
`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenEnv, token)
			var written bytes.Buffer
			before := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&written, nil)))
			t.Cleanup(func() { slog.SetDefault(before) })

			if _, err := config.Load(write(t, tc.body)); err != nil {
				t.Fatalf("Load refused a file carrying a key it no longer reads: %v", err)
			}
			// The numbers in it do nothing, and an operator who left them there has to
			// hear that once (docs/specs/hub-config.md#startup).
			line := written.String()
			if !strings.Contains(line, "a threshold in the configuration file is ignored") {
				t.Errorf("startup said %q, want it to name the key it ignores", line)
			}
			if got := strings.Count(line, "key="); got != 1 {
				t.Errorf("startup said %q, want one warning naming one key", line)
			}
		})
	}
}

// spec: hub-config.md#resolution — a layer is not required to stand alone: what looks too
// fast at one layer can be right once a more specific layer lowers the tick.
func TestLoadAcceptsWhatOnlyAMoreSpecificLayerMakesValid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"the classes lower the tick under a top-level interval", `
base_tick: 5m
sensors:
  disk: { interval: 1m }
classes:
  laptop:
    base_tick: 30s
  server:
    base_tick: 30s
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
`},
		{"the node collects above a tick its class lowered", `
classes:
  fast:
    profile: [disk]
    silence_after: 10m
    base_tick: 1m
    sensors:
      disk: { interval: 1m }
nodes:
  laptop-a:
    class: fast
    token_env: MONITOR_TOKEN_LAPTOP_A
    sensors:
      disk: { interval: 2m }
`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenEnv, token)

			if _, err := config.Load(write(t, tc.body)); err != nil {
				t.Fatalf("Load refused a configuration it resolves: %v", err)
			}
		})
	}
}

// spec: hub-config.md#startup — the file itself must be there.
func TestLoadRejectsMissingFile(t *testing.T) {
	t.Setenv(tokenEnv, token)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	_, err := config.Load(missing)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("error = %v, want it to name the missing path", err)
	}
}

// spec: hub-config.md#tokens
func TestLoadRejectsUnusableTokens(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"an unset variable", ""},
		{"a token shorter than 32 characters", "short-token"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenEnv, tc.value)
			_, err := config.Load(write(t, minimal))
			if err == nil {
				t.Fatalf("Load accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tokenEnv) {
				t.Errorf("error = %q, want it to name the variable", err)
			}
			if strings.Contains(err.Error(), tc.value) && tc.value != "" {
				t.Errorf("error = %q, want it to keep the token out", err)
			}
		})
	}
}

// spec: hub-config.md#startup — two nodes may not share one variable.
func TestLoadRejectsSharedTokenVariable(t *testing.T) {
	t.Setenv(tokenEnv, token)
	body := minimal + "  server-b:\n    class: server\n    token_env: " + tokenEnv + "\n"

	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "server-b") {
		t.Fatalf("error = %v, want it to name both nodes", err)
	}
}

// spec: hub-config.md#resolution — a node with nothing but a class and a token.
func TestResolveFallsBackToProductDefaults(t *testing.T) {
	n := node(t, load(t, minimal), "laptop-a")

	if n.Agent.BaseTick != 5*time.Minute {
		t.Errorf("base tick = %v, want the product default 5m", n.Agent.BaseTick)
	}
	if len(n.Agent.Filesystems) == 0 {
		t.Error("filesystems are empty, want the product allow-list")
	}
	disk, ok := n.Agent.Sensors["disk"]
	if !ok || !disk.Enabled || disk.Interval != time.Hour {
		t.Errorf("disk = %+v, want it enabled every 1h on a laptop", disk)
	}
	if n.Token != token {
		t.Errorf("token = %q, want the value of %s", n.Token, tokenEnv)
	}
	if n.SilenceAfter != 48*time.Hour {
		t.Errorf("silence_after = %v, want the laptop default 48h", n.SilenceAfter)
	}
}

// spec: hub-config.md#resolution — the skip list layers like the allow-list, and an empty
// list is a value: it means nothing is skipped.
func TestResolveLayersSkipMounts(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"the product default", minimal, defaultSkipMountsForTest},
		{"the file's top level", minimal + "\nskip_mounts: [\"/mnt/scratch/\"]\n", []string{"/mnt/scratch/"}},
		{"the class over the top level", minimal +
			"\nskip_mounts: [\"/mnt/scratch/\"]\nclasses:\n  laptop:\n    skip_mounts: [\"/Volumes/tmp/\"]\n",
			[]string{"/Volumes/tmp/"}},
		{"an empty list skips nothing", minimal + "\nskip_mounts: []\n", []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := node(t, load(t, tc.body), "laptop-a").Agent.SkipMounts

			if len(got) != len(tc.want) {
				t.Fatalf("skip_mounts = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("skip_mounts = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// spec: hub-config.md#configuration-version — the skip list reaches the agent, so it counts.
func TestVersionChangesWithTheSkipList(t *testing.T) {
	before := node(t, load(t, minimal), "laptop-a").Version
	after := node(t, load(t, minimal+"\nskip_mounts: [\"/mnt/scratch/\"]\n"), "laptop-a").Version

	if before == after {
		t.Errorf("version stayed %q after the skip list changed", before)
	}
}

// spec: hub-config.md#resolution — sensor default → class → node, most specific last.
func TestResolveLayersSensorInterval(t *testing.T) {
	tests := []struct {
		name string
		body string
		want time.Duration
	}{
		{"the sensor default", minimal + "\nsensors:\n  disk: { interval: 30m }\n", 30 * time.Minute},
		{"the class over the sensor default", minimal +
			"\nsensors:\n  disk: { interval: 30m }\nclasses:\n  laptop:\n    sensors:\n      disk: { interval: 2h }\n",
			2 * time.Hour},
		{"the node over the class", strings.TrimSuffix(minimal, "\n") +
			"\n    sensors:\n      disk: { interval: 10m }\n", 10 * time.Minute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := node(t, load(t, tc.body), "laptop-a")
			if got := n.Agent.Sensors["disk"].Interval; got != tc.want {
				t.Errorf("disk interval = %v, want %v", got, tc.want)
			}
		})
	}
}

// spec: hub-config.md#resolution — a sensor switched off is delivered as disabled.
func TestResolveDeliversDisabledSensor(t *testing.T) {
	body := strings.TrimSuffix(minimal, "\n") + "\n    sensors:\n      disk: { enabled: false }\n"

	disk := node(t, load(t, body), "laptop-a").Agent.Sensors["disk"]
	if disk.Enabled {
		t.Errorf("disk = %+v, want it delivered as disabled", disk)
	}
}

// spec: hub-config.md#resolution — a sensor outside the profile, switched on for one node.
func TestResolveEnablesSensorOutsideProfile(t *testing.T) {
	body := strings.TrimSuffix(minimal, "\n") +
		"\n    sensors:\n      battery: { enabled: true }\nsensors:\n  battery: { interval: 30m }\n"

	battery, ok := node(t, load(t, body), "laptop-a").Agent.Sensors["battery"]
	if !ok || !battery.Enabled || battery.Interval != 30*time.Minute {
		t.Errorf("battery = %+v, want it enabled every 30m", battery)
	}
}

// spec: hub-config.md#resolution — a sensor no layer mentions is not delivered.
func TestResolveOmitsUnmentionedSensor(t *testing.T) {
	if _, ok := node(t, load(t, minimal), "laptop-a").Agent.Sensors["battery"]; ok {
		t.Error("battery was delivered, want it absent")
	}
}

// spec: hub-config.md#resolution — the base tick and the allow-list layer the same way.
func TestResolveLayersBaseTickAndFilesystems(t *testing.T) {
	body := strings.TrimSuffix(minimal, "\n") +
		"\n    base_tick: 1m\nfilesystems: [apfs]\nclasses:\n  laptop:\n    base_tick: 10m\n"

	n := node(t, load(t, body), "laptop-a")
	if n.Agent.BaseTick != time.Minute {
		t.Errorf("base tick = %v, want the node value 1m", n.Agent.BaseTick)
	}
	if len(n.Agent.Filesystems) != 1 || n.Agent.Filesystems[0] != "apfs" {
		t.Errorf("filesystems = %v, want the file's list", n.Agent.Filesystems)
	}
}

// spec: hub-config.md#configuration-version
func TestVersionIsStableAcrossLoads(t *testing.T) {
	first := node(t, load(t, minimal), "laptop-a").Version
	second := node(t, load(t, minimal), "laptop-a").Version

	if first != second {
		t.Errorf("version = %q then %q, want it derived from the configuration alone", first, second)
	}
	if len(first) != 12 {
		t.Errorf("version = %q, want 12 hex characters", first)
	}
}

// spec: hub-config.md#configuration-version
func TestVersionChangesWithDeliveredValue(t *testing.T) {
	before := node(t, load(t, minimal), "laptop-a").Version
	after := node(t, load(t, minimal+"\nsensors:\n  disk: { interval: 30m }\n"), "laptop-a").Version

	if before == after {
		t.Errorf("version stayed %q after the disk interval changed", before)
	}
}

// spec: hub-config.md#configuration-version — a hub-only value is not delivered.
func TestVersionIgnoresHubOnlyValue(t *testing.T) {
	before := node(t, load(t, minimal), "laptop-a").Version
	body := minimal + "\nclasses:\n  laptop:\n    silence_after: 12h\n"
	after := node(t, load(t, body), "laptop-a").Version

	if before != after {
		t.Errorf("version changed from %q to %q, want silence_after not to reach the agent", before, after)
	}
}

// spec: hub-config.md#configuration-version — it identifies the configuration, not the node.
func TestVersionIsSharedByIdenticalNodes(t *testing.T) {
	const secondEnv = "MONITOR_TOKEN_LAPTOP_B"
	t.Setenv(secondEnv, strings.Repeat("second-", 5))
	body := minimal + "  laptop-b:\n    class: laptop\n    token_env: " + secondEnv + "\n"

	cfg := load(t, body)
	if a, b := node(t, cfg, "laptop-a").Version, node(t, cfg, "laptop-b").Version; a != b {
		t.Errorf("versions %q and %q differ for identical configurations", a, b)
	}
}

func TestNodeReportsUnknownNode(t *testing.T) {
	if _, ok := load(t, minimal).Node("server-b"); ok {
		t.Error("Node reported an unlisted node, want it unknown")
	}
}
