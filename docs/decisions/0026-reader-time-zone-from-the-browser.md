# 0026. The reader's time zone comes from the browser in a cookie; pages stay server-rendered

- **Status:** accepted
- **Amends:** the "no cookie" clause of [0023](0023-proxy-holds-the-web-perimeter.md) — the
  hub now sets exactly one cookie, carrying the reader's time zone and nothing else. The
  perimeter decision itself stands untouched.
- **Date:** 2026-09-16
- **Source:** [web spec](../specs/web.md), [ADR 0005](0005-poc-stack.md),
  [ADR 0008](0008-english-repo-bilingual-ui.md)

## Context

Every timestamp on the hub's pages is written in UTC: `internal/i18n` formats with `.UTC()`
because the hub stores UTC and, until now, the reader was assumed to accept it. They do not:
"collected 07:05" read in the evening is a fact about the wrong day.

The zone is a property of the reader, not of the data. The hub cannot derive it — the
request carries a language (`Accept-Language`) but no zone, and the host's own zone is the
server's, not the reader's. Only the browser knows, through
`Intl.DateTimeFormat().resolvedOptions().timeZone`.

[0005](0005-poc-stack.md) put rendering on the server with `html/template` and no frontend
build, and more pages are planned. Whatever is decided has to hold for pages not written yet.

## Decision

- **The browser states its zone in a `tz` cookie**, an IANA name, written by a few lines of
  inline script in the shared page shell. The script writes the cookie, reads it back, and
  fetches the page again only when the read-back returns what it wrote: a browser that will
  not store cookies is served UTC once, not reloaded forever. It reloads rather than
  navigating to the same address, which keeps the whole address — a query, a fragment — and
  adds nothing to the browser's history.
- **The hub never writes that cookie.** It only reads it. A hub that wrote a canonical name
  back over the browser's own would be answered by the script rewriting it, forever.
- **The hub formats every page-facing time in that zone**, server-side as before: the
  printer of `internal/i18n` carries a location, and `Time`, `Clock`, `Day` and `Zone` — the
  one that names the zone for a chart — all render in it.
- **A zone the hub will not accept is UTC.** The name is bounded before it reaches
  `time.LoadLocation`: at most 64 bytes, characters `A-Za-z0-9_+-` and `/`, no leading `/`,
  and it must name a region — contain a `/`, or be exactly `UTC`. That last clause is the one with teeth: a
  zone database also answers to flat names, `Local` among them, and `Local` is the hub
  host's own clock. Rendering the server's zone would be worse than UTC, because it looks
  right.
- **The zone is stated once per page**, next to the time where there is one and on the chart
  next to its time axis, where the per-tick labels have no room for a marker.
- **HTML pages answer `Cache-Control: no-store` and `Vary: Cookie, Accept-Language`.** A
  page now varies by a cookie, and a stored pre-cookie copy would pin a reader to UTC with
  nothing to correct it — the script does nothing once the cookie is already right. The
  pages already varied by `Accept-Language` and said so to nobody; one header closes both.
- **Nothing else moves.** `/api/v1/history` and `/api/v1/series` stay RFC 3339 UTC, the
  stored measurement is untouched, notification timestamps stay UTC as they are today — the
  configured zone still only picks the digest's hour — and log lines and CLI output stay UTC.
  Where the chart's axis ticks sit does not change either; only the labels' zone does.
- **The zone database ships in the hub binary** (`time/tzdata`), so a host carrying none
  still formats correctly. A host with its own copy keeps using it, which is Go's ordering
  and not worth overriding.

## Consequences

- A page added later gets the reader's zone from the shared shell and the request's printer.
  That is the reason this was chosen over rewriting timestamps in the browser — but it is
  not free of every mistake: a future handler that builds a printer without the request's
  location is silently UTC, the same way a forgotten wrapper would have been.
- The pages depend on a cookie and on scripting for this one thing, and degrade to UTC
  without either. It is the first cookie in play here at all — written by the page, read by
  the hub, never set by it. It carries no identity and no secret,
  which is why [0023](0023-proxy-holds-the-web-perimeter.md) is amended rather than reopened.
- The proxy must not put a `Content-Security-Policy` in front of the pages that blocks the
  shell's inline script; blocked, it degrades to UTC silently and nothing here reports it.
  Stated as a requirement in [nginx-requirements.md](../nginx-requirements.md).
- A first visit from a new browser costs one extra round trip. Later visits cost nothing.
- The hub binary grows by the embedded zone database (~450 KB).
- The cookie is attacker-controlled input reaching `time.LoadLocation`, which does open
  files. It cannot escape the zone directory, and the bound above is what keeps the rest
  honest.
- **There is no way for a reader to choose a zone other than their browser's.** A work
  laptop pinned to UTC, or someone watching a machine in another country, has no override.
  The first reader who needs one reopens this decision.
- Two pages of the hub now share one shell — the head and the script — rather than each
  carrying a copy of it. A third page starts from that shell.

## Alternatives

- **Rewriting timestamps in the browser** — emit `<time datetime="…">` and reformat with
  `Intl.DateTimeFormat` on load. Rejected mainly because the chart's axis ticks are chosen
  and labelled server-side, so they would stay UTC while the prose around them moved; and
  because it costs work in every template and every future page.
- **Setting the cookie without navigating again** — the first page is UTC, every later page
  is right. Rejected: this panel is opened cold, glanced at, and closed. The first page is
  the one that matters, and a panel that is right only on the second look is a panel that is
  wrong.
- **The browser's UTC offset instead of an IANA name** (`getTimezoneOffset`) — needs no zone
  database at all and would delete the 450 KB. Rejected: one offset is wrong for every
  instant on the other side of a daylight-saving change, which a 7-day window routinely
  spans. A chart that bends by an hour in the middle is worse than UTC.
- **A display zone in the hub's configuration** — rejected: it describes an installation,
  not a reader, and it is the wrong shape for a panel opened from a laptop that travels.
- **A `?tz=` query parameter, the way `?lang=` already works** — rejected as the *carrier*:
  it has to be threaded through every link the pages generate, which is the per-page cost
  the cookie avoids, and a shared address would then carry the sharer's zone. A reader-facing
  override is a separate question, left open above.
- **Guessing the zone from the language, or from what the nodes report** — rejected: a
  Russian-speaking reader is not necessarily in a Russian zone, and a wrong guess is worse
  than UTC, which is at least unambiguous.
- **A client hint for the zone** — rejected: none is standardised, and a header no browser
  sends is a cookie with extra steps.
