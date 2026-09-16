// This file drives the real install-hub.sh as a follow run hands over to it: the target on
// the staged host decides whether it installs, asks for its release's binary, finds nothing
// to do, or answers with the version it needs.
package deploy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// followHub is one follow hand-over to install-hub.sh, staged under destDir. An empty target
// leaves no target file at all. binary is the release's hub binary, and digest its SHA-256.
type followHub struct {
	destDir string
	target  string
	release string
	newest  string
	answer  string
	binary  string
	digest  string
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
	f.digest = fileDigest(t, f.binary)
	if err := os.WriteFile(f.answer, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		writeTarget(t, f.destDir, target)
	}
	return f
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
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

// args is the first hand-over of a follow run: the five options and no binary.
func (f followHub) args() []string {
	return []string{"--follow-target", "--release", f.release, "--newest", f.newest, "--digest", f.digest, "--answer", f.answer}
}

// withBinary is the hand-over that follows an installer asking for its release's binary.
func (f followHub) withBinary() []string {
	return append(f.args(), "--binary", f.binary)
}

func (f followHub) start(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	if args == nil {
		args = f.args()
	}
	return hubRun{destDir: f.destDir, args: args}.start(t)
}

func (f followHub) mustStart(t *testing.T, args ...string) (stdout string) {
	t.Helper()
	stdout, stderr, err := f.start(t, args...)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}
	return stdout
}

