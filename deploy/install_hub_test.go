// This file drives the real install-hub.sh. Every run is staged under DESTDIR, so the
// machine running the test is never touched and the Debian layout can be asserted from
// either operating system.
package deploy_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const hubScript = "./install-hub.sh"

// hubRun is one invocation of install-hub.sh, staged under destDir.
type hubRun struct {
	destDir string
	args    []string
	pathDir string // prepended to PATH, to catch a service command being run
	umask   string
}

func (r hubRun) start(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	path := os.Getenv("PATH")
	if r.pathDir != "" {
		path = r.pathDir + string(os.PathListSeparator) + path
	}
	command := exec.Command(hubScript, r.args...)
	if r.umask != "" {
		shell := []string{"-c", "umask " + r.umask + `; exec "$0" "$@"`, hubScript}
		command = exec.Command("/bin/sh", append(shell, r.args...)...)
	}
	command.Env = []string{"PATH=" + path, "DESTDIR=" + r.destDir}
	var out, errs bytes.Buffer
	command.Stdout, command.Stderr = &out, &errs
	err = command.Run()
	return out.String(), errs.String(), err
}

func (r hubRun) mustRun(t *testing.T) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, err := r.start(t)
	if err != nil {
		t.Fatalf("install-hub.sh %v: %v\nstdout: %s\nstderr: %s", r.args, err, stdout, stderr)
	}
	return stdout, stderr
}

// hubBinary is what the script copies: a small executable file, not a built hub. Its mode is
// deliberately not the layout's, so a run that merely copied it across would fail.
func hubBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitor-hub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho monitor-hub 1.2.3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func installHub(t *testing.T, destDir string, extra ...string) (stdout, stderr string) {
	t.Helper()
	args := append([]string{"--binary", hubBinary(t)}, extra...)
	return hubRun{destDir: destDir, args: args}.mustRun(t)
}

// hubLayout is the spec's layout table for the hub host, plus the examples this installer
// writes. spec: deployment.md#where-things-live
func hubLayout() []installedFile {
	return []installedFile{
		{"usr/local/bin/monitor-hub", 0o755},
		{"etc/systemd/system/monitor-hub.service", 0o644},
		{"etc/monitor/hub.yaml.example", 0o644},
		{"etc/monitor/hub.env.example", 0o600},
	}
}

func assertHubLayout(t *testing.T, destDir string) {
	t.Helper()
	got := tree(t, destDir)
	if len(got) != len(hubLayout()) {
		t.Errorf("installed %d files, want %d: %v", len(got), len(hubLayout()), sortedKeys(got))
	}
	for _, file := range hubLayout() {
		info, err := os.Stat(filepath.Join(destDir, file.path))
		if err != nil {
			t.Errorf("%s: %v", file.path, err)
			continue
		}
		if info.Mode().Perm() != file.mode {
			t.Errorf("%s has mode %o, want %o", file.path, info.Mode().Perm(), file.mode)
		}
	}
}

// spec: installer.md#installing-the-hub — a host with no hub gets the binary, the service
// definition and the examples, and the run says which configuration is still missing.
func TestAFreshHubInstallWritesTheLayoutAndNamesWhatIsMissing(t *testing.T) {
	destDir := t.TempDir()

	stdout, _ := installHub(t, destDir)

	assertHubLayout(t, destDir)
	for _, want := range []string{"hub.yaml", "hub.env", "install -o monitor -g monitor"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the run does not name %q:\n%s", want, stdout)
		}
	}
}

// spec: installer.md#installing-the-hub — with both configuration files present the run says
// the service is what to look at, not what to create.
func TestAHubInstallWithConfigurationSaysTheServiceIsRunning(t *testing.T) {
	destDir := t.TempDir()
	writeHubConfig(t, destDir)

	stdout, _ := installHub(t, destDir)

	if strings.Contains(stdout, "will not start until") {
		t.Errorf("the run reports missing configuration it was given:\n%s", stdout)
	}
	if !strings.Contains(stdout, "systemctl status monitor-hub.service") {
		t.Errorf("the run does not name the service command:\n%s", stdout)
	}
}

