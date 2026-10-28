# 2026-10-28 — Backups a job reports

The reader runs nightly archives from servers to a backup host, copies the repositories to
a disk at home, prunes and checks them there, and tests a restore monthly. They asked to
see in the monitor how many archives each repository holds, their sizes and dates, the
failures, and the results of the checks. Decision:
[0049](../decisions/0049-a-backup-job-reports-through-a-status-file.md).

## Where the figures come from

Three ways were weighed:

1. **The jobs write a status file, a sensor reads it.** The jobs already hold every figure
   at the end of a run; the agent needs no secret and opens no repository. Chosen.
2. **The sensor reads the repositories itself.** The jobs stay as they are, but the agent
   would hold every key and passphrase and fight the backups for the repository's lock.
3. **A generic file of arbitrary metrics.** It would serve any script, but the monitor would
   no longer know the names and units of what it shows.

## Who reports

Three places could report: the host that makes the archive, the host that stores it, and
the machine that copies it. The storing host cannot read a repository without its key, so
it sees only the size and the time of the last write, and a client that fails to write
reports its own failure. The first version therefore has the making host and the copying
machine report; the storing host can join later as one more job in the same format.

## Where the path lives

A parameter from the hub ([0044](../decisions/0044-a-sensor-takes-its-parameters-from-the-hub.md))
was weighed against a directory the spec fixes. Only the job and the sensor need the path,
so the spec names it to both, and the hub's file stays as it is.

## Beside the monitor

The external dead-man checks the jobs already ping stay: the hub runs on one of the hosts
whose backup it watches, and a monitor that went down with that host could not say so.
