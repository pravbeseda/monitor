# What a backup of the hub host has to keep

The hub host is provisioned by Ansible, in the repository that owns that host. This file is
the requirement handed to it: **what a backup of the hub must keep, and how to prove a
restore works**, not how the role is written
([ADR 0043](decisions/0043-the-hub-hosts-backup-belongs-to-its-provisioning.md)). Per
[ADR 0007](decisions/0007-public-repository.md) no host name, storage address or key may
enter this repository.

Related: [install.md](install.md#restore-on-another-host) is the restore this backup feeds,
[specs/deployment.md](specs/deployment.md#where-things-live) owns the paths quoted here, and
[hub-host-node-requirements.md](hub-host-node-requirements.md) installs the agent whose
`systemd.failed_units` reports a failed run.

## What is kept, and what is not

| What | Kept by |
|---|---|
| the database, `/var/lib/monitor/monitor.db` — history, thresholds, the event log | **this backup** |
| `/etc/monitor/hub.yaml`, `/etc/monitor/hub.env`, `/etc/monitor/hub.target` | the play, from its variables and the vault |
| the binaries, the units, the kept install script | a release and the play |

The database is the only part nothing else can rebuild. Without it a restored hub starts
empty: no history, and no thresholds, so nothing has a level and every node looks fine.

## Requirements

1. **A snapshot is taken with SQLite, never by copying the file**
   ([ADR 0005](decisions/0005-poc-stack.md)): `cp`, `rsync` or a file-level backup of
   `/var/lib/monitor` is not a backup of the hub. The role installs the `sqlite3` package —
   Debian does not by default — and the hub keeps running while the snapshot is taken:

   ```sh
   set -eu
   rm -f "$dir/monitor.db.tmp"
   sqlite3 -readonly -cmd '.timeout 10000' /var/lib/monitor/monitor.db \
       "VACUUM INTO '$dir/monitor.db.tmp'"
   test "$(sqlite3 -readonly "$dir/monitor.db.tmp" 'PRAGMA integrity_check')" = ok
   sync "$dir/monitor.db.tmp"
   mv "$dir/monitor.db.tmp" "$dir/monitor.db"
   ```

   - `-readonly` makes a missing database an error; without it `sqlite3` creates an empty
     one, and the empty snapshot passes every check.
   - `VACUUM INTO` refuses a target that already holds data, hence the `rm`, and does not
     flush what it wrote, hence the `sync`.
   - The rename makes the snapshot appear whole, so the off-host copy never picks up half of
     one.

2. **The snapshot runs as `monitor`.** Nothing in it needs root, and a snapshot written by
   `monitor` needs no change of owner. `$dir` is a directory of the role's choosing outside
   `/var/lib/monitor`, owned by `monitor`, mode `0700`. It holds the newest snapshot only:
   the history of snapshots lives off the host.

3. **It runs daily as a `Type=oneshot` service** with `User=monitor` and `UMask=0077` — the
   default umask leaves the snapshot readable by everyone — started by a timer with
   `OnCalendar=daily` and `Persistent=true`, so a run missed while the host was down happens
   soon after it is back. The commands above run as one script, so any of them failing fails
   the unit. A sandboxed unit needs write access to `/var/lib/monitor`: reading a WAL
   database writes its `-shm`.

4. **The off-host copy runs only after a snapshot succeeded** — from the snapshot unit's
   `OnSuccess=`, or as a unit ordered after it — as `monitor` or root, since `$dir` is
   readable by no one else. It takes `$dir` and nothing under `/var/lib/monitor`, and fails
   its own unit when it fails. Its tool is the role's choice, and so is whether it carries
   the host's other data as well.

5. **The copy is encrypted before it leaves the host**, with a key the storage never sees,
   and the key is in the vault: a key that exists only on the host is lost with it. A copy
   of the key on the host, readable by root only, is acceptable — the host holds the live
   database unencrypted anyway. The database is personal data rather than a secret, but it is
   everything the monitor knows about its owner.

6. **Retention keeps at least seven daily and four weekly copies.** A threshold deleted by
   mistake, or a database damaged by a bad release, is often noticed days later, and the
   newest copy by then already carries the damage.

7. **A failed run shows on the monitor.** Either unit left `failed` raises
   `systemd.failed_units` on the hub host's own node, once that node exists
   ([hub-host-node-requirements.md](hub-host-node-requirements.md)). Then a person, not the
   role, sets a threshold on that series on the hub's `/thresholds` page — `above`, warning
   at `0` — or nothing has a level to leave.

8. **A restore by the play puts the snapshot in place before the play first starts the
   hub**, with the `-wal` and `-shm` beside it removed, as
   [install.md](install.md#restore-on-another-host) does by hand. Requirement 3 of
   [hub-host-node-requirements.md](hub-host-node-requirements.md) starts the hub on every
   play, and the host's own agent reports to it over loopback whatever the DNS says, so a
   hub started on an empty database takes measurements into a file about to be replaced.
   This needs a deliberate step in the role.

9. **A restore is performed once before the backup counts**, and again whenever the tool,
   the key or the storage changes: see [How we check it is done](#how-we-check-it-is-done).

## What we are not asking for

- No copy of `hub.yaml` or `hub.env`: the vault already holds every value in them, and a
  second copy of the secrets is a second place to leak them from.
- No downtime: the hub is never stopped for a snapshot.

## How we check it is done

On the hub host:

```sh
systemctl list-timers                              # the snapshot timer, with a next run
systemctl status <snapshot unit> <off-host unit>   # the last run of each succeeded
sudo ls -l "$dir"                                  # monitor.db from today, monitor 0600
```

Then the restore, by hand on a second host — never the hub host, and never the play run
against it, which would claim the hub's name and certificate. Use nothing but the vault and
access to the backup storage, besides the provisioning repository's variables: the dead
host's disk is not available in a real restore.

1. Fetch the newest copy and decrypt it.
2. Follow steps 1 to 3 of [install.md](install.md#restore-on-another-host), then start the
   hub alone: never step 4, which takes the real hub's name, and no agent, which would report
   into the real hub as its host's node. Write `notify: { channel: log }` into the drill's
   `hub.yaml`, since every node is silent there and the drill must not wake anyone.
3. Through an `ssh` tunnel to the second host's loopback port: `/debug` lists the nodes of
   the real hub, the top-level `watched` of `/api/v1/state` matches the real hub's, and a
   series' history reaches back to before the snapshot.

Take the second host down afterwards: it holds the database.
