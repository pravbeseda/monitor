# 0035. Mission control is rendered by the hub; TypeScript waits for a skin that needs a client

- **Status:** accepted
- **Date:** 2026-09-23
- **Amends:** the line of [0005](0005-poc-stack.md) that keeps skins in TypeScript over the
  State API, and the answer to question 1 in [poc.md](../poc.md) that repeats it
- **Source:** [mission-control spec](../specs/mission-control.md),
  [design notes](../log/2026-09-23-mission-control.md)

## Context

[0005](0005-poc-stack.md) chose Go for the hub and kept TypeScript for skins. Everything
built on the web side since then is server-rendered, though: the drill-down chart
([0018](0018-history-through-the-api.md)), the reader's zone
([0026](0026-reader-time-zone-from-the-browser.md)), the self-refreshing shell
([0029](0029-pages-refresh-by-fetching-their-own-address.md)) and the en/ru catalogue of
`internal/i18n` ([0008](0008-english-repo-bilingual-ui.md)).

Mission control is the first skin after the debug table. It is a list of what needs
attention, refreshed every 30 seconds. A TypeScript client would have to repeat the zone,
the refresh and the catalogue in the browser, and bring Node, a bundle, its own linters and
a fifth CI gate ([0011](0011-quality-gates.md)) with it.

## Decision

- **Mission control is a page the hub renders** with `html/template`, from the same state
  the State API answers, under the rule the debug table already follows: it shows nothing
  the endpoint would not return at the same instant.
- **It is the hub's primary view, at `/`.** The debug table moves to `/debug` and stays as
  the debugging mode [0001](0001-semantic-core-and-skins.md) requires.
- **TypeScript arrives with the first skin that needs a client** — one that draws, like the
  city — and brings its toolchain and CI gate then, as its own decision.

## Consequences

- Mission control gets the reader's zone, the refresh and both languages from the shell
  with no new code, and the hub stays one binary with no frontend build.
- The State API is not yet proven by an external consumer; the invariant above is what
  keeps the page from drifting past it, and a skin outside the hub can read the same fields.
- A bookmark of `/` now opens mission control; the table is one link away.
- A skin in TypeScript later is a new consumer of `/api/v1/state`, not a rewrite of this
  page.

## Alternatives

- **Mission control in TypeScript, as 0005 foresaw.** Rejected for this skin: two to three
  sessions of toolchain and a second copy of the zone, refresh and catalogue logic, for a
  page the server renders completely. It stays the plan for a skin that draws.
- **Mission control at a new address, the table staying at `/`.** Rejected: the concept
  names mission control the primary view, and the first thing opened should be the one that
  answers "is everything fine".
