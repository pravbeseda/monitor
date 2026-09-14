// Package deploy_test reads the shipped service definitions as data. They are constants —
// everything that varies between installations is in an environment file — so what can be
// tested is what they name.
package deploy_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/agent"
)

const (
	agentUnit  = "systemd/monitor-agent.service"
	hubUnit    = "systemd/monitor-hub.service"
	updateUnit = "systemd/monitor-hub-update.service"
	timerUnit  = "systemd/monitor-hub-update.timer"
	agentPlist = "launchd/io.github.pravbeseda.monitor-agent.plist"
	agentEnv   = "agent.env.example"
	hubEnv     = "hub.env.example"
)

// keptScript is where the hub host keeps the install script its update timer runs.
const keptScript = "/usr/local/libexec/monitor/monitor-install.sh"

// exampleHub is the only URL any file here may carry.
const exampleHub = "https://hub.example.com"

// plistDoctype is Apple's fixed boilerplate. It is removed before the scan for hosts and
// addresses, so that scan judges only the lines we wrote.
const plistDoctype = `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`

// shipped is every file this directory installs on a node.
var shipped = []string{agentUnit, hubUnit, updateUnit, timerUnit, agentPlist, agentEnv, hubEnv}

// layout is the spec's table read from the other side: the paths each unit has to name.
var layout = []struct {
	name  string
	file  string
	paths []string
}{
	{
		name:  "agent unit",
		file:  agentUnit,
		paths: []string{"/usr/local/bin/monitor-agent", "/etc/monitor/agent.env"},
	},
	{
		name: "hub unit",
		file: hubUnit,
		paths: []string{
			"/usr/local/bin/monitor-hub",
			"/etc/monitor/hub.env",
			"/etc/monitor/hub.yaml",
			"/var/lib/monitor/monitor.db",
		},
	},
	{
		name:  "hub update unit",
		file:  updateUnit,
		paths: []string{keptScript},
	},
	{
		name: "hub update timer",
		file: timerUnit,
	},
	{
		name: "agent plist",
		file: agentPlist,
		paths: []string{
			"/usr/local/bin/monitor-agent",
			"/usr/local/etc/monitor/agent.env",
			"/var/log/monitor-agent.log",
		},
	},
}

// allowedNames is every dotted name a shipped file may contain. Anything else is a host, a
// domain or a secret that someone pasted in.
var allowedNames = map[string]bool{
	"agent.env":                          true,
	"hub.env":                            true,
	"hub.yaml":                           true,
	"monitor.db":                         true,
	"monitor-agent.log":                  true,
	"network-online.target":              true,
	"multi-user.target":                  true,
	"timers.target":                      true,
	"hub.target":                         true,
	"monitor-install.sh":                 true,
	"installer.md":                       true,
	"io.github.pravbeseda.monitor-agent": true,
	"hub.example.com":                    true,
	"deployment.md":                      true,
	"config.example.yaml":                true,
}

var (
	// absolutePath captures the character in front of the path as well, so that a relative
	// path (docs/specs/deployment.md) and an XML closing tag (</key>) are not read as
	// absolute paths.
	absolutePath = regexp.MustCompile(`(^|[^A-Za-z0-9._/<-])(/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)`)
	assignment   = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)=(.*)$`)
	// A bare RestartSec= or RestartSec=0 is a busy loop, not "after a short delay".
	restartDelay = regexp.MustCompile(`(?m)^RestartSec=[1-9][0-9]*(ms|s|min)?$`)
	urlLiteral   = regexp.MustCompile(`[a-z][a-z0-9+.-]*://[^\s"'<]+`)
	ipAddress    = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	dottedName   = regexp.MustCompile(`[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)+`)
	letter       = regexp.MustCompile(`[A-Za-z]`)
)

