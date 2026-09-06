# Spec: Installer

- **Status:** approved
- **Owns:** `deploy/monitor-install.sh` — the script an operator runs — and the contents of
  the installer archive a release carries: `install.sh`, `install-hub.sh`, and the copies of
  `install-agent.sh` and the service definitions that travel with them
- **Decisions:** [0005](../decisions/0005-poc-stack.md),
  [0007](../decisions/0007-public-repository.md),
  [0019](../decisions/0019-deployment-layout.md),
  [0020](../decisions/0020-agent-reads-its-environment-file.md),
  [0021](../decisions/0021-shell-is-linted-too.md),
  [0022](../decisions/0022-updates-are-pulled.md)

## Purpose

One command puts a hub or an agent on a machine, and the same command upgrades it. This spec
owns what that command does and what it refuses.

It is the first half of [0022](../decisions/0022-updates-are-pulled.md) — points 1, 2 and 6:
a release that carries its own installer, verified before anything from it runs. The timer,
the hub naming a target version, and an installer that answers with a version instead of
installing are the second half and are not here. Until they exist, **an upgrade is the
operator running the same command again**, and nothing on the machine updates itself.

`install-agent.sh` keeps the contract [deployment.md](deployment.md) gives it — a binary that
already exists, passed as `--binary` — because it is what repairs a machine this installer
broke. The installer calls it rather than replacing it.

## The two halves, and what is not frozen yet

**`monitor-install.sh` is what the operator runs**, over `curl` or from a checkout. It
downloads a release, checks it, unpacks it and hands over; it decides nothing else. It
carries the release signing key, because a release cannot vouch for itself
([0022](../decisions/0022-updates-are-pulled.md), point 2). That copy is the one in
`deploy/release-signing-key.pub` — nothing generates one from the other, and a test asserting
they are the same bytes is what keeps them from drifting. Beyond the shell it needs `curl`,
`openssl`, `tar` and `find`, and it names whichever is missing.

**No machine keeps a copy of the key that a release cannot replace.** The script is fetched
for each run; what a release leaves behind is the binary, the service definition and the
examples, and every one of those the next release overwrites. So rotating the signing key
stays what [release.md](release.md) already describes — three commands and a commit. The resident, frozen half of
[0022](../decisions/0022-updates-are-pulled.md) arrives with the timer, and **that** is the
unit of work which must answer where an offline copy of the private key lives and how many
keys the frozen half carries; this spec deliberately leaves both open, because it freezes
nothing.

**The installer travels inside the release**, as `monitor-installer-<version>.tar.gz` listed
in that release's signed manifest ([release.md](release.md) owns the asset; this spec owns
its contents). It holds `install.sh`, the per-binary installers, the service definitions and
the example configuration, laid out as `deploy/` is, because `install-agent.sh` finds its
service definitions beside itself.

**An asset is named, not searched for.** The run builds each asset's name from the version
and the platform it decided on, and requires the manifest to list exactly that name. A
signature therefore says "this is what that release published under that name", which is
what makes a renamed or substituted asset detectable ([release.md](release.md)).

