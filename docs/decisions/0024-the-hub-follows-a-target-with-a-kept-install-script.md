# 0024. The hub follows a target file with a kept copy of the install script

- **Status:** accepted
- **Amended by:** [0025](0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md),
  for point 4's answer contract, point 5's test of a release already in place, the daily
  timer, and the consequences naming the frozen options and the rollback floor
- **Amended by:** [0028](0028-agents-follow-a-target-the-hub-serves.md), which answers
  point 7 for a kept script on a node: one key there too
- **Date:** 2026-09-14
- **Source:** [0022](0022-updates-are-pulled.md) points 3, 5, 6 and 7, and the open key
  question [release.md](../specs/release.md#generating-and-rotating-the-key) left for this
  unit of work

## Context

[0022](0022-updates-are-pulled.md) decided that every machine pulls its own updates through a
frozen stub, that the release's installer chooses the version, and that the hub's target is
set on the hub's own host. It left the stub's shape, the target's form and the key the stub
carries to the unit that builds it. This is that unit, for the hub alone: the hub host is one
Debian machine provisioned by Ansible, and the agents stay on the manual path until their
target is served by the hub.

`monitor-install.sh` already does everything 0022 asks of a stub — fetch the newest release,
verify its manifest with a key it carries, unpack the installer and hand over — and a staged
suite proves it. What it lacks is the second half of point 6: an installer that can name a
version instead of installing, and a stub that fetches that version when it does.

## Decision

1. **The stub is `monitor-install.sh` itself**, kept on the hub host at
   `/usr/local/libexec/monitor/monitor-install.sh` and run as
   `monitor-install.sh hub --follow-target`. There is one verifier and one fetch path, not a
   second copy of either.
2. **The resident half is that copy, `monitor-hub-update.service` and
   `monitor-hub-update.timer`.** They are placed and changed by the manual path of
   [install.md](../install.md) or by whatever provisions the host, never by a release: a
   release that could rewrite them could break the thing that repairs it.
3. **The target is `/etc/monitor/hub.target`**, one line holding `latest` or a version.
   `latest` follows the newest release; a version pins the hub to it, rollback included.
4. **The installer answers into a file the stub names.** A follow run hands over with
   `--follow-target --release <version> --newest <version> --answer <file>`; the installer
   installs when the target resolves to its own release, and otherwise writes the version it
   needs into that file and installs nothing. The stub fetches that one release and hands
   over again; a second answer stops the run.
5. **A follow run that finds the target's release already in place touches nothing**, the
   service included: the binary and the service definition are the release's, and the service
   is running that binary. The timer runs daily, and a restart costs a moment of `502` behind
   the proxy. Anything short of that is installed as usual, so a run stopped before its
   restart is finished by the next one.
6. **The stub keeps the downgrade guard for the newest release.** Only a version an installer
   named may be older than the hub in place, so an origin serving an old genuine release as
   the newest cannot roll the hub back on its own.
7. **The stub carries one public key and there is no copy of the private half.** Losing the
   key means placing a new stub over the manual path, which on one provisioned host is one
   play. This is accepted for the hub and is answered again before a stub reaches a node that
   has to be touched by hand.

## Consequences

- A merge reaches the hub within a day unless the target pins a version or the pull request
  carries `release:none`. Holding a version back and rolling back are edits to one file.
- The answer contract of point 4 is frozen from the first kept stub on: every future
  installer must accept those four options and keep the meaning of the answer file.
- A target can roll back no further than the first release whose installer understands
  `--follow-target`. Below it, the older installer refuses the unknown option and nothing is
  installed.
- A key rotation gains a step: replacing the kept stub on the hub host, which then verifies
  no release signed with the old key, so the rollback floor rises to the first release signed
  with the new one. Until an agent keeps a stub, the hub host is the only machine it touches.
- Every run hands over to the newest release's installer first, so a broken newest installer
  blocks rollback too; the remedy is a fixed release or the manual path.
- The hub host's provisioning writes the target and the resident half, and runs the update
  service once instead of calling the install script with a version for the hub.

## Alternatives

- **A separate, smaller stub script** — rejected: it would be a second verifier and a second
  fetch path kept in step with `monitor-install.sh`, for a size difference that buys nothing
  on a host provisioned by a play.
- **The release installer places the stub and the timer** — rejected by the regress
  [0022](0022-updates-are-pulled.md) records: a broken release would break its own repair.
- **The target holds a version only** — rejected: on a provisioned host the play already
  installs a pinned version, so the timer would add nothing.
- **No target: the hub follows the newest release** — rejected: it gives up rollback and
  contradicts [0022](0022-updates-are-pulled.md) point 5.
- **The stub reads the target itself** — rejected for the reason 0022 gives: how a target is
  read belongs in the release, where it can still change.
- **The answer as an exit status or a line of output** — rejected: a status carries no
  version, and parsing output couples the frozen stub to wording a release may change.
- **Restarting the hub on every run** — rejected: a daily outage for no change.
- **Two keys, or an offline copy of the private half** — deferred, not rejected. Either
  spares a rotation the hands on every machine; with one provisioned machine that cost is one
  play.
