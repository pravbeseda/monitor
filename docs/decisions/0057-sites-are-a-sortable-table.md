# 0057. Show the sites as one table that sorts by any of its figures

- **Status:** accepted; amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) — the
  sites are rows of a table, not cards;
  [0053](0053-a-computer-card-is-lines-on-a-shared-scale.md) — sites no longer keep the rows
  card; and [0055](0055-a-computer-card-line-is-its-values.md) — the sites have no card for a
  computer's card to be narrower than
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#site-table)

## Context

A site's card showed its first two rows as large numbers side by side, each with a line of
its last day, then the rest as a run of text and a sentence at its foot. Which two came
first depended on what needed attention, so one card led with views and the next with
requests; a server-error chart was a flat line under two dashed thresholds, and "All within
thresholds" closed nearly every card. Sites are many and alike — the same four figures each
— and the questions asked of them are comparative: which gets the most traffic, which
errs, which is slow.

## Decision

- The sites section is one table, a row per site: its name with a dot in its state's
  colour, requests per minute, a line of that series' last day with no threshold drawn,
  views a day, server errors, response p95, and a last column for what needs attention
  beyond them and for when a stopped site last reported.
- A value cell is the newest value in its mark's colour with why it is marked under it, as
  a computer card's line says it; a dash where the site has none.
- Every header but the day line's and the last column's sorts the rows by its column, on
  the server: the sort is
  a query parameter, so the page's own refresh and a shared link keep it. A value column
  sorts from the largest first, a second click reverses, sites without the value stand last.
- With no sort chosen, rows stand as cards do: the red ones first, then by name.
- On a narrow screen the table scrolls sideways, the site column staying in view.
- Computers and services keep their cards.

## Consequences

- The overview reads a second query parameter, `sort`, beside `lang`.
- A threshold on requests per minute is said under the value, not drawn: the item's page
  still draws it.
- A new site metric reaches the table only under "Also", and only while it needs attention,
  until the spec gives it a column; a column is a second place a site metric needs code, as
  a slot is for computers.

## Alternatives

- **Cards led by the traffic and its day line, then fixed lines.** Built first in this
  change and dropped before merge: comparable card to card, but sorting is what the reader
  asked for, and cards cannot be sorted by a figure without moving under the reader.
- **Lines only, as a computer's card.** The most compact card, but no shape of traffic and
  no sorting.
- **Sorting in the browser.** No round trip, but it needs client code of the skin's own,
  which [0035](0035-mission-control-is-rendered-by-the-hub.md) keeps out until a skin needs
  a client, and the page's refresh swaps the body in, which would drop a sort held only in
  the browser.
