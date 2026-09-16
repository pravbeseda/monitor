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
  [0022](../decisions/0022-updates-are-pulled.md),
  [0024](../decisions/0024-the-hub-follows-a-target-with-a-kept-install-script.md),
  [0025](../decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md),
  [0027](../decisions/0027-the-hub-installer-reuses-the-binary-in-place.md)

## Purpose

One command puts a hub or an agent on a machine, and the same command upgrades it. On the
hub host a kept copy of that command, run by a timer, keeps the hub on the version the host
names. This spec owns what the command does and what it refuses, in both uses.

It is [0022](../decisions/0022-updates-are-pulled.md) for the hub, shaped by
[0024](../decisions/0024-the-hub-follows-a-target-with-a-kept-install-script.md): a release
that carries its own installer, verified before anything from it runs, and an installer that
answers with a version instead of installing. An agent is still upgraded by the operator
running the same command again.

`install-agent.sh` keeps the contract [deployment.md](deployment.md) gives it — a binary that
already exists, passed as `--binary` — because it is what repairs a machine this installer
broke. The installer calls it rather than replacing it.

## The two halves

**`monitor-install.sh` is what the operator runs**, over `curl` or from a checkout. It
downloads a release, checks it, unpacks it and hands over; it decides nothing else. It
carries the release signing key, because a release cannot vouch for itself
([0022](../decisions/0022-updates-are-pulled.md), point 2). That copy is the one in
`deploy/release-signing-key.pub` — nothing generates one from the other, and a test asserting
they are the same bytes is what keeps them from drifting. Beyond the shell it needs `curl`,
`openssl`, `tar` and `find`, and it names whichever is missing.

