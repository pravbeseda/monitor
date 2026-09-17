// This file drives the real install-agent.sh as a follow run hands over to it: the hub named
// in the staged agent.env answers the node's target, and the installer finds nothing to do,
// installs around the binary in place, asks for its release's binary, or names another.
package deploy_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeHub answers /api/v1/agent/target as a test scripts it and records what it was asked.
type fakeHub struct {
	url    string
	mu     sync.Mutex
	status int
	body   string
	header map[string]string
	asked  []*http.Request
}

func newFakeHub(t *testing.T, status int, body string) *fakeHub {
	t.Helper()
	h := &fakeHub{status: status, body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.asked = append(h.asked, r)
		for key, value := range h.header {
			w.Header().Set(key, value)
		}
		w.WriteHeader(h.status)
		_, _ = w.Write([]byte(h.body))
	}))
	t.Cleanup(server.Close)
	h.url = server.URL
	return h
}

func (h *fakeHub) requests() []*http.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*http.Request(nil), h.asked...)
}

// followAgent is one follow hand-over to install-agent.sh, staged under destDir, on a node
// installed by hand against hub. binary is the release's agent binary, and digest its SHA-256.
type followAgent struct {
	destDir string
	hub     *fakeHub
	release string
	newest  string
	answer  string
	binary  string
	digest  string
}

