package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
)

// Synthetic credentials, built at runtime like the node token (ADR 0007).
var drive = map[string]string{
	"MONITOR_GDRIVE_CLIENT_ID":     "synthetic-client-id",
	"MONITOR_GDRIVE_CLIENT_SECRET": "synthetic-client-secret",
	"MONITOR_GDRIVE_REFRESH_TOKEN": "synthetic-refresh-token",
}

const serviceNode = `
  cloud:
    class: service
    sensors:
      gdrive: { enabled: true }
`

func withDrive(t *testing.T) {
	t.Helper()
	for name, value := range drive {
		t.Setenv(name, value)
	}
}

// spec: services.md#startup — a service node needs no token, and the hub collects it.
func TestLoadAcceptsAServiceNodeWithoutAToken(t *testing.T) {
	withDrive(t)
	cfg := load(t, minimal+serviceNode)

	cloud := node(t, cfg, "cloud")
	if !cloud.Service() {
		t.Error("cloud is not collected by the hub, want it to be")
	}
	if cloud.Token != "" {
		t.Errorf("cloud holds a token, want none")
	}
	if gdrive := cloud.Agent.Sensors["gdrive"]; !gdrive.Enabled || gdrive.Interval != time.Hour {
		t.Errorf("gdrive = %+v, want it enabled every 1h", gdrive)
	}
	if cloud.SilenceAfter != 3*time.Hour {
		t.Errorf("silence_after = %v, want the service default 3h", cloud.SilenceAfter)
	}
	if node(t, cfg, "laptop-a").Service() {
		t.Error("laptop-a is collected by the hub, want its agent to report it")
	}
	want := config.GoogleDrive{
		ClientID:     drive["MONITOR_GDRIVE_CLIENT_ID"],
		ClientSecret: drive["MONITOR_GDRIVE_CLIENT_SECRET"],
		RefreshToken: drive["MONITOR_GDRIVE_REFRESH_TOKEN"],
	}
	if got := cfg.GoogleDrive(); got != want {
		t.Error("the Google Drive credentials are not the environment's")
	}
}

// spec: services.md#startup
func TestLoadRejectsAMisconfiguredServiceNode(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string // fragments the error must name
	}{
		{"a service node with token_env", minimal + serviceNode + "    token_env: MONITOR_TOKEN_CLOUD\n",
			[]string{"cloud"}},
		{"a node of a class the file introduces, without token_env", minimal +
			"  cloud:\n    class: remote\nclasses:\n  remote:\n    silence_after: 3h\n",
			[]string{"cloud"}},
		{"a service node enabling no sensor", minimal + "  cloud:\n    class: service\n",
			[]string{"cloud"}},
		{"a service node enabling a sensor the hub does not carry", minimal + serviceNode +
			"      disk: { enabled: true }\n", []string{"cloud", "disk"}},
		{"the service class enabling a sensor the hub does not carry", minimal +
			"classes:\n  service:\n    profile: [load]\n", []string{"service", "load"}},
		{"an agent's node enabling gdrive", minimal + "    sensors:\n      gdrive: { enabled: true }\n",
			[]string{"laptop-a", "gdrive"}},
		{"an agent's class enabling gdrive", minimal + "classes:\n  server:\n    profile: [disk, gdrive]\n",
			[]string{"server", "gdrive"}},
		{"a file whose machines are of a class named service", minimal +
			"  server-b:\n    class: service\n    token_env: MONITOR_TOKEN_LAPTOP_A\n" +
			"classes:\n  service:\n    silence_after: 10m\n    profile: [disk, load]\n",
			[]string{"service", "reserved", "rename"}},
		{"a silence window shorter than two intervals and three ticks", minimal + serviceNode +
			"classes:\n  service:\n    silence_after: 2h10m\n", []string{"cloud", "silence_after"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withDrive(t)
			t.Setenv(tokenEnv, token)
			_, err := config.Load(write(t, tc.body))
			if err == nil {
				t.Fatalf("Load accepted %s", tc.name)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
		})
	}
}

// spec: services.md#startup — the window is judged against the collections a failure spans:
// two intervals and three base ticks.
func TestLoadAcceptsASilenceWindowOfTwoIntervalsAndThreeTicks(t *testing.T) {
	withDrive(t)
	load(t, minimal+serviceNode+"classes:\n  service:\n    silence_after: 2h15m\n")
}

// spec: services.md#startup — each credential is required while a service node runs gdrive,
// and the error names the variable, never a value.
func TestLoadRejectsAMissingGoogleDriveCredential(t *testing.T) {
	for name := range drive {
		t.Run(name, func(t *testing.T) {
			withDrive(t)
			t.Setenv(name, "")
			t.Setenv(tokenEnv, token)
			_, err := config.Load(write(t, minimal+serviceNode))
			if err == nil {
				t.Fatalf("Load accepted an unset %s", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error = %q, want it to name %s", err, name)
			}
			for _, value := range drive {
				if strings.Contains(err.Error(), value) {
					t.Errorf("error = %q, want it to keep every credential out", err)
				}
			}
		})
	}
}

// spec: services.md#startup — no node runs gdrive, so nothing asks for its credentials.
func TestLoadNeedsNoGoogleDriveCredentialWithoutAServiceNode(t *testing.T) {
	for name := range drive {
		t.Setenv(name, "")
	}
	if got := load(t, minimal).GoogleDrive(); got != (config.GoogleDrive{}) {
		t.Error("credentials were read with no service node, want none")
	}
}

// spec: services.md#startup — a top-level switch reaches only the classes whose host can run
// the sensor, so neither an agent's sensor nor gdrive turns a working file into an error.
func TestTopLevelSensorSwitchReachesOnlyItsOwnHost(t *testing.T) {
	withDrive(t)
	body := minimal + serviceNode + "sensors:\n  systemd: { enabled: true }\n  gdrive: { enabled: true }\n"
	cfg := load(t, body)

	if _, runs := node(t, cfg, "cloud").Agent.Sensors["systemd"]; runs {
		t.Error("cloud runs systemd, want the agent's sensor kept off the service node")
	}
	laptop := node(t, cfg, "laptop-a").Agent.Sensors
	if _, runs := laptop["gdrive"]; runs {
		t.Error("laptop-a runs gdrive, want it kept off an agent's node")
	}
	if !laptop["systemd"].Enabled {
		t.Error("laptop-a does not run systemd, want the top-level switch to reach it")
	}
}

// spec: services.md#invariants — a debug print of the configuration shows no credential.
func TestGoogleDriveCredentialsStayOutOfAPrint(t *testing.T) {
	withDrive(t)
	cfg := load(t, minimal+serviceNode)
	for _, printed := range []string{cfg.GoogleDrive().String(), cfg.String()} {
		for _, value := range drive {
			if strings.Contains(printed, value) {
				t.Errorf("%q shows a credential", printed)
			}
		}
	}
}
