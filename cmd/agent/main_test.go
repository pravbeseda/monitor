package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/version"
)

// spec: agent.md#local-configuration — all three values are deployment settings.
func TestSettingsHaveNoDefaults(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		token string
		want  string
	}{
		{"no hub", nil, "secret", "--hub"},
		{"no node", []string{"--hub", "https://hub.example"}, "secret", "--node"},
		{"no token", []string{"--hub", "https://hub.example", "--node", "laptop-a"}, "", "MONITOR_TOKEN"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenVariable, tc.token)

			_, err := settings(tc.args, io.Discard)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// -h is a request, not a failure: it prints the flags and the caller exits without an error.
func TestSettingsAnswersHelpWithTheFlagList(t *testing.T) {
	var out bytes.Buffer

	_, err := settings([]string{"-h"}, &out)

	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", err)
	}
	for _, flagName := range []string{"-hub", "-node", "-env-file"} {
		if !strings.Contains(out.String(), flagName) {
			t.Errorf("usage = %q, want it to list %s", out.String(), flagName)
		}
	}
}

// spec: release.md#the-version-a-binary-reports — a downloaded binary can be asked which
// version it is, before it has a hub, a node or a token to be configured with.
func TestSettingsAnswerVersionBeforeAnythingIsConfigured(t *testing.T) {
	t.Setenv(tokenVariable, "")
	var out bytes.Buffer

	_, err := settings([]string{"--version"}, &out)

	if !errors.Is(err, errVersionRequested) {
		t.Fatalf("error = %v, want errVersionRequested", err)
	}
	want := "monitor-agent " + version.Current
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}

// envFile writes an environment file in a temporary directory and returns its path.
func envFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// spec: agent.md#local-configuration — under a service nothing else is set, so the file
// supplies all three values, and a key the agent does not know is ignored.
func TestSettingsTakeTheThreeValuesFromTheEnvironmentFile(t *testing.T) {
	t.Setenv(tokenVariable, "")
	path := envFile(t, `MONITOR_HUB=https://hub.example.com
MONITOR_NODE=laptop-a
MONITOR_TOKEN=replace-me
MONITOR_SOMETHING_ELSE=ignored
`)

	opts, err := settings([]string{"--env-file", path}, io.Discard)

	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	want := options{hub: "https://hub.example.com", node: "laptop-a", token: "replace-me"}
	if opts != want {
		t.Errorf("settings = %+v, want %+v", opts, want)
	}
}

// spec: agent.md#local-configuration — anything given explicitly wins over the file: a flag
// over its key, and MONITOR_TOKEN in the process environment over the file's token.
func TestExplicitValuesBeatTheEnvironmentFile(t *testing.T) {
	t.Setenv(tokenVariable, "from-the-environment")
	path := envFile(t, `MONITOR_HUB=https://hub.example.com
MONITOR_NODE=laptop-a
MONITOR_TOKEN=from-the-file
`)

	opts, err := settings([]string{
		"--env-file", path,
		"--hub", "https://other.example.com",
		"--node", "server-b",
	}, io.Discard)

	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	want := options{hub: "https://other.example.com", node: "server-b", token: "from-the-environment"}
	if opts != want {
		t.Errorf("settings = %+v, want %+v", opts, want)
	}
}

// spec: agent.md#local-configuration — a file that cannot be read is named, and one that
// parses but leaves a value empty fails exactly as an unset value does.
func TestSettingsRefuseAnEnvironmentFileThatSuppliesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.env")
	tests := []struct {
		name string
		path string
		want string
	}{
		{"unreadable", missing, missing},
		{"no hub", envFile(t, "MONITOR_NODE=laptop-a\n"), "--hub"},
		{"no node", envFile(t, "MONITOR_HUB=https://hub.example.com\n"), "--node"},
		{"no token", envFile(t, "MONITOR_HUB=https://hub.example.com\nMONITOR_NODE=laptop-a\n"), tokenVariable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tokenVariable, "")

			_, err := settings([]string{"--env-file", tc.path}, io.Discard)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}
