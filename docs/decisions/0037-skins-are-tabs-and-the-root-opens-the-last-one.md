# 0037. Skins are tabs, each at its own address; `/` opens the one last chosen

- **Status:** accepted
- **Date:** 2026-09-24
- **Amends:** [0035](0035-mission-control-is-rendered-by-the-hub.md), which put mission
  control at `/`; and the clause of [0026](0026-reader-time-zone-from-the-browser.md) that
  allows one cookie carrying the zone and nothing else — a second one now carries the skin,
  written and read the same way
- **Source:** [timeline spec](../specs/timeline.md), [web spec](../specs/web.md#skins),
  [design notes](../log/2026-09-24-timeline.md)

## Context

The hub renders two skins — mission control and the debug table — and a third, the
timeline, is being added; prototypes of three more are waiting. The reader wants to move
between them in one click and to land on the one they use without choosing it again.
[0001](0001-semantic-core-and-skins.md) makes skins interchangeable consumers of one
state, so nothing in the core cares which one is open.

Two constraints shape how a choice is remembered. The hub authenticates nobody on the web
and keeps no session ([0023](0023-proxy-holds-the-web-perimeter.md)); the one cookie it
reads, the reader's zone, is written by the page and only read by the hub
([0026](0026-reader-time-zone-from-the-browser.md)). And an open page refreshes itself by
fetching its own address with the browser's cookies
([0029](0029-pages-refresh-by-fetching-their-own-address.md)), and reloads whole after an
upgrade, so the hub cannot tell from a request whether a person chose the page or the page
fetched itself.

## Decision

- **Every skin has an address of its own**: mission control at `/board`, the timeline at
  `/timeline`, the table at `/debug`. An address is what a bookmark or a shared link names,
  so it always opens that skin.
- **Every page carries the same row of tabs**, one per skin, the open one marked.
- **A click on a tab is the choice.** The page's script stores the skin in a cookie as the
  click happens; the hub only reads it, as it reads the zone. Opening an address by a
  bookmark or a link, a refresh and a reload choose nothing.
- **`/` redirects to the remembered skin**, to mission control when there is none, keeping
  the query.

## Consequences

- A skin is added by giving it an address and a tab; nothing else changes.
- `/` is no longer a page: a bookmark of it follows the reader's last choice. A page left
  open at `/` across the upgrade stops refreshing until it is reloaded once.
- The choice belongs to one browser and needs scripting, as the zone does: another device,
  or a browser that stores nothing, opens mission control.
- The hub still writes no cookie and keeps no session, so the perimeter of
  [0023](0023-proxy-holds-the-web-perimeter.md) stands; the page now writes two cookies
  instead of one, both preferences of the reader and neither a credential.

## Alternatives

- **`/` renders the remembered skin itself.** Rejected: one address would show different
  pages to the same reader, so a link to `/` could not say what it opens, and the page's own
  refresh would depend on a cookie another tab can change.
- **The hub sets the cookie whenever it renders a skin.** Rejected: a refresh and an
  upgrade's reload are renders too, so two open tabs would take turns overwriting the
  choice; telling them apart needs a marker on the refresh and still misses the reload. It
  would also make the hub write a cookie, which 0023 rules out.
- **The page stores the skin whenever it is opened by navigation.** Rejected: a link to
  `/debug` from mission control's "more unusual series" would then change the choice, which
  the reader never made.
- **Local storage and a redirect by script.** Rejected: the hub would render one skin and
  the browser would then move to another, a visible jump on every visit.
