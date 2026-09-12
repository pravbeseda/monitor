package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/version"
)

// spec: hub-config.md#startup — the deployment paths have no defaults.
func TestParseFlagsRequiresDeploymentPaths(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no flags at all", nil, "--config"},
		{"a configuration but no database", []string{"--config", "config.yaml"}, "--db"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags(tc.args, io.Discard)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// spec: hub-config.md#startup — neither flag nor variable given.
func TestParseFlagsDefaultsToLocalhost(t *testing.T) {
	t.Setenv(listenEnv, "")
	if err := os.Unsetenv(listenEnv); err != nil {
		t.Fatalf("unset %s: %v", listenEnv, err)
	}

	opts, err := parseFlags([]string{"--config", "config.yaml", "--db", "monitor.db"}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if !strings.HasPrefix(opts.listen, "127.0.0.1:") {
		t.Errorf("listen = %q, want the hub bound to localhost (ADR 0005)", opts.listen)
	}
}

// spec: release.md#the-version-a-binary-reports — a downloaded binary can be asked which
// version it is, before it has a configuration or a database.
func TestParseFlagsAnswerVersionBeforeAnythingIsConfigured(t *testing.T) {
	var out bytes.Buffer

	_, err := parseFlags([]string{"--version"}, &out)

	if !errors.Is(err, errVersionRequested) {
		t.Fatalf("error = %v, want errVersionRequested", err)
	}
	want := "monitor-hub " + version.Current
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}

// -h is a request, not a failure: it prints the flags and the caller exits without an error.
func TestParseFlagsAnswersHelpWithTheFlagList(t *testing.T) {
	var out bytes.Buffer

	_, err := parseFlags([]string{"-h"}, &out)

	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", err)
	}
	for _, flagName := range []string{"-config", "-db", "-listen"} {
		if !strings.Contains(out.String(), flagName) {
			t.Errorf("usage = %q, want it to list %s", out.String(), flagName)
		}
	}
}

// spec: hub-config.md#startup — a hub that cannot bind its listener says so and stops.
// Three review rounds found three faults in run's shutdown arrangement, and each of them
// showed up here first: a deadlock makes this test time out rather than fail quietly.
func TestRunReportsAListenerItCannotBind(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer func() { _ = taken.Close() }()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	config := "nodes:\n  laptop-a:\n    class: laptop\n    token_env: MONITOR_TOKEN_LAPTOP_A\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MONITOR_TOKEN_LAPTOP_A", strings.Repeat("a", 40))

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run([]string{
			"--config", configPath,
			"--db", filepath.Join(dir, "monitor.db"),
			"--listen", taken.Addr().String(),
		}, &out)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "serve on") {
			t.Fatalf("error = %v, want it to name the address it could not serve on", err)
		}
		// spec: hub-config.md#startup — the line it logs names the address in force, which
		// is the only way an operator sees which of the two settings won.
		if want := "listening on " + taken.Addr().String(); !strings.Contains(out.String(), want) {
			t.Errorf("startup line = %q, want it to contain %q", out.String(), want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return: the stop is waiting on something it never cancels")
	}
}

// spec: hub-config.md#startup — the port a host has free is a deployment setting, so the
// service takes it from its environment file rather than from the unit.
func TestParseFlagsTakesTheListenAddressFromTheEnvironment(t *testing.T) {
	t.Setenv(listenEnv, "127.0.0.1:8090")

	opts, err := parseFlags([]string{"--config", "config.yaml", "--db", "monitor.db"}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.listen != "127.0.0.1:8090" {
		t.Errorf("listen = %q, want the address %s named", opts.listen, listenEnv)
	}
}

// spec: hub-config.md#startup — the flag wins, so a run by hand can reach a port the
// service does not use.
func TestParseFlagsPrefersTheListenFlagOverTheEnvironment(t *testing.T) {
	t.Setenv(listenEnv, "127.0.0.1:8090")

	opts, err := parseFlags([]string{
		"--config", "config.yaml", "--db", "monitor.db", "--listen", "127.0.0.1:9999",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.listen != "127.0.0.1:9999" {
		t.Errorf("listen = %q, want the flag to win over %s", opts.listen, listenEnv)
	}
}

// spec: hub-config.md#startup — an empty variable is a variable nobody set. systemd
// keeps a bare `MONITOR_LISTEN=` line, and binding "" would serve port 80 on every address.
func TestParseFlagsIgnoresAnEmptyListenVariable(t *testing.T) {
	t.Setenv(listenEnv, "")

	opts, err := parseFlags([]string{"--config", "config.yaml", "--db", "monitor.db"}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.listen != defaultListen {
		t.Errorf("listen = %q, want the default %s", opts.listen, defaultListen)
	}
}

// spec: hub-config.md#startup — a hub on a public interface serves the pages and the read
// API to anyone, because the credential in front of them belongs to the proxy (ADR 0023).
func TestParseFlagsRefusesAnAddressThatIsNotLoopback(t *testing.T) {
	paths := []string{"--config", "config.yaml", "--db", "monitor.db"}
	tests := []struct {
		name string
		env  string
		args []string
	}{
		{"every interface, from the environment", "0.0.0.0:8090", paths},
		{"every interface, unnamed host", ":8090", paths},
		{"a routable address, from the flag", "", append(append([]string{}, paths...), "--listen", "192.0.2.10:8090")},
		{"a name that is not an address", "example:8090", paths},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(listenEnv, tc.env)

			_, err := parseFlags(tc.args, io.Discard)

			if err == nil || !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("error = %v, want a refusal naming loopback", err)
			}
		})
	}
}

// spec: hub-config.md#startup — localhost is the loopback address under another name, and
// an operator who writes it should not be refused.
func TestParseFlagsAcceptsLoopbackByName(t *testing.T) {
	t.Setenv(listenEnv, "localhost:8090")

	opts, err := parseFlags([]string{"--config", "config.yaml", "--db", "monitor.db"}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.listen != "localhost:8090" {
		t.Errorf("listen = %q, want the address kept as written", opts.listen)
	}
}
