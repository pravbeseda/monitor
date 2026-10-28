# 0049. A backup job reports through a status file the agent reads

- **Status:** accepted
- **Date:** 2026-10-28
- **Source:** [backup sensor spec](../specs/backup-sensor.md),
  [design notes](../log/2026-10-28-backup-sensor.md),
  [#51](https://github.com/pravbeseda/monitor/issues/51)

## Context

Backups fail silently, and the ones that matter on a server are jobs — a nightly archive to
another host, a copy of it to a disk at home, a periodic check that the copy still restores.
Time Machine keeps its own record of its backups
([timemachine-sensor.md](../specs/timemachine-sensor.md)); a job keeps none that the agent
could find. The job is also the only party that knows what it did: how many archives the
repository holds, their size, whether its verification passed. Reading a repository
directly needs its key and passphrase, and takes the repository's lock while a backup may
want it.

[#51](https://github.com/pravbeseda/monitor/issues/51) left three points open: what "a
backup" is to the monitor, where its path lives, and how one node watches several.

## Decision

- **A backup job writes a status file after every run, and the `backup` sensor reads it.**
  The file is the job's own account of its latest run: whether it failed, when it last
  succeeded and, when the job has them, the repository's figures. The agent runs nothing,
  holds no secret of the backup and decides nothing, as with every sensor.
- **The directory is fixed by the sensor's spec**, one path per system, the agent's own
  beside the hub's data directory, and is no parameter of the configuration: only the job
  and the sensor need it, and the spec names it to both.
  [0010](0010-agent-configuration.md)'s contract is untouched, and the job's provisioning,
  not the agent, creates the directory.
- **One file is one job of one backup**, and names both. A node watches as many backups
  and jobs as it has files; each pair is a series of every metric, labelled `backup` and
  `job`.
- **The metrics are the sensor's own**, a fixed set it names
  ([0048](0048-a-sensor-names-its-metrics.md)): an age since the last success, whether the
  latest run failed, and the repository's archive count, the age of its newest archive and
  its sizes. A file cannot add a metric. The sensor runs in the compiled-in `server` and
  `laptop` profiles; a node with no directory reports nothing.
- **The dates are the job's**, aged by the agent's clock, so a job that stops running stops
  writing and its ages grow: a threshold on an age catches a job that died, a threshold on
  the failure flag one that ran and failed.

## Consequences

- [#51](https://github.com/pravbeseda/monitor/issues/51) closes: a backup is what its job
  reports, its path lives nowhere in the configuration, and one node watches several by
  label.
- Monitoring a backup is a change to the job, not to the monitor: it writes one file in the
  documented format, atomically, after each run.
- Any tool can report this way; nothing in the sensor is specific to one backup program.
- The monitor sees what the job says it did. A job that reports success for an archive that
  never landed is not caught by this sensor; the repository's own host can report its side
  as another job, in the same format.
- The reason a run failed is not a number, so it stays in the job's log; the monitor shows
  that it failed and since when.

## Alternatives

- **The sensor reads the repository itself** — rejected: the agent would hold every
  backup's key and passphrase, run a heavy command on a schedule, and contend for the lock
  with the backups it watches.
- **The age of a marker file** (its modification time) — rejected: it says when a job last
  touched the file, not whether that run succeeded, and carries no counts or sizes.
- **The job posts to ingest itself** — rejected: every job would hold its node's token, and
  its reports would carry neither the manifest that names their metrics
  ([0048](0048-a-sensor-names-its-metrics.md)) nor a sensor interval for their series to age
  by ([0033](0033-a-subject-is-a-series.md)).
- **The job's exit status**, as its service unit's result — rejected: a failed unit already
  counts in `systemd.failed_units`, without saying which backup, and a unit's result carries
  no success date and no figures, and has no launchd equivalent.
- **A generic file of arbitrary metrics**, a textfile collector — rejected: the file would
  name metrics the sensor cannot name or validate, against
  [0048](0048-a-sensor-names-its-metrics.md) and the typed parameters of
  [0044](0044-a-sensor-takes-its-parameters-from-the-hub.md).
- **The directory as a parameter from the hub**
  ([0044](0044-a-sensor-takes-its-parameters-from-the-hub.md)) — rejected for now: the path
  would live in the job and in the hub's file, and no node needs a second location.