**The hub host keeps one copy of the key a release cannot replace**: the kept script the
timer runs ([Following a target](#following-a-target)). An operator's run still fetches the
script each time, and what a release leaves behind is the binary, the service definition and
the examples, every one of which the next release overwrites. So a rotation is what
[release.md](release.md) describes plus replacing the kept script on the hub host. It carries
one key and no copy of the private half exists
([0024](../decisions/0024-the-hub-follows-a-target-with-a-kept-install-script.md), point 7).

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
signature, the archive and the one binary the machine needs, and verifies each of them before
it is used; a follow run downloads the binary last, only once an installer asks for it, and
checks it against the manifest it already verified for that release in the same run. The
installer downloads nothing and verifies nothing: giving it either would put a verifier
inside the release, which is the release vouching for itself. The digest it computes of the
binary in place, with `openssl`, decides whether there is anything to do and whether that
binary is kept: a match keeps bytes that only root could have put there
([0027](../decisions/0027-the-hub-installer-reuses-the-binary-in-place.md)), and a false
mismatch asks for a binary the kept script verifies.
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

A follow run hands over with five options of its own and, at first, no binary. Only the
hub's installer accepts them today; the form names no role of its own, so an agent's
installer can take the same five
([0025](../decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md)):

```
sh <unpacked>/install.sh <role> --follow-target --release <this release's version> \
    --newest <newest version> --digest <sha256 of this release's binary for the role> \
    --answer <file> [--binary <verified path>]
```

- `--answer` names an empty file. An installer that exits 0 and leaves it empty has nothing
  further for the run to do — it installed, found nothing to change, or declined — and the
  run claims nothing of its own. One that writes into it installs nothing and exits 0; what
  it writes is one `MAJOR.MINOR.PATCH`, a trailing newline allowed, and anything else is
  refused by the run. Another release's version asks for that release; its own `--release`
  asks for this release's binary. A non-zero exit is a refusal whatever the file holds.
- `--binary` is given only after an installer asked for it, together with the same five
  options, and an installer handed it answers nothing: an answer then stops the run.
- The installer downloads nothing. `--newest` is how `latest` is resolved without asking the
  origin, and every hand-over of one run passes the same value; `--digest` is how it tells,
  without the binary, whether the binary in place is already this release's.
- A hand-over to the release an installer named runs that release's installer, out of a
  directory of its own, with an empty answer file.

**What a kept script depends on is frozen from the first kept script on**, because it stays
on its host until someone replaces it: these five options and the answer's meaning — at most
three hand-overs, a binary only after an answer naming the installer's own release, and an
answer after `--binary` stopping the run — the
handover form above, the two URL shapes of [Where a release is fetched
from](#where-a-release-is-fetched-from), the asset names and the manifest format of
[release.md](release.md), the archive being one directory holding `install.sh`, the signature
algorithm, and the command line `monitor-hub-update.service` starts. Changing one of them is
replacing the script on every host that keeps one.

## Behaviour

One row = one test. Anchors: `spec: installer.md#<heading>`. **Every refusal exits non-zero
and says why on stderr**, and no row repeats that. The rows are tested against a synthetic
release served over loopback, using the seams below.

Some rows a staged suite cannot reach, because staging is defined as not doing those things:
creating the account and accepting an existing one, refusing one that can log in, telling
systemd about the unit and starting the service, correcting an owner, refusing a host with no
systemd, ignoring an inherited `TMPDIR`, treating an installed binary owned by anyone but
root as replaceable, stripping an archive's owner and setuid bits, which an unprivileged
tar would not have restored anyway, refusing a target owned by anyone but root, not taking a
hub binary owned by anyone but root for the release's binary in place, and telling
a running hub on the release's binary from a stopped one or one still on the binary it
replaced. They are proved on a real host; everything else in these
tables is proved by the suite.

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

### Following a target

`monitor-install.sh hub --follow-target` is what `monitor-hub-update.service` runs from the
kept copy ([deployment.md](deployment.md#where-things-live)). The target is
`/etc/monitor/hub.target`, holding exactly `latest` or one `MAJOR.MINOR.PATCH`, a trailing
newline allowed. The rows of [Fetching and checking a release](#fetching-and-checking-a-release)
and [Unpacking](#unpacking) hold for each release a follow run fetches.

| Event | Outcome |
|---|---|
| target `latest` | the newest release's hub is installed |
| target naming the newest version | the same |
| target naming an older release | that release's hub is installed, even over a newer one in place: a target is a deliberate choice, and the downgrade guard judges the newest release alone |
| the newest release older than the hub in place | nothing is installed whatever the target, and the run says so: an origin serving an old release as the newest cannot roll the hub back |
| any follow run | the newest release's manifest and installer are downloaded and verified first, even when the target names another, and no binary is downloaded before an installer asks for it |
| a hub already running the release its target resolves to | no binary is downloaded, and nothing is installed |
| a hub whose binary in place is already the release its target resolves to, but that is not running it or whose service definition differs | no binary is downloaded, and the rest is installed around the binary in place |
| an installer that asks for its own release's binary | that binary is downloaded once, checked against the manifest this run already verified for that release, and handed over with the same options |
| an installer handed its binary that answers anyway | nothing is installed, and the run says that installer answered after it was given its binary |
| target naming a release whose manifest lists no installer archive | nothing is installed, and the run says that release predates the installer |
| target naming a release whose installer does not take this hand-over | nothing is installed, and that installer's refusal is passed through |
| target naming a release that cannot be fetched once the newest one was verified | nothing is installed, and the run names the version |
| a hand-over to the release an installer named | it runs that release's installer, given that release's own `--digest`, the same newest version and an empty answer |
| the named release's installer names a version other than its own | nothing is installed, and the run names the version it followed and the one named after it |
| an answer that is not one `MAJOR.MINOR.PATCH` | nothing more is fetched, nothing is installed, and the run names the answer |
| an installer that exits 0 leaving the answer empty | the run ends with that installer's output and adds no claim of its own |
| `--follow-target` with the `agent` role | the usage on stderr: an agent's target is the hub's to name, and that is not built |
| `--follow-target` with `--version` or `--allow-downgrade` | the usage on stderr |

### Answering a follow run

What `install.sh hub` does when it is handed the five options of [The handover](#the-handover).

| Event | Outcome |
|---|---|
| a target that resolves to `--release`, with the hub already running that release — the binary in place is a regular file with the digest `--digest` names and the layout's mode and, on a real run, owner, the service definition is the release's and, on a real run, the service runs that binary | nothing is written, the answer stays empty, the run says the hub is already at that version, and no service command runs |
| a target that resolves to `--release`, the binary in place already that release — a regular file with the digest, mode and owner above — but the rest not so — a service definition that differs, a service that is stopped or still running the binary that was replaced — and no `--binary` | the answer stays empty, the run says it keeps the binary in place, and it installs as [Installing the hub](#installing-the-hub) says with that binary left untouched and its path not among those printed, so a configured hub is started and a run stopped before its restart is finished by the next one ([0027](../decisions/0027-the-hub-installer-reuses-the-binary-in-place.md)) |
| a target that resolves to `--release`, the binary in place not that release — another binary, a symlink or anything but a regular file, one whose mode or, on a real run, owner is not the layout's, or none — and no `--binary` | the answer holds `--release`, nothing is written outside it, and the exit is 0 |
| a target that resolves to `--release`, the hub not already running it, with `--binary` | it installs as [Installing the hub](#installing-the-hub) says and the answer stays empty, so a run stopped before its restart is finished by the next one |
| a target that resolves to another version | the answer holds that version, nothing is written outside it, and the exit is 0 |
| target `latest` | it resolves to `--newest` |
| no target file | nothing is installed, and the run names the file and the two forms it may take |
| a target that is empty, holds anything beside the value and one trailing newline, or holds neither `latest` nor `MAJOR.MINOR.PATCH` | nothing is installed, and the run names the file |
| the target or `/etc/monitor` a symlink, writable by group or other, or on a real run owned by anyone but root | nothing is installed, and the run names the path: the target chooses what root installs |
| only some of `--follow-target`, `--release`, `--newest`, `--digest` and `--answer` | nothing is installed, and the run names what is missing |
| `--release` or `--newest` that is not `MAJOR.MINOR.PATCH`, `--digest` that is not 64 lowercase hexadecimal digits, or `--answer` naming no file or a file that is not empty | nothing is installed, and the run names the value |

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
| `DESTDIR` naming an absolute path that is not `/` | everything is staged under it and read from under it, the target and the binary in place included; no account is created, and no service command runs |
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
  old origin. The copy the hub host keeps on purpose is replaced by hand for the same reason
  ([install.md](../install.md#keeping-the-hub-upgraded-unattended)).
- **A release older than this work** carries binaries and no archive: the run says so and
  installs nothing. Reaching it is the manual path of [install.md](../install.md).
- **A machine that is both hub and node** runs the command once per role, and gets one
  environment file per binary, as [0019](../decisions/0019-deployment-layout.md) requires.
- **The hub is upgraded under a running service.** Replacing the binary does not restart it
  by itself; the run restarts the service, and a hub that will not start on the new binary is
  recovered by the manual path.
- **Rotating the signing key** touches five places: the secret in the release environment,
  `deploy/release-signing-key.pub`, the copy inside `monitor-install.sh`, the fingerprint
  [install.md](../install.md) publishes, and the kept script on the hub host. The copy and the
  fingerprint are asserted by tests, so missing either turns the suite red rather than the
  fleet; the kept script is replaced over the manual path. A kept script with the new key verifies no release
  signed with the old one, so a rotation also raises the rollback floor to the first release
  signed with the new key.
- **A lost key and a leaked one differ.** A lost key leaves the kept script refusing every
  new release until it is replaced; the hub keeps the version it has. A leaked key is still
  accepted by the kept script, so after a leak the timer is stopped, or the script replaced,
  before anything else.
- **A signature does not prove freshness.** Whoever answers for the origin can serve a
  genuine older release for ever, and every check here passes, `latest` included. The origin
  is a product default that only a staged run may override, and a signed statement of what
  is current is not built.
- **Two runs at once on one machine** are not supported: the environment file is read and
  rewritten, so a token rotated by one run can be lost by the other. Each file still ends as
  one of the two runs left it. An operator's run while the timer's is in progress is the same
  case.
- **The rollback floor.** A target can go back no further than the first release whose
  installer takes `--digest`
  ([0025](../decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md)):
  v0.1.2's installer refuses `--digest` as an unknown option, an older one refuses
  `--follow-target` the same way, and a release older still carries no installer at all. Going below the floor is the manual path.
- **A hub that never counts as running** — one whose configuration is missing, so its service
  is never started, one crash-looping between restarts, or one stopped by hand — is installed
  again around the binary in place at every run, and a configured one is started again. Each
  such run rewrites the service definition and the examples, reloads systemd, and names what an
  unconfigured hub is missing. Its binary is downloaded only when the one in place is not the release's
  ([0027](../decisions/0027-the-hub-installer-reuses-the-binary-in-place.md)). A hub stopped
  for maintenance stays stopped only with its update timer stopped first
  ([install.md](../install.md#keeping-the-hub-upgraded-unattended)).
- **A broken newest installer blocks every target**, rollback included, because every run
  hands over to it first. The remedy is a fixed release, or the manual path.
- **A target changed between hand-overs** is read afresh by every installer. If it no longer
  names the release being handed over, the run stops — as for an answer naming yet another
  version, or for an answer after `--binary` — and the next run follows the new target.
- **An installer that neither installs nor answers** looks like a success to the run. It is
  the release's own business, and the installer's output says what it did.

## Where a release is fetched from

The origin is one base URL, a product default naming this repository, joined into two shapes:
the newest release is the version the origin's `releases/latest` answers with, and an asset is
`<origin>/releases/download/v<version>/<asset name>`. A staged run may point both at another
base, which is what lets the rows above be tested against a release served over loopback.

## Out of scope

- An agent following a target: the hub naming it, the agent's installer asking for it, and a
  kept script and timer on Debian and macOS — the rest of
  [0022](../decisions/0022-updates-are-pulled.md). Before a kept script reaches a node that
  is touched by hand, how many keys it carries is answered again
  ([0024](../decisions/0024-the-hub-follows-a-target-with-a-kept-install-script.md), point 7).
- Placing the kept script, its units and the target: [install.md](../install.md) and the
  host's provisioning. What they look like on disk, and every other path, owner and mode, is
  [deployment.md](deployment.md).
- What a release contains and how it is signed: [release.md](release.md).
- nginx, TLS and authentication in front of the hub.

## Open questions

None.
