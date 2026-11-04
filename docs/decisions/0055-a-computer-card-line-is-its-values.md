# 0055. Draw nothing between a computer card line's name and its values

- **Status:** accepted; amends [0053](0053-a-computer-card-is-lines-on-a-shared-scale.md) —
  a line is no longer a bullet on a scale its section shares; amended by
  [0057](0057-a-site-card-leads-with-its-traffic.md) — site cards are as narrow as computer
  cards
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#computer-card)

## Context

A computer's card laid each slot out as a bullet: a scale from zero, the usual band with its
norm, ticks at the thresholds and a dot at the value. In use the scale took the card's middle
third, so three cards stood in a row where the reader wanted four, and what the reader looked
at was the numbers and their colour — the mark's colour on the value and the "usually X" or
"threshold X" under it already say whether a value is out of line.

## Decision

- A line of a computer's card is its name at the left and its values at the right, nothing
  drawn between them; the slots, their order, the gathering and the marks stay as 0053 and
  0054 set them.
- A computer's card is narrower than a site's or a service's: four stand in a row at the
  page's full width.

## Consequences

- A slot's scale end, the band, the norm, the ticks and the dot leave the card, and with them
  the screen reader's words for them.
- The State API keeps `low` and `high` beside `norm`: they are the anomaly's band, and no skin
  draws them for now.
- How close a value is to its threshold before it crosses is read from the number alone; the
  threshold itself is on the item's page.

## Alternatives

- **Keep the bullet and narrow it.** Four cards fit, but the scale shrinks to a strip where a
  band and two ticks no longer read.
- **Keep the bullet only on a marked line.** The card's height and layout would jump as states
  change, which 0053 rejected for the same reason.
