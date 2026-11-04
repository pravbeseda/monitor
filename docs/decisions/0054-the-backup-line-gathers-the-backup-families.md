# 0054. Let a computer card's backup line gather the backup families

- **Status:** accepted; amends [0053](0053-a-computer-card-is-lines-on-a-shared-scale.md) —
  what needs attention in the backup families is said on the backup line, not as extra lines
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#computer-card)

## Context

A backup reports many series: per job its success age, its newest archive's age, the
failure flag, the archive count, the bytes stored; Time Machine its age per destination.
When a backup stalls, most of them turn unusual together. Under 0053 each became an extra
line beside the "Backup" slot, which already said the same: two of the card's lines were
"Newest archive …" cut to "Newest ar…", indistinguishable by job, the rest counted as
"N more". The card told one fact four times and none of the copies could be read.

## Decision

- The backup slot **gathers** every other series of the families `backup` and
  `timemachine` its computer has: none of them is a line, a value, a tick, or counted in
  "N more". Their marks compete for what the backup line says under its value, after the
  series on its scale and in the page order among themselves.
- A gathered series that speaks is said by its **mark's word alone** in its mark's colour;
  its name and its reason ("usually 4.0 h", "threshold 0.00") are said on hover and to a
  screen reader. Besides the levels and unusual, a gathered series speaks when it is
  watched and holds no fresh data, as an extra line would have shown it.
- A backup slot that reads a dash gathers nothing: a job that never succeeded keeps its
  failure as an extra line.
- Gathering is layout only: the card's state, its order and its section's counts still
  come from every series, and the item's page lists each of them.

## Consequences

- The card says that the backup needs attention, and how urgently, in one place: the most
  urgent gathered mark reaches the backup line in its colour; the others are on the item's
  page.
- Which job or destination spoke is one hover away, not on the card.
- A new family of backup sensor joins the gathering only by being added to the list of
  families; other families keep their extra lines.

## Alternatives

- **The gathered series' short name and reason under the value** ("archive: usually
  13.8 h"): says more, but the values column is narrow and the Russian names are long; it
  would be cut as the extra lines were.
- **The reason alone** ("usually 13.8 h"): reads as the norm of the value above it, which is
  another series' — wrong at a glance.
- **Gathering by the slot's muted values**, the way the load line shows its 1- and 5-minute
  averages: a value per job does not fit the column and never wraps.