// newFollowAgent installs an agent from another binary than the release's, so the release is
// not in place until a test puts it there.
func newFollowAgent(t *testing.T, hub *fakeHub, release, newest string) followAgent {
	t.Helper()
	f := followAgent{
		destDir: t.TempDir(),
		hub:     hub,
		release: release,
		newest:  newest,
		answer:  filepath.Join(t.TempDir(), "answer"),
		binary:  filepath.Join(t.TempDir(), "monitor-agent"),
	}
	if err := os.WriteFile(f.binary, []byte("#!/bin/sh\necho monitor-agent "+release+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.digest = fileDigest(t, f.binary)
	if err := os.WriteFile(f.answer, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	install(t, f.destDir, agentBinary(t), hub.url, testNode, testToken)
	return f
}

func (f followAgent) args() []string {
	return []string{"--follow-target", "--release", f.release, "--newest", f.newest, "--digest", f.digest, "--answer", f.answer}
}

func (f followAgent) withBinary() []string {
	return append(f.args(), "--binary", f.binary)
}

func (f followAgent) start(t *testing.T, r run) (stdout, stderr string, err error) {
	t.Helper()
	r.destDir = f.destDir
	if r.args == nil {
		r.args = f.args()
	}
	return r.start(t)
}

func (f followAgent) mustStart(t *testing.T, r run) (stdout string) {
	t.Helper()
	stdout, stderr, err := f.start(t, r)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}
	return stdout
}

func (f followAgent) answered(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// releaseInPlace installs the release's binary the way an operator's run does.
func (f followAgent) releaseInPlace(t *testing.T) {
	t.Helper()
	install(t, f.destDir, f.binary, f.hub.url, testNode, testToken)
}

func (f followAgent) envPath() string { return filepath.Join(f.destDir, hostLayout().env.path) }

// spec: installer.md#answering-a-follow-run-for-the-agent — the node asks the hub its
// agent.env names, once, with the token as a bearer token, and answers with the version the
// hub names.
func TestAnAgentFollowRunAsksTheHubAndAnswersItsTarget(t *testing.T) {
	hub := newFakeHub(t, http.StatusOK, "1.1.0\n")
	f := newFollowAgent(t, hub, "1.2.3", "1.2.3")
	before := tree(t, f.destDir)

	f.mustStart(t, run{})

	if got := strings.TrimSpace(f.answered(t)); got != "1.1.0" {
		t.Errorf("the answer holds %q, want 1.1.0", got)
	}
	asked := hub.requests()
	if len(asked) != 1 {
		t.Fatalf("the hub was asked %d times, want once", len(asked))
	}
	if asked[0].Method != http.MethodGet || asked[0].URL.Path != "/api/v1/agent/target" {
		t.Errorf("the run asked %s %s", asked[0].Method, asked[0].URL.Path)
	}
	if got := asked[0].Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("the run authenticated with %q", got)
	}
	assertSameTree(t, before, tree(t, f.destDir))
}

// spec: installer.md#answering-a-follow-run-for-the-agent — a MONITOR_HUB with a trailing slash
// is joined as the agent joins it.
func TestAnAgentFollowRunJoinsTheHubAsTheAgentDoes(t *testing.T) {
	hub := newFakeHub(t, http.StatusOK, "1.1.0\n")
	f := newFollowAgent(t, hub, "1.2.3", "1.2.3")
	install(t, f.destDir, agentBinary(t), hub.url+"/", testNode, testToken)

	f.mustStart(t, run{})

	if asked := hub.requests(); len(asked) != 1 || asked[0].URL.Path != "/api/v1/agent/target" {
		t.Errorf("the run asked %v, want /api/v1/agent/target once", asked)
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — latest resolves to the newest
// release, and a target naming this release without its binary in place asks for it.
func TestAnAgentFollowRunAsksForItsOwnReleasesBinary(t *testing.T) {
	for _, body := range []string{"latest\n", "latest", "1.2.3\n"} {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			f := newFollowAgent(t, newFakeHub(t, http.StatusOK, body), "1.2.3", "1.2.3")
			before := tree(t, f.destDir)

			f.mustStart(t, run{})

			if got := strings.TrimSpace(f.answered(t)); got != "1.2.3" {
				t.Errorf("the answer holds %q, want 1.2.3", got)
			}
			assertSameTree(t, before, tree(t, f.destDir))
		})
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — a hub that names no target has the
// node install nothing, and that is not a failure.
func TestAnAgentFollowRunInstallsNothingWhenTheHubNamesNoTarget(t *testing.T) {
	f := newFollowAgent(t, newFakeHub(t, http.StatusNoContent, ""), "1.2.3", "1.2.3")
	before := tree(t, f.destDir)

	stdout := f.mustStart(t, run{})

	if !strings.Contains(stdout, "names no target") {
		t.Errorf("the run does not say the hub names no target:\n%s", stdout)
	}
	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q", got)
	}
	assertSameTree(t, before, tree(t, f.destDir))
}

// spec: installer.md#answering-a-follow-run-for-the-agent — an agent already at its target is
// left alone.
func TestAnAgentFollowRunLeavesAnUnchangedAgentAlone(t *testing.T) {
	f := newFollowAgent(t, newFakeHub(t, http.StatusOK, "latest\n"), "1.2.3", "1.2.3")
	f.releaseInPlace(t)
	before := tree(t, f.destDir)

	stdout := f.mustStart(t, run{pathDir: failingInitTools(t)})

	if !strings.Contains(stdout, "already") {
		t.Errorf("the run does not say the agent is already at that version:\n%s", stdout)
	}
	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q", got)
	}
	assertSameTree(t, before, tree(t, f.destDir))
}

// spec: installer.md#answering-a-follow-run-for-the-agent — the release's binary in place with
// a service definition that differs is kept, and the rest is installed around it from
// agent.env, whatever stdin and MONITOR_TOKEN carry.
func TestAnAgentFollowRunKeepsTheReleasesBinaryInPlace(t *testing.T) {
	f := newFollowAgent(t, newFakeHub(t, http.StatusOK, "latest\n"), "1.2.3", "1.2.3")
	f.releaseInPlace(t)
	binary := filepath.Join(f.destDir, hostLayout().binary.path)
	before, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(f.destDir, hostLayout().service.path)
	if err := os.WriteFile(service, []byte("an older service definition\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := f.mustStart(t, run{stdin: "token-from-stdin", token: "token-from-environment"})

	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q; the binary in place is the release's", got)
	}
	if !strings.Contains(stdout, "keeping the binary in place") {
		t.Errorf("the run does not say it keeps the binary in place:\n%s", stdout)
	}
	if strings.Contains(stdout, "/usr/local/bin/monitor-agent") {
		t.Errorf("the run names the binary among the paths it wrote:\n%s", stdout)
	}
	assertLayout(t, f.destDir)
	assertEnv(t, f.destDir, f.hub.url, testNode, testToken)
	if body, _ := os.ReadFile(service); string(body) == "an older service definition\n" {
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

// spec: installer.md#answering-a-follow-run-for-the-agent — handed its binary, the run
// installs it with the values of agent.env and nothing from stdin or MONITOR_TOKEN, and asks
// the hub again.
func TestAnAgentFollowRunInstallsTheBinaryItAskedFor(t *testing.T) {
	hub := newFakeHub(t, http.StatusOK, "1.2.3\n")
	f := newFollowAgent(t, hub, "1.2.3", "1.3.0")

	stdout, stderr, err := f.start(t, run{args: f.withBinary(), stdin: "token-from-stdin", token: "token-from-environment"})
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	if got := f.answered(t); got != "" {
		t.Errorf("the answer holds %q", got)
	}
	assertLayout(t, f.destDir)
	assertEnv(t, f.destDir, hub.url, testNode, testToken)
	if got := fileDigest(t, filepath.Join(f.destDir, hostLayout().binary.path)); got != f.digest {
		t.Error("the binary installed is not the one handed over")
	}
	if n := len(hub.requests()); n != 1 {
		t.Errorf("the hand-over with the binary asked the hub %d times, want once", n)
	}
	for _, token := range []string{testToken, "token-from-stdin", "token-from-environment"} {
		if strings.Contains(stdout+stderr, token) {
			t.Errorf("the run printed a token:\n%s%s", stdout, stderr)
		}
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — the token reaches curl on its
// stdin, never in its arguments or its environment, even when the caller exported a variable of
// any name the script assigns: an exported variable keeps its export through an assignment.
func TestAnAgentFollowRunKeepsTheTokenOutOfCurlsArgumentsAndEnvironment(t *testing.T) {
	f := newFollowAgent(t, newFakeHub(t, http.StatusOK, "1.1.0\n"), "1.2.3", "1.2.3")
	realCurl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatal(err)
	}
	shims := t.TempDir()
	record := filepath.Join(t.TempDir(), "curl")
	write(t, filepath.Join(shims, "curl"), "#!/bin/sh\n"+
		"printf 'args: %s\\n' \"$*\" >> '"+record+"'\n"+
		"env >> '"+record+"'\n"+
		"exec '"+realCurl+"' \"$@\"\n")
	chmod(t, filepath.Join(shims, "curl"), 0o755)

	var planted []string
	for _, name := range assignedVariables(t, script, followScript) {
		planted = append(planted, name+"=planted")
	}
	f.mustStart(t, run{pathDir: shims, token: testToken, env: planted})

	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("curl was never run: %v", err)
	}
	if strings.Contains(string(body), testToken) {
		t.Errorf("the token reached curl's arguments or environment:\n%s", body)
	}
}

// shellAssignment is how a POSIX sh script gives a lowercase variable a value: at the start of a line
// or after a case pattern or a || or &&, as a for loop's variable, or as read's.
var shellAssignment = regexp.MustCompile(`(?:^|\) |\|\| |&& )([a-z_][a-z0-9_]*)=|\bfor ([a-z_][a-z0-9_]*) in\b|\bread -r ([a-z_][a-z0-9_]*)`)

// assignedVariables is every lowercase variable the scripts assign, comments aside.
func assignedVariables(t *testing.T, files ...string) []string {
	t.Helper()
	names := map[string]bool{}
	for _, file := range files {
		for _, line := range strings.Split(read(t, file), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			for _, match := range shellAssignment.FindAllStringSubmatch(line, -1) {
				names[match[1]+match[2]+match[3]] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no assignments; the pattern no longer reads the scripts")
	}
	return slices.Sorted(maps.Keys(names))
}

// hubAnswer is how the fake hub answers a refusal row; a row without one gets a valid target,
// so what it refuses is the node's own state.
type hubAnswer struct {
	status int
	body   string
	header map[string]string
}

type refusal struct {
	name    string
	answer  *hubAnswer
	prepare func(t *testing.T, f *followAgent)
	args    func(f followAgent) []string
	names   string
}

// spec: installer.md#answering-a-follow-run-for-the-agent — every way the run refuses before or
// after asking the hub; none of them installs or answers anything, and a row whose hub answers
// nothing asks it nothing.
func TestAnAgentFollowRunRefuses(t *testing.T) {
	page := "<html><body>" + strings.Repeat("an error page longer than any target ", 4) + "</body></html>"
	tests := []refusal{
		{name: "no agent.env", prepare: func(t *testing.T, f *followAgent) {
			if err := os.Remove(f.envPath()); err != nil {
				t.Fatal(err)
			}
		}, names: "agent.env: a follow run upgrades an agent installed by hand first"},
		{name: "agent.env writable by group", prepare: func(t *testing.T, f *followAgent) {
			chmod(t, f.envPath(), 0o620)
		}, names: "agent.env is writable by group or other"},
		{name: "agent.env a symlink", prepare: func(t *testing.T, f *followAgent) {
			elsewhere := filepath.Join(t.TempDir(), "agent.env")
			if err := os.Rename(f.envPath(), elsewhere); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, f.envPath()); err != nil {
				t.Fatal(err)
			}
		}, names: "agent.env is a symlink"},
		{name: "its directory writable by other", prepare: func(t *testing.T, f *followAgent) {
			chmod(t, filepath.Dir(f.envPath()), 0o757)
		}, names: "monitor is writable by group or other"},
		{name: "a line the agent would refuse", prepare: func(t *testing.T, f *followAgent) {
			appendLine(t, f.envPath(), "not an assignment")
		}, names: "line 4"},
		{name: "a lone carriage return inside a value, which ends the line for the agent", prepare: func(t *testing.T, f *followAgent) {
			dropKey("MONITOR_TOKEN")(t, f)
			appendLine(t, f.envPath(), "MONITOR_TOKEN=token\raaaa")
		}, names: "line 3"},
		{name: "a carriage return inside a comment", prepare: func(t *testing.T, f *followAgent) {
			appendLine(t, f.envPath(), "# a note\rMONITOR_HUB=https://elsewhere.example.com")
		}, names: "line 4"},
		{name: "a value the agent would read back as another", prepare: func(t *testing.T, f *followAgent) {
			dropKey("MONITOR_NODE")(t, f)
			appendLine(t, f.envPath(), `MONITOR_NODE=" laptop-a"`)
		}, names: "read back as something else"},
		{name: "no MONITOR_HUB", prepare: dropKey("MONITOR_HUB"), names: "MONITOR_HUB"},
		{name: "no MONITOR_NODE", prepare: dropKey("MONITOR_NODE"), names: "MONITOR_NODE"},
		{name: "an empty MONITOR_TOKEN", prepare: func(t *testing.T, f *followAgent) {
			dropKey("MONITOR_TOKEN")(t, f)
			appendLine(t, f.envPath(), "MONITOR_TOKEN=")
		}, names: "MONITOR_TOKEN"},
		{name: "--hub given", args: func(f followAgent) []string {
			return append(f.args(), "--hub", f.hub.url)
		}, names: "--hub"},
		{name: "--node given", args: func(f followAgent) []string {
			return append(f.args(), "--node", testNode)
		}, names: "--node"},
		{name: "only some of the five options", args: func(f followAgent) []string {
			return f.args()[:7]
		}, names: "--answer"},
		{name: "a digest that is not one", args: func(f followAgent) []string {
			return append(f.args()[:5], "--digest", "abc", "--answer", f.answer)
		}, names: "--digest"},
		{name: "a hub in clear that is not loopback", prepare: setHub("http://hub.example.com"), names: "MONITOR_HUB"},
		{name: "a loopback look-alike carrying userinfo", prepare: setHub("http://127.0.0.1@hub.example.com"), names: "MONITOR_HUB"},
		{name: "another loopback address", prepare: setHub("http://127.0.0.2:8080"), names: "MONITOR_HUB"},
		{name: "a hub that cannot be reached", prepare: setHub("http://127.0.0.1:1"), names: "cannot be reached for this node's target: http://127.0.0.1:1"},
		{name: "the hub refusing the token", answer: &hubAnswer{status: http.StatusUnauthorized, body: `{"error":"no"}`},
			names: "refused this node's token"},
		{name: "a proxy asking for its credential", answer: &hubAnswer{status: http.StatusUnauthorized,
			header: map[string]string{"WWW-Authenticate": `Basic realm="hub"`}}, names: "proxy"},
		{name: "a proxy asking for its credential with a page", answer: &hubAnswer{status: http.StatusUnauthorized,
			body: page, header: map[string]string{"WWW-Authenticate": `Basic realm="hub"`}}, names: "proxy"},
		{name: "a hub older than the endpoint", answer: &hubAnswer{status: http.StatusNotFound}, names: "404"},
		{name: "a broken hub", answer: &hubAnswer{status: http.StatusInternalServerError}, names: "500"},
		{name: "a proxy whose hub is down, with a page", answer: &hubAnswer{status: http.StatusBadGateway, body: page},
			names: "502"},
		{name: "a redirect", answer: &hubAnswer{status: http.StatusFound, header: map[string]string{"Location": "/elsewhere"}},
			names: "302"},
	}
	for _, body := range []string{"1.4", "v1.2.3", "01.2.3", "1.1234567890.0", "latest\n\n", "\nlatest", "latest\r\n",
		"", strings.Repeat("1", 100), "<html>latest</html>"} {
		tests = append(tests, refusal{name: "an answer " + body, answer: &hubAnswer{status: http.StatusOK, body: body},
			names: "not a target"})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer := hubAnswer{status: http.StatusOK, body: "1.1.0\n"}
			if test.answer != nil {
				answer = *test.answer
			}
			hub := newFakeHub(t, answer.status, answer.body)
			hub.header = answer.header
			f := newFollowAgent(t, hub, "1.2.3", "1.2.3")
			if test.prepare != nil {
				test.prepare(t, &f)
			}
			before := tree(t, f.destDir)
			var args []string
			if test.args != nil {
				args = test.args(f)
			}

			stdout, stderr, err := f.start(t, run{args: args})

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, test.names) {
				t.Errorf("the refusal does not name %q:\n%s", test.names, stderr)
			}
			if strings.Contains(stdout+stderr, testToken) {
				t.Errorf("the refusal printed the token:\n%s%s", stdout, stderr)
			}
			if quoted := strings.TrimSpace(answer.body); answer.status == http.StatusOK && len(quoted) > 3 &&
				strings.Contains(stderr, quoted) {
				t.Errorf("the refusal quotes the hub's answer:\n%s", stderr)
			}
			if asked := len(hub.requests()); test.answer == nil && asked != 0 {
				t.Errorf("the run asked the hub %d times before refusing", asked)
			}
			assertSameTree(t, before, tree(t, f.destDir))
			if got := f.answered(t); got != "" {
				t.Errorf("a refusal answered %q", got)
			}
		})
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — agent.env's directory as a symlink
// is refused like a symlinked file. Apart from the table, because the tree it compares cannot be
// walked through the link.
func TestAnAgentFollowRunRefusesAnEnvironmentFileInASymlinkedDirectory(t *testing.T) {
	hub := newFakeHub(t, http.StatusOK, "1.1.0\n")
	f := newFollowAgent(t, hub, "1.2.3", "1.2.3")
	dir := filepath.Dir(f.envPath())
	moved := filepath.Join(t.TempDir(), "monitor")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, dir); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := f.start(t, run{})

	if err == nil || !strings.Contains(stderr, "monitor is a symlink") {
		t.Errorf("the run did not refuse the symlinked directory: %v\n%s", err, stderr)
	}
	if n := len(hub.requests()); n != 0 {
		t.Errorf("the run asked the hub %d times", n)
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — latest resolves to --newest, not to
// the release handed over.
func TestAnAgentFollowRunResolvesLatestToTheNewestRelease(t *testing.T) {
	f := newFollowAgent(t, newFakeHub(t, http.StatusOK, "latest\n"), "1.2.3", "1.3.0")

	f.mustStart(t, run{})

	if got := strings.TrimSpace(f.answered(t)); got != "1.3.0" {
		t.Errorf("the answer holds %q, want the newest 1.3.0", got)
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — a hub on its own host is reached in
// clear over loopback, by name or by address, with or without a port.
func TestAnAgentFollowRunReachesALoopbackHubInClear(t *testing.T) {
	hub := newFakeHub(t, http.StatusOK, "1.1.0\n")
	port := hub.url[strings.LastIndex(hub.url, ":"):]
	for _, address := range []string{"http://127.0.0.1" + port, "http://localhost" + port} {
		t.Run(address, func(t *testing.T) {
			f := newFollowAgent(t, hub, "1.2.3", "1.2.3")
			setHub(address)(t, &f)

			f.mustStart(t, run{})

			if got := strings.TrimSpace(f.answered(t)); got != "1.1.0" {
				t.Errorf("the answer holds %q, want 1.1.0", got)
			}
		})
	}
}

// failingInitTools is a PATH directory whose service commands fail the run if called.
func failingInitTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range []string{"systemctl", "launchctl"} {
		write(t, filepath.Join(dir, tool), "#!/bin/sh\necho \"$0 was called\" >&2\nexit 1\n")
		chmod(t, filepath.Join(dir, tool), 0o755)
	}
	return dir
}

func assertSameTree(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("the tree changed from %v to %v", sortedKeys(before), sortedKeys(after))
		return
	}
	for path, body := range before {
		if string(after[path]) != string(body) {
			t.Errorf("%s changed", path)
		}
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func rewriteEnv(t *testing.T, f *followAgent, edit func(line string) string) {
	t.Helper()
	body, err := os.ReadFile(f.envPath())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		if line = edit(line); line != "" {
			lines = append(lines, line)
		}
	}
	if err := os.WriteFile(f.envPath(), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dropKey(key string) func(t *testing.T, f *followAgent) {
	return func(t *testing.T, f *followAgent) {
		rewriteEnv(t, f, func(line string) string {
			if strings.HasPrefix(line, key+"=") {
				return ""
			}
			return line
		})
	}
}

func setHub(address string) func(t *testing.T, f *followAgent) {
	return func(t *testing.T, f *followAgent) {
		rewriteEnv(t, f, func(line string) string {
			if strings.HasPrefix(line, "MONITOR_HUB=") {
				return "MONITOR_HUB=" + address
			}
			return line
		})
	}
}

// spec: installer.md#answering-a-follow-run-for-the-agent — a binary in place that is not the
// release as the layout installs it asks for the release's binary rather than being kept.
func TestAnAgentFollowRunAsksForTheBinaryOverAnUnfinishedOne(t *testing.T) {
	for _, test := range []struct {
		name  string
		spoil func(t *testing.T, path string)
	}{
		{"a binary its group may write to", func(t *testing.T, path string) { chmod(t, path, 0o775) }},
		{"a symlink to the release's binary", func(t *testing.T, path string) {
			elsewhere := filepath.Join(t.TempDir(), "monitor-agent")
			if err := os.Rename(path, elsewhere); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"no binary", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFollowAgent(t, newFakeHub(t, http.StatusOK, "latest\n"), "1.2.3", "1.2.3")
			f.releaseInPlace(t)
			test.spoil(t, filepath.Join(f.destDir, hostLayout().binary.path))

			stdout := f.mustStart(t, run{})

			if strings.Contains(stdout, "already") || strings.Contains(stdout, "keeping") {
				t.Errorf("the run took an unfinished agent for the release:\n%s", stdout)
			}
			if got := strings.TrimSpace(f.answered(t)); got != "1.2.3" {
				t.Errorf("the answer holds %q, want 1.2.3", got)
			}
		})
	}
}
