// This file drives the real monitor-install.sh against a synthetic release served over
// loopback and signed with a key the test generates. Nothing here reaches GitHub, and every
// run is staged under DESTDIR.
package deploy_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const bootstrap = "./monitor-install.sh"

// origin is a synthetic release: the assets a run may ask for, the manifest that names them
// and the signature over it.
type origin struct {
	dir     string            // where the assets live on disk
	assets  map[string][]byte // by asset name, guarded by mu: the server reads it
	version string
	pub     string
	priv    string
	url     string
	mu      sync.Mutex
	asked   []string
}

// set, remove and requests are the only ways the test touches what the server serves, so the
// handler's goroutine and the test's never reach the map unsynchronised.
func (o *origin) set(name string, body []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.assets[name] = body
}

func (o *origin) get(name string) []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.assets[name]
}

func (o *origin) remove(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.assets, name)
}

func (o *origin) requests() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.asked...)
}

func newOrigin(t *testing.T) *origin {
	t.Helper()
	dir := t.TempDir()
	o := &origin{dir: dir, assets: map[string][]byte{}, version: "1.2.3"}
	o.priv, o.pub = keyPair(t, dir, "origin")
	o.assets[o.binaryName("agent")] = []byte("#!/bin/sh\necho monitor-agent 1.2.3\n")
	o.assets[o.binaryName("hub")] = []byte("#!/bin/sh\necho monitor-hub 1.2.3\n")
	o.assets["monitor-installer-"+o.version+".tar.gz"] = installerArchive(t, nil)
	o.sign(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		o.asked = append(o.asked, r.URL.Path)
		o.mu.Unlock()
		switch {
		case r.URL.Path == "/releases/latest":
			http.Redirect(w, r, "/releases/tag/v"+o.version, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/releases/tag/"):
			_, _ = w.Write([]byte("the release page"))
		case strings.HasPrefix(r.URL.Path, "/releases/download/"):
			name := filepath.Base(r.URL.Path)
			tag := filepath.Base(filepath.Dir(r.URL.Path))
			o.mu.Lock()
			body, ok := o.assets[name]
			o.mu.Unlock()
			if !ok || tag != "v"+o.version {
				http.Error(w, "no such asset", http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	o.url = server.URL
	return o
}

// binaryName is the name a run builds for itself, so the test and the script have to agree.
func (o *origin) binaryName(role string) string {
	goos, arch := runtime.GOOS, runtime.GOARCH
	return fmt.Sprintf("monitor-%s-%s-%s-%s", role, o.version, goos, arch)
}

// sign rewrites the manifest over whatever the assets currently are, and signs it.
func (o *origin) sign(t *testing.T) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	manifest := ""
	for name, body := range o.assets {
		if name == "SHA256SUMS" || name == "SHA256SUMS.sig" {
			continue
		}
		sum := sha256.Sum256(body)
		manifest += fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	o.assets["SHA256SUMS"] = []byte(manifest)
	path := filepath.Join(o.dir, "SHA256SUMS")
	write(t, path, manifest)
	openssl(t, "dgst", "-sha256", "-sign", o.priv, "-out", path+".sig", path)
	signature, err := os.ReadFile(path + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	o.assets["SHA256SUMS.sig"] = signature
}

// installerArchive is what a release carries: install.sh and the per-role installers, which
// record how they were called instead of installing anything. extra entries are added
// verbatim, which is how a hostile archive is built.
func installerArchive(t *testing.T, extra map[string]*tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(zip)
	files := map[string]string{
		"install.sh":       "#!/bin/sh\nrole=$1\nshift\nexec sh \"$(dirname \"$0\")/install-$role.sh\" \"$@\"\n",
		"install-agent.sh": recordingInstaller("agent"),
		"install-hub.sh":   recordingInstaller("hub"),
	}
	for name, body := range files {
		header := &tar.Header{Name: "monitor-installer/" + name, Mode: 0o755, Size: int64(len(body))}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, header := range extra {
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// recordingInstaller writes what it was handed into $RECORD, which the run inherits.
func recordingInstaller(role string) string {
	return "#!/bin/sh\nprintf '" + role + " args: %s\\n' \"$*\" >> \"$RECORD\"\n" +
		"printf 'binary: %s\\n' \"$(cat \"${2:-/dev/null}\" 2>/dev/null)\" >> \"$RECORD\"\n" +
		"printf 'stdin: [%s]\\n' \"$(cat)\" >> \"$RECORD\"\n" +
		"for sibling in \"$(dirname \"$0\")\"/*; do\n" +
		"  printf 'sibling: %s %s %s\\n' \"$(ls -ln \"$sibling\" | awk '{print $1}')\" " +
		"\"$(ls -ln \"$sibling\" | awk '{print $3}')\" \"$(basename \"$sibling\")\" >> \"$RECORD\"\n" +
		"done\nexit 0\n"
}

// bootstrapRun is one invocation of the real script against a synthetic origin.
type bootstrapRun struct {
	origin  *origin
	destDir string
	record  string
	key     string // MONITOR_RELEASE_KEY; the origin's own public half by default
	env     []string
	pathDir string
	args    []string
	stdin   string
}

func (r bootstrapRun) start(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	path := os.Getenv("PATH")
	if r.pathDir != "" {
		path = r.pathDir
	}
	key := r.key
	if key == "" {
		key = r.origin.pub
	}
	command := exec.Command(bootstrap, r.args...)
	command.Env = append([]string{
		"PATH=" + path,
		"DESTDIR=" + r.destDir,
		"MONITOR_RELEASE_ORIGIN=" + r.origin.url,
		"MONITOR_RELEASE_KEY=" + key,
		"RECORD=" + r.record,
		"TMPDIR=" + t.TempDir(),
	}, r.env...)
	command.Stdin = strings.NewReader(r.stdin)
	var out, errs bytes.Buffer
	command.Stdout, command.Stderr = &out, &errs
	err = command.Run()
	return out.String(), errs.String(), err
}

func newBootstrapRun(t *testing.T, o *origin, args ...string) bootstrapRun {
	t.Helper()
	return bootstrapRun{origin: o, destDir: t.TempDir(), record: filepath.Join(t.TempDir(), "record"), args: args}
}

// spec: installer.md#fetching-and-checking-a-release — a run installs the newest release's
// asset for this platform and this role, and says which version it installed.
func TestABootstrapRunInstallsTheNewestRelease(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	record, err := os.ReadFile(run.record)
	if err != nil {
		t.Fatalf("the installer was never reached: %v\n%s%s", err, stdout, stderr)
	}
	for _, want := range []string{"agent args:", "--hub https://hub.example.com", "--node laptop-a"} {
		if !strings.Contains(string(record), want) {
			t.Errorf("the handover does not carry %q:\n%s", want, record)
		}
	}
	if !strings.Contains(stdout, "1.2.3") {
		t.Errorf("the run does not name the version it installed:\n%s", stdout)
	}
	if asked := o.requests(); !strings.Contains(strings.Join(asked, " "), o.binaryName("agent")) {
		t.Errorf("the run asked for no binary of its own platform: %v", asked)
	}
}

// spec: installer.md#fetching-and-checking-a-release — every way a release can fail to be
// what it says it is, and none of them installs anything.
func TestABootstrapRunThatMustInstallNothing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		prepare func(t *testing.T, o *origin)
		says    string
		quiet   bool // the row says nothing is fetched at all
	}{
		{
			name: "a manifest changed after signing",
			prepare: func(_ *testing.T, o *origin) {
				o.set("SHA256SUMS", append(o.get("SHA256SUMS"), []byte("deadbeef  extra\n")...))
			},
			says: "signature",
		},
		{
			name: "a manifest signed by another key",
			prepare: func(t *testing.T, o *origin) {
				other, _ := keyPair(t, o.dir, "someone-else")
				o.priv = other
				o.sign(t)
			},
			says: "signature",
		},
		{
			name: "an asset whose digest is not the manifest's",
			prepare: func(_ *testing.T, o *origin) {
				o.set(o.binaryName("agent"), []byte("#!/bin/sh\necho tampered\n"))
			},
			says: "digest",
		},
		{
			name: "a release whose manifest lists no installer archive",
			prepare: func(t *testing.T, o *origin) {
				o.remove("monitor-installer-" + o.version + ".tar.gz")
				o.sign(t)
			},
			says: "predates the installer",
		},
		{
			name: "a release with no binary for this platform",
			prepare: func(t *testing.T, o *origin) {
				o.remove(o.binaryName("agent"))
				o.sign(t)
			},
			says: "lists no monitor-agent-1.2.3",
		},
		{
			name: "a signature file that is empty",
			prepare: func(_ *testing.T, o *origin) {
				o.set("SHA256SUMS.sig", []byte{})
			},
			says: "signature",
		},
		{
			name: "a signature file that is not a signature",
			prepare: func(_ *testing.T, o *origin) {
				o.set("SHA256SUMS.sig", []byte("not a signature at all\n"))
			},
			says: "signature",
		},
		{
			name:  "a version that is not MAJOR.MINOR.PATCH",
			args:  []string{"--version", "1.2"},
			says:  "not a version",
			quiet: true,
		},
		{
			name: "a version no release carries",
			args: []string{"--version", "9.9.9"},
			says: "9.9.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			if test.prepare != nil {
				test.prepare(t, o)
			}
			args := append([]string{"agent", "--hub", "https://hub.example.com", "--node", "laptop-a"}, test.args...)
			run := newBootstrapRun(t, o, args...)

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, test.says) {
				t.Errorf("the refusal does not say %q:\n%s", test.says, stderr)
			}
			if _, err := os.Stat(run.record); err == nil {
				t.Error("a refusal still handed over to an installer")
			}
			if asked := o.requests(); test.quiet && len(asked) != 0 {
				t.Errorf("the run fetched %v; this row fetches nothing", asked)
			}
		})
	}
}

// spec: installer.md#unpacking — an archive that reaches outside its own directory is not
// unpacked, however good its signature is.
func TestAHostileArchiveIsNotUnpacked(t *testing.T) {
	tests := []struct {
		name  string
		entry *tar.Header
		says  string
	}{
		{"an absolute path", &tar.Header{Name: "/etc/passwd", Mode: 0o644, Size: 0}, "reaches outside itself"},
		{"a path that climbs out", &tar.Header{Name: "monitor-installer/../../evil", Mode: 0o644, Size: 0}, "reaches outside itself"},
		{"a symlink", &tar.Header{Name: "monitor-installer/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, "not a file or a directory"},
		{"a second top-level entry", &tar.Header{Name: "elsewhere/install.sh", Mode: 0o755, Size: 0}, "not one directory of files"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			o.set("monitor-installer-"+o.version+".tar.gz", installerArchive(t, map[string]*tar.Header{"x": test.entry}))
			o.sign(t)
			run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; the archive had to stop it\n%s%s", stdout, stderr)
			}
			if _, err := os.Stat(run.record); err == nil {
				t.Error("a hostile archive still reached an installer")
			}
			if !strings.Contains(stderr, test.says) {
				t.Errorf("the refusal does not say %q:\n%s", test.says, stderr)
			}
		})
	}
}

// spec: installer.md#unpacking — what lands takes no owner and no setuid bit from the
// archive, whoever packed it. A suite that is not root cannot make this fail on its own —
// tar restores neither for an unprivileged user — so this pins the expectation for the day
// the same code runs as root, and the flags that carry it are read in review.
func TestUnpackingStripsSetuid(t *testing.T) {
	o := newOrigin(t)
	archive := installerArchive(t, map[string]*tar.Header{
		"x": {Name: "monitor-installer/setuid", Mode: 0o4755, Size: 0, Uid: 12345, Gid: 12345},
	})
	o.set("monitor-installer-"+o.version+".tar.gz", archive)
	o.sign(t)
	run := newBootstrapRun(t, o, "hub")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	record, err := os.ReadFile(run.record)
	if err != nil {
		t.Fatalf("the installer was never reached: %v\n%s%s", err, stdout, stderr)
	}
	found := false
	for _, line := range strings.Split(string(record), "\n") {
		fields := strings.Fields(strings.TrimPrefix(line, "sibling: "))
		if !strings.HasPrefix(line, "sibling: ") || len(fields) != 3 || fields[2] != "setuid" {
			continue
		}
		found = true
		if strings.ContainsAny(fields[0], "sS") {
			t.Errorf("the extracted file kept a setuid or setgid bit: %s", fields[0])
		}
		if fields[1] == "12345" {
			t.Errorf("the extracted file kept the archive's owner: %s", fields[1])
		}
	}
	if !found {
		t.Fatalf("the installer saw no file called setuid beside it:\n%s", record)
	}
}

// spec: installer.md#the-handover — the hub takes no token, so its installer is given
// /dev/null however the run itself was invoked.
func TestTheHubIsHandedNoStdin(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "hub")
	run.stdin = "a token nobody asked for"

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	record, err := os.ReadFile(run.record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "stdin: []") {
		t.Errorf("the hub installer was handed something on stdin:\n%s", record)
	}
}

// spec: installer.md#staged-installs — the seams that let this test exist are refused by a
// run that is not staged, so they cannot become a way around the key.
func TestTheSeamsAreRefusedByARunThatIsNotStaged(t *testing.T) {
	for _, seam := range []string{"MONITOR_RELEASE_ORIGIN", "MONITOR_RELEASE_KEY"} {
		t.Run(seam, func(t *testing.T) {
			o := newOrigin(t)
			run := newBootstrapRun(t, o, "hub")
			run.destDir = ""
			run.env = []string{"MONITOR_RELEASE_ORIGIN=", "MONITOR_RELEASE_KEY=", seam + "=" + o.url}

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; the seam had to stop it\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, seam) {
				t.Errorf("the refusal does not name %s:\n%s", seam, stderr)
			}
		})
	}
}

// spec: installer.md#fetching-and-checking-a-release — openssl, curl and tar are the tools
// the run needs, and a machine without one is told which.
func TestABootstrapRunNamesTheToolItIsMissing(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "hub")
	run.pathDir = t.TempDir()

	stdout, stderr, err := run.start(t)

	if err == nil {
		t.Fatalf("the run succeeded; an empty PATH had to stop it\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "is not installed, and this needs it") {
		t.Errorf("the refusal is not this script's own:\n%s", stderr)
	}
	if asked := o.requests(); len(asked) != 0 {
		t.Errorf("a run without its tools still fetched %v", asked)
	}
}

// spec: installer.md#fetching-and-checking-a-release — a release older than what is installed
// is an operator's slip, and the guard says so rather than installing it.
func TestABootstrapRunRefusesToGoBackwards(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	installed := filepath.Join(run.destDir, "usr", "local", "bin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(installed, "monitor-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho monitor-agent 9.9.9\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := run.start(t)
	if err == nil {
		t.Fatalf("the run succeeded; 1.2.3 is older than 9.9.9\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "9.9.9") || !strings.Contains(stderr, "--allow-downgrade") {
		t.Errorf("the refusal does not name the installed version and the way past it:\n%s", stderr)
	}

	run.args = append(run.args, "--allow-downgrade")
	if stdout, stderr, err = run.start(t); err != nil {
		t.Fatalf("--allow-downgrade did not install it: %v\n%s%s", err, stdout, stderr)
	}
}

// spec: installer.md#the-two-halves-and-what-is-not-frozen-yet — the key the script carries is
// the key the repository publishes; two copies that could drift are worse than one.
func TestTheCarriedKeyIsTheShippedKey(t *testing.T) {
	script, err := os.ReadFile(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := os.ReadFile("release-signing-key.pub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), strings.TrimSpace(string(shipped))) {
		t.Errorf("%s does not carry the key in release-signing-key.pub", bootstrap)
	}
}

// spec: installer.md#the-two-halves-and-what-is-not-frozen-yet — the fingerprint the install
// guide publishes is what an operator checks a downloaded script against, so it has to be
// this repository's key and not a stale copy of an older one.
func TestTheGuidePublishesThisKeysFingerprint(t *testing.T) {
	der := exec.Command("openssl", "pkey", "-pubin", "-in", "release-signing-key.pub", "-outform", "DER")
	pipe, err := der.Output()
	if err != nil {
		t.Fatalf("reading the shipped key: %v", err)
	}
	sum := sha256.Sum256(pipe)
	fingerprint := hex.EncodeToString(sum[:])

	guide, err := os.ReadFile("../docs/install.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(guide), fingerprint) {
		t.Errorf("docs/install.md does not publish this key's fingerprint %s", fingerprint)
	}
}

// spec: installer.md#fetching-and-checking-a-release — a version given explicitly is the one
// installed, and the run does not ask the origin what the newest is.
func TestAVersionGivenIsTheVersionInstalled(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "hub", "--version", "1.2.3")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}

	for _, path := range o.requests() {
		if strings.Contains(path, "releases/latest") {
			t.Errorf("the run asked for the newest release although it was given one: %v", o.requests())
		}
	}
}

// spec: installer.md#fetching-and-checking-a-release — 1.10.0 is newer than 1.9.0, which no
// comparison of the two as text agrees with.
func TestAVersionIsComparedNumerically(t *testing.T) {
	o := newOrigin(t)
	o.version = "1.10.0"
	o.set(o.binaryName("agent"), []byte("#!/bin/sh\necho monitor-agent 1.10.0\n"))
	o.set("monitor-installer-1.10.0.tar.gz", installerArchive(t, nil))
	o.remove("monitor-agent-1.2.3-" + hostPlatform())
	o.remove("monitor-installer-1.2.3.tar.gz")
	o.sign(t)

	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	installed := filepath.Join(run.destDir, "usr", "local", "bin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "monitor-agent"),
		[]byte("#!/bin/sh\necho monitor-agent 1.9.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("1.10.0 was refused over an installed 1.9.0: %v\n%s%s", err, stdout, stderr)
	}
}

func hostPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// spec: installer.md#fetching-and-checking-a-release — an installed binary that cannot say
// which version it is does not block the run, and the run says why it went ahead. A binary
// whose version is not one this project publishes is the same case.
func TestABinaryThatCannotSayItsVersionDoesNotBlockTheRun(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	installed := filepath.Join(run.destDir, "usr", "local", "bin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"#!/bin/sh\nexit 1\n",
		"#!/bin/sh\necho monitor-agent not-a-version\n",
		"#!/bin/sh\necho monitor-agent 1.2\n",
		"#!/bin/sh\necho monitor-agent 999999999999999999999999999.0.0\n",
	} {
		if err := os.WriteFile(filepath.Join(installed, "monitor-agent"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}

		stdout, stderr, err := run.start(t)
		if err != nil {
			t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "could not tell") {
			t.Errorf("the run does not say it could not read the installed version:\n%s", stdout)
		}
	}
}

// spec: installer.md#fetching-and-checking-a-release — an installed binary another account
// could have replaced is not run at all: its version is worth less than the risk of asking.
func TestAReplaceableBinaryIsNotRunToReadItsVersion(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	installed := filepath.Join(run.destDir, "usr", "local", "bin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(installed, "monitor-agent")
	// It would refuse the run if it were read: it reports a version newer than the release.
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho monitor-agent 9.9.9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Explicitly, because WriteFile takes the umask off the mode it is given.
	if err := os.Chmod(binary, 0o777); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the run failed: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "another account can replace it") {
		t.Errorf("the run does not say why it did not read the version:\n%s", stdout)
	}
}

// spec: installer.md#fetching-and-checking-a-release — a version with three numeric parts is
// read whatever its length, so a valid one never disables the guard by accident.
func TestALongVersionStillBlocksADowngrade(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "agent", "--hub", "https://hub.example.com", "--node", "laptop-a")
	installed := filepath.Join(run.destDir, "usr", "local", "bin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "monitor-agent"),
		[]byte("#!/bin/sh\necho monitor-agent 100.100.100\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := run.start(t)

	if err == nil {
		t.Fatalf("1.2.3 was installed over 100.100.100\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "100.100.100") {
		t.Errorf("the refusal does not name the installed version:\n%s", stderr)
	}
}

// spec: installer.md#fetching-and-checking-a-release — the ways a run can be asked the wrong
// question, none of which fetches anything.
func TestABootstrapRunRefusesItsArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		says string
	}{
		{"no role", nil, "no role"},
		{"an unknown role", []string{"gateway"}, "unknown role"},
		{"an unknown option", []string{"agent", "--wat"}, "--wat"},
		{"an option without its value", []string{"agent", "--version"}, "--version"},
		{"an empty version", []string{"agent", "--version", ""}, "--version"},
		{"--hub given to the hub role", []string{"hub", "--hub", "https://hub.example.com"}, "--hub"},
		{"--node given to the hub role", []string{"hub", "--node", "laptop-a"}, "--node"},
		{"a role given twice", []string{"agent", "agent"}, "unknown option"},
		{"an empty --node", []string{"agent", "--node", ""}, "--node"},
		{"a version with a trailing dot", []string{"agent", "--version", "1.2.3."}, "not a version"},
		{"a version with a doubled dot", []string{"agent", "--version", "1..3"}, "not a version"},
		{"a component no shell compares as a number", []string{"agent", "--version", "9999999999.0.0"}, "not a version"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			run := newBootstrapRun(t, o, test.args...)

			stdout, stderr, err := run.start(t)

			if err == nil {
				t.Fatalf("the run succeeded; it had to refuse\n%s%s", stdout, stderr)
			}
			if !strings.Contains(stderr, test.says) {
				t.Errorf("the refusal does not say %q:\n%s", test.says, stderr)
			}
			if asked := o.requests(); len(asked) != 0 {
				t.Errorf("a refused argument still fetched %v", asked)
			}
		})
	}
}

// spec: installer.md#fetching-and-checking-a-release — -h is a request: the usage goes to
// stdout and the run succeeds.
func TestBootstrapHelpGoesToStdout(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "-h")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("-h failed: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "usage:") || stderr != "" {
		t.Errorf("the usage is not on stdout alone:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

// spec: installer.md#fetching-and-checking-a-release — an origin that cannot be reached
// leaves the machine as it was.
func TestAnOriginThatCannotBeReachedInstallsNothing(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "hub")
	run.env = []string{"MONITOR_RELEASE_ORIGIN=http://127.0.0.1:1"}

	stdout, stderr, err := run.start(t)

	if err == nil {
		t.Fatalf("the run succeeded; the origin is not there\n%s%s", stdout, stderr)
	}
	if _, err := os.Stat(run.record); err == nil {
		t.Error("an unreachable origin still reached an installer")
	}
}

// spec: installer.md#unpacking — the working directory is the run's own and is gone when the
// run ends, whether it installed anything or refused.
func TestTheWorkingDirectoryIsGoneAfterwards(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"a run that installs", []string{"hub"}},
		{"a run that refuses", []string{"hub", "--version", "9.9.9"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			run := newBootstrapRun(t, o, test.args...)
			tmp := t.TempDir()
			run.env = []string{"TMPDIR=" + tmp}

			// Whether it installed or refused is this row's business only in that both
			// leave the same nothing behind.
			_, _, _ = run.start(t)

			left, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			if len(left) != 0 {
				t.Errorf("the run left %d entries behind in its TMPDIR", len(left))
			}
		})
	}
}

// realArchive packs the installer exactly as the release workflow does, so what the tests
// unpack is what a release carries.
func realArchive(t *testing.T) []byte {
	t.Helper()
	staging := filepath.Join(t.TempDir(), "monitor-installer")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	copies := map[string]string{
		"install.sh":             "install.sh",
		"install-hub.sh":         "install-hub.sh",
		"install-agent.sh":       "install-agent.sh",
		"hub.env.example":        "hub.env.example",
		"agent.env.example":      "agent.env.example",
		"../config.example.yaml": "hub.yaml.example",
	}
	for source, name := range copies {
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(staging, name), body, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"systemd", "launchd"} {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(staging, dir, entry.Name()), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	packed, err := exec.Command("tar", "-czf", "-", "-C", filepath.Dir(staging), "monitor-installer").Output()
	if err != nil {
		t.Fatalf("packing the installer: %v", err)
	}
	return packed
}

// spec: installer.md#installing-the-agent — the whole chain, with the installer a release
// really carries: the node ends in the state deployment.md describes.
func TestTheWholeChainInstallsAnAgent(t *testing.T) {
	o := newOrigin(t)
	o.set("monitor-installer-"+o.version+".tar.gz", realArchive(t))
	o.set(o.binaryName("agent"), []byte("#!/bin/sh\necho monitor-agent 1.2.3\n"))
	o.sign(t)
	run := newBootstrapRun(t, o, "agent", "--hub", exampleHub, "--node", testNode)
	run.stdin = testToken

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the chain failed: %v\n%s%s", err, stdout, stderr)
	}

	assertLayout(t, run.destDir)
	assertEnv(t, run.destDir, exampleHub, testNode, testToken)
}

// spec: installer.md#installing-the-hub — the same chain for the hub, which is where the
// examples and the stopped service come from.
func TestTheWholeChainInstallsAHub(t *testing.T) {
	o := newOrigin(t)
	o.set("monitor-installer-"+o.version+".tar.gz", realArchive(t))
	o.set(o.binaryName("hub"), []byte("#!/bin/sh\necho monitor-hub 1.2.3\n"))
	o.sign(t)
	run := newBootstrapRun(t, o, "hub")

	stdout, stderr, err := run.start(t)
	if err != nil {
		t.Fatalf("the chain failed: %v\n%s%s", err, stdout, stderr)
	}

	assertHubLayout(t, run.destDir)
	if !strings.Contains(stdout, "hub.yaml") {
		t.Errorf("the run does not name the configuration it still needs:\n%s", stdout)
	}
}

// spec: installer.md#staged-installs — a run that is not staged and not root writes nothing,
// and says which it is.
func TestARunThatIsNeitherStagedNorRootRefuses(t *testing.T) {
	o := newOrigin(t)
	run := newBootstrapRun(t, o, "hub")
	run.destDir = ""
	run.env = []string{"MONITOR_RELEASE_ORIGIN=", "MONITOR_RELEASE_KEY="}

	stdout, stderr, err := run.start(t)

	if err == nil {
		t.Fatalf("the run succeeded; the suite does not run as root\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "must run as root") {
		t.Errorf("the refusal does not name the cause:\n%s", stderr)
	}
	if asked := o.requests(); len(asked) != 0 {
		t.Errorf("a run that could not install still fetched %v", asked)
	}
}
