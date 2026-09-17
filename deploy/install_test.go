// This file drives the real install-agent.sh. Every run is staged under DESTDIR, so the
// machine running the test is never touched, and the layout asserted is the host's own —
// CI runs the suite on Debian and on macOS, which is what exercises both branches.
package deploy_test

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/agent"
)

const script = "./install-agent.sh"

// Synthetic throughout (ADR 0007): no run here names a real node or token. The only URL any
// file in this directory may carry is exampleHub, from deploy_test.go.
const (
	testNode  = "laptop-a"
	testToken = "token-aaaa"
)

// installedFile is one file the script writes and the mode the layout table gives it.
type installedFile struct {
	path string // relative to DESTDIR
	mode os.FileMode
}

// nodeLayout is the spec's layout table for one operating system.
type nodeLayout struct {
	binary  installedFile
	env     installedFile
	service installedFile
	log     *installedFile // launchd only: nil on Debian
	status  string         // the command a successful run points at
}

func (l nodeLayout) files() []installedFile {
	files := []installedFile{l.binary, l.env, l.service}
	if l.log != nil {
		files = append(files, *l.log)
	}
	return files
}

// spec: deployment.md#where-things-live — the paths and modes, read for the system the test
// is running on.
func hostLayout() nodeLayout {
	if runtime.GOOS == "darwin" {
		log := installedFile{"var/log/monitor-agent.log", 0o600}
		return nodeLayout{
			binary:  installedFile{"usr/local/bin/monitor-agent", 0o755},
			env:     installedFile{"usr/local/etc/monitor/agent.env", 0o600},
			service: installedFile{"Library/LaunchDaemons/io.github.pravbeseda.monitor-agent.plist", 0o644},
			log:     &log,
			status:  "launchctl print system/io.github.pravbeseda.monitor-agent",
		}
	}
	return nodeLayout{
		binary:  installedFile{"usr/local/bin/monitor-agent", 0o755},
		env:     installedFile{"etc/monitor/agent.env", 0o600},
		service: installedFile{"etc/systemd/system/monitor-agent.service", 0o644},
		status:  "systemctl status monitor-agent.service",
	}
}

// run is one invocation of the script, staged under destDir.
type run struct {
	destDir string
	args    []string
	stdin   string // what the token arrives on when it comes that way
	token   string // MONITOR_TOKEN in the environment; empty leaves it unset
	pathDir string // prepended to PATH, to catch a service command being run
	onlyDir bool   // PATH is pathDir alone, so what is missing from it is really missing
	umask   string // the caller's umask, when the run has to survive a hostile one
}

func (r run) start(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	path := os.Getenv("PATH")
	switch {
	case r.onlyDir:
		path = r.pathDir
	case r.pathDir != "":
		path = r.pathDir + string(os.PathListSeparator) + path
	}
	cmd := exec.Command(script, r.args...)
	if r.umask != "" {
		// exec keeps $0 as the script, so the arguments below still land on it.
		shell := []string{"-c", "umask " + r.umask + `; exec "$0" "$@"`, script}
		cmd = exec.Command("/bin/sh", append(shell, r.args...)...)
	}
	cmd.Env = []string{"PATH=" + path, "DESTDIR=" + r.destDir}
	if r.token != "" {
		cmd.Env = append(cmd.Env, "MONITOR_TOKEN="+r.token)
	}
	cmd.Stdin = strings.NewReader(r.stdin)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err = cmd.Run()
	return out.String(), errs.String(), err
}

// mustRun fails the test unless the run succeeds.
func (r run) mustRun(t *testing.T) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, err := r.start(t)
	if err != nil {
		t.Fatalf("install-agent.sh %v: %v\nstdout: %s\nstderr: %s", r.args, err, stdout, stderr)
	}
	return stdout, stderr
}

// agentBinary is what the script copies: a small executable file, not a built agent. Its
// mode is deliberately not the 0755 the layout table asks for, so that a run which merely
// copied the source's mode across would fail the layout assertion.
func agentBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitor-agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	return path
}

