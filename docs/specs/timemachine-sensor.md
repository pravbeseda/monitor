# Spec: Time Machine sensor

- **Status:** approved
- **Owns:** `internal/sensor/timemachine` (agent): where it reads a Mac's backups from, and the
  metric and label it reports
- **Decisions:** [0003](../decisions/0003-sensors-are-modules.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0014](../decisions/0014-macos-available-space.md),
  [0033](../decisions/0033-a-subject-is-a-series.md)

## Purpose

Backups are the reading that fails silently: a Mac that stopped backing up says nothing. This
sensor reports how long ago each Time Machine destination last received a backup, so a
threshold such as `above 172800` (two days) turns a backup that stopped into an alert. Like
every sensor it decides nothing: the series has no level until someone sets one on its page
([thresholds.md](thresholds.md)).

It takes no parameter: Time Machine keeps its own list of destinations, so there is nothing
for the configuration to name. Backups that do need one are
[#51](https://github.com/pravbeseda/monitor/issues/51).

## Measurements

| Metric | Labels | Value |
|---|---|---|
| `timemachine.backup_age_seconds` | `destination` | whole seconds from the destination's latest backup to the agent's clock, truncated |

`destination` is the name of the destination's volume as Time Machine last saw it, carried
verbatim. One destination is one series.

**The dates are Time Machine's own**, read from its system preferences through the
preferences service — what `defaults export /Library/Preferences/com.apple.TimeMachine -`
prints — where each destination lists the backups it holds (`SnapshotDates`). Attempts
(`AttemptDates`) are not backups, and the local snapshot a backup starts from is not one
either. The service answers without Full Disk Access, which `tmutil` and a direct read of the
file need ([log](../log/2026-09-30-time-machine.md)); whether the file,
`/Library/Preferences/com.apple.TimeMachine.plist`, exists is what tells a Mac that never set
Time Machine up from one whose preferences could not be read.

## Behaviour

One row = one test. Anchors: `spec: timemachine-sensor.md#<heading>`.

### Age

| Machine state | Collected |
|---|---|
| one destination, `Backups`, whose latest backup was 1 day, 2 hours and 0.9 s before the agent's clock | `timemachine.backup_age_seconds{destination="Backups"}` 93 600 |
| a destination whose backups are listed out of order | the age of the latest of them |
| two destinations, `Backups A` and `Backups B` | one measurement for each, labelled by its own name |
| a destination named `Резервные копии — Mac` | labelled `Резервные копии — Mac`, byte for byte |
| a destination holding no backup, beside one that holds some | a measurement for the second only, and no error |
| a destination with no recorded name, beside a named one | a measurement for the named one only, and no error |
| two destinations with the same name, beside a third | a measurement for the third only, and no error: one label cannot tell the two apart |
| a destination whose latest backup is later than the agent's clock — a clock set back — beside one in the past | a measurement for the second only, and no error: a negative age is not a reading, though an age of 0 is |
| preferences holding other settings but no `Destinations`, or an empty list of them | no measurements and no error |
| no Time Machine preferences file — never set up | no measurements and no error |
| the preferences file exists, and the preferences service answers with an empty dictionary | no measurements and an error the agent logs: unread preferences are not an empty list |
| the preferences cannot be read, or any part of them parsed | no measurements and an error the agent logs |
| Linux, the sensor enabled for it anyway | no measurements and no error |

### Applicability

| Machine | Manifest entry |
|---|---|
| macOS | `timemachine`, applicable |
| Linux | `timemachine`, not applicable |

## Invariants

- Collection is read-only and cheap: one read of the preferences, nothing on the backup disk.
- A destination with no backup, no name, a shared name or a date ahead of the clock is left
  out, never reported as zero: `0` would read as a backup that just finished.
- Every measurement names the sensor that produced it, so its series ages by that sensor's
  interval ([0033](../decisions/0033-a-subject-is-a-series.md)).
- No cgo: the one cgo file stays the disk sensor's
  ([0014](../decisions/0014-macos-available-space.md)).

## Edge cases

- **A disconnected destination** keeps its dates, so its age keeps growing while the agent
  reports: that is the reading, not a gap.
- **Automatic backups switched off** look the same: the age grows until a threshold fires.
- **The latest backup deleted** — by hand, or thinned by Time Machine — moves the age back to
  the one before it, since the list holds the backups still on the destination.
- **A destination just added** has no series until its first backup completes, so one whose
  first backup never completes is not seen: there is no date to age from.
- **A destination removed from Time Machine** stops producing its series. The row stays on
  `/debug`, marked as holding no fresh data, since only a removable series is hidden
  ([history.md](history.md#page)), and whatever level it held is frozen
  ([evaluation.md](evaluation.md#freezing)). **A renamed one** leaves the same row behind, and the new
  name starts a new series.
- **A Mac that sleeps** usually reports nothing until it wakes, and the first reading after
  wake carries the full age; a backup run during a maintenance wake is counted like any
  other.

## Out of scope

- Backups other than Time Machine → [#51](https://github.com/pravbeseda/monitor/issues/51).
- A failed backup attempt: an attempt that did not complete leaves the age growing, and that
  is what a threshold sees.
- Free space on the destination: the disk sensor reports it while the volume is mounted.
- A series for the freshest backup across all destinations: a derived series belongs to the
  semantic engine, not to a sensor.

## Open questions

None.
