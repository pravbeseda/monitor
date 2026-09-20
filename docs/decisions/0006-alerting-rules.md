# 0006. Alert on transitions; only critical is instant

- **Status:** accepted; the recovery margin is superseded by
  [0013](0013-relative-hysteresis.md), the delivery of `critical → warning` is amended by
  [0016](0016-leaving-critical-is-instant.md), and where thresholds live is clarified by
  [0007](0007-public-repository.md) and then superseded by
  [0032](0032-thresholds-are-set-in-the-interface.md), which takes thresholds out of the
  hub's YAML and into the interface; the transition rules and the digest stand
- **Date:** 2026-08-28
- **Source:** [concept](../concept.md), [POC](../poc.md)

## Context

Notifications are the point of the project ("silence while all is well") and also the
fastest route to a bot that gets muted within a month.

## Decision

- Alert on a state **transition** (ok → warn, warn → crit), not on every measurement.
- **Hysteresis**: return to ok only above `warn_below + 3` points, so a metric sitting on
  the boundary cannot flap (superseded by [0013](0013-relative-hysteresis.md)).
- Only critical is delivered instantly. Warnings accumulate into a daily digest.
- An unresolved critical repeats at most once a day.
- Node silence is a separate alert driven by the node class `silence_after`
  (see [0002](0002-push-not-pull.md)).
- The POC channel is a Telegram bot on long polling: no inbound webhook required.

## Consequences

- The hub must keep a state and transition log (event log); the same log feeds the event
  stream that skins subscribe to (see [0001](0001-semantic-core-and-skins.md)).
- Thresholds and rules live in the hub's YAML configuration, never in code.

## Alternatives

Sending every threshold violation — rejected: a guaranteed mute.
