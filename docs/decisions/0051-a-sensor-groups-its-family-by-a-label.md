# 0051. A sensor groups its family by a label; the skin draws each group as a panel

- **Status:** accepted
- **Date:** 2026-11-03
- **Source:** [2026-11-03 family panels](../log/2026-11-03-family-panels.md),
  [0048](0048-a-sensor-names-its-metrics.md), [0001](0001-semantic-core-and-skins.md)
- **Amends:** [0046](0046-an-overview-of-items-rendered-by-the-hub.md) — the item's page
  charts a series only when its family is not grouped by a label it carries

## Context

An item's page charts every series it has ([overview](../specs/overview.md#item-page)). A
backup mirror that reports three jobs for each of two repositories has 26 backup series,
so the page shows 26 charts, and a chart of an age or of a failure flag says little that
its newest value does not. A reader asks a question about each repository: did its jobs
run, and how long ago. The series need to be gathered by the thing they describe and
shown as a summary, with each figure still leading to its chart.

The same shape is seen in other families: a disk's volumes, Time Machine's destinations,
an external sensor's queues. A fix for backups alone would have to be done again for
each of them. And a summary that is useful for backups is not the same as one for disks,
so a single generic layout cannot be the whole answer.

## Decision

- **A sensor declares, for a family of its metrics, the label that tells its subjects
  apart**: the **group label**. Each group is the series of one item in that family that
  have the same value of the group label. `backup` is grouped by `backup`, so `web` and
  `vpn` are two groups.
- The declaration is part of the manifest, next to the family's name, and is kept the way
  names are kept ([0048](0048-a-sensor-names-its-metrics.md)): it is stored with the
  newest value, it lasts while the node is silent and across restarts, and it applies to
  a family that the same request carries a measurement of.
- **A page draws each group as one panel**; today that page is the item's page, and the
  other skins are unchanged. The panel says the group's state and its
  series' newest values, and every value links to its series' history. A family that
  declares no group label is drawn as before.
- **What a panel looks like is the skin's choice.** A skin has a generic panel that works
  for every family. It may also register its own panel for a family, keyed by the family
  id, and that panel shows every series of the group. The generic panel is what an
  external sensor's family gets. A panel of its own needs code in the skin.

## Consequences

- A family can be shown as panels without any code in the hub. A built-in or external
  sensor only has to declare the group label.
- The hub knows a family only for a panel of its own. That is skin code, and it does not
  touch the semantic core ([0001](0001-semantic-core-and-skins.md)). Backups get the first
  such panel.
- The manifest grows by one small map. An agent from an older release does not declare a
  group label, so its families are charted until it updates, or until another node
  declares the label.
- A group label is never removed. It is replaced by a newer declaration, the same as a
  name.

## Alternatives

- **A hub table mapping each family to its group label**: rejected. It costs less now,
  but the hub would have to know each family, and an external sensor would get nothing
  without a change to the hub. This is the hub catalogue that
  [0048](0048-a-sensor-names-its-metrics.md#alternatives) already rejected for names.
- **The sensor also describes the panel's layout** (which figure leads, in what order,
  with what words): rejected. The contract between the agent and the hub would then carry
  fields for one skin, which [0001](0001-semantic-core-and-skins.md) rules out, and the
  description would keep growing toward a layout language.
- **Grouping set by the reader in the interface, stored the way thresholds are**:
  rejected for now. Every family would be charted out of the box until someone set it,
  and the interface would need a settings form. It could still be added later as an
  override of the declared group label.
- **Guessing the grouping from the labels**: rejected. `backup` carries `backup` and
  `job`, while `disk` carries `mount`, `fs` and `removable`. Nothing in a label key says
  which one names the subject.
