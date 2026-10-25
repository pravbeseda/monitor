# Spec: Disk sensor

- **Status:** approved
- **Owns:** `internal/sensor/disk` (agent): mount enumeration, filtering, and the label
  contract of `disk.free_bytes` and `disk.free_pct`
- **Decisions:** [0003](../decisions/0003-sensors-are-modules.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0033](../decisions/0033-a-subject-is-a-series.md)

## Purpose

The disk sensor reads how much space each mounted volume has left and returns it as
measurements. It decides nothing beyond what the OS itself hides from its user: which
filesystem types and mount prefixes count comes from the hub's configuration, and whether a
value is alarming is the evaluation engine's business.

Its labels are a contract. They form the storage key together with the metric id, so a
volume that changes its labels between runs breaks its own history.

## Measurements

Every collected volume yields both metrics, so that a volume can be judged in whichever of
the two reads better on it ([0033](../decisions/0033-a-subject-is-a-series.md)) and charted
in either:

| Metric | Value |
|---|---|
| `disk.free_bytes` | the space the system reports as available for important use |
| `disk.free_pct` | 100 × that value ÷ total size, rounded to two decimals |

Available, not free: the blocks a filesystem reserves for root are not space the machine
can use, and reporting them would delay every alert by the size of the reserve.

What "available" means is the operating system's answer, not a system call chosen once
([0014](../decisions/0014-macos-available-space.md)): on Linux the blocks `statfs` leaves
to an unprivileged user, on macOS `kCFURLVolumeAvailableCapacityForImportantUsageKey`,
which counts purgeable space — the local snapshots and caches macOS deletes by itself
when a volume fills. On a Mac the two differ by tens of gigabytes.

Both metrics carry the same labels, so one volume is one series in two units:

| Label | Value |
|---|---|
| `mount` | the mount point exactly as the OS reports it |
| `fs` | the filesystem type, lower case (`apfs`, `ext4`) |
| `removable` | `"true"` on a volume the OS reports as removable, `"false"` otherwise |

Removable is what the OS says it is — `MNT_REMOVABLE` on macOS,
`/sys/block/<device>/removable` on Linux — and `false` when that cannot be read. A mount
point under `/Volumes` or `/mnt` proves nothing: an internal disk mounted there would
carry a wrong label for the life of its history, because the label is part of the key.

## Behaviour

One row = one test. Anchors: `spec: disk-sensor.md#<heading>`.

### Enumeration

| Machine state | Collected |
|---|---|
| a mounted volume whose type is in the allow-list | `disk.free_bytes` and `disk.free_pct` for it |
| a volume whose type is not in the allow-list (`devfs`, `tmpfs`, `overlay`, `nfs`) | nothing: it is skipped |
| a mount point under one of the skipped prefixes | nothing: the hub's skip list names what is not worth watching |
| on macOS, a volume the OS hides from its user (`nobrowse`): system volumes, Recovery, simulator runtimes | nothing, whatever the skip list says: what Finder does not show is not the owner's to watch |
| several volumes that share one pool of free space (an APFS container, bind mounts of one device) | one measurement pair, for the shortest mount point of the group |
| a container whose shortest mount point is hidden (a hidden `/Volumes/vm` beside a visible `/Volumes/system-a`) | one measurement pair, for the shortest visible mount point; nothing when every member is hidden |
| several partitions of one physical disk (two NTFS volumes on a stick) | one measurement pair each: separate pools are separate volumes |
| on macOS, a mounted snapshot of an APFS volume — Time Machine mounts them during a backup | grouped with the container it was taken from, so nothing of its own while that container has a watched volume with a shorter mount point |
| on macOS, a mounted snapshot the OS does not hide, whose container has no other watched volume | collected as that container's one measurement pair, like any other sole member |
| a volume reporting zero total blocks | nothing: a percentage of nothing is not a number |
| a volume that vanishes between enumeration and reading | nothing for it; every other volume is still collected |
| a volume the agent may not read | nothing for it; every other volume is still collected |
| macOS refuses the available-capacity question, or answers zero | the `statfs` value is reported instead: system volumes and backup targets answer zero by design |
| the mount table cannot be read at all | no measurements and an error the agent logs |
| no volume passes the allow-list | no measurements; the request itself still carries the heartbeat |