func read(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

// comment matches what a service definition ignores: a shell-style line in a unit, an XML
// comment in a plist, and Apple's fixed doctype.
var comment = regexp.MustCompile(`(?ms)^[ \t]*#.*?$|<!--.*?-->|<!DOCTYPE.*?>`)

// code is the file with its comments removed. A path a unit only mentions in a comment is a
// path the service never uses, and asserting on it would pass an ExecStart that lost it.
func code(t *testing.T, name string) string {
	t.Helper()
	return comment.ReplaceAllString(read(t, name), "")
}

// spec: deployment.md#where-things-live — every path is fixed there, which is what lets a
// unit be a constant.
func TestUnitsNameThePathsTheSpecFixes(t *testing.T) {
	for _, unit := range layout {
		t.Run(unit.name, func(t *testing.T) {
			body := code(t, unit.file)
			for _, want := range unit.paths {
				if !strings.Contains(body, want) {
					t.Errorf("%s does not name %s", unit.file, want)
				}
			}
		})
	}
}

// spec: deployment.md#where-things-live — a path outside the table is one the install script
// and the guide do not know about.
func TestUnitsNameNoPathTheSpecDoesNotFix(t *testing.T) {
	for _, unit := range layout {
		t.Run(unit.name, func(t *testing.T) {
			allowed := map[string]bool{}
			for _, path := range unit.paths {
				allowed[path] = true
				allowed[filepath.Dir(path)] = true // the directory a fixed path lives in
			}
			for _, match := range absolutePath.FindAllStringSubmatch(code(t, unit.file), -1) {
				if !allowed[match[2]] {
					t.Errorf("%s names %s, which the layout table does not fix", unit.file, match[2])
				}
			}
		})
	}
}

// serviceCommand is the command line each service definition starts, however that system
// spells one: an ExecStart line for systemd, a ProgramArguments array for launchd.
func serviceCommand(t *testing.T, file string) []string {
	t.Helper()
	if filepath.Ext(file) == ".plist" {
		var doc struct {
			Args []string `xml:"dict>array>string"`
		}
		if err := xml.Unmarshal([]byte(read(t, file)), &doc); err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		return doc.Args
	}
	for _, line := range strings.Split(code(t, file), "\n") {
		if command, found := strings.CutPrefix(line, "ExecStart="); found {
			return strings.Fields(command)
		}
	}
	t.Fatalf("%s has no ExecStart", file)
	return nil
}

// spec: deployment.md#the-environment-files — the agent reads the file itself, so each
// service passes it that path and nothing else. Spelling the hub URL or the node name out
// here would also put them in `ps` for every local user (deployment.md#invariants).
func TestTheAgentServicesStartTheBinaryWithItsEnvironmentFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		envFile string
	}{
		{"agent unit", agentUnit, "/etc/monitor/agent.env"},
		{"agent plist", agentPlist, "/usr/local/etc/monitor/agent.env"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := []string{"/usr/local/bin/monitor-agent", "--env-file", tc.envFile}
			if got := serviceCommand(t, tc.file); !slices.Equal(got, want) {
				t.Errorf("%s starts %q, want %q", tc.file, got, want)
			}
		})
	}
}

// spec: deployment.md#the-environment-files — the agent unit must not hand the file to
// systemd as well. That would pass every other test here while putting the token back into
// the agent's own environment, where anything that can read the process can read it.
func TestTheAgentUnitDoesNotAlsoLoadTheEnvironmentFile(t *testing.T) {
	if strings.Contains(code(t, agentUnit), "EnvironmentFile=") {
		t.Errorf("%s sets EnvironmentFile=; the agent reads the file itself", agentUnit)
	}
}

// spec: deployment.md#the-environment-files — the token reaches the process through the
// environment file, so a service definition that named it could also hold it.
func TestNoServiceDefinitionNamesTheToken(t *testing.T) {
	for _, file := range []string{agentUnit, hubUnit, agentPlist} {
		t.Run(file, func(t *testing.T) {
			if strings.Contains(read(t, file), "TOKEN") {
				t.Errorf("%s names the token; it belongs in the environment file only", file)
			}
		})
	}
}

// spec: deployment.md#invariants — no file this repository ships names a host, a node, a URL
// or a token (ADR 0007).
func TestNoShippedFileCarriesAHostAddressOrSecret(t *testing.T) {
	for _, file := range shipped {
		t.Run(file, func(t *testing.T) {
			body := strings.ReplaceAll(read(t, file), plistDoctype, "")
			if found := ipAddress.FindString(body); found != "" {
				t.Errorf("%s carries the address %s", file, found)
			}
			for _, found := range urlLiteral.FindAllString(body, -1) {
				if found != exampleHub {
					t.Errorf("%s carries the URL %s, want %s or none", file, found, exampleHub)
				}
			}
			for _, found := range dottedName.FindAllString(body, -1) {
				if !letter.MatchString(found) || allowedNames[found] {
					continue
				}
				t.Errorf("%s names %s, which reads like a host or a secret", file, found)
			}
		})
	}
}

