# 0036. An anomaly is a value outside the band its series kept the week before

- **Status:** accepted
- **Date:** 2026-09-23
- **Amends:** [0030](0030-the-state-api-reports-the-stored-verdict.md) — the State API now
  computes each series' anomaly on read, beside staleness; it still evaluates no level
- **Source:** [anomaly spec](../specs/anomaly.md),
  [design notes](../log/2026-09-23-mission-control.md)

## Context

Mission control is empty while all is well and surfaces what is off
([concept](../concept.md)). Levels exist only where somebody set a threshold
([0032](0032-thresholds-are-set-in-the-interface.md)); most series have none. The concept
asks for an anomaly rank — each series against its own norm — and
[0001](0001-semantic-core-and-skins.md) puts it in the core, so every skin ranks alike.

The series the hub holds today disagree about what "usual" looks like. Load is idle with a
nightly spike. Uptime climbs and resets to zero at a reboot. A count of failed services is
zero all week. Free space on a busy volume drifts down, and on an archive volume does not
move at all. Whatever the rule is, it knows none of this: a metric costs no code
([0033](0033-a-subject-is-a-series.md)).

## Decision

- **The norm is the week before yesterday.** A series' newest value is compared with the
  points it reported from eight days to one day before the current hour began. Leaving the
  last day out keeps a change visible for about a day instead of letting it become its own
  norm within hours.
- **The band is the 1st to 99th percentile, and the norm its median**, by nearest rank, so
  every number shown is one the series really reported. Each side is measured on its own; a
  side with no width reaches to the week's extreme on that side, and none is narrower than
  1% of the norm's magnitude or the smallest step between two distinct values the week
  holds on that side.
- **The score is the distance from the norm in widths of that side, to two decimals; 2 or
  more ranks.** A series at zero all week has sides of no width, and any move off zero ranks
  first, with no score.
- **It is computed on read**, the norm once per hour and the score for every answer, and
  stored nowhere. It writes no event, and is not a level: it cannot be held, entered or
  left.
- **It is shown, never notified.** Neither an instant message nor the digest carries it,
  until the board shows how often anomalies occur.
- **A series can be excluded on its own page**, beside its threshold, and stored with the
  data independently of it ([0032](0032-thresholds-are-set-in-the-interface.md)): an excluded series carries no
  anomaly. It is the cure for a series unusual by nature, chosen over teaching the rule to
  recognise one.

## Consequences

- Every series with two days of history is judged with nothing set, including on a hub that
  watches nothing.
- A nightly spike is part of the norm; a value twice as far from the norm as the band's
  edge is not.
- A lasting change ranks for about a day, then becomes the norm. A lasting state is a
  threshold's job.
- A series that only climbs has a wide band. A reboot ranks `uptime.boot_seconds` only
  after about eleven days of uptime, and a laptop's uptime ranks for about a day after each
  weekend asleep, because it counts sleep that the band, drawn from waking hours, never
  saw. Excluding the series on its page is how a reader silences that.
- A steadily filling disk does not rank; one filling about 3.4 times faster than the week
  before, for a whole day, does.
- The anomaly of the past cannot be reported until `at=` exists, since nothing is stored;
  it can be recomputed from the points, which are all kept.
- No per-series tolerance exists — a series either ranks by the common cut-off or is
  excluded; a tolerance waits for a series the exclusion is too blunt for, as a per-subject
  hysteresis margin waits for the first noisy metric ([0013](0013-relative-hysteresis.md)).

## Alternatives

- **A z-score against the median and MAD of the week.** Rejected: an idle machine's MAD is
  a few hundredths, so its nightly job scores in the dozens every night; and a monotonic
  series such as uptime stays "anomalous" for three to four days after a reboot, until the
  median crosses over.
- **The week ending now.** Rejected: a change becomes its own 99th percentile once it has
  lasted 1% of the week — under two hours — and a reader who glances twice a day misses it.
- **Scoring the rate of change.** Rejected: it catches a jump and forgets a plateau, so a
  load that stays five times higher disappears after one point.
- **Storing anomalies on the evaluation tick like levels
  ([0030](0030-the-state-api-reports-the-stored-verdict.md)).** Rejected: a level is stored
  because events and alerts derive from it and hysteresis needs the previous one; an anomaly
  has neither, and scoring the value the answer carries keeps the two consistent.
- **Recomputing norms in the background, at least hourly.** Rejected: a norm then depends
  on when the refresh last ran, which no reader can see, and a fresh hub answers without
  anomalies until it has. Pinning the period to the hour makes every answer's norm exact.
- **Comparing an hour with the same hour on other days.** Deferred: it needs a week of
  points per hour of day to be stable, and none of today's series has shown a daily pattern
  the week-wide band misreads.
- **Recognising a series that only climbs and judging it only on a drop.** Rejected: no
  hand-tuning, but a heuristic in the core that is hard to explain and to test, and it
  would still leave every other series unusual by nature without a cure.
- **Accepting the known noise and deciding later.** Rejected by the user: a laptop's uptime
  ranking every Monday is a certainty, not a risk.
- **Sending anomalies in the digest.** Deferred by the user: a detector that turns out noisy
  would fill the digest before anyone saw it on the board.
