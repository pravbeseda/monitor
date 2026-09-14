// This file drives the real monitor-install.sh as the hub's update timer runs it: against a
// synthetic origin holding more than one release, whose installers answer as a test scripts
// them to.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// answeringArchive is an installer whose install-hub.sh records how it was called and what
// arrived on its stdin, and writes named into the answer file it is given; an empty named
// answers nothing. failing makes it refuse instead, as an installer that predates following
// would.
func answeringArchive(t *testing.T, named string, failing bool) []byte {
	t.Helper()
	hub := "#!/bin/sh\nprintf 'hub args: %s\\n' \"$*\" >> \"$RECORD\"\n" +
		"printf 'binary: %s\\n' \"$(cat \"$2\")\" >> \"$RECORD\"\n" +
		"printf 'stdin: [%s]\\n' \"$(cat)\" >> \"$RECORD\"\n"
	if failing {
		hub += "echo 'install-hub.sh: unknown option: --follow-target' >&2\nexit 1\n"
	} else {
		hub += "answer=\nwhile [ $# -gt 0 ]; do\n  [ \"$1\" != --answer ] || answer=$2\n  shift\ndone\n" +
			"[ -z '" + named + "' ] || printf '" + named + "\\n' > \"$answer\"\nexit 0\n"
	}
	return packInstaller(t, map[string]string{"install.sh": dispatcherScript, "install-hub.sh": hub}, nil)
}

// followOrigin is an origin whose newest release, 1.2.3, answers with named.
func followOrigin(t *testing.T, named string) *origin {
	t.Helper()
	o := newOrigin(t)
	o.set("monitor-installer-1.2.3.tar.gz", answeringArchive(t, named, false))
	o.sign(t)
	return o
}

