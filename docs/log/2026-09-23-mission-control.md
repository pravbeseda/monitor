# 2026-09-23 — Mission control and the first anomaly

The MVP's host sensors had merged, and the question was whether to start on skins. The
[MVP note](2026-09-22-mvp-scope.md) had rejected a board over levels alone, because mission
control needs the semantic engine's fields and the engine was not built. The roadmap put the
whole engine first.

## The order of the work

Three options were weighed: the whole engine and then skins, as the roadmap said; a board
over levels now, reversing the note of the day before; or the slice of the engine that
mission control consumes, and the board right after it. The user chose the third: the order
of the roadmap holds, and nothing is built into the engine that no skin reads yet.

## What the board surfaces

- **Levels only** — rejected: the board would repeat the table and Telegram.
- **Levels and a deviation from each series' own norm** — chosen.
- **The same plus a forecast** ("full in five days") — deferred as a later field.

## Health 0–100 — not now

A number over the board needs a formula nobody could defend yet, and the state's level
already answers "is everything fine" in one word. It belongs to the organism skin's pulse.

## Anomalies are not notified

Shown on the board only. A digest line waits until the board has shown how often anomalies
occur: a noisy detector would fill the digest before anyone saw it.

## Server-rendered, not TypeScript

[0005](../decisions/0005-poc-stack.md) and [poc.md](../poc.md) said skins stay TypeScript.
Everything the web side built since — zone, refresh, catalogue — is server-rendered, and a
TypeScript board would have to repeat it and bring a toolchain for a list. The user chose
the hub's own page: [0035](../decisions/0035-mission-control-is-rendered-by-the-hub.md).
TypeScript waits for a skin that draws.

## Which deviation

The concept note had said "z-score against the metric's own norm". Worked through on the
series the hub collects, it failed twice: an idle machine's median absolute deviation is a
few hundredths, so its nightly job scores in the dozens every night; and uptime, which only
climbs, stays far from its median for days after a reboot. A band of percentiles measured
per side fixes the first, and taking the norm from the week before yesterday keeps a change
visible for a day rather than for hours; the second needed more (below). The reasoning and
the other rejected rules are in [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md).

## A series unusual by nature

The first spec review ran the rule against the series the hub collects and found noise that
is certain rather than likely: a laptop's uptime counts sleep its band never saw, so it
ranks for about a day after every weekend; and a series that only climbs has a band so wide
that a reboot shows only after some eleven days of uptime. Three answers were weighed —
accept it and decide later, a per-series switch on the series' own page, or a rule that
recognises a climbing series and judges it only on a drop. The user chose the switch: it
cures any series unusual by nature, not just this one, and keeps the core free of a
heuristic nobody could explain.

## What the spec reviews changed

- **The norm is pinned to the hour.** The first draft measured the week against the judged
  value's own timestamp while recomputing norms in the background hourly, so no row about
  the period's bounds could be tested. The period now ends 24 hours before the current hour
  began, and a norm is computed on the first read of each hour.
- **A side with no width reaches to the week's extreme, over a floor of the data's own
  step.** Otherwise an idle machine whose job ran in fewer than 1% of its samples made every
  0.01 of load rank first, with no score.
- **The score is compared as reported**, to two decimals: with two-decimal data about a
  third of exact boundary cases compute a hair under 2.
- **A sleeping laptop is quiet, not broken.** A watched series with no fresh data is an item
  only while its node still reports other series; otherwise a laptop asleep inside its
  `silence_after` filled the board every night.
- **Ranked series are marked on `/debug`**, so the line after the fifth anomaly leads
  somewhere that shows the rest.
