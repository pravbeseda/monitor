# 0033. A subject is a series, and a threshold is one comparison per level

- **Status:** accepted; staleness for a series that names no sensor is amended by
  [0034](0034-a-series-without-a-sensor-still-ages.md)
- **Date:** 2026-09-20
- **Source:** supersedes [0012](0012-threshold-model.md); follows
  [0032](0032-thresholds-are-set-in-the-interface.md)

## Context

[0012](0012-threshold-model.md) made one default fit volumes of every size — a floor, plus
a band guarded by absolute headroom — and paid for it with a rule shape the hub knows by
heart: exactly two series read together (free bytes and free percent), both compared with
`<`, plus a role that drops the band for backup volumes. Everything downstream inherited
that shape: what a level belongs to, what a message says, what the file's threshold grammar
can express. A second metric with an alert — load, uptime, the age of a backup — fits none
of it.

With [0032](0032-thresholds-are-set-in-the-interface.md) the numbers are set per volume by
the person who knows what the volume is for. The band's reason for existing — keeping one
default from crying wolf on an 8 TB volume — is gone with the default.

## Decision

**A subject is a series**: `(node, metric, labels)`. A volume contributes two of them,
`disk.free_bytes` and `disk.free_pct` with the same labels, and a level is put on whichever
one fits that volume.

**A configured subject carries a direction and up to two values.** The direction is `below`
or `above` and belongs to the subject, not to the level; `warning` and `critical` are
independent and either may be absent. Entry is the strict comparison; exit negates it with
the 20% margin of [0013](0013-relative-hysteresis.md) measured on the threshold's
magnitude, so it works for a negative threshold as well as a positive one:

```
direction: below     enter L when v < T(L)     leave L when v >= T(L) + 0.2·|T(L)|
direction: above     enter L when v > T(L)     leave L when v <= T(L) - 0.2·|T(L)|
```

**A subject with nothing configured has no level** — no colour, no event, no notification.

**Rules, joins, volume roles and the rule catalogue disappear.** Nothing in the hub knows
that `disk.free_bytes` is about disks.

**A measurement names the sensor that produced it**, and the series keeps that name, because
staleness is defined as three times the sensor's interval and no rule is left to say which
sensor a metric comes from.

## Consequences

- A new metric with an alert is configuration, not code: collect it, open its page, give it
  a direction and a number.
- `disk.free_pct` stays worth collecting — it is now how a proportional level is expressed,
  not one half of a formula.
- **The band is gone**, and what replaces it is a threshold on bytes and a threshold on
  percent, firing whichever comes first — the disjunction [0012](0012-threshold-model.md)
  rejected. That rejection stands where it was aimed: as a *default* for every volume, OR is
  strictly more eager than percentages alone. As a per-volume choice, eagerness is the
  choice of whoever set both numbers, and either can be left empty.
- **Levels and events recorded against the old rule-shaped subjects are not carried over**:
  they name a subject that no longer exists. Nothing alerts until a subject is configured
  anyway ([0032](0032-thresholds-are-set-in-the-interface.md)).
- A measurement from an agent too old to name its sensor still stores, charts and alerts; it
  only loses staleness, until that agent updates itself ([0028](0028-agents-follow-a-target-the-hub-serves.md)).
  `agent_target` has no default, so on a node whose target the file never names, "until it
  updates itself" is never: setting it is part of the upgrade
  ([install.md](../install.md)).
- Forecast alerting stays a later addition on top of thresholds, as it was under
  [0012](0012-threshold-model.md).

## Alternatives

- **Keep the join and configure floor, ratio and ceiling per volume** — rejected: it keeps a
  disk-shaped formula in the hub for the sake of a generality that hand-set numbers no
  longer need, and it makes every future metric declare which series it joins.
- **A rule expression language in the store** (operators, `and`/`or`, several series) —
  rejected: a vocabulary invented for one composite rule nobody has to express any more. It
  can be added later without moving what a subject is.
- **One threshold per subject with the direction inferred from the metric** — rejected:
  "low is bad" holds for free space and fails for load or the age of a backup. Inferring it
  from a name is the rule catalogue again, wearing a different hat.
- **One list of subjects rather than two** — the shape
  [0030](0030-the-state-api-reports-the-stored-verdict.md) rejected as "readings as subjects
  with `rule: null`" — is what this decision arrives at, by another road: there is no `rule`
  to be null, every series is a subject, and the only question left is whether anything
  watches it. The rejection stood while a subject was a different kind of thing from a
  series; it does not survive their becoming one.
- **A single `warning`-only level** — rejected: instant delivery is defined on `critical`
  ([0016](0016-leaving-critical-is-instant.md)), so dropping it would drop the only alert
  that wakes anyone.
