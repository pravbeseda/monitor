# 0057. Lead a site's card with its traffic and lay its other figures out as fixed lines

- **Status:** accepted; amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) — a
  site's card is a lead and fixed lines, not its rows;
  [0053](0053-a-computer-card-is-lines-on-a-shared-scale.md) — sites no longer keep the rows
  card; and [0055](0055-a-computer-card-line-is-its-values.md) — site cards are as narrow as
  computer cards
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#site-card)

## Context

A site's card showed its first two rows as large numbers side by side, each with a line of
its last day, then the rest as a run of text and a sentence at its foot. Which two came
first depended on what needed attention, so one card led with views and the next with
requests; a server-error chart was a flat line under two dashed thresholds; "0.00" took the
space of a headline; and "All within thresholds" closed nearly every card. What a reader wants
of a site first is its traffic and its shape over the day; the error rate and the response
time are almost always quiet and need a place, not a chart.

## Decision

- A site's card leads with `site.requests_per_min`: its value large, and a line of its last
  day across the card, with no threshold drawn on it.
- Under it, fixed lines as a computer's card draws them
  ([0055](0055-a-computer-card-line-is-its-values.md)): views a day, server errors, response
  p95, a dash where the site has none, then what needs attention beyond them as extra lines.
- The card carries no reason, and says when it last reported only once it stopped.
- Site cards are as narrow as computer cards: four in a row at full width.
- A service's card keeps the rows card for now.

## Consequences

- Site cards compare line by line, as computer cards do; the shape of traffic stays the one
  chart on the card.
- A threshold on requests per minute is said beside the value, not drawn: the item's page
  still draws it.
- A new site metric reaches the card only as an extra line, and only while it needs
  attention, until the spec gives it a slot; a slot is a second place a site metric needs
  code, as for computers.

## Alternatives

- **Lines only, as a computer's card.** The most compact, but loses the shape of the
  traffic, which is what tells a spike from a steady load.
- **A small chart on every line.** Comparable and shows trends, but the error and latency
  charts are flat nearly always: noise at a size too small to read.
- **A table, one row per site.** Compares best on a wide screen, but breaks on a phone and
  looks unlike the computers above it.
