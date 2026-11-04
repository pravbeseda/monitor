# 0056. Order the cards of an overview section by name

- **Status:** accepted; amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) —
  cards no longer go from the worst state to the best
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#order)

## Context

The overview ordered a section's cards from the worst state to the best. A section holds a
handful of cards, and with a computer's card showing its slots in fixed places
([0053](0053-a-computer-card-is-lines-on-a-shared-scale.md)) the reader finds a computer by
where it stands; a card that moves whenever a state changes has to be searched for again,
while its colour stripe and pill already make a bad state stand out wherever it stands.

## Decision

Every section's cards are ordered by the item's name, whatever their state: character by
character in code point order, a capital letter as its small one in any alphabet, digits as
characters, names equal but for case by their bytes. The sections keep their fixed order, and a heading still counts its
states in the order silent, critical, warning, unusual, no fresh data.

## Consequences

- A card keeps its place as states change and as the page refreshes itself.
- A critical card can stand last in a section; on a narrow screen it can be below the fold.

## Alternatives

- **The worst first, as before.** Brings what is wrong to the top, but the layout reshuffles
  on every change, and the reader asked for a fixed one. What is wrong is still found at a
  glance: the heading counts it, and the card's stripe and pill colour it.
- **Natural order, `server-9` before `server-10`, or a language's collation, `ё` beside
  `е`.** Reads better with such names, but costs a parser or a collation dependency for a
  section of a handful of cards; the code points of the small letters are enough until such
  names appear.
- **Byte order.** Simpler, but puts every capitalised name before every small one.
