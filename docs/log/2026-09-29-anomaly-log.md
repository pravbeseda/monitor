# 2026-09-29 — Anomalies in the log

Issue [#54](https://github.com/pravbeseda/monitor/issues/54): the timeline showed anomalies
only in "now", because nothing recorded them
([0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)). The direction —
record their start and end — was chosen when the timeline was designed
([2026-09-24](2026-09-24-timeline.md)); this session settled how
([0042](../decisions/0042-an-anomalys-start-and-end-are-recorded.md)).

## Where an anomaly ends

Starting at twice the band and ending at the same line would flap on any series that hovers
there. Three ends were weighed: back inside the band (a score of 1), the relative 20% margin
thresholds use (1.6), and no hysteresis with a minimum duration before anything is recorded.
The user first chose the band: it is the range the page shows as "usually", and it damps a
noisy series the most.

The consistency review of the spec then found that a series climbing at a steady pace
never comes back to 1. The norm period ends a day before the value judged, so the value
lies about 108 hours past the median and the band's top about 82 hours past it, 26 before
the value: 108 / 82 ≈ 1.3 widths, whatever the pace. An uptime or an evenly filling disk
would have stayed unusual for good after a single spike. Asked again, with a third option
of ending after an hour below 2, the user chose 1.6. The reader never sees the number;
they see "usual again".

The band shown with a start is the 1st and 99th percentiles, not the score's edges: those
include a floor, and a number invented by the floor would break 0036's rule that every
number shown is one the series reported.

## Decided without asking

- **A table of its own, not rows in `events`.** The digest, the instant-message rule and the
  lanes' level spans all read `events`; an `unusual` pseudo-level would have to be filtered
  out of each, and one missed filter notifies an anomaly. An interval also holds its own
  end, which a chain of transitions would have to rebuild.
- **The tick records them**, as it records levels
  ([0015](../decisions/0015-evaluation-on-a-tick.md)): a read must not write, and one
  writer keeps two ticks from racing over one record.
- **A stale series holds its anomaly.** Closing it would write an end for a value that never
  came back, every night a laptop sleeps; the lanes already hide the hours a series was not
  fresh.
- **Exclusion, or a lost norm, withdraws the record** without an end line: the reader
  withdrew the question, and a lane stops painting it from that tick.
- **The tick's order for one series**: an exclusion withdraws even a stale series'
  anomaly, staleness holds it, a fresh series without a norm withdraws it, and only then is
  the value judged. The review found the draft ambiguous between the second and the third.
- **Anomalies are recorded last in the pass**, after the digest, so a failure there costs
  no level, message or digest, and at one instant an anomaly lists above a transition.
- **The state judges by the held record**, so "now" and the lanes agree; "now" stays up to a
  tick ahead because it scores the newest value itself.