// spec: deployment.md#the-environment-files — the examples show the keys and none of the
// values, because every value is a secret or a deployment setting (ADR 0007).
func TestExampleEnvironmentFilesHoldPlaceholdersOnly(t *testing.T) {
	placeholders := map[string]bool{exampleHub: true, "laptop-a": true, "replace-me": true}
	tests := []struct {
		file string
		keys []string
	}{
		{agentEnv, []string{"MONITOR_HUB", "MONITOR_NODE", "MONITOR_TOKEN"}},
		{hubEnv, []string{"MONITOR_TELEGRAM_TOKEN", "MONITOR_TELEGRAM_CHAT_ID"}},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			body := read(t, tc.file)
			set := map[string]bool{}
			for _, match := range assignment.FindAllStringSubmatch(body, -1) {
				set[match[1]] = true
				if !placeholders[match[2]] {
					t.Errorf("%s sets %s to %q, want a placeholder", tc.file, match[1], match[2])
				}
			}
			for _, key := range tc.keys {
				if !set[key] {
					t.Errorf("%s assigns no %s", tc.file, key)
				}
			}
		})
	}
}

// spec: deployment.md#where-things-live — the hub runs as the unprivileged monitor account;
// the agent runs as root, because it stats every mounted volume.
func TestOnlyTheHubRunsAsAnAccount(t *testing.T) {
	hub := read(t, hubUnit)
	for _, want := range []string{"User=monitor", "Group=monitor"} {
		if !strings.Contains(hub, want) {
			t.Errorf("%s has no %s", hubUnit, want)
		}
	}
	agent := read(t, agentUnit)
	for _, unwanted := range []string{"User=", "Group="} {
		if strings.Contains(agent, unwanted) {
			t.Errorf("%s sets %s; the agent runs as root", agentUnit, unwanted)
		}
	}
	if strings.Contains(read(t, agentPlist), "UserName") {
		t.Errorf("%s sets UserName; a launchd daemon runs as root", agentPlist)
	}
}

// spec: deployment.md#the-services — it is restarted after a short delay, and goes on being
// restarted however fast it keeps failing.
func TestBothUnitsRestartWithoutARateLimit(t *testing.T) {
	for _, file := range []string{agentUnit, hubUnit} {
		t.Run(file, func(t *testing.T) {
			body := code(t, file)
			for _, want := range []string{"Restart=always", "StartLimitIntervalSec=0"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s has no %s", file, want)
				}
			}
			if !restartDelay.MatchString(body) {
				t.Errorf("%s sets no RestartSec with a delay in it", file)
			}
		})
	}
}

// spec: deployment.md#the-services — launchd's half of the same guarantee: start at load,
// restart on exit.
func TestThePlistStartsAtLoadAndKeepsTheAgentAlive(t *testing.T) {
	body := read(t, agentPlist)
	for _, key := range []string{"RunAtLoad", "KeepAlive"} {
		enabled := regexp.MustCompile(`<key>` + key + `</key>\s*<true/>`)
		if !enabled.MatchString(body) {
			t.Errorf("%s does not set %s", agentPlist, key)
		}
	}
}

// spec: deployment.md#the-environment-files — no shell stands between launchd and the
// agent: POSIX `.` executes the file it reads, so a token pasted with a space in it would
// run its own tail and log the failure.
func TestThePlistStartsTheAgentWithoutAShell(t *testing.T) {
	if strings.Contains(read(t, agentPlist), "/bin/sh") {
		t.Errorf("%s still goes through a shell; the agent reads the environment file itself", agentPlist)
	}
}

// spec: deployment.md#the-services — the node reboots and the service comes back without
// anyone logging in. Deleting [Install] makes `systemctl enable` a silent no-op, which is
// exactly the failure nobody notices until the next reboot.
func TestBothUnitsComeBackAfterAReboot(t *testing.T) {
	for _, file := range []string{agentUnit, hubUnit} {
		t.Run(file, func(t *testing.T) {
			body := code(t, file)
			for _, want := range []string{
				"[Install]",
				"WantedBy=multi-user.target",
				"Wants=network-online.target",
				"After=network-online.target",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s has no %s", file, want)
				}
			}
		})
	}
}