### Labels

| Machine state | Labels |
|---|---|
| an internal volume | `mount`, `fs`, `removable: "false"` |
| a volume the OS reports as removable | the same, with `removable: "true"` |
| the same volume on the next collection | byte-identical labels, so the series continues |

### Applicability

| Machine | Manifest entry |
|---|---|
| any supported platform | `disk`, applicable — every machine has at least one volume |

## Invariants

- One unreadable volume never costs the others: collection returns what it could read.
- Collection is read-only and cheap — one `statfs` per mount, no directory walking.
- The sensor holds no state between collections: the same machine and the same allow-list
  produce the same measurements.
- The allow-list and the skip list arrive from the hub; the sensor has no list of its own
  ([0010](../decisions/0010-agent-configuration.md)). Its one built-in filter is the OS's
  hidden flag, which configuration cannot turn off.
- Which volume of a container is kept depends on the mount points and the filters
  alone, so it does not change between collections and a series keeps its history.

## Edge cases

- **Several volumes of one APFS container** (`/`, `/System/Volumes/Data`) report the same
  free space, so only one of them is collected: the shortest mount point of the container,
  which is the one an operator recognises. Reporting all of them would multiply every
  future alert by the number of volumes the container happens to have.
- **An unplugged external drive** simply stops producing measurements. `removable: "true"`
  is what lets the hub tell that from a vanished internal disk
  ([evaluation](evaluation.md#freezing), [history](history.md#page)). macOS flags a mounted
  disk image as removable too, so an installer opened and ejected leaves no row on the page.
- **A volume macOS hides** is marked `nobrowse` in the mount table; Finder leaves it out
  for the same reason. Xcode mounts simulator runtimes as such volumes, and has moved them
  from one directory to another between releases, so a skip-list prefix catches them only
  until the next move — the flag catches them wherever they land. The skip list stays for
  what the OS shows but nobody watches; its macOS defaults predate the flag and keep older
  agents quiet. Linux has no such flag.
- **A disk its owner hid** — `hdiutil attach -nobrowse`, `nobrowse` in `fstab` — is not
  watched either, and no setting brings it back. Hiding a disk from Finder and watching it
  here is a combination nobody has asked for; the flag is the OS's answer and is taken as is.
- **A mounted snapshot** names its source device after its last `@`
  (`com.apple.TimeMachine.….local@/dev/disk3s5`), so it is grouped with the container it was
  taken from and loses to that container's shorter mount points — `/` for a local snapshot,
  the backup disk's own volume for one under `/Volumes/.timemachine/`. Reporting it would add
  a volume for every backup Time Machine runs, each one vanishing when the backup ends.
- **A mount point with spaces or non-ASCII characters** is carried verbatim; labels are
  not sanitised, because the label is what identifies the volume.
- **Purgeable space on macOS** is counted as available, so a Mac reports what Finder
  shows rather than what `df` does. The panel and the operating system agree, and a
  threshold is not spent on space the system reclaims on its own.
- **A volume remounted at a different path** starts a new series: the mount point is part
  of the identity, and the alternative — matching by device — breaks when a disk is
  reformatted.

## Out of scope

- Thresholds and health for a volume → [evaluation](evaluation.md),
  [thresholds.md](thresholds.md).
- Scheduling: when the sensor runs and how often → agent spec,
  [0010](../decisions/0010-agent-configuration.md).
- Inode exhaustion, SMART health, IO latency — later sensors, not this one.
- Windows volumes: the POC covers macOS and Debian.

## Open questions

None.