// install is the ordinary invocation: the three flags, the token on stdin.
func install(t *testing.T, destDir, binary, hub, node, token string) (stdout, stderr string) {
	t.Helper()
	return run{
		destDir: destDir,
		args:    []string{"--binary", binary, "--hub", hub, "--node", node},
		stdin:   token,
	}.mustRun(t)
}

// tree is every file below dir, keyed by its path relative to dir.
func tree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	found := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		found[rel] = body
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

// assertLayout checks that the run produced exactly the layout table's files, each with the
// mode the table gives it.
func assertLayout(t *testing.T, destDir string) {
	t.Helper()
	want := hostLayout().files()
	got := tree(t, destDir)
	if len(got) != len(want) {
		t.Errorf("installed %d files, want %d: %v", len(got), len(want), sortedKeys(got))
	}
	for _, file := range want {
		if _, ok := got[file.path]; !ok {
			t.Errorf("%s was not installed; the tree holds %v", file.path, sortedKeys(got))
			continue
		}
		info, err := os.Stat(filepath.Join(destDir, file.path))
		if err != nil {
			t.Fatalf("stat %s: %v", file.path, err)
		}
		if info.Mode().Perm() != file.mode {
			t.Errorf("%s has mode %04o, want %04o", file.path, info.Mode().Perm(), file.mode)
		}
	}
}

func sortedKeys(files map[string][]byte) []string {
	return slices.Sorted(maps.Keys(files))
}

// installedEnv is the environment file the run wrote, through the parser the agent uses.
func installedEnv(t *testing.T, destDir string) map[string]string {
	t.Helper()
	values, err := agent.ReadEnvFile(filepath.Join(destDir, hostLayout().env.path))
	if err != nil {
		t.Fatalf("parse the installed environment file: %v", err)
	}
	return values
}

func assertEnv(t *testing.T, destDir, hub, node, token string) {
	t.Helper()
	values := installedEnv(t, destDir)
	for key, want := range map[string]string{
		"MONITOR_HUB":   hub,
		"MONITOR_NODE":  node,
		"MONITOR_TOKEN": token,
	} {
		if values[key] != want {
			t.Errorf("the environment file has %s=%q, want %q", key, values[key], want)
		}
	}
}

// spec: deployment.md#installing-on-a-fresh-node — the binary lands executable, the
// environment file holds the three values and is readable by its owner only, in the layout
// the host's own system fixes. The token arrives on stdin, and on MONITOR_TOKEN.
func TestAFreshInstallWritesTheLayoutTheSpecFixes(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		token string
	}{
		{"token on stdin", testToken, ""},
		{"token in the environment", "", testToken},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir, binary := t.TempDir(), agentBinary(t)
			stdout, stderr := run{
				destDir: destDir,
				args:    []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
				stdin:   tc.stdin,
				token:   tc.token,
			}.mustRun(t)

			assertLayout(t, destDir)
			assertEnv(t, destDir, exampleHub, testNode, testToken)

			shipped := []byte(read(t, binary))
			if installed := tree(t, destDir)[hostLayout().binary.path]; !bytes.Equal(installed, shipped) {
				t.Errorf("the installed binary differs from the one --binary named")
			}
			assertNoToken(t, stdout, stderr)
		})
	}
}

// spec: deployment.md#installing-on-a-fresh-node — the token appears in no message. It is
// not an argument either, which is why the script offers no flag for it.
func assertNoToken(t *testing.T, streams ...string) {
	t.Helper()
	for _, stream := range streams {
		if strings.Contains(stream, testToken) {
			t.Errorf("a stream carries the token: %s", stream)
		}
	}
}

// spec: deployment.md#installing-on-a-fresh-node — any successful run prints every path it
// wrote and the command that shows the service's state.
func TestASuccessfulRunPrintsWhatItWroteAndHowToSeeTheService(t *testing.T) {
	destDir := t.TempDir()
	stdout, _ := install(t, destDir, agentBinary(t), exampleHub, testNode, testToken)

	for _, file := range hostLayout().files() {
		if !strings.Contains(stdout, filepath.Join(destDir, file.path)) {
			t.Errorf("the run does not print %s; it printed:\n%s", file.path, stdout)
		}
	}
	if !strings.Contains(stdout, hostLayout().status) {
		t.Errorf("the run does not print %q; it printed:\n%s", hostLayout().status, stdout)
	}
	// spec: deployment.md#staged-installs — and it says which of the two runs this was, so
	// that a stray DESTDIR cannot pass for an installed node.
	if !strings.Contains(stdout, "no service was registered") {
		t.Errorf("a staged run does not say so; it printed:\n%s", stdout)
	}
}

