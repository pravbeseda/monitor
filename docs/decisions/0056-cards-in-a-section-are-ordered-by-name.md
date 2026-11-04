# 0056. Order the cards of an overview section by name, the red ones first

- **Status:** accepted; amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) —
  cards no longer go from the worst state to the best, only the red ones lead
- **Date:** 2026-11-04
- **Source:** [overview.md](../specs/overview.md#order)

## Context

The overview ordered a section's cards from the worst state to the best. A section holds a
handful of cards, and with a computer's card showing its slots in fixed places
([0053](0053-a-computer-card-is-lines-on-a-shared-scale.md)) the reader finds a computer by
where it stands; a card that moves whenever a state changes has to be searched for again,
and unusual comes and goes several times a day. A critical or silent card is the exception:
on a phone it can sit below the fold, and it is the one card the reader must not miss.

## Decision

A section's cards stand in two runs: the red ones — critical or silent — first, then every
other card; each run is ordered by the item's name: character by character in code point
order, a capital letter as its small one in any alphabet, digits as characters, names equal
but for case by their bytes. Unusual, warning and the rest share one run. The sections keep
their fixed order, and a heading still counts its states in the order silent, critical,
warning, unusual, no fresh data.

## Consequences

- A card keeps its place as it turns unusual or warning and back; it moves only when it
  turns red or stops being red, which is the change worth the jump.
- A red card stands ahead of every other card in its section.

## Alternatives

- **The worst first, as before.** Brings what is wrong to the top, but the layout reshuffles
  on every change — unusual comes and goes several times a day — and the reader asked for a
  fixed one.
- **By name alone.** The steadiest layout, but a critical card can sit last in a section,
  below the fold on a phone; the reader asked for the red ones on top.
- **Natural order, `server-9` before `server-10`, or a language's collation, `ё` beside
  `е`.** Reads better with such names, but costs a parser or a collation dependency for a
  section of a handful of cards; the code points of the small letters are enough until such
  names appear.
- **Byte order.** Simpler, but puts every capitalised name before every small one.
