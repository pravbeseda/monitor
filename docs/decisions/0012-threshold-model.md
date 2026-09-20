# 0012. Disk thresholds are a floor plus a proportional band

- **Status:** superseded by [0033](0033-a-subject-is-a-series.md), whose
  [0032](0032-thresholds-are-set-in-the-interface.md) also replaces the layering below
- **Date:** 2026-08-28
- **Source:** [POC](../poc.md) question 4

## Context

Percentages misjudge both ends of the size range: 10% of 8 TB needs no attention, 10% of
128 GB is nearly full. Absolute limits fail the other way — 20 GB free is comfortable on a
laptop and alarming on a backup array. Backup and Time Machine volumes break both rules:
they sit nearly full by design and would alert forever.

## Decision

A disk threshold is a floor plus a band, so the rule adapts to the size of the volume
instead of needing an exception per volume:

```
alert when   free < floor                          # threshold
      or     (free% < ratio  and  free < ceiling)  # threshold, guarded by size
```

| Level | Floor | Ratio | Ceiling |
|---|---|---|---|
| warning | 10 GB | 15% | 100 GB |
| critical | 4 GB | 7% | 40 GB |

The floor catches any volume that is genuinely close to full, whatever its size. The band
catches a volume running low proportionally, but only while its absolute headroom is small
enough to matter — so an 8 TB volume with 1.1 TB free stays silent, a 128 GB volume with
19 GB free warns, and any volume with 3 GB free is critical. Recovery negates this whole rule
with a margin on every comparison, rather than clearing conditions one by one
([0013](0013-relative-hysteresis.md)).

A disjunction alone cannot do this: OR can only make alerting more eager than percentages
already were, which is the opposite of what large volumes need.

**Volume roles carry their own rule.** A volume declared `role: backup` drops percentages
entirely and keeps absolute headroom (warning below 50 GB, critical below 10 GB), because a
backup volume being full is its normal condition, not an incident.

**Overrides use the layering of [0010](0010-agent-configuration.md)** down to one volume:
sensor default → node class → node → single volume, keyed by node and mount point. A volume
the hub has not seen takes the defaults until it is given its own.

## Consequences

- The evaluation engine must support both `or` and `and` within one level's rule. That is
  the shape thresholds take in general, not a special case for disks.
- Both `disk.free_pct` and `disk.free_bytes` are collected, as the wire format already has
  them — neither can be dropped as redundant.
- Recovery is the negated rule with a 20% margin on every comparison
  ([0013](0013-relative-hysteresis.md)), not a per-condition clearance: a 128 GB volume
  leaves warning at 23.04 GB free through the ratio — 18% of 128 GB — and an 8 TB volume at
  120 GB free through the ceiling. Absolute headroom is what flaps in practice — logs
  rotate, caches grow and shrink — so the margin on every comparison is not a theoretical
  nicety.
- Forecast-based alerting ("full in ~12 days") is a later addition on top of these
  thresholds, once history is long enough — not a replacement.

## Alternatives

- **Percentages only, with a manual exception per large volume** — rejected: it needs a hand
  written rule for nearly every volume, which is a patch, not a model.
- **Percentage or absolute headroom, whichever fires first** — rejected after review: a
  disjunction is strictly more eager than percentages alone, so it leaves the large-volume
  false positive it was meant to remove.
- **Absolute limits only** — rejected: predictable, but it puts every volume back into manual
  configuration.
- **Forecasts instead of thresholds** — deferred: no history exists at stage 2, and a short
  series forecasts badly.