// spec: deployment.md#re-running — a run with the same arguments and the same binary leaves
// every installed file byte-identical. Compared by content, because a copy is what a
// timestamp would show changing.
func TestASecondRunWithTheSameInputsChangesNothing(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)
	before := tree(t, destDir)

	install(t, destDir, binary, exampleHub, testNode, testToken)

	after := tree(t, destDir)
	if len(before) != len(after) {
		t.Fatalf("the second run left %v, want %v", sortedKeys(after), sortedKeys(before))
	}
	for name, body := range before {
		if !bytes.Equal(after[name], body) {
			t.Errorf("%s changed on a second run with the same inputs", name)
		}
	}
	assertLayout(t, destDir)
}

// spec: deployment.md#re-running — a run with a newer binary replaces the installed one.
// The restart that follows is a service command, so a staged run cannot show it
// (deployment.md#staged-installs).
func TestARerunReplacesTheBinary(t *testing.T) {
	destDir := t.TempDir()
	install(t, destDir, agentBinary(t), exampleHub, testNode, testToken)

	newer := filepath.Join(t.TempDir(), "monitor-agent")
	if err := os.WriteFile(newer, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write the newer binary: %v", err)
	}
	install(t, destDir, newer, exampleHub, testNode, testToken)

	want := []byte(read(t, newer))
	if got := tree(t, destDir)[hostLayout().binary.path]; !bytes.Equal(got, want) {
		t.Errorf("the re-run did not replace the installed binary")
	}
	assertLayout(t, destDir)
}

// spec: deployment.md#re-running — a run with a different --hub or --node carries the new
// value into the environment file.
func TestARerunWritesADifferentHubOrNodeThrough(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	install(t, destDir, binary, "https://other.example.com", "server-b", testToken)

	assertEnv(t, destDir, "https://other.example.com", "server-b", testToken)
	assertLayout(t, destDir)
}

// spec: deployment.md#re-running — a run whose token differs replaces the stored one, and
// the file stays readable by its owner only.
func TestARerunReplacesTheStoredToken(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	install(t, destDir, binary, exampleHub, testNode, "token-bbbb")

	assertEnv(t, destDir, exampleHub, testNode, "token-bbbb")
	env := string(tree(t, destDir)[hostLayout().env.path])
	if strings.Contains(env, testToken) {
		t.Errorf("the environment file still carries the replaced token")
	}
	assertLayout(t, destDir)
}

// spec: deployment.md#re-running — a run with no token available keeps the stored one and
// succeeds. Writing an empty token over a working one would break a node during a routine
// upgrade (deployment.md#edge-cases).
func TestARerunWithoutATokenKeepsTheStoredOne(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	install(t, destDir, binary, exampleHub, "server-b", "")

	assertEnv(t, destDir, exampleHub, "server-b", testToken)
	assertLayout(t, destDir)
}

// handSpeltToken is every spelling of the MONITOR_TOKEN line the agent's parser reads as the
// token: it trims the blanks around a key and around a value and strips one matching pair of
// surrounding quotes (ADR 0020). A line the agent reads as the token is a line the script
// has to read as the token too.
var handSpeltToken = []string{
	"MONITOR_TOKEN = " + testToken,
	`MONITOR_TOKEN="` + testToken + `"`,
	"  MONITOR_TOKEN  =  '" + testToken + "'  ",
}

