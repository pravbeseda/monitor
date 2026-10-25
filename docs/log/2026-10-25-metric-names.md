# 2026-10-25 — Readable metric names, and the overview card closer to its mockup

The reader compared the overview with the mockup it was chosen from and asked for two
things: a readable name in place of every metric id, on every page, and the card's two main
numbers with their charts side by side. Decision on names:
[0048](../decisions/0048-a-sensor-names-its-metrics.md).

## Where a name comes from

Three ways were weighed:

1. **A catalogue in the hub, by metric id.** Half a day, every metric that exists today
   named. Against it: the hub learns every metric, a new sensor costs a hub change, and the
   first external sensor ([0003](../decisions/0003-sensors-are-modules.md)) would need a
   second mechanism anyway.
2. **The sensor declares the name**, in both languages, in the manifest it already sends.
   About a day. The reader called it the most universal, and it is the one that continues
   "a measurement declares itself" ([0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
   [0033](../decisions/0033-a-subject-is-a-series.md)): the id and the unit are already
   declared by the sensor, the name is the third thing it knows.
3. **The reader names series in the interface**, stored like thresholds. No code per metric,
   but ids everywhere out of the box. Kept as a possible override later.

The first answer recommended 1; on the reader's question it was re-weighed and 2 chosen. Its
costs: user-facing strings in the agent, a transition during which old agents show ids.

The overview's spec had dropped readable names on purpose — names for some metrics would
have cost code per metric, against 0032. Declared names remove that objection rather than
overrule it.

## The card

Three styles were drawn on synthetic data: the mockup as it was, a denser card with the
reason under the name, and a card with a coloured stripe on the left of every card and the
state as a pill. The reader chose the stripe. "Two charts side by side" was read as the
card's two main numbers; the item page already shows two charts to a row on a wide screen.