func (f followHub) answered(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// installed puts the release's hub in place the way an operator's run does.
func (f followHub) installed(t *testing.T) {
	t.Helper()
	hubRun{destDir: f.destDir, args: []string{"--binary", f.binary}}.mustRun(t)
}

// spec: installer.md#answering-a-follow-run — a target that resolves to the release handed
// over, with that release's binary, installs it, and the answer stays empty.
func TestAFollowRunInstallsTheReleaseItsTargetNames(t *testing.T) {
	for _, target := range []string{"1.2.3\n", "1.2.3"} {
		t.Run(strings.TrimSpace(target), func(t *testing.T) {
			f := newFollowHub(t, target, "1.2.3", "1.3.0")

			f.mustStart(t, f.withBinary()...)

			assertHubLayout(t, f.destDir, "etc/monitor/hub.target")
			if got := f.answered(t); got != "" {
				t.Errorf("the answer holds %q; an installer that installs leaves it empty", got)
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run — without the binary, a target that resolves to
// the release handed over asks for that release's binary, and nothing is written.
func TestAFollowRunAsksForItsOwnReleasesBinary(t *testing.T) {
	f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.3.0")

	f.mustStart(t)

	if got := strings.TrimSpace(f.answered(t)); got != "1.2.3" {
		t.Errorf("the answer holds %q, want 1.2.3", got)
	}
	if files := sortedKeys(tree(t, f.destDir)); len(files) != 1 {
		t.Errorf("an answering run wrote files: %v", files)
	}
}

// spec: installer.md#answering-a-follow-run — target latest resolves to --newest.
func TestAFollowRunResolvesLatestToTheNewestRelease(t *testing.T) {
	for _, test := range []struct {
		name   string
		newest string
		answer string
	}{
		{"the newest is this release", "1.2.3", "1.2.3"},
		{"the newest is another release", "1.3.0", "1.3.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFollowHub(t, "latest\n", "1.2.3", test.newest)

			f.mustStart(t)

			if got := strings.TrimSpace(f.answered(t)); got != test.answer {
				t.Errorf("the answer holds %q, want %s", got, test.answer)
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run — a target naming another release is answered
// with that version, and nothing is written but the answer.
func TestAFollowRunAnswersWithTheVersionItsTargetNames(t *testing.T) {
	f := newFollowHub(t, "1.0.0\n", "1.2.3", "1.2.3")

	f.mustStart(t)

	if got := strings.TrimSpace(f.answered(t)); got != "1.0.0" {
		t.Errorf("the answer holds %q, want 1.0.0", got)
	}
	if files := sortedKeys(tree(t, f.destDir)); len(files) != 1 {
		t.Errorf("an answering run wrote files: %v", files)
	}
}

// spec: installer.md#answering-a-follow-run — a hub already running the release is left
// alone, told by its digest alone: nothing is written, nothing is asked, and the run says why.
// spec: installer.md#staged-installs — the target and the binary in place are read from under
// DESTDIR.
func TestAFollowRunLeavesAnUnchangedHubAlone(t *testing.T) {
	f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.2.3")
	f.installed(t)
	example := filepath.Join(f.destDir, "etc/monitor/hub.env.example")
	// An install run would overwrite this, so it surviving shows nothing was written.
	if err := os.WriteFile(example, []byte("edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := f.mustStart(t)

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

// spec: installer.md#answering-a-follow-run — a hub whose binary in place is the release's but
// whose service definition differs keeps that binary: nothing is asked, and the rest of the
// hub is installed around it.
func TestAFollowRunKeepsTheReleasesBinaryInPlace(t *testing.T) {
	f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.2.3")
	f.installed(t)
	binary := filepath.Join(f.destDir, "usr/local/bin/monitor-hub")
	before, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(f.destDir, "etc/systemd/system/monitor-hub.service")
	if err := os.WriteFile(unit, []byte("# an older unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := f.mustStart(t)

	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q; a binary in place that is the release's needs no download", got)
	}
	if !strings.Contains(stdout, "keeping the binary in place") {
		t.Errorf("the run does not say it keeps the binary in place:\n%s", stdout)
	}
	if strings.Contains(stdout, "/usr/local/bin/monitor-hub") {
		t.Errorf("the run names the binary among the paths it wrote:\n%s", stdout)
	}
	assertHubLayout(t, f.destDir, "etc/monitor/hub.target")
	if body, _ := os.ReadFile(unit); string(body) == "# an older unit\n" {
		t.Error("the service definition was not installed")
	}
	after, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("the binary in place was replaced")
	}
}

// spec: installer.md#answering-a-follow-run — a binary in place that is not the release's asks
// for its binary rather than being kept, and the binary then finishes the hub.
func TestAFollowRunAsksAgainForAnUnfinishedHub(t *testing.T) {
	for _, test := range []struct {
		name  string
		spoil func(t *testing.T, destDir string)
	}{
		{"a binary its group may write to", func(t *testing.T, destDir string) {
			chmod(t, filepath.Join(destDir, "usr/local/bin/monitor-hub"), 0o775)
		}},
		{"a symlink to the release's binary", func(t *testing.T, destDir string) {
			path := filepath.Join(destDir, "usr/local/bin/monitor-hub")
			elsewhere := filepath.Join(t.TempDir(), "monitor-hub")
			if err := os.Rename(path, elsewhere); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"another binary", func(t *testing.T, destDir string) {
			path := filepath.Join(destDir, "usr/local/bin/monitor-hub")
			if err := os.WriteFile(path, []byte("#!/bin/sh\necho monitor-hub 1.2.2\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFollowHub(t, "1.2.3\n", "1.2.3", "1.2.3")
			f.installed(t)
			test.spoil(t, f.destDir)

			stdout := f.mustStart(t)
			if strings.Contains(stdout, "already") {
				t.Errorf("the run took an unfinished hub for up to date:\n%s", stdout)
			}
			if got := strings.TrimSpace(f.answered(t)); got != "1.2.3" {
				t.Fatalf("the answer holds %q, want 1.2.3", got)
			}

			if err := os.WriteFile(f.answer, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			f.mustStart(t, f.withBinary()...)
			assertHubLayout(t, f.destDir, "etc/monitor/hub.target")
			if got := fileDigest(t, filepath.Join(f.destDir, "usr/local/bin/monitor-hub")); got != f.digest {
				t.Error("the binary handed over is not the one in place")
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run — every way a follow hand-over is refused, none
// of which installs or answers anything.
func TestAFollowRunRefuses(t *testing.T) {
	without := func(option string) func(f followHub) []string {
		return func(f followHub) []string {
			args := f.args()
			for i, arg := range args {
				if arg != option {
					continue
				}
				if option == "--follow-target" {
					return append(args[:i:i], args[i+1:]...)
				}
				return append(args[:i:i], args[i+2:]...)
			}
			return args
		}
	}
	with := func(option, value string) func(f followHub) []string {
		return func(f followHub) []string {
			args := f.args()
			for i, arg := range args {
				if arg == option {
					args[i+1] = value
				}
			}
			return args
		}
	}

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
		{name: "no --follow-target", target: "latest\n", args: without("--follow-target"), names: "--follow-target"},
		{name: "no --release", target: "latest\n", args: without("--release"), names: "--release"},
		{name: "no --newest", target: "latest\n", args: without("--newest"), names: "--newest"},
		{name: "no --digest", target: "latest\n", args: without("--digest"), names: "needs --digest"},
		{
			name:   "a --binary naming no file",
			target: "1.2.3\n",
			args: func(f followHub) []string {
				return append(f.args(), "--binary", filepath.Join(f.destDir, "no-such-hub"))
			},
			names: "no file",
		},
		{name: "no --answer", target: "latest\n", args: without("--answer"), names: "--answer"},
		{name: "a --release that is not a version", target: "latest\n", args: with("--release", "v1.2.3"), names: "v1.2.3"},
		{name: "a --newest that is not a version", target: "latest\n", args: with("--newest", "v1.3.0"), names: "v1.3.0"},
		{name: "a --digest that is too short", target: "latest\n", args: with("--digest", "abc123"), names: "abc123"},
		{
			name:   "a --digest in capitals",
			target: "latest\n",
			args:   with("--digest", strings.Repeat("A", 64)),
			names:  strings.Repeat("A", 64),
		},
		{
			name:   "an --answer naming no file",
			target: "latest\n",
			args: func(f followHub) []string {
				return with("--answer", f.answer+".missing")(f)
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
