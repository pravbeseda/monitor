# 0053. Lay a computer's card out as lines on a scale its section shares

- **Status:** accepted, amended by
  [0054](0054-the-backup-line-gathers-the-backup-families.md) — the backup line gathers the
  backup families — by [0055](0055-a-computer-card-line-is-its-values.md) — a line is
  its name and its values, no scale — and by
  [0057](0057-a-site-card-leads-with-its-traffic.md) — a site's card is a lead and lines;
  amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) — a
  computer's card carries no line of the day, and its slots are a second place a metric
  needs code — and [0048](0048-a-sensor-names-its-metrics.md) — a slot is named by the
  skin, the declared name of its series said on hover
- **Date:** 2026-11-04
- **Source:** [2026-11-04 computer card](../log/2026-11-04-computer-card.md),
  [overview.md](../specs/overview.md#computer-card)

## Context

A computer's card showed its two first rows as large numbers with a line of their last day,
the rest as a run of text, and a sentence at its foot. In use the lines said almost nothing —
a day of free memory or disk is flat on a scale from zero — the text was long, and two cards
could not be read against each other because each put different series in the same place.
What a card has to say first is why it holds its state; what a reader wants second is to
compare one computer with the next.

## Decision

- A computer's card is a fixed set of **slots** — load, memory, disk, backup, in that
  order — each a line of the same height on every computer card, a dash where the computer
  has no such series; a computer with none of the four has no slots. What needs attention
  beyond them follows as extra lines.
- A line is a **bullet**: a scale from zero, the band the series kept the week before
  yesterday drawn on it with its norm, the readable threshold values as ticks, and a dot at
  the newest value in the colour of its mark.
- A slot's scale is **shared by every computer card of the section**, so a line compares
  with the same line of the next card.
- The State API carries the band — `low` and `high` beside `norm` — so a skin can draw it.
- Sites and services keep their cards: their series share no scale from one item to the
  next.

## Consequences

- The day's line leaves a computer's card; it stays on the item's page, on `/history`, and on
  the cards of sites and services.
- The sentence at the foot of a computer's card goes: the mark's colour, the norm or the
  threshold beside the value, and the failed units in the card's head say the same in less.
- A new slot is a product decision taken in the spec, not configuration.

## Alternatives

- **Keep the lines, scaled to the value instead of zero.** Shows movement, but a day of
  noise looks like an event, and two cards still do not compare.
- **The value's distance from its norm, in widths of its band.** Says "unusual" loudest,
  but a disk 18% free and one 75% free look the same; comparing computers is lost.
- **A scale per card.** The band always shows, but comparison is lost the same way.
- **A table, one row per computer.** Compares best on a wide screen, but breaks on a phone
  and leaves gaps where computers differ.
- **Exceptions only — problem cards in full, the rest folded to one line.** Quietest, but
  the layout jumps as states change and healthy computers cannot be compared.
- **Chips, a sentence, plain bars.** Considered in the same pass; see the design note.