// respellToken rewrites the installed MONITOR_TOKEN line the way an operator spelt it by
// hand, and refuses to go on unless the agent still reads that line as the token.
func respellToken(t *testing.T, destDir, line string) {
	t.Helper()
	envFile := filepath.Join(destDir, hostLayout().env.path)
	body := regexp.MustCompile(`(?m)^MONITOR_TOKEN=.*$`).ReplaceAllLiteralString(read(t, envFile), line)
	if err := os.WriteFile(envFile, []byte(body), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}
	values, err := agent.ReadEnvFile(envFile)
	if err != nil || values["MONITOR_TOKEN"] != testToken {
		t.Fatalf("the agent does not read %q as the token: %q, %v", line, values["MONITOR_TOKEN"], err)
	}
}

// spec: deployment.md#re-running — a run with no token available keeps the stored one,
// whichever way that line is spelt: a token the agent works from and the script cannot see
// is a refused upgrade on a node that was running fine.
func TestARerunKeepsAStoredTokenHoweverItIsSpelt(t *testing.T) {
	for _, spelling := range handSpeltToken {
		t.Run(spelling, func(t *testing.T) {
			destDir, binary := t.TempDir(), agentBinary(t)
			install(t, destDir, binary, exampleHub, testNode, testToken)
			respellToken(t, destDir, spelling)

			install(t, destDir, binary, exampleHub, "server-b", "")

			assertEnv(t, destDir, exampleHub, "server-b", testToken)
			assertLayout(t, destDir)
		})
	}
}

// spec: deployment.md#re-running — and a rotation rewrites that line in place, whichever way
// it is spelt: appending a second MONITOR_TOKEN would leave the revoked one on disk.
func TestARotationRewritesAStoredTokenHoweverItIsSpelt(t *testing.T) {
	for _, spelling := range handSpeltToken {
		t.Run(spelling, func(t *testing.T) {
			destDir, binary := t.TempDir(), agentBinary(t)
			install(t, destDir, binary, exampleHub, testNode, testToken)
			respellToken(t, destDir, spelling)

			install(t, destDir, binary, exampleHub, testNode, "token-bbbb")

			assertEnv(t, destDir, exampleHub, testNode, "token-bbbb")
			env := string(tree(t, destDir)[hostLayout().env.path])
			if strings.Contains(env, testToken) {
				t.Errorf("the environment file still carries the revoked token:\n%s", env)
			}
			if count := strings.Count(env, "MONITOR_TOKEN"); count != 1 {
				t.Errorf("the environment file has %d MONITOR_TOKEN lines, want 1:\n%s", count, env)
			}
		})
	}
}

// spec: deployment.md#edge-cases — the environment file edited by hand is supported: the run
// rewrites MONITOR_HUB and MONITOR_NODE and leaves every other line alone.
func TestARerunLeavesLinesItDoesNotOwnAlone(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	envFile := filepath.Join(destDir, hostLayout().env.path)
	body := []byte(read(t, envFile))
	if err := os.WriteFile(envFile, append([]byte("# hand-written note\n"), body...), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}

	install(t, destDir, binary, "https://other.example.com", testNode, "")

	edited := string(tree(t, destDir)[hostLayout().env.path])
	if !strings.Contains(edited, "# hand-written note") {
		t.Errorf("the run dropped a line it does not own:\n%s", edited)
	}
	assertEnv(t, destDir, "https://other.example.com", testNode, testToken)
}

// spec: deployment.md#edge-cases — a hand-edited file is supported, and a hand-indented line
// is still that key: the agent's own parser ignores leading blanks (ADR 0020), so a token
// behind one is a stored token rather than a node that refuses its next upgrade.
func TestARerunReadsAnIndentedLineAsThatKey(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	envFile := filepath.Join(destDir, hostLayout().env.path)
	indented := regexp.MustCompile(`(?m)^`).ReplaceAllString(read(t, envFile), "  ")
	if err := os.WriteFile(envFile, []byte(indented), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}

	install(t, destDir, binary, "https://other.example.com", testNode, "")

	assertEnv(t, destDir, "https://other.example.com", testNode, testToken)
}

// spec: deployment.md#re-running — a file saved with CRLF line endings holds the same values
// to the agent, which ends a line at a lone carriage return (ADR 0020), so a token-less
// re-run against one keeps the stored token rather than refusing over a value nobody passed.
func TestARerunReadsAFileWithCRLFLineEndings(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	envFile := filepath.Join(destDir, hostLayout().env.path)
	body := read(t, envFile)
	if err := os.WriteFile(envFile, []byte(strings.ReplaceAll(body, "\n", "\r\n")), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}

	install(t, destDir, binary, exampleHub, testNode, "")

	assertEnv(t, destDir, exampleHub, testNode, testToken)
}