func writeHubConfig(t *testing.T, destDir string) {
	t.Helper()
	dir := filepath.Join(destDir, "etc", "monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{"hub.yaml": 0o640, "hub.env": 0o600} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# the operator's own\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
}

// spec: installer.md#installing-the-hub — a re-run replaces the binary and leaves the
// operator's configuration exactly as it was.
func TestAHubRerunReplacesTheBinaryAndKeepsConfiguration(t *testing.T) {
	destDir := t.TempDir()
	writeHubConfig(t, destDir)
	installHub(t, destDir)

	newer := filepath.Join(t.TempDir(), "monitor-hub")
	if err := os.WriteFile(newer, []byte("#!/bin/sh\necho monitor-hub 9.9.9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	hubRun{destDir: destDir, args: []string{"--binary", newer}}.mustRun(t)

	installed, err := os.ReadFile(filepath.Join(destDir, "usr/local/bin/monitor-hub"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "9.9.9") {
		t.Errorf("the binary was not replaced: %s", installed)
	}
	config, err := os.ReadFile(filepath.Join(destDir, "etc/monitor/hub.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != "# the operator's own\n" {
		t.Errorf("the configuration was rewritten: %s", config)
	}
}

// spec: installer.md#installing-the-hub — a configuration file whose mode is not the
// layout's is corrected, and the run says which.
func TestAHubInstallCorrectsTheModeOfAConfigurationFile(t *testing.T) {
	destDir := t.TempDir()
	writeHubConfig(t, destDir)
	env := filepath.Join(destDir, "etc/monitor/hub.env")
	if err := os.Chmod(env, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _ := installHub(t, destDir)

	info, err := os.Stat(env)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("hub.env has mode %o, want 600", info.Mode().Perm())
	}
	if !strings.Contains(stdout, "hub.env") {
		t.Errorf("the run does not say which file it corrected:\n%s", stdout)
	}
}

// spec: installer.md#staged-installs — a staged run registers nothing: no service command is
// on the PATH it is given, and calling one would be visible.
func TestAStagedHubRunCallsNoServiceCommand(t *testing.T) {
	destDir := t.TempDir()
	pathDir := t.TempDir()
	marker := filepath.Join(pathDir, "called")
	for _, name := range []string{"systemctl", "useradd", "adduser"} {
		stub := filepath.Join(pathDir, name)
		body := "#!/bin/sh\necho " + name + " \"$@\" >> " + marker + "\n"
		if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	hubRun{destDir: destDir, args: []string{"--binary", hubBinary(t)}, pathDir: pathDir}.mustRun(t)

	if body, err := os.ReadFile(marker); err == nil {
		t.Errorf("a staged run called: %s", body)
	}
}

// spec: installer.md#installing-the-hub — a directory the layout owns must not be a symlink
// somebody else planted, and a run that meets one writes nothing.
func TestAHubInstallRefusesASymlinkedDirectory(t *testing.T) {
	destDir := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(destDir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(destDir, "etc", "monitor")); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := hubRun{destDir: destDir, args: []string{"--binary", hubBinary(t)}}.start(t)

	if err == nil {
		t.Fatal("the run succeeded; a symlinked directory had to stop it")
	}
	if !strings.Contains(stderr, "symlink") {
		t.Errorf("the refusal does not name the cause:\n%s", stderr)
	}
}

// spec: installer.md#staged-installs — DESTDIR is a staging prefix, and a value that stages
// nothing is refused rather than quietly writing into the real system.
func TestAHubInstallRefusesADestdirThatStagesNothing(t *testing.T) {
	for _, destDir := range []string{"/", "//", "relative/path", "/.", "/etc/..", "/tmp/../"} {
		t.Run(destDir, func(t *testing.T) {
			_, stderr, err := hubRun{destDir: destDir, args: []string{"--binary", hubBinary(t)}}.start(t)

			if err == nil {
				t.Fatal("the run succeeded; this DESTDIR had to stop it")
			}
			if !strings.Contains(stderr, "DESTDIR") {
				t.Errorf("the refusal does not name DESTDIR:\n%s", stderr)
			}
		})
	}
}

// spec: installer.md#installing-the-hub — the refusals a run makes before it writes anything.
func TestAHubInstallRefusalsWriteNothing(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		names string
	}{
		{"no binary", nil, "--binary"},
		{"a binary that is not there", []string{"--binary", "/no/such/hub"}, "no file"},
		{"an unknown option", []string{"--binary", "x", "--wat"}, "--wat"},
		{"an option without its value", []string{"--binary"}, "--binary"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			destDir := t.TempDir()

			_, stderr, err := hubRun{destDir: destDir, args: test.args}.start(t)

			if err == nil {
				t.Fatal("the run succeeded; it had to refuse")
			}
			if !strings.Contains(stderr, test.names) {
				t.Errorf("the refusal does not name %q:\n%s", test.names, stderr)
			}
			if files := tree(t, destDir); len(files) != 0 {
				t.Errorf("a refusal wrote files: %v", sortedKeys(files))
			}
		})
	}
}

// spec: installer.md#installing-the-hub — the examples are the release's, so a run replaces
// them however they were edited, and the operator's own files are never touched.
func TestAHubInstallOverwritesTheExamples(t *testing.T) {
	destDir := t.TempDir()
	installHub(t, destDir)
	example := filepath.Join(destDir, "etc/monitor/hub.env.example")
	if err := os.WriteFile(example, []byte("edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	installHub(t, destDir)

	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "edited by hand") {
		t.Errorf("the example was not replaced: %s", body)
	}
}

// spec: installer.md#installing-the-hub — the directories the layout names are created, and
// the database's is one of them.
func TestAHubInstallCreatesTheDataDirectory(t *testing.T) {
	destDir := t.TempDir()

	installHub(t, destDir)

	info, err := os.Stat(filepath.Join(destDir, "var/lib/monitor"))
	if err != nil {
		t.Fatalf("the data directory is missing: %v", err)
	}
	if !info.IsDir() {
		t.Error("the data directory is not a directory")
	}
}

// spec: deployment.md#where-things-live — the modes are the table's, whatever umask the
// operator happens to be running under.
func TestTheHubModesDoNotFollowTheCallersUmask(t *testing.T) {
	destDir := t.TempDir()

	hubRun{destDir: destDir, args: []string{"--binary", hubBinary(t)}, umask: "000"}.mustRun(t)

	assertHubLayout(t, destDir)
}

// spec: installer.md#fetching-and-checking-a-release — -h is a request here too: the usage
// goes to stdout and the run succeeds.
func TestHubInstallerHelpGoesToStdout(t *testing.T) {
	stdout, stderr, err := hubRun{destDir: t.TempDir(), args: []string{"-h"}}.start(t)
	if err != nil {
		t.Fatalf("-h failed: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "usage:") || stderr != "" {
		t.Errorf("the usage is not on stdout alone:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}
