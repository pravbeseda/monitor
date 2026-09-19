# Spec: Web presentation

- **Status:** approved
- **Owns:** what every hub HTML page shares — how it learns the time zone the reader is in,
  how it says which zone it used, how an open page keeps itself current, and the shell that
  carries all three: `internal/hub/shell.go`,
  `internal/hub/templates/shell.html` and the printer's zone in `internal/i18n`. Formatting
  itself stays with `internal/i18n`; what a page *contains* stays with that page's own spec
  ([history.md](history.md) for `/history` and for the values on `/`); the reader's language
  is settled by [0008](../decisions/0008-english-repo-bilingual-ui.md) and needs nothing
  here. The JSON API is not a reader: nothing here touches it.
- **Decisions:** [0005](../decisions/0005-poc-stack.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0026](../decisions/0026-reader-time-zone-from-the-browser.md),
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md)

## Purpose

A timestamp means nothing until it is read in the zone the reader lives in: "collected at
07:05" is a different fact in UTC and in the reader's evening. The hub stores UTC and
answers UTC on the API, so the zone is a property of the *reader*, not of the data — and
the only party that knows it is the browser.

This spec owns that translation — how the browser's zone reaches the hub, what a page does
before it has it, and what happens when it is wrong or refused — and keeping an open page
current. It owns no page's content and no number's format.

A panel is also left open. A page that shows what was true when it was opened, and says
nothing about it, is wrong the same quiet way a page in the wrong zone is: it looks right.
So an open page keeps itself current, and says so when it cannot.

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
| any HTML page of the hub, in a browser tab | the monitor's own icon, a green pulse line on the dark page colour, carried inside the page so it costs no request of its own |

### Keeping an open page current {#live}

| While the page is open and in front | What the reader sees |
|---|---|
| a node reports a new value | the new value within 30 seconds, without the page reloading, at the same distance from the top of the page |
| nothing changes | nothing moves: no flicker, and the scroll position, a text selection and the focus stay as they are |
| a series stops arriving and nothing is reported at all | its stale mark, or its row leaving, within 30 seconds of when a fresh load would first show it |
| a chart | follows the hub's present as a fresh load would, since its window ends there; how often that visibly moves it depends on the window's length |
| the tab is in the background | nothing is fetched; brought back to the front, the page shows the current state as soon as the hub answers |
| a page brought back by the back or forward button | the same: current as soon as the hub answers |
| the hub or the proxy does not answer within 15 seconds, answers with a failure, or answers with something that is not a hub page | the page as it was, under a notice in the reader's language that it is not being refreshed; the notice leaves with the first refresh that succeeds |
| the hub cannot read its data | the same notice over the page as it was, on the index and on a chart alike: the last good rendering outlives a failure |
| the proxy stops accepting the reader's credentials | the browser asks for them, as a reload would; refused, the page stays under the notice and is not refreshed again until the reader reloads it |
| the hub answers with a refusal of the page's query | that refusal, as a fresh load of the address would show it |
| the address carries a query — a chart window, a language | the refreshed page is what reloading that address would show: the same window, the same language |
| the browser has moved to another zone, or dropped what it stored | the next refresh in the browser's current zone |
| the hub was upgraded, or the page's language changed | the page reloads once, whole, in the new rendering |
| the reader uses the back button afterwards | the page they came from; refreshing added nothing to the browser's history |
| scripting is turned off | the page as it was drawn, never refreshed; reloading it by hand still works |

What the script does in a browser — the timing, the swap, the scroll, the notice — is
checked by hand in one; the hub's side of it, what every page carries for the script, is
tested.

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
- Learning the zone costs a page at most one extra arrival, and a browser that stores
  nothing still gets a page.
- A refreshed page shows what reloading its address at that moment would, never a mix of
  two renderings.
- A page in front that is not being kept current says so within a minute: with no notice on
  it, it is at most a minute behind the hub — counted from when it came to the front — or
  has its scripting turned off.
- Whether an answer counts is settled before what it looks like: a failure, a refused
  credential, a redirect or an answer that is not a hub page puts the notice up, whatever
  it carries; only a hub page that succeeded or refused the query is shown, reloading the
  page whole when it comes from another version of the hub or in another language.

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
  is the rendering it was left with, refreshed as soon as the hub answers. Which of
  the two a reader gets is their browser's to decide, and neither is wrong.
- **A page that gets shorter on a refresh** — a row left, a node went — keeps the scroll
  position where it still exists and ends at the new bottom otherwise.
- **A refresh that changes something** replaces the page's content, so a text selection or
  a focused link inside it is lost then. Only a change costs that; an unchanged refresh
  touches nothing. A chart on a short window changes on nearly every refresh, so a
  selection on it rarely lasts longer than a period.
- **A slow answer** puts the notice up after 15 seconds and is still waited for; when it
  arrives, it is shown and the notice leaves. One still missing after two minutes is given
  up on, so a connection that died does not stop the page refreshing once the hub is back;
  a credential prompt nobody answers is dismissed then too, and asked again on the next
  refresh. A refresh never starts while the previous one
  is in flight, so a hub under load gets one request per open page, never a queue of them.
- **Leaving the page** while a refresh is in flight is not a failure: the notice does not go
  up on the way out, nor on a page the browser restores later.
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
- Delivering a change to the page the instant it arrives —
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md) says why a
  30-second fetch is enough for now.
- Whether a page may be opened at all, and any policy the proxy sets in front of it —
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md) and
  [nginx-requirements.md](../nginx-requirements.md).

## Open questions

None.
