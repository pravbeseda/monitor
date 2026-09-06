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

func TestParseFlagsDefaultsToLocalhost(t *testing.T) {
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

	done := make(chan error, 1)
	go func() {
		done <- run([]string{
			"--config", configPath,
			"--db", filepath.Join(dir, "monitor.db"),
			"--listen", taken.Addr().String(),
		}, io.Discard)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "serve on") {
			t.Fatalf("error = %v, want it to name the address it could not serve on", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return: the stop is waiting on something it never cancels")
	}
}
