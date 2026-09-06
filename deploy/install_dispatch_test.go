// This file drives the real install.sh, the entry point a release carries. The per-role
// installers it hands over to are recorded rather than run, because what is under test is
// the handover itself.
package deploy_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const dispatcher = "./install.sh"

// recordingRelease is an unpacked release whose per-role installers write down how they were
// called, so the contract between the two halves is observable.
func recordingRelease(t *testing.T) (dir, record string) {
	t.Helper()
	dir = t.TempDir()
	record = filepath.Join(dir, "record")
	source, err := os.ReadFile(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), source, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"hub", "agent"} {
		body := "#!/bin/sh\nprintf '%s args: %s\\n' " + role + " \"$*\" >> " + record + "\n" +
			"printf 'stdin: %s\\n' \"$(cat)\" >> " + record + "\n" +
			"printf 'destdir: %s\\n' \"${DESTDIR:-}\" >> " + record + "\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, "install-"+role+".sh"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, record
}

func dispatch(t *testing.T, dir, stdin string, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	command := exec.Command("sh", append([]string{filepath.Join(dir, "install.sh")}, args...)...)
	command.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	command.Stdin = strings.NewReader(stdin)
	var out, errs bytes.Buffer
	command.Stdout, command.Stderr = &out, &errs
	err = command.Run()
	return out.String(), errs.String(), err
}

// spec: installer.md#the-handover — the role chooses the installer, the arguments reach it
// unchanged, the token arrives on its stdin and DESTDIR is inherited.
func TestTheDispatcherHandsOverWhatItWasGiven(t *testing.T) {
	dir, record := recordingRelease(t)

	_, stderr, err := dispatch(t, dir, "token-aaaa", []string{"DESTDIR=/staged"},
		"agent", "--binary", "/tmp/monitor-agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	if err != nil {
		t.Fatalf("the dispatcher failed: %v\n%s", err, stderr)
	}

	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"agent args: --binary /tmp/monitor-agent --hub https://hub.example.com --node laptop-a",
		"stdin: token-aaaa",
		"destdir: /staged",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the handover does not carry %q:\n%s", want, body)
		}
	}
}

// spec: installer.md#the-handover — the run exits with the installer's status.
func TestTheDispatcherExitsWithTheInstallersStatus(t *testing.T) {
	dir, _ := recordingRelease(t)
	failing := "#!/bin/sh\necho 'the hub installer refused' >&2\nexit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "install-hub.sh"), []byte(failing), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := dispatch(t, dir, "", nil, "hub", "--binary", "/tmp/monitor-hub")

	var exit *exec.ExitError
	if !asExitError(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("exit status = %v, want 3", err)
	}
	if !strings.Contains(stderr, "the hub installer refused") {
		t.Errorf("the installer's own message did not come through:\n%s", stderr)
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// spec: installer.md#the-handover — a role this release cannot install, and a release that
// carries no installer for one, are both refusals rather than a partial run.
func TestTheDispatcherRefuses(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		prepare func(t *testing.T, dir string)
		names   string
	}{
		{name: "no role", names: "usage"},
		{name: "an unknown role", args: []string{"gateway"}, names: "unknown role"},
		{
			name: "a release with no installer for the role",
			args: []string{"hub", "--binary", "/tmp/monitor-hub"},
			prepare: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "install-hub.sh")); err != nil {
					t.Fatal(err)
				}
			},
			names: "carries no installer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, record := recordingRelease(t)
			if test.prepare != nil {
				test.prepare(t, dir)
			}

			_, stderr, err := dispatch(t, dir, "", nil, test.args...)

			if err == nil {
				t.Fatal("the run succeeded; it had to refuse")
			}
			if !strings.Contains(stderr, test.names) {
				t.Errorf("the refusal does not say %q:\n%s", test.names, stderr)
			}
			if _, err := os.Stat(record); err == nil {
				t.Error("a refusal still handed over to an installer")
			}
		})
	}
}