func handOvers(t *testing.T, run bootstrapRun) []string {
	t.Helper()
	body, err := os.ReadFile(run.record)
	if err != nil {
		return nil
	}
	var calls []string
	for _, line := range strings.Split(string(body), "\n") {
		if call, ok := strings.CutPrefix(line, "hub args: "); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

// fetchedTags is every release tag the run downloaded an asset from, in order.
func fetchedTags(o *origin) []string {
	var tags []string
	for _, path := range o.requests() {
		rest, ok := strings.CutPrefix(path, "/releases/download/")
		if !ok {
			continue
		}
		tag, _, _ := strings.Cut(rest, "/")
		if len(tags) == 0 || tags[len(tags)-1] != tag {
			tags = append(tags, tag)
		}
	}
	return tags
}

func installBinary(t *testing.T, destDir, body string) {
	t.Helper()
	dir := filepath.Join(destDir, "usr", "local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "monitor-hub"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// spec: installer.md#following-a-target — a target that resolves to the newest release is
// installed by its installer, handed the four options and no stdin, and the run adds no
// claim of its own.
func TestAFollowRunHandsTheNewestReleaseOver(t *testing.T) {
	o := followOrigin(t, "")
	run := newBootstrapRun(t, o, "hub", "--follow-target")
	run.stdin = "nothing the hub should read"

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	calls := handOvers(t, run)
	if len(calls) != 1 {
		t.Fatalf("the run handed over %d times, want once: %v", len(calls), calls)
	}
	for _, want := range []string{"--follow-target", "--release 1.2.3", "--newest 1.2.3", "--answer "} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("the hand-over does not carry %q: %s", want, calls[0])
		}
	}
	if record, _ := os.ReadFile(run.record); !strings.Contains(string(record), "stdin: []") {
		t.Errorf("the installer was handed something on stdin:\n%s", record)
	}
	if strings.Contains(stdout, "installing") {
		t.Errorf("the run claims an install of its own:\n%s", stdout)
	}
}

// spec: installer.md#following-a-target — a named older release is fetched after the newest,
// and its own installer is handed the same newest version; the downgrade guard does not stand
// in the way of a target.
func TestAFollowRunFetchesTheReleaseTheTargetNames(t *testing.T) {
	o := followOrigin(t, "1.0.0")
	o.publish(t, "1.0.0", answeringArchive(t, "", false))
	run := newBootstrapRun(t, o, "hub", "--follow-target")
	installBinary(t, run.destDir, "#!/bin/sh\necho monitor-hub 1.2.3\n")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	calls := handOvers(t, run)
	if len(calls) != 2 {
		t.Fatalf("the run handed over %d times, want twice: %v", len(calls), calls)
	}
	for _, want := range []string{"--release 1.0.0", "--newest 1.2.3"} {
		if !strings.Contains(calls[1], want) {
			t.Errorf("the second hand-over does not carry %q: %s", want, calls[1])
		}
	}
	record, _ := os.ReadFile(run.record)
	if !strings.Contains(string(record), "binary: #!/bin/sh\necho monitor-hub 1.0.0") {
		t.Errorf("the second installer was not handed the named release's binary:\n%s", record)
	}
	if tags := fetchedTags(o); strings.Join(tags, " ") != "v1.2.3 v1.0.0" {
		t.Errorf("the run fetched %v, want the newest release and then the named one", tags)
	}
}

// spec: installer.md#following-a-target — an origin serving an older release as the newest
// cannot roll the hub back.
func TestAFollowRunRefusesANewestOlderThanTheHubInPlace(t *testing.T) {
	o := followOrigin(t, "")
	run := newBootstrapRun(t, o, "hub", "--follow-target")
	installBinary(t, run.destDir, "#!/bin/sh\necho monitor-hub 9.9.9\n")

	stdout, stderr, err := run.start(t)

	if err == nil {
		t.Fatalf("the run succeeded; 1.2.3 is older than 9.9.9\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "9.9.9") || !strings.Contains(stderr, "a follow run installs nothing") {
		t.Errorf("the refusal does not name the version in place as a follow run's:\n%s", stderr)
	}
	if calls := handOvers(t, run); len(calls) != 0 {
		t.Errorf("the run still handed over: %v", calls)
	}
}

// spec: installer.md#following-a-target — the ways the version an installer named cannot be
// followed, none of which installs anything past the first hand-over.
func TestAFollowRunThatCannotFollowTheAnswer(t *testing.T) {
	tests := []struct {
		name      string
		named     string
		prepare   func(t *testing.T, o *origin)
		says      []string
		handOvers int
		fetched   string // the tags the run downloaded from
	}{
		{
			name:      "a release that predates the installer",
			named:     "1.0.0",
			prepare:   func(t *testing.T, o *origin) { o.publish(t, "1.0.0", nil) },
			says:      []string{"predates the installer"},
			handOvers: 1,
			fetched:   "v1.2.3 v1.0.0",
		},
		{
			name:  "a release whose signature does not verify",
			named: "1.0.0",
			prepare: func(t *testing.T, o *origin) {
				o.publish(t, "1.0.0", answeringArchive(t, "", false))
				o.tamper("1.0.0", "SHA256SUMS.sig", []byte("not a signature\n"))
			},
			says:      []string{"signature"},
			handOvers: 1,
			fetched:   "v1.2.3 v1.0.0",
		},
		{
			name:  "a release whose installer does not follow a target",
			named: "1.0.0",
			prepare: func(t *testing.T, o *origin) {
				o.publish(t, "1.0.0", answeringArchive(t, "", true))
			},
			says:      []string{"unknown option: --follow-target"},
			handOvers: 2,
			fetched:   "v1.2.3 v1.0.0",
		},
		{
			name:      "a release the origin does not serve",
			named:     "7.7.7",
			says:      []string{"7.7.7"},
			handOvers: 1,
			fetched:   "v1.2.3 v7.7.7",
		},
		{
			name:  "an installer that names a version in turn",
			named: "1.0.0",
			prepare: func(t *testing.T, o *origin) {
				o.publish(t, "1.0.0", answeringArchive(t, "1.1.0", false))
			},
			says:      []string{"1.0.0", "1.1.0"},
			handOvers: 2,
			fetched:   "v1.2.3 v1.0.0",
		},
		{name: "an answer that is not a version", named: "stable", says: []string{"not a version: stable"}, handOvers: 1, fetched: "v1.2.3"},
		{name: "an answer that is half a version", named: "1.2", says: []string{"not a version: 1.2"}, handOvers: 1, fetched: "v1.2.3"},
		{name: "an answer of two versions", named: "1.0.0\\n1.1.0", says: []string{"not a version"}, handOvers: 1, fetched: "v1.2.3"},
		{name: "an answer with a second, empty line", named: "1.0.0\\n", says: []string{"not a version"}, handOvers: 1, fetched: "v1.2.3"},
		{name: "an answer naming its own release", named: "1.2.3", says: []string{"its own version"}, handOvers: 1, fetched: "v1.2.3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := followOrigin(t, test.named)
			if test.prepare != nil {
				test.prepare(t, o)
			}
			run := newBootstrapRun(t, o, "hub", "--follow-target")

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			for _, want := range test.says {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, stderr)
				}
			}
			if calls := handOvers(t, run); len(calls) != test.handOvers {
				t.Errorf("the run handed over %d times, want %d: %v", len(calls), test.handOvers, calls)
			}
			if tags := strings.Join(fetchedTags(o), " "); tags != test.fetched {
				t.Errorf("the run fetched from %q, want %q", tags, test.fetched)
			}
		})
	}
}

