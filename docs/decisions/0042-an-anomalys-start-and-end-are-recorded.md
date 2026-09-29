# 0042. An anomaly's start and end are recorded by the evaluation tick

- **Status:** accepted
- **Date:** 2026-09-29
- **Amends:** [0036](0036-an-anomaly-is-a-value-outside-its-weeks-band.md) — an anomaly is
  now held until its score falls back to 1.6, and its start and end are stored;
  [0038](0038-a-lane-is-summarised-on-read.md) — a lane also reads the stored anomalies
- **Source:** [anomaly spec](../specs/anomaly.md#record),
  [timeline spec](../specs/timeline.md#model),
  [design notes](../log/2026-09-29-anomaly-log.md), issue
  [#54](https://github.com/pravbeseda/monitor/issues/54)

## Context

An anomaly was computed on read and stored nowhere
([0036](0036-an-anomaly-is-a-value-outside-its-weeks-band.md)), so the timeline could show
one only in "now": its lanes and its list of changes had no record of when a series became
unusual or stopped being so. The user chose to record that rather than recompute past
anomalies on every read ([design notes](../log/2026-09-24-timeline.md#anomalies-in-the-history)).

A record needs a start and an end that do not flap. A load that hovers around twice its
band would otherwise write a pair of entries every few minutes, and a list of fifty
changes would be nothing else. An end needs the previous verdict, which is what 0036 found
an anomaly did not have when it rejected storing one on the tick: without hysteresis
there was nothing to hold, with it there is.

## Decision

- **An anomaly starts when the score reaches 2 in either direction, or is `null`, and ends
  when its magnitude falls to 1.6 or less** — the cut-off less the 20% margin levels clear
  by ([0013](0013-relative-hysteresis.md)). Between the two the series stays unusual.
- **The evaluation tick records both**, last in the pass, against the same snapshot and
  instant as the levels ([0015](0015-evaluation-on-a-tick.md)): a page read writes nothing,
  and the record keeps one writer. A pass that cannot read the norms records no anomaly and
  has judged its levels as ever.
- **They are kept apart from level transitions**, as intervals of their own: when a series
  became unusual, the value and the band it was judged against, and when and at what value
  it came back. The digest and the notifications read level transitions only, so an
  anomaly stays shown and never notified without anything having to filter it out.
- **A stale series holds its anomaly**, as it holds its level: stale values judge nothing.
  An anomaly the reader withdraws by excluding the series, or one whose series is fresh
  with no norm, is closed at the tick that finds it so, with no "usual again": the value
  did not come back, the question was withdrawn.
- **The state judges by the same rule**: a series is unusual in an answer when its newest
  value scores so against the anomaly the record holds open for it, so "now" and the lanes
  agree, with "now" at most one tick ahead.

## Consequences

- The timeline's lanes gain an "unusual" cell and its changes an anomaly's start and end.
- A series between 1.6 and 2 widths from its norm ranks in "now" while it has not come
  back since it crossed 2, and not otherwise; it ranks after every score of 2 or more.
- Schema change: one table of anomaly intervals.
- The tick reads each series' norm once an hour whether or not anyone reads a page; the
  tick and the state share one cache of norms, which the hub builds once and hands to both.
- A change that lasts still ends after about a day, when the week it is compared with
  catches up with it.
- A series that climbs or falls at a steady pace scores about 1.3 against a norm that
  ended a day ago, so its record ends once its pace is back to the week's.
- The band shown with a start is the 1st to 99th percentile, values the series really
  reported, as 0036 requires of every number shown.
- Notifying an anomaly stays deferred; the record is what would tell how often one occurs.

## Alternatives

- **Recompute each past hour's anomaly on every read.** Rejected earlier by the user: 24
  norms per series per refresh, and a norm period that moves with each hour.
- **Rows in the level-transition log with `unusual` as a level.** Rejected: the digest, the
  instant-message rule and the timeline's level spans all read that log, and each would
  need to learn to skip a level that is not one. An interval also has what a chain of
  transitions has to rebuild.
- **End when the value is back inside its band, a score of 1.** Chosen by the user first,
  then reversed: a series climbing at a steady pace sits about 1.3 widths from a norm that
  ended a day ago, so an uptime or an evenly filling disk would stay unusual for good once
  it had been unusual once.
- **No hysteresis, recording only an anomaly that lasted some minutes.** Rejected by the
  user: every record comes late by that delay, "now" and the lanes disagree meanwhile, and
  the delay is one more constant.
- **End after a score below 2 held for an hour.** Rejected by the user: another rule and
  another constant, and every end an hour late.
- **Close an anomaly when its series goes stale.** Rejected: a series asleep overnight would
  "come back" every evening with no value that did, and a lane already paints only the
  hours a series was fresh.