// spec: deployment.md#refusing — every refusal is decided before the first file is written,
// so a rejected run leaves the node exactly as it was.
//
// The refusals DESTDIR suppresses have no staged run to assert them from
// (deployment.md#staged-installs). "Not root" is asserted below against a real install; the
// other two cannot be reached from a suite at all, because the root check fires first — a
// run as root would find its own host's init system and its own root-owned directories.
func TestARefusalNamesItsCauseAndWritesNothing(t *testing.T) {
	binary := agentBinary(t)
	unreadable := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(unreadable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write the non-executable file: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent")

	tests := []struct {
		name  string
		args  []string
		stdin string
		names string
	}{
		{
			name:  "the binary is missing",
			args:  []string{"--binary", missing, "--hub", exampleHub, "--node", testNode},
			stdin: testToken,
			names: missing,
		},
		{
			name:  "the binary is not executable",
			args:  []string{"--binary", unreadable, "--hub", exampleHub, "--node", testNode},
			stdin: testToken,
			names: unreadable,
		},
		{
			name:  "no --binary",
			args:  []string{"--hub", exampleHub, "--node", testNode},
			stdin: testToken,
			names: "--binary",
		},
		{
			name:  "no --hub",
			args:  []string{"--binary", binary, "--node", testNode},
			stdin: testToken,
			names: "--hub",
		},
		{
			name:  "no --node",
			args:  []string{"--binary", binary, "--hub", exampleHub},
			stdin: testToken,
			names: "--node",
		},
		{
			name:  "--hub without a value",
			args:  []string{"--binary", binary, "--hub"},
			stdin: testToken,
			names: "--hub",
		},
		{
			name:  "no token anywhere",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
			stdin: "",
			names: "MONITOR_TOKEN",
		},
		{
			// The environment file is one KEY=VALUE per line, so a value carrying a line
			// break would write a second line the agent reads as configuration — a mistyped
			// --node could override the stored token.
			name:  "a value carrying a newline",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", "server-b\nMONITOR_TOKEN=x"},
			stdin: testToken,
			names: "line break",
		},
		{
			// A lone carriage return ends a line for the agent's parser too (ADR 0020), so a
			// value carrying one smuggles a second key past a guard that only knows \n.
			name:  "a value carrying a carriage return",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", "server-b\rMONITOR_HUB=https://elsewhere.example.com"},
			stdin: testToken,
			names: "line break",
		},
		{
			// And the token takes the same route: it arrives on stdin, where a carriage
			// return is not the end of the line.
			name:  "a token carrying a carriage return",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
			stdin: testToken + "\rMONITOR_HUB=https://elsewhere.example.com",
			names: "line break",
		},
		{
			// The agent refuses the whole file over a value that opens with a quote and
			// never closes it, so installing one leaves a node that never starts, its hub
			// and its name taken down with it.
			name:  "a value opening with a quote it does not close",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", `"server-b`},
			stdin: testToken,
			names: "quote",
		},
		{
			name:  "a token opening with a quote it does not close",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
			stdin: "'" + testToken,
			names: "quote",
		},
		{
			// The agent trims the blanks before it looks for the closing quote, so a quote
			// standing behind a space is the same unreadable file as the row above.
			name:  "a value whose unclosed quote hides behind a blank",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", " 'server-b"},
			stdin: testToken,
			names: "quote",
		},
		{
			// Written verbatim, read back trimmed: the node would run under a name nobody
			// typed, which is worse than being told to type it again.
			name:  "a value padded with blanks",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", " server-b "},
			stdin: testToken,
			names: "read back as something else",
		},
		{
			// The same, for the quoting the file format itself does: the agent strips one
			// matching pair, so the value installed is not the value passed.
			name:  "a value in matching quotes",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", `"server-b"`},
			stdin: testToken,
			names: "read back as something else",
		},
		{
			// Read as one line, the rest of a multi-line paste went unseen: the token
			// installed truncated and the run reported success.
			name:  "a token pasted with a line break in it",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
			stdin: testToken + "\nMONITOR_HUB=https://elsewhere.example.com",
			names: "line break",
		},
		{
			// The agent trims Unicode whitespace this script cannot see, so a token pasted
			// with a non-breaking space would install as one string and be read back as
			// another — a node that authenticates nowhere and reports nothing.
			name:  "a token holding a non-breaking space",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode},
			stdin: "\u00a0" + testToken,
			names: "printable ASCII",
		},
		{
			name:  "an unknown flag",
			args:  []string{"--binary", binary, "--hub", exampleHub, "--node", testNode, "--token", testToken},
			stdin: "",
			names: "usage",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()
			stdout, stderr, err := run{destDir: destDir, args: tc.args, stdin: tc.stdin}.start(t)
			if err == nil {
				t.Fatalf("the run succeeded; it should have refused")
			}
			if !strings.Contains(stdout+stderr, tc.names) {
				t.Errorf("the refusal does not name %q; it said:\n%s%s", tc.names, stdout, stderr)
			}
			if written := tree(t, destDir); len(written) != 0 {
				t.Errorf("the refused run wrote %v", sortedKeys(written))
			}
			assertNoToken(t, stdout, stderr)
		})
	}
}

// spec: deployment.md#refusing — a real install refuses for a user that is not root and says
// to re-run under sudo. It fires before any check that could write, so the run touches
// nothing on the machine the suite happens to be running on.
func TestARealInstallRefusesForAUserThatIsNotRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this refusal is the one root passes; a real install would touch this machine")
	}

	stdout, stderr, err := run{
		args:  []string{"--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode},
		stdin: testToken,
	}.start(t)
	if err == nil {
		t.Fatalf("a real install succeeded without root; it should have refused")
	}
	if !strings.Contains(stdout+stderr, "sudo") {
		t.Errorf("the refusal does not say to re-run under sudo; it said:\n%s%s", stdout, stderr)
	}
	assertNoToken(t, stdout, stderr)
}

// spec: deployment.md#refusing — the service definition the script installs sits beside it,
// and a run that copied the script alone refuses instead of installing a node with no
// service. Unlike the three refusals a staged suite cannot reach, this one it can.
func TestARunWithoutItsServiceDefinitionRefuses(t *testing.T) {
	alone := filepath.Join(t.TempDir(), "install-agent.sh")
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read the script: %v", err)
	}
	if err := os.WriteFile(alone, body, 0o755); err != nil {
		t.Fatalf("copy the script: %v", err)
	}

	destDir := t.TempDir()
	cmd := exec.Command(alone, "--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DESTDIR=" + destDir}
	cmd.Stdin = strings.NewReader(testToken)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("the run succeeded without a service definition beside it:\n%s", out)
	}
	if !strings.Contains(string(out), "service definition") {
		t.Errorf("the refusal does not name the missing definition; it said:\n%s", out)
	}
	if written := tree(t, destDir); len(written) != 0 {
		t.Errorf("the refused run wrote %v", sortedKeys(written))
	}
}

// spec: deployment.md#refusing — the agent refuses the whole file over one line it cannot
// read, so a re-run over a file carrying such a line refuses instead of reporting success on
// a node that will not start. A comment and a blank line are not such a line.
func TestARerunRefusesAFileTheAgentWouldRefuse(t *testing.T) {
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)
	envFile := filepath.Join(destDir, hostLayout().env.path)
	kept := read(t, envFile)

	if err := os.WriteFile(envFile, []byte(kept+"export MONITOR_EXTRA=1\n"), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}
	_, stderr, err := run{destDir: destDir, args: []string{"--binary", binary, "--hub", exampleHub, "--node", testNode}, stdin: testToken}.start(t)
	if err == nil {
		t.Fatalf("the run succeeded over a file the agent refuses")
	}
	if !strings.Contains(stderr, "line 4") {
		t.Errorf("the refusal does not name the line; it said:\n%s", stderr)
	}

	for _, line := range []string{"MONITOR_NODE=server-b\rMONITOR_EXTRA=1", "# a note\rMONITOR_HUB=https://elsewhere.example.com"} {
		if err := os.WriteFile(envFile, []byte(kept+line+"\n"), 0o600); err != nil {
			t.Fatalf("edit the environment file: %v", err)
		}
		_, stderr, err := run{destDir: destDir, args: []string{"--binary", binary, "--hub", exampleHub, "--node", testNode}, stdin: testToken}.start(t)
		if err == nil || !strings.Contains(stderr, "line 4") {
			t.Errorf("a carriage return inside line 4 was not refused by its number: %v\n%s", err, stderr)
		}
	}

	if err := os.WriteFile(envFile, []byte("# a note\n\n"+kept), 0o600); err != nil {
		t.Fatalf("edit the environment file: %v", err)
	}
	install(t, destDir, binary, exampleHub, testNode, "")

	if edited := read(t, envFile); !strings.Contains(edited, "# a note") {
		t.Errorf("the run dropped a comment it does not own:\n%s", edited)
	}
	assertEnv(t, destDir, exampleHub, testNode, testToken)
}

// spec: deployment.md#staged-installs — no service is registered, started or restarted. The
// stand-ins on PATH record any call, so a run that reached for one is visible.
func TestAStagedRunCallsNoServiceCommand(t *testing.T) {
	pathDir := t.TempDir()
	marker := filepath.Join(pathDir, "called")
	for _, tool := range []string{"systemctl", "launchctl"} {
		stub := "#!/bin/sh\necho \"$0 $*\" >> " + marker + "\n"
		if err := os.WriteFile(filepath.Join(pathDir, tool), []byte(stub), 0o755); err != nil {
			t.Fatalf("write the %s stand-in: %v", tool, err)
		}
	}

	// Without this, a broken PATH would make the assertion below pass for the wrong reason.
	control := exec.Command("/bin/sh", "-c", "systemctl daemon-reload")
	control.Env = []string{"PATH=" + pathDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	if err := control.Run(); err != nil {
		t.Fatalf("the stand-in is not reachable on PATH: %v", err)
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("the stand-in records no call, so this test could not detect one: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("clear the record: %v", err)
	}

	destDir := t.TempDir()
	run{
		destDir: destDir,
		args:    []string{"--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode},
		stdin:   testToken,
		pathDir: pathDir,
	}.mustRun(t)

	if called, err := os.ReadFile(marker); err == nil {
		t.Errorf("a staged run called a service command: %s", called)
	}
	assertLayout(t, destDir)
}

// spec: deployment.md#staged-installs — neither root nor an init system is required. The run
// gets a PATH holding the ordinary tools and nothing else, so `systemctl` and `launchctl`
// are genuinely absent rather than merely unused.
func TestAStagedRunNeedsNoInitSystemOnPath(t *testing.T) {
	pathDir := t.TempDir()
	for _, tool := range []string{"basename", "dirname", "mkdir", "rm", "cp", "chmod", "mv", "uname", "id", "mktemp", "cat"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("find %s: %v", tool, err)
		}
		if err := os.Symlink(found, filepath.Join(pathDir, tool)); err != nil {
			t.Fatalf("link %s: %v", tool, err)
		}
	}
	for _, tool := range []string{"systemctl", "launchctl"} {
		if _, err := exec.LookPath(tool); err == nil {
			t.Logf("%s exists on this host, and is deliberately left off the run's PATH", tool)
		}
	}

	destDir := t.TempDir()
	run{
		destDir: destDir,
		args:    []string{"--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode},
		stdin:   testToken,
		pathDir: pathDir,
		onlyDir: true,
	}.mustRun(t)

	assertLayout(t, destDir)
	assertEnv(t, destDir, exampleHub, testNode, testToken)
}

// spec: deployment.md#staged-installs — the refusals a real install makes do not apply. A
// real install requires the environment file's directory to be root's, and under DESTDIR
// that directory belongs to whoever runs the suite.
func TestAStagedRunAcceptsADirectoryThatIsNotRootsOwn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the staging tree would be root's, so this asserts nothing")
	}
	destDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(destDir, hostLayout().env.path)), 0o755); err != nil {
		t.Fatalf("prepare the staging tree: %v", err)
	}

	install(t, destDir, agentBinary(t), exampleHub, testNode, testToken)

	assertEnv(t, destDir, exampleHub, testNode, testToken)
	assertLayout(t, destDir)
}

// spec: deployment.md#where-things-live — the modes are the table's whatever umask the
// operator ran with. Under a permissive umask a run that only copied modes across would
// leave the environment file readable by every account on the node — and the directory it
// sits in writable by every account, which the file modes alone do not show.
func TestTheModesDoNotFollowTheCallersUmask(t *testing.T) {
	destDir := t.TempDir()
	run{
		destDir: destDir,
		args:    []string{"--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode},
		stdin:   testToken,
		umask:   "000",
	}.mustRun(t)

	assertLayout(t, destDir)
	assertDirModes(t, destDir)
}

// assertDirModes checks every directory the run created below destDir. Each file's mode is
// set explicitly, so a directory is the only thing the caller's umask can reach: /etc/monitor
// left drwxrwxrwx is a token every account on the node can replace.
func assertDirModes(t *testing.T, destDir string) {
	t.Helper()
	err := filepath.WalkDir(destDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || path == destDir {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("the directory %s has mode %04o, want 0755", path, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", destDir, err)
	}
}

// spec: deployment.md#invariants — a run that fails after it has begun writing stops at that
// failure and names what it already wrote. Here the environment file's directory is a
// regular file, so the run dies with the binary already installed.
func TestARunThatFailsAfterWritingNamesWhatItWrote(t *testing.T) {
	destDir := t.TempDir()
	envDir := filepath.Dir(filepath.Join(destDir, hostLayout().env.path))
	if err := os.MkdirAll(filepath.Dir(envDir), 0o755); err != nil {
		t.Fatalf("prepare the staging tree: %v", err)
	}
	if err := os.WriteFile(envDir, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("block the environment file's directory: %v", err)
	}

	stdout, stderr, err := run{
		destDir: destDir,
		args:    []string{"--binary", agentBinary(t), "--hub", exampleHub, "--node", testNode},
		stdin:   testToken,
	}.start(t)
	if err == nil {
		t.Fatalf("the run succeeded; it should have failed on the environment file")
	}

	binary := filepath.Join(destDir, hostLayout().binary.path)
	if !strings.Contains(stdout+stderr, binary) {
		t.Errorf("the failure does not name the binary it had already written:\n%s%s", stdout, stderr)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Errorf("the run rolled back instead of stopping: %v", err)
	}
	assertNoToken(t, stdout, stderr)
}

// spec: deployment.md#where-things-live — the service definition installed is the one this
// repository ships, byte for byte: it is a constant, so nothing is rendered into it.
func TestTheInstalledServiceIsTheShippedFile(t *testing.T) {
	destDir := t.TempDir()
	install(t, destDir, agentBinary(t), exampleHub, testNode, testToken)

	source := agentUnit
	if runtime.GOOS == "darwin" {
		source = agentPlist
	}
	shipped := []byte(read(t, source))
	if installed := tree(t, destDir)[hostLayout().service.path]; !bytes.Equal(installed, shipped) {
		t.Errorf("the installed service definition differs from %s", source)
	}
}

// spec: deployment.md#where-things-live — the agent log exists in the launchd layout only,
// and it is created by the install because launchd would otherwise create it world-readable
// on first start.
func TestTheAgentLogBelongsToTheLaunchdLayoutOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the log file is a macOS row of the layout table")
	}
	destDir, binary := t.TempDir(), agentBinary(t)
	install(t, destDir, binary, exampleHub, testNode, testToken)

	logFile := filepath.Join(destDir, "var/log/monitor-agent.log")
	if err := os.WriteFile(logFile, []byte("a line the agent already wrote\n"), 0o600); err != nil {
		t.Fatalf("write the log: %v", err)
	}

	install(t, destDir, binary, exampleHub, testNode, testToken)

	if kept := read(t, logFile); kept == "" {
		t.Errorf("the second run truncated the agent's log")
	}
}
