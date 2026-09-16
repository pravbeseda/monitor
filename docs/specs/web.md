# Spec: Web presentation

- **Status:** approved
- **Owns:** what every hub HTML page shares — how it learns the time zone the reader is in,
  how it says which zone it used, and the shell that carries both: `internal/hub/shell.go`,
  `internal/hub/templates/shell.html` and the printer's zone in `internal/i18n`. Formatting
  itself stays with `internal/i18n`; what a page *contains* stays with that page's own spec
  ([history.md](history.md) for `/history` and for the values on `/`); the reader's language
  is settled by [0008](../decisions/0008-english-repo-bilingual-ui.md) and needs nothing
  here. The JSON API is not a reader: nothing here touches it.
- **Decisions:** [0005](../decisions/0005-poc-stack.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0026](../decisions/0026-reader-time-zone-from-the-browser.md)

## Purpose

A timestamp means nothing until it is read in the zone the reader lives in: "collected at
07:05" is a different fact in UTC and in the reader's evening. The hub stores UTC and
answers UTC on the API, so the zone is a property of the *reader*, not of the data — and
the only party that knows it is the browser.

This spec owns that one translation: how the browser's zone reaches the hub, what a page
does before it has it, and what happens when it is wrong or refused. It owns no page's
content and no number's format.

## Behaviour

### The reader's zone {#zone}

| Request | What the reader sees |
|---|---|
| a first visit, from a browser in `Europe/Moscow` | the page arrives a second time on its own, and from then on every time on it is Moscow, marked `MSK` |
| a later visit from that browser, while it still holds what it stored | the same, and no second arrival |
| a first visit carrying a query — a chart window, a language | that same query after the second arrival; nothing the reader asked for is lost, and no extra step appears in the browser's history |
| a first visit with scripting turned off | every time in UTC, marked `UTC` |
| a visit with scripting turned off, from a browser that has been here before | the zone that browser reported last time |
| a browser reporting a zone no zone database carries — stale, invented, implausible, or naming the hub host's own clock rather than the reader's | every time in UTC, marked `UTC`, as a page and not an error |
| a browser that will not store what the page asks it to remember | every time in UTC, arriving once and not again |
| the reader's browser moves to another zone | the next page shows the new zone |
| a chart | its time axis read in the reader's zone, and that zone named once on the chart |
| `GET /api/v1/history` or `/api/v1/series`, from the same browser | RFC 3339 UTC, unchanged ([history.md](history.md)) |
| a Telegram alert or a digest of the same event | unchanged; a notification is not a page |

The zone is stated, not assumed: a reader who moved and a reader who did not must be able to
tell which of them they are.

### The shell {#shell}

| Request | What the reader sees |
|---|---|
| any HTML page of the hub | `Cache-Control: no-store`, and `Vary: Cookie, Accept-Language` — the answer depends on who is asking and is never a stored copy |
| the same address opened again after the browser has learned its zone | the reader's zone, never the earlier answer served again |
| a refusal or a failure rendered as a page | the same zone handling as any other page: a first visit that fails still leaves the reader in their own zone afterwards |

## Invariants

- Every page says which zone its times are in, and no two times on one page are in
  different zones.
- A zone changes how an instant is written, never which instant it is: no value moves, none
  is dropped, and none is invented.
- Two pages loaded a moment apart in one browser show one instant identically.
- A page is written in the reader's zone or in UTC, never in a third one — in particular
  never in the hub host's.
- No zone a browser reports makes the hub open anything outside its zone database, fail a
  request, or answer twice as slowly.
- A page arrives at most twice for one browser zone, and a browser that stores nothing still
  gets a page.

## Edge cases

- **A zone name the hub will not accept** — longer than 64 bytes, carrying anything outside
  `A-Za-z0-9_+-` and `/`, opening with a `/`, or naming no region (no `/` in it) — is UTC.
  The last of those is the one that matters: the flat names a zone database also answers to
  include one meaning "this machine's own clock", which would render the hub host's zone and
  satisfy nobody. A browser already in UTC reports a flat name too and is refused by the
  same rule, which costs it nothing: what a refusal renders in is the zone it asked for.
- **A zone with no abbreviation** — most of the world outside the Americas and Europe — is
  marked by its offset, `+07`, which is what its readers recognise anyway.
- **A day boundary inside a chart's window** falls where the reader's zone puts it, so the
  same points read from two zones can label a tick with different days. Which day a tick is
  labelled with follows the reader; where the ticks sit does not change.
- **A window crossing a daylight-saving change** labels hours the way that zone's clocks do,
  repeating or skipping one. No point moves and no point is dropped. The zone a chart names
  is the one its window ends in, so half such an axis is captioned by the other half's name —
  an hour's worth of imprecision in a caption, against a second caption on every chart.
- **A browser that drops what it stored** — Safari caps a cookie written by a script at
  seven days — is a first visit again, and converges the same way. Nothing accumulates.
- **A page reached by the back button** is fetched again wherever a page that may not be
  stored is also not restored, and shows the current zone; where it is restored instead, it
  is the rendering it was left with, since nothing runs on a restore. Which of the two a
  reader gets is their browser's to decide, and neither is wrong.
- **A storage failure on the index** is plain text rather than a page, as it was before any
  of this, so it carries no shell and leaves the reader's zone unlearnt until the hub answers
  again. It is not cached either way.
- **Two readers in different zones** are two browsers. Nothing is shared between them, and
  neither sees the other's zone.

## Out of scope

- How a number, a byte size or a date is *written* in each language, and how the language is
  chosen — [0008](../decisions/0008-english-repo-bilingual-ui.md) and `internal/i18n`.
- What a page contains and which queries it accepts — that page's own spec.
- The zone a digest is sent in: a property of the installation, configured
  ([evaluation.md](evaluation.md)), not of whoever opens the panel.
- Whether a page may be opened at all, and any policy the proxy sets in front of it —
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md) and
  [nginx-requirements.md](../nginx-requirements.md).

## Open questions

None.