// spec: installer.md#following-a-target — a follow run is the hub's, and chooses its version
// from the target alone.
func TestAFollowRunRefusesArgumentsThatChooseForIt(t *testing.T) {
	for _, test := range []struct {
		args []string
		says string
	}{
		{[]string{"agent", "--follow-target", "--hub", exampleHub, "--node", testNode}, "--follow-target is the hub's"},
		{[]string{"hub", "--follow-target", "--version", "1.2.3"}, "not --version"},
		{[]string{"hub", "--follow-target", "--allow-downgrade"}, "only when the target names it"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			o := followOrigin(t, "")
			run := newBootstrapRun(t, o, test.args...)

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, test.says) || !strings.Contains(stderr, "usage:") {
				t.Errorf("the refusal does not say %q with the usage:\n%s", test.says, stderr)
			}
			if asked := o.requests(); len(asked) != 0 {
				t.Errorf("a refused follow run still fetched %v", asked)
			}
		})
	}
}

// spec: installer.md#following-a-target — the whole chain with the installer a release really
// carries: the target on the host decides which hub lands.
func TestTheWholeChainFollowsTheTarget(t *testing.T) {
	for _, test := range []struct {
		target string
		lands  string
	}{
		{"latest\n", "monitor-hub 1.2.3"},
		{"1.2.3\n", "monitor-hub 1.2.3"},
		{"1.0.0\n", "monitor-hub 1.0.0"},
	} {
		t.Run(strings.TrimSpace(test.target), func(t *testing.T) {
			o := newOrigin(t)
			o.set("monitor-installer-1.2.3.tar.gz", realArchive(t))
			o.sign(t)
			o.publish(t, "1.0.0", realArchive(t))
			run := newBootstrapRun(t, o, "hub", "--follow-target")
			writeTarget(t, run.destDir, test.target)

			stdout, stderr, err := run.start(t)
			if err != nil {
				t.Fatalf("the chain failed: %v\n%s%s", err, stdout, stderr)
			}

			installed, err := os.ReadFile(filepath.Join(run.destDir, "usr/local/bin/monitor-hub"))
			if err != nil {
				t.Fatalf("no hub landed: %v\n%s%s", err, stdout, stderr)
			}
			if !strings.Contains(string(installed), test.lands) {
				t.Errorf("the hub that landed is %q, want %s", installed, test.lands)
			}
		})
	}
}
