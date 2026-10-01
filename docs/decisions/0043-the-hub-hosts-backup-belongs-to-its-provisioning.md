# 0043. The hub host's backup belongs to its provisioning; the repository states what it must keep

- **Status:** accepted
- **Date:** 2026-10-01
- **Source:** [issue #19](https://github.com/pravbeseda/monitor/issues/19),
  [hub-backup-requirements.md](../hub-backup-requirements.md),
  [hub-host-node-requirements.md](../hub-host-node-requirements.md)

## Context

Issue #19 asked for `deploy/backup-hub.sh`, run as root: snapshot the database, archive it
with `hub.yaml` and `hub.env`, encrypt the archive and copy it off the host. It was written
while the hub host was set up by hand and `hub.env` existed nowhere else, so losing the host
meant a fresh token on every node.

Since then the host is provisioned by Ansible from the repository that owns it, and that
play writes `hub.yaml` and `hub.env` whole on every run from its own variables and vault
([hub-host-node-requirements.md](../hub-host-node-requirements.md), requirement 10). Both
come back with one play. The database does not: since
[0032](0032-thresholds-are-set-in-the-interface.md) it holds the thresholds as well as the
history, and a hub restored without it watches nothing and looks healthy. A host provisioned
this way has no off-host copy of anything unless its play adds one, and the hub is not the
only thing on such a host that needs it.

## Decision

- **This repository ships no backup tool.** It states what a backup of the hub host has to
  keep, how the database is copied safely and how a restore is proven —
  [hub-backup-requirements.md](../hub-backup-requirements.md) — and how a restore is done —
  [install.md](../install.md#restore-on-another-host).
- **The provisioning repository implements it**: a scheduled snapshot of the database with
  SQLite's own online backup, run as `monitor`, and an encrypted off-host copy of it whose
  key the vault keeps.
- **The backup holds the database only.** `hub.yaml` and `hub.env` are restored from the
  vault, the binary from a release.
- **The hub gains no snapshot command.** The `sqlite3` package does it in one line.

## Consequences

- Restoring the hub host takes both repositories: the play rebuilds the host, the backup
  returns its database. Neither alone is enough, and the requirements document is what keeps
  them in step, as [nginx-requirements.md](../nginx-requirements.md) does for the proxy.
- An installation without such a provisioning repository gets the commands, not a tool: the
  restore section and the snapshot script are written for a person.
- A failed snapshot shows on the monitor itself, as `systemd.failed_units` on the hub host's
  node, once that series has a threshold. A snapshot that silently stops being scheduled
  does not, until the age of a file can be watched
  ([#51](https://github.com/pravbeseda/monitor/issues/51)).

## Alternatives

- **`deploy/backup-hub.sh`, as #19 proposed** — rejected: it keeps a second copy of every
  secret the vault already holds, and ships the archive with a tool chosen for the hub alone
  while the rest of the host needs an off-host copy just as much.
- **Running the snapshot as root, as #19 proposed** — rejected: with `hub.env` out of the
  backup nothing in it needs root, and the account that owns the database is the one that
  reads it.
- **A snapshot command in the hub** — rejected: Go code, a spec and tests for what one
  `sqlite3` line already does, and the provisioning would still schedule, encrypt and ship
  it.
- **A cron job** — rejected in favour of a systemd timer: a failed oneshot unit is a state
  the monitor already reads, where a failing cron job reaches only a mailbox nobody reads.
