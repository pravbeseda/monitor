# 0034. A series that names no sensor ages by its node's longest interval

- **Status:** accepted
- **Date:** 2026-09-21
- **Source:** amends [0033](0033-a-subject-is-a-series.md); found on the running hub, where a
  volume that had been gone for days still held a row

## Context

[0033](0033-a-subject-is-a-series.md) made staleness a property of the sensor a measurement
names: `stale_after` is three times the interval the node resolves for that sensor, and a
series whose newest value names no sensor was left with no freshness rule at all. That read
as harmless — an agent too old to name its sensor would name it after the next upgrade, and
until then its series would only lose a verdict.

It is not harmless. A series with no freshness rule never becomes stale, and the page hides
or marks a row by exactly that verdict. A volume that is gone — an ejected disk image, an
unplugged drive, a snapshot a backup mounted once — stops reporting for ever, so nothing
will ever restore the sensor name it lacks, and its row stays on the page as a current
reading. The rule that was meant to be temporary is permanent precisely where it hurts.

Storage makes such series ordinary rather than exotic: the `series` table is derived from
the measurements and is rebuilt, not altered, when its schema changes
([0031](0031-a-table-of-series.md)), and the measurements do not carry sensor names. Every
schema change to that table therefore leaves every existing series without one.

## Decision

A series whose newest value names no sensor is aged by the longest interval among the
sensors its node runs, by the same 3× factor and the same inclusive bound as any other
series. A node that runs no sensor at all freezes such a series outright: nothing will
refresh it. `stale` is therefore always a verdict for a subject that belongs to a node,
never absent.

The bound is read from the configuration at the instant of judgement, like every other
interval, so it follows a node's sensors as they are added, slowed or switched off.

Chart gaps keep the old answer: a series with no interval is drawn as one continuous line
([history](../specs/history.md#gaps)). A gap says a collection that was due did not arrive,
and for such a series nothing says what was due; staleness asks whether anything will come
at all, which the node's own cadence answers well enough.

## Consequences

- No row outlives the thing it describes. A vanished volume leaves the page three of the
  interval it is aged by after its last reading, whether or not its series kept a sensor
  name.
- A rebuild of the `series` table costs accuracy: until each series reports again, it is
  aged by a bound that is coarser than its own. In one direction it costs more than
  accuracy — a series frozen because its node runs its sensor no longer becomes sensorless
  on the rebuild and is aged by a live bound again, so its row can read fresh for up to
  that bound before it ages off a second time.
- The consequence [0033](0033-a-subject-is-a-series.md) records — that a measurement from an
  agent too old to name its sensor "only loses staleness" — no longer holds: such a series
  is aged, coarsely, from the moment it is stored.
- A sensorless series is aged by an interval derived from a node's *other* sensors, so
  switching off the sensor that held the longest interval can freeze one, and adding a
  slower sensor can thaw one, though nothing about the series itself changed.
- Something that pushes measurements on its own cadence without naming a sensor is judged
  by a bound that is not its own. Naming a sensor in the measurement remains the way to be
  judged by the right one ([ingest](../specs/ingest.md#wire-format)).
- The wire format loses a case rather than gaining one: consumers of the
  [State API](../specs/state.md) may treat `stale` as a boolean.

## Alternatives

- **Backfill the sensor name from the metric id** (`disk.free_pct` → `disk`). Rejected: it
  puts the metric-to-sensor table that [0033](0033-a-subject-is-a-series.md) deleted back
  into the hub, and it only works for metrics this build happens to know — the opposite of
  "a metric costs no code".
- **Carry the sensor on every measurement row** so a rebuilt series table can recover it.
  Rejected here, not for ever: it costs a column on the largest table to answer a question
  only the newest point asks, and it would not reach a single row already stored.
- **Drop a series that has not reported for N days.** Rejected as the answer to this
  problem: retention is a separate decision with its own setting, and deleting history is a
  heavier remedy than not calling a week-old reading current. It stays open as its own
  question.
- **Leave the rule and delete the stale rows by hand.** Rejected: it was tried on the
  running hub, and the rows came back with the next volume that vanished.