**What is fetched is what is checked.** `monitor-install.sh` downloads the manifest, its
signature, the archive and the one binary the machine needs, and verifies every one of them
before the first is used. The installer downloads nothing and verifies nothing: giving it
either would put a verifier inside the release, which is the release vouching for itself.
The rules it applies are `deploy/verify-release.sh`'s, and the two must agree
([release.md](release.md#verifying-an-artifact)).

**The trust in the script itself rests on TLS and on the GitHub account.** Whoever serves
`monitor-install.sh` chooses the key inside it, so nothing below can detect a substitution:
that is the trust an operator gives once, and the reason the file is small enough to read
first. The fingerprint of the key it carries is published in [install.md](../install.md), so a
downloaded copy can be checked against something outside itself. The script's own hash is
not: it would change with every edit and go stale in the guide, while the fingerprint changes
only when the key is rotated.

**The whole script is one function, invoked on its last line**, so a transfer cut short
executes nothing rather than half of itself.

## The handover

What one half says to the other is the contract [0022](../decisions/0022-updates-are-pulled.md)
warns is the expensive one to change, so it is written down rather than left to the code:

```
sh <unpacked>/install.sh <role> --binary <verified path> [--hub <url> --node <name>]
```

- The agent's token reaches the installer on stdin, which is the run's own stdin passed
  through. A `hub` run is given `/dev/null` instead, because the hub takes no token and a run
  in a pipeline would otherwise hand it whatever is on that pipe.
- The environment is inherited, `DESTDIR` and `MONITOR_TOKEN` among it —
  [deployment.md](deployment.md) makes that variable the token's other documented route, and
  a run cannot take it away without breaking the route it names. What the run sets for
  itself, and therefore hands on, is `LC_ALL`, `umask` and — on a real run — `TMPDIR`.
- The binary is already verified and already executable when the installer sees it.
- The run exits with the installer's status, and passes its output through unchanged.

## Behaviour

One row = one test. Anchors: `spec: installer.md#<heading>`. **Every refusal exits non-zero
and says why on stderr**, and no row repeats that. The rows are tested against a synthetic
release served over loopback, using the seams below.

Some rows a staged suite cannot reach, because staging is defined as not doing those things:
creating the account and accepting an existing one, refusing one that can log in, telling
systemd about the unit and starting the service, correcting an owner, refusing a host with no
systemd, ignoring an inherited `TMPDIR`, and stripping an archive's owner and setuid bits,
which an unprivileged tar would not have restored anyway. They are proved on a real host;
everything else in these tables is proved by the suite.

`monitor-install.sh <role> [options]`, where `<role>` is `hub` or `agent`.

### Fetching and checking a release

| Event | Outcome |
|---|---|
| a run for `hub` or for `agent` | the binary in place is the one the newest release names for that role and this platform, and its version is that release's |
| `--version 1.2.3` | that release is used instead of the newest |
| `--version` that is not `MAJOR.MINOR.PATCH`, or whose component is wider than a shell compares as a number | nothing is fetched, and the run names the value it refused |
| `--version` naming a release that does not exist | nothing is installed, and the run names the version |
| a manifest whose signature does not verify | nothing is unpacked and nothing is installed |
| a manifest signed by another key | the same |
| a signature file that is empty or not a signature | the same |
| an artifact whose digest is not the one the manifest names | the same |
| a release whose manifest lists no installer archive | nothing is installed, and the run says that release predates the installer |
| a release with no binary for this platform | nothing is installed, and the run names the asset it wanted |
| the release cannot be reached, or the download is cut short | nothing is installed and the machine is left as it was |
| a version older than the one the installed binary of that role reports | nothing is installed, and the run says so; `--allow-downgrade` installs it |
| `1.10.0` against an installed `1.9.0` | it installs: versions compare number by number, not as text |
| nothing installed, or a binary that will not run or reports something that is not a version | it installs, and says it could not tell what was there |
| an installed binary that another account could have replaced — it or its directory writable by group or other, or on a real run owned by anyone but root | its version is not read at all, and the run says so and installs |
| an option this run does not know, or one given without its value | the usage on stderr |
| `--hub` or `--node` given to the `hub` role | the usage on stderr |
| no `curl`, `openssl`, `tar` or `find` on `PATH` | the run names the one that is missing |
| a script truncated in transit | nothing runs at all |
| a machine whose shell runs under Rosetta | the `darwin-arm64` asset is what lands: the run reads `uname` and `sysctl.proc_translated` off `PATH` |
| a run that installs anything | it prints the version it installed, every path it wrote, and the command that shows the service's state |
| `-h` | the usage on stdout, exit 0 |
| no role, an unknown role, or a role given twice | the usage on stderr |

The verdict on a signature is `openssl`'s exit status and never its output, for the reason
[release.md](release.md#verifying-an-artifact) records.

### Unpacking

| Event | Outcome |
|---|---|
| any run | everything downloaded lands in a directory this run made for itself, readable by nobody else, and gone when the run ends — on success, on a refusal and on `INT`, `TERM` or `HUP` |
| an inherited `TMPDIR` | a real run ignores it and makes its directory under a root-owned base; only a staged run honours it |
| a run killed outright | what it may leave behind holds no secret: the token never reaches the disk |
| an archive holding an absolute path, a `..` path, or anything but a file or a directory | nothing is extracted and nothing is installed |
| an archive whose entries are not one top-level directory holding `install.sh` | nothing is installed |
| an archive whose files carry an owner, a setuid or a setgid bit | what lands takes none of them |

### Installing the hub

| Event | Outcome |
|---|---|
| a run on a host with no hub and no configuration | the binary, the service definition, the account and the directories of [deployment.md](deployment.md#where-things-live) are in place, the examples are beside where the real files belong, and the service is installed but not started |
| the same run | it names each configuration file that is missing and prints the command that installs an example as the real one, with the owner and mode the layout fixes |
| a run where `hub.yaml` and `hub.env` are both present | the service ends up running on the new binary |
| a run where an older hub is installed and configured | the binary and the service definition are replaced, the configuration is untouched, and the service comes back on the new binary |
| a `monitor` account that already exists and cannot log in | it is used unchanged |
| a `monitor` account whose shell is a login shell | nothing is installed: the run names the account rather than hand it the hub's secrets |
| `/etc/monitor` or `/var/lib/monitor` that is a symlink, or is owned by anyone but the account the layout names | nothing is installed, and the run names the path |
| a configuration file whose owner or mode is not the layout's | it is corrected, and the run says which |
| examples the operator has edited | they are overwritten; the examples are the release's and the real files are the operator's |
| a real run on a host with no systemd — macOS among them | nothing is installed: the hub is a Debian service ([0005](../decisions/0005-poc-stack.md)) |

### Installing the agent

| Event | Outcome |
|---|---|
| a run with `--hub`, `--node` and a token on stdin | the node ends as [deployment.md](deployment.md#installing-on-a-fresh-node) describes it |
| a run on a host that already has an agent, with no token | the binary is replaced, the stored token is kept, and `--hub` and `--node` are still required |
| a run with no token available and none stored | nothing is installed, and the run says where a token is read from |
| a run against a stored environment file the agent itself would refuse | nothing is installed ([0020](../decisions/0020-agent-reads-its-environment-file.md)) |
| any run | the token reaches the installer on stdin and appears in no argument, no message and no environment of a child process |

### Staged installs

| Event | Outcome |
|---|---|
| `DESTDIR` naming an absolute path that is not `/` | everything is staged under it, no account is created, and no service command runs |
| `DESTDIR` relative, or naming a path that resolves to `/` — `/`, `//`, `/.`, `/etc/..` | nothing is written, and the run says why: a staged run that writes into the real system is not a staged run |
| `DESTDIR` empty | it is not a staged run at all, and the row below applies |
| no `DESTDIR` and not root | nothing is written, and the run says it needs root |
| `MONITOR_RELEASE_ORIGIN` or `MONITOR_RELEASE_KEY` in a run that is not staged | nothing is fetched, and the run names the variable it refused; both are answered before the check for root, so a run that is not root still says which one was wrong |
| either of them in a staged run | honoured: the release is fetched from that origin and checked with that key |

## Invariants

- No code from a release runs, and no file from one is installed, before its digest and the
  manifest's signature have been checked — with the key `monitor-install.sh` carries, or with
  the one `MONITOR_RELEASE_KEY` names, which only a staged run accepts.
- A run that writes a single file outside `DESTDIR` used the key the script carries.
- The token lives in the run's memory and on the pipe it hands over: it reaches no file, no
  argument and no message. The environment variable [deployment.md](deployment.md) documents
  stays the operator's to use or not.
- An asset is required to be in the manifest under the name the run built for it, so what a
  signature says is what that release published under that name.
- The installer that runs from a release downloads nothing.
- No configuration is invented. The one value the script does carry is where releases come
  from, and it is a product default like any other ([0007](../decisions/0007-public-repository.md)).
- Nothing is written outside the run's own directory until every check has passed; after
  that, a failure stops and names what it wrote, and does not roll back
  ([deployment.md](deployment.md#invariants)).
- A second run against the same bytes leaves every installed file byte-identical and the
  service running. The same version is not a promise of the same bytes: a release's assets
  are mutable, as [release.md](release.md) records.
- The downgrade guard is protection against an operator's slip, never against an attack: the
  version it compares against is reported by the very binary it is protecting, so a run that
  cannot read one installs rather than refusing. Reading it means running that binary as
  root, which is done only where nobody but root could have put it there.

## Edge cases

- **`curl … | sudo sh` consumes stdin**, so a token cannot arrive that way. The one-line form
  is for upgrades, and it still needs its arguments —
  `curl … | sudo sh -s -- agent --hub … --node …`. A first install with a token is the
  two-step form: download the script into a directory only root can write, check the key it
  carries against the fingerprint [install.md](../install.md) publishes, run it with the
  token on stdin, and delete it — a copy left lying around is a copy with an old key and an
  old origin.
- **A release older than this work** carries binaries and no archive: the run says so and
  installs nothing. Reaching it is the manual path of [install.md](../install.md).
- **A machine that is both hub and node** runs the command once per role, and gets one
  environment file per binary, as [0019](../decisions/0019-deployment-layout.md) requires.
- **The hub is upgraded under a running service.** Replacing the binary does not restart it
  by itself; the run restarts the service, and a hub that will not start on the new binary is
  recovered by the manual path.
- **Rotating the signing key** touches four places: the secret in the release environment,
  `deploy/release-signing-key.pub`, the copy inside `monitor-install.sh`, and the fingerprint
  [install.md](../install.md) publishes. The last two are asserted by tests, so missing
  either turns the suite red rather than the fleet. Nothing on any machine holds a key that a
  release cannot replace, which is what keeps rotation cheap at this stage.
- **A signature does not prove freshness.** Whoever answers for the origin can serve a
  genuine older release for ever, and every check here passes. The origin is a product
  default that only a staged run may override, and a signed statement of what is current is
  the second half's problem ([0022](../decisions/0022-updates-are-pulled.md)).
- **Two runs at once on one machine** are not supported: the environment file is read and
  rewritten, so a token rotated by one run can be lost by the other. Each file still ends as
  one of the two runs left it.

## Where a release is fetched from

The origin is one base URL, a product default naming this repository, joined into two shapes:
the newest release is the version the origin's `releases/latest` answers with, and an asset is
`<origin>/releases/download/v<version>/<asset name>`. A staged run may point both at another
base, which is what lets the rows above be tested against a release served over loopback.

## Out of scope

- The timer, the resident half it needs, the hub naming a target version, and the rollback
  that follows — the rest of [0022](../decisions/0022-updates-are-pulled.md), including where
  an offline copy of the private key lives and how many keys the frozen half carries.
- What an installation looks like on disk: [deployment.md](deployment.md) owns every path,
  owner and mode, and this spec installs what that one describes.
- What a release contains and how it is signed: [release.md](release.md).
- nginx, TLS and authentication in front of the hub.

## Open questions

None. The key questions this work uncovered belong to the unit that introduces a resident
half, and are recorded under Out of scope rather than answered here.
