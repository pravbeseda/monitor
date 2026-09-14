// This file drives the real install-hub.sh as a follow run hands over to it: the target on
// the staged host decides whether it installs or answers with the version it needs.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// followHub is one follow hand-over to install-hub.sh, staged under destDir. An empty target
// leaves no target file at all.
type followHub struct {
	destDir string
	target  string
	release string
	newest  string
	answer  string
	binary  string
}

func newFollowHub(t *testing.T, target, release, newest string) followHub {
	t.Helper()
	f := followHub{
		destDir: t.TempDir(),
		target:  target,
		release: release,
		newest:  newest,
		answer:  filepath.Join(t.TempDir(), "answer"),
		binary:  hubBinary(t),
	}
	if err := os.WriteFile(f.answer, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		writeTarget(t, f.destDir, target)
	}
	return f
}

func writeTarget(t *testing.T, destDir, body string) {
	t.Helper()
	dir := filepath.Join(destDir, "etc", "monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.target"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f followHub) args() []string {
	return []string{"--binary", f.binary, "--follow-target", "--release", f.release, "--newest", f.newest, "--answer", f.answer}
}

func (f followHub) start(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	if args == nil {
		args = f.args()
	}
	return hubRun{destDir: f.destDir, args: args}.start(t)
}

func (f followHub) answered(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// spec: installer.md#answering-a-follow-run — a target that resolves to the release handed
// over installs it, and the answer stays empty.
func TestAFollowRunInstallsTheReleaseItsTargetNames(t *testing.T) {
	for _, target := range []string{"1.2.3\n", "1.2.3"} {
		t.Run(strings.TrimSpace(target), func(t *testing.T) {
			f := newFollowHub(t, target, "1.2.3", "1.3.0")

			stdout, stderr, err := f.start(t)
			if err != nil {
				t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
			}

			assertHubLayout(t, f.destDir, "etc/monitor/hub.target")
			if got := f.answered(t); got != "" {
				t.Errorf("the answer holds %q; an installer that installs leaves it empty", got)
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run — target latest resolves to --newest.
func TestAFollowRunResolvesLatestToTheNewestRelease(t *testing.T) {
	t.Run("the newest is this release", func(t *testing.T) {
		f := newFollowHub(t, "latest\n", "1.2.3", "1.2.3")

		stdout, stderr, err := f.start(t)
		if err != nil {
			t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(f.destDir, "usr/local/bin/monitor-hub")); err != nil {
			t.Errorf("the hub was not installed: %v", err)
		}
	})

	t.Run("the newest is another release", func(t *testing.T) {
		f := newFollowHub(t, "latest\n", "1.2.3", "1.3.0")

		stdout, stderr, err := f.start(t)
		if err != nil {
			t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
		}
		if got := strings.TrimSpace(f.answered(t)); got != "1.3.0" {
			t.Errorf("the answer holds %q, want 1.3.0", got)
		}
	})
}

// spec: installer.md#answering-a-follow-run — a target naming another release is answered
// with that version, and nothing is written but the answer.
func TestAFollowRunAnswersWithTheVersionItsTargetNames(t *testing.T) {
	f := newFollowHub(t, "1.0.0\n", "1.2.3", "1.2.3")

	stdout, stderr, err := f.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	if got := strings.TrimSpace(f.answered(t)); got != "1.0.0" {
		t.Errorf("the answer holds %q, want 1.0.0", got)
	}
	if files := sortedKeys(tree(t, f.destDir)); len(files) != 1 {
		t.Errorf("an answering run wrote files: %v", files)
	}
}

// spec: installer.md#answering-a-follow-run — a hub already at the target's binary is left
// alone: nothing is written, and the run says why.
// spec: installer.md#staged-installs — the target and the binary in place are read from under
// DESTDIR.
func TestAFollowRunLeavesAnUnchangedHubAlone(t *testing.T) {
	f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.2.3")
	hubRun{destDir: f.destDir, args: []string{"--binary", f.binary}}.mustRun(t)
	example := filepath.Join(f.destDir, "etc/monitor/hub.env.example")
	// An install run would overwrite this, so it surviving shows nothing was written.
	if err := os.WriteFile(example, []byte("edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := f.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "already") {
		t.Errorf("the run does not say the hub is already at that version:\n%s", stdout)
	}
	if body, _ := os.ReadFile(example); string(body) != "edited by hand\n" {
		t.Errorf("an unchanged hub was installed over: the example now holds %q", body)
	}
	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q; nothing further is needed", got)
	}
}

// spec: installer.md#answering-a-follow-run — identical binary bytes are not enough: a service
// definition that differs from the release's is installed over, as a run stopped half-way
// would leave it.
func TestAFollowRunInstallsOverAnUnfinishedHub(t *testing.T) {
	f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.2.3")
	hubRun{destDir: f.destDir, args: []string{"--binary", f.binary}}.mustRun(t)
	service := filepath.Join(f.destDir, "etc/systemd/system/monitor-hub.service")
	if err := os.WriteFile(service, []byte("# an older unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := f.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	if strings.Contains(stdout, "already") {
		t.Errorf("the run skipped a hub whose service definition is not the release's:\n%s", stdout)
	}
	if body, _ := os.ReadFile(service); string(body) == "# an older unit\n" {
		t.Error("the service definition was not replaced")
	}
}

// spec: installer.md#answering-a-follow-run — every way a follow hand-over is refused, none
// of which installs or answers anything.
func TestAFollowRunRefuses(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		prepare func(t *testing.T, f *followHub)
		args    func(f followHub) []string
		names   string
	}{
		{name: "no target file", names: "hub.target"},
		{name: "an empty target", target: "\n", names: "hub.target"},
		{name: "a target with a second line", target: "latest\n\n", names: "hub.target"},
		{name: "a target padded with a blank", target: "latest \n", names: "hub.target"},
		{name: "a target that is not a version", target: "1.2\n", names: "hub.target"},
		{name: "a target that is neither", target: "stable\n", names: "hub.target"},
		{
			name:   "a target that is a symlink",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				path := filepath.Join(f.destDir, "etc/monitor/hub.target")
				elsewhere := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(elsewhere, []byte("latest\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(elsewhere, path); err != nil {
					t.Fatal(err)
				}
			},
			names: "hub.target",
		},
		{
			name:   "a target writable by its group",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				chmod(t, filepath.Join(f.destDir, "etc/monitor/hub.target"), 0o664)
			},
			names: "hub.target",
		},
		{
			name:   "a target writable by anyone",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				chmod(t, filepath.Join(f.destDir, "etc/monitor/hub.target"), 0o646)
			},
			names: "hub.target",
		},
		{
			name:   "a target in a directory anyone may write to",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				chmod(t, filepath.Join(f.destDir, "etc/monitor"), 0o777)
			},
			names: "/etc/monitor",
		},
		{
			name:   "a target in a directory its group may write to",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				chmod(t, filepath.Join(f.destDir, "etc/monitor"), 0o775)
			},
			names: "/etc/monitor",
		},
		{
			name:   "no --newest",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--release", f.release, "--answer", f.answer}
			},
			names: "--newest",
		},
		{
			name:   "no --answer",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--release", f.release, "--newest", f.newest}
			},
			names: "--answer",
		},
		{
			name:   "a --release that is not a version",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--release", "v1.2.3", "--newest", f.newest, "--answer", f.answer}
			},
			names: "v1.2.3",
		},
		{
			name:   "no --release",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--newest", f.newest, "--answer", f.answer}
			},
			names: "--release",
		},
		{
			name:   "no --follow-target",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--release", f.release, "--newest", f.newest, "--answer", f.answer}
			},
			names: "--follow-target",
		},
		{
			name:   "a --newest that is not a version",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--release", f.release, "--newest", "v1.3.0", "--answer", f.answer}
			},
			names: "v1.3.0",
		},
		{
			name:   "an --answer naming no file",
			target: "latest\n",
			args: func(f followHub) []string {
				return []string{"--binary", f.binary, "--follow-target", "--release", f.release, "--newest", f.newest, "--answer", f.answer + ".missing"}
			},
			names: "--answer",
		},
		{
			name:   "an --answer that is not empty",
			target: "latest\n",
			prepare: func(t *testing.T, f *followHub) {
				if err := os.WriteFile(f.answer, []byte("1.0.0\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			names: "--answer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFollowHub(t, test.target, "1.2.3", "1.3.0")
			if test.prepare != nil {
				test.prepare(t, &f)
			}
			before := sortedKeys(tree(t, f.destDir))
			answerBefore := f.answered(t)
			var args []string
			if test.args != nil {
				args = test.args(f)
			}

			stdout, stderr, err := f.start(t, args...)

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, test.names) {
				t.Errorf("the refusal does not name %q:\n%s", test.names, stderr)
			}
			if after := sortedKeys(tree(t, f.destDir)); strings.Join(after, " ") != strings.Join(before, " ") {
				t.Errorf("a refusal wrote files: %v", after)
			}
			if got := f.answered(t); got != answerBefore {
				t.Errorf("a refusal answered %q", got)
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run — a target whose directory is a symlink is refused
// like one that is a symlink itself. Apart from the table above, because the tree it compares
// cannot be walked through the link.
func TestAFollowRunRefusesATargetInASymlinkedDirectory(t *testing.T) {
	f := newFollowHub(t, "latest\n", "1.2.3", "1.3.0")
	configDir := filepath.Join(f.destDir, "etc", "monitor")
	moved := filepath.Join(t.TempDir(), "monitor")
	if err := os.Rename(configDir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, configDir); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := f.start(t)

	if err == nil {
		t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "symlink") || !strings.Contains(stderr, "etc/monitor") {
		t.Errorf("the refusal does not name the symlinked directory:\n%s", stderr)
	}
	if got := f.answered(t); got != "" {
		t.Errorf("a refusal answered %q", got)
	}
	if entries, _ := os.ReadDir(moved); len(entries) != 1 {
		t.Errorf("a refusal wrote beside the target: %v", entries)
	}
}

// spec: installer.md#answering-a-follow-run — the installer and the kept script judge a
// version by one grammar; the two copies of it cannot share a file, so they are held equal.
func TestBothScriptsJudgeAVersionAlike(t *testing.T) {
	function := func(file string) string {
		body := read(t, file)
		start := strings.Index(body, "\nis_version() {\n")
		if start < 0 {
			t.Fatalf("%s defines no is_version", file)
		}
		end := strings.Index(body[start:], "\n}\n")
		return body[start : start+end]
	}
	if function(bootstrap) != function(hubScript) {
		t.Errorf("is_version differs between %s and %s", bootstrap, hubScript)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