// spec: deployment.md#the-services — the update runs the kept script as root, once, after the
// network is up; the command line is part of what a kept script depends on
// (installer.md#the-handover).
func TestTheUpdateUnitRunsTheKeptScriptOnce(t *testing.T) {
	body := code(t, updateUnit)
	want := []string{keptScript, "hub", "--follow-target"}
	if got := serviceCommand(t, updateUnit); !slices.Equal(got, want) {
		t.Errorf("%s starts %q, want %q", updateUnit, got, want)
	}
	for _, line := range []string{"Type=oneshot", "Wants=network-online.target", "After=network-online.target"} {
		if !strings.Contains(body, line) {
			t.Errorf("%s has no %s", updateUnit, line)
		}
	}
	for _, unwanted := range []string{"User=", "Group="} {
		if strings.Contains(body, unwanted) {
			t.Errorf("%s sets %s; the update writes where only root may", updateUnit, unwanted)
		}
	}
}

// spec: deployment.md#the-services — a failed run is a failed unit, and what the run prints
// reaches the system log: nothing may swallow either.
func TestTheUpdateUnitReportsWhatTheRunDid(t *testing.T) {
	body := code(t, updateUnit)
	for _, unwanted := range []string{"SuccessExitStatus=", "StandardOutput=", "StandardError="} {
		if strings.Contains(body, unwanted) {
			t.Errorf("%s sets %s; a run's status and output are the unit's as they are", updateUnit, unwanted)
		}
	}
}

// spec: deployment.md#the-services — the update does not depend on the hub, and only the timer
// is armed at boot: a service with an [Install] section would run an update on every boot.
func TestTheUpdateIsStartedByItsTimerAlone(t *testing.T) {
	body := code(t, updateUnit)
	for _, unwanted := range []string{"[Install]", "monitor-hub.service"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("%s names %s; the update runs from its timer, whatever state the hub is in", updateUnit, unwanted)
		}
	}
}

// spec: deployment.md#the-services — once a day, spread over an hour, caught up after the host
// was down, and armed again on every boot.
func TestTheTimerRunsTheUpdateDaily(t *testing.T) {
	body := code(t, timerUnit)
	for _, line := range []string{
		"OnCalendar=daily",
		"RandomizedDelaySec=1h",
		"Persistent=true",
		"[Install]",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(body, line) {
			t.Errorf("%s has no %s", timerUnit, line)
		}
	}
	if strings.Contains(body, "OnBootSec=") {
		t.Errorf("%s sets OnBootSec=; the update runs at boot only when a run was missed", timerUnit)
	}
}

// spec: deployment.md#where-things-live — the hub's database is the account's alone, and
// SQLite creates it with whatever umask the service was started with.
func TestTheHubUnitKeepsItsDatabasePrivate(t *testing.T) {
	if !strings.Contains(code(t, hubUnit), "UMask=0077") {
		t.Errorf("%s sets no UMask=0077; SQLite would create the database world-readable", hubUnit)
	}
}

// spec: deployment.md#the-services — both streams reach the log, and the startup error of a
// missing token arrives on stderr.
func TestThePlistLogsBothStreams(t *testing.T) {
	body := code(t, agentPlist)
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		routed := regexp.MustCompile(`<key>` + key + `</key>\s*<string>/var/log/monitor-agent\.log</string>`)
		if !routed.MatchString(body) {
			t.Errorf("%s does not send %s to /var/log/monitor-agent.log", agentPlist, key)
		}
	}
}

// spec: agent.md#local-configuration — the shipped example is the reference shape of the file
// the agent parses, so the parser is what has to accept it: a comment or a blank line the
// example uses freely and the parser refused would be found by an operator, not by a test.
func TestTheExampleEnvironmentFileParses(t *testing.T) {
	values, err := agent.ReadEnvFile(agentEnv)
	if err != nil {
		t.Fatalf("parse %s: %v", agentEnv, err)
	}
	for _, key := range []string{"MONITOR_HUB", "MONITOR_NODE", "MONITOR_TOKEN"} {
		if values[key] == "" {
			t.Errorf("%s supplies no %s", agentEnv, key)
		}
	}
}
