# 2026-09-30 — Time Machine

The MVP left backups out because a backup sensor seemed to need a parameter — which file,
which job — and that would extend the contract of
[0010](../decisions/0010-agent-configuration.md)
([#51](https://github.com/pravbeseda/monitor/issues/51)). Time Machine does not: it keeps its
own list of destinations and the dates of the backups on each, so a Mac's backups are read
with no configuration at all, and #51 narrows to the backups that do name something.

## Where the dates come from

- **`tmutil latestbackup` — rejected.** It refuses without Full Disk Access, and granting a
  daemon that is a manual step on every Mac the installer cannot take.
- **Reading the preferences file directly — rejected** for the same reason: privacy
  protection blocks it, even for root.
- **The preferences service (`defaults export`) — chosen.** It answers without Full Disk
  Access. Its one trap is that a domain it cannot read comes back as an empty dictionary, the
  same answer as "never configured"; the file's existence, which `stat` can see, tells the two
  apart.
- **`AttemptDates`, `ReferenceLocalSnapshotDate`, `LastBackupActivity` — rejected** in favour
  of `SnapshotDates`: an attempt may produce no backup, the local snapshot marks a backup's
  start, and `LastBackupActivity` is a local-time string for no destination in particular.

## One series per destination

Time Machine backs up to several destinations in turn, and a destination kept by accident — a
second backup volume on the same disk — is easy to miss. One series per destination shows
each one falling behind; a single "freshest backup anywhere" series would hide it. That series
is a derived one, and derived series belong to the semantic engine, not to a sensor — the
same line [host-sensors.md](../specs/host-sensors.md) draws for load per core.

## What the spec reviews changed

- **One bad destination no longer blanks the rest.** The first draft failed the whole
  reading on a duplicate name or a date ahead of the clock; the disk sensor's rule — one
  unreadable volume never costs the others — won, so such a destination is left out alone.
- **An empty answer is an error when the file exists**, not "no destinations": otherwise the
  sensor built to catch a silent failure would fail silently itself.
- **A removed destination stays on `/debug`**, marked stale and frozen, rather than
  "vanishing like a disk": only removable series are hidden.
- **The threshold example became a number of seconds**: the thresholds page takes a unit
  suffix on sizes only.
