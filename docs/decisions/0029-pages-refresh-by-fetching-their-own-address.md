# 0029. Pages refresh themselves by fetching their own address

- **Status:** accepted
- **Date:** 2026-09-18
- **Source:** [web spec](../specs/web.md#live), [ADR 0005](0005-poc-stack.md),
  [ADR 0026](0026-reader-time-zone-from-the-browser.md)

## Context

The hub's pages are rendered on the server ([0005](0005-poc-stack.md)) and show the state at
the moment they were loaded. A panel is left open on a screen, and nothing on it says the
state it shows has moved on: the reader has to remember to reload, and loses their place in
the page when they do.

Two kinds of change reach a page. One arrives with an ingest request. The other arrives with
nothing at all: a series that stops arriving is marked stale, or leaves the index, because
the hub's clock moved on ([history.md](../specs/history.md#page)); a chart's window ends at
the hub's present and slides with it. Whatever refreshes a page has to see both.

Agents report every few minutes (`base_tick` defaults to 5m), and the panel is personal:
one or two readers, not a crowd.

## Decision

- **The shared shell carries a script that fetches the page's own address every 30 seconds**
  and, when the answer's `<body>` differs from the one the page shows, puts it in place of
  the old one. The document stays, so the window keeps its scroll position; an unchanged
  answer touches nothing.
- **The answer is whatever a reload would show.** Same address, same cookies: the query and
  the language come along without the script knowing about either, and every change a fresh
  load would show — time-driven ones included — reaches the open page the same way. The
  script restates the reader's zone ([0026](0026-reader-time-zone-from-the-browser.md))
  before every fetch, so a browser that moved, or dropped its cookie, is answered in the
  zone it is in now.
- **Whether an answer counts is settled first, then how it is shown**; the cases are in
  [web.md](../specs/web.md#live). An answer that does not count leaves the page as it is
  under a notice, because on a panel the last good numbers with a warning are worth more
  than an error in their place. An answer that counts is chosen between by a marker in the
  shell naming the rendering — the hub's version and the page's language: the same rendering
  replaces the body, another reloads the page whole, since its head differs too. The marker
  also carries the notice's text, so the script holds no user-facing string. A refused
  credential stops refreshing until the reader reloads by hand, or the browser would ask for
  it every period.
- **A hidden tab fetches nothing**, and a page brought to the front, or back from the
  browser's history, refreshes at once.
- **The period is a constant in the shell**, not configuration: it describes how the tool
  behaves, not an installation, and nobody has asked to change it.

## Consequences

- A page in front is never silently more than a minute behind the hub
  ([web.md](../specs/web.md#invariants)).
- Every open, visible page renders on the hub twice a minute. For a personal panel that is
  noise; a hub with many readers would want a push instead (below).
- A page's body is replaced only when it changed, and then a text selection or a focused
  element inside it is lost — often, on a chart whose window slides visibly between two
  refreshes. That is the price of not diffing the page node by node.
- The hub gains no endpoint, no connection it holds open and no state per reader. The proxy
  gains one requirement: a `Content-Security-Policy`, if it sets one, must let a page fetch
  its own origin as well as run the shell's inline script
  ([nginx-requirements.md](../nginx-requirements.md)).
- When the proxy stops accepting a reader's credentials, the browser asks for them, as a
  reload would; the script cannot prevent that prompt, only keep it from repeating once
  refused, and from repeating more often than every two minutes while nobody answers it.
- The script's behaviour in a browser is checked by hand, as [web.md](../specs/web.md#live)
  states. A browser-driven test would need a browser in CI, which is a separate decision.
- A page added later refreshes itself because it uses the shell; it needs to do nothing and
  can opt out of nothing.

## Alternatives

- **Server-Sent Events of new values, pushed on every ingest** — instant. Rejected for now:
  it needs a publisher inside the hub, a connection held open per reader, buffering turned
  off in the proxy — another requirement on the Ansible repository — and a hook in the
  evaluation tick, since the changes that arrive with no ingest would be missed otherwise.
  None of that buys anything when agents report every five minutes. It is not the event
  stream of state transitions [concept.md](../concept.md) plans for skins
  ([0001](0001-semantic-core-and-skins.md)); that one carries meaning, not values, and is
  decided separately.
- **Reloading the page on a timer** (`location.reload()` or a `<meta http-equiv="refresh">`)
  — the simplest, and browsers restore the scroll position themselves. Rejected: the page
  blinks every period whether anything changed or not, a selection is lost every time, and a
  failure replaces the panel with the browser's own error page instead of a notice.
- **A change token on a new endpoint** — the page polls a cheap `/api/v1/...` value and
  reloads only when it moves. Rejected: the token has to account for the time-driven changes
  too, which means computing most of the page to decide whether to compute the page; and it
  is a contract to keep for one consumer.
- **Diffing the page node by node** (a morphing library) — would keep a selection across a
  change. Rejected: a dependency, or a few hundred lines, for a selection on a page that
  changes every few minutes at most.
