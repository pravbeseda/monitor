# Spec: Thresholds

- **Status:** approved
- **Owns:** the page that sets what a series is judged by — `GET /thresholds` and the save
  it accepts — and what may be stored as a threshold. What a stored threshold *means*, and
  when it produces a level, an event or a message, stays with
  [evaluation.md](evaluation.md); the shell every page shares, its zone and its refresh stay
  with [web.md](web.md); the series a page can be opened for come from
  [history.md](history.md).
- **Decisions:** [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
  [0033](../decisions/0033-a-subject-is-a-series.md)

## Purpose

Nothing alerts until someone says what "bad" means for one particular series
([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)). This is where they say
it: one page per series, two numbers and a direction, saved into the hub's own store and
applied by the next evaluation tick without a restart.

The page is deliberately the whole configuration surface for alerting. There is no file to
edit, no default to inherit and no layer above it: what the form shows is what the hub
judges by.

## Model

**A configuration belongs to one series** — `(node, metric, labels)`, the subject of
[evaluation](evaluation.md#model). It carries a **direction**, `below` or `above`, and up
to two **values**, `warning` and `critical`, written in the unit of the series. Either
value may be left empty; a configuration with both empty is no configuration at all and is
removed.

**Consistency is per direction.** With `below`, `critical` must be strictly below
`warning`; with `above`, strictly above it. Equal values would make one of the two levels
unreachable, so they are refused rather than silently ordered.

**A series is addressed the way [history](history.md#selection) addresses one**:
`?node=…&metric=…&label.<name>=…`, naming every label the series carries, because a
threshold belongs to one series and not to a family of them.

**The unit is the series' own**, derived from the metric id as everywhere else
([history.md](history.md#wire-format)): a `_bytes` metric takes a size, a `_pct` metric a
percentage number, a `_seconds` metric a duration in seconds, anything else a plain number.
The form says which it is asking for. A size may be written with a decimal unit — `10GB` is
10 000 000 000, as sizes are written everywhere in this project — and everything else is a
plain number; what is stored is always the number in the series' own unit.

**A save is refused unless the browser says it came from this page.** The hub has no
session and no login of its own ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)),
so the guard against another site posting through a reader's cached credentials is the
request's own origin, not a stored token: a save whose `Origin` names another host than the
one it was sent to is refused before anything is read. The host is what is compared — the
hub answers the proxy over plain HTTP, so its own scheme is not the reader's.

## Behaviour

One row = one test. Anchors: `spec: thresholds.md#<heading>`.

### The form {#form}

| Request | What the reader sees |
|---|---|
| a series with nothing configured | an empty form: direction `below`, both values blank, and a line saying the series has no level until a value is set |
| a series with a configuration | its direction and values as stored, in the unit the metric implies |
| any series the hub has values for | the same form, whatever the metric: nothing about it is particular to disks |
| a series the hub has never stored | `404` as a page: a threshold is set on something that reports |
| a series of a node the file no longer names | the form as ever, saying the series is not judged while its node is not configured |
| a series whose stored threshold this build cannot read | the form, with the unreadable values blank and a line saying the stored configuration could not be read and saving will replace it |
| the page while the reader is typing in it | never refreshed under them: a page carrying a form the reader has touched is left alone, whatever [web.md](web.md#live) does to every other page |
| a query naming fewer labels than the series carries, or an extra one | `404`: the address names one series or none |
| a metric or label value that is not a valid query at all | `400` as a page, in the manner of [history](history.md#refusals) |
| the page in Russian | every label, unit and message from the Russian catalogue ([0008](../decisions/0008-english-repo-bilingual-ui.md)) |
| a method other than `GET` or `POST` | `405` |

### Saving {#saving}

| Save | Result |
|---|---|
| direction and both values, consistent | stored; the reader is sent back to the form, which shows what is now stored, and the next tick judges by it |
| one value filled, the other blank | stored: a subject may have only a warning or only a critical |
| both values blank | the configuration is removed; the subject loses its level without an event or a recovery message ([evaluation.md](evaluation.md#configuration-changes)) |
| a value that is not a finite number | refused, naming the field; nothing is stored, and what the reader typed is still in the form |
| a `_bytes` value written `10GB` | accepted and stored as 10 000 000 000; the form shows it back as a size |
| a `_bytes` value written as a bare number | accepted as that many bytes, which is what the field says it is asking for |
| a `_bytes` value no round size names — 20 123 456 789 | shown back as that number, not rounded to a size: redrawing the form must not rewrite the threshold |
| a `_pct` value written `12%`, or any value with a unit the metric does not take | refused, naming the field |
| `critical` not strictly beyond `warning` in the chosen direction | refused, naming both fields; nothing is stored |
| a direction that is neither `below` nor `above` | refused; nothing is stored |
| a save for a series the hub has never stored | `404`; nothing is stored |
| a save whose `Origin` is another site, or a cross-site form post | refused as a page; nothing is stored |
| a save with no `Origin` at all — an old browser, a hand-made request | refused the same way: the form's own saves always carry one |
| two saves for one series | the later one is what is stored: a form is a statement of the whole configuration, not a patch |
| a save reloaded by the browser afterwards | nothing is saved twice: the answer to a save is a redirect, not a page |
| a save while the proxy no longer accepts the reader's credentials | the proxy refuses it and nothing reaches the hub ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)) |

### What changes elsewhere {#effects}

| After a save | Result |
|---|---|
| a threshold set on a series that is already past it | the next tick transitions it, and a critical is announced at once ([evaluation.md](evaluation.md#notifications)) |
| a threshold set or cleared | no agent is affected and no `config_version` changes ([hub-config.md](hub-config.md#configuration-version)) |
| a threshold cleared for a subject standing in `critical` | no recovery message: the question was withdrawn, not answered ([evaluation.md](evaluation.md#configuration-changes)) |
| any save | the hub restarts nothing and rereads no file |

## Invariants

- The store never holds what the form would refuse: a direction outside `below`/`above`, a
  value that is not finite, or a `critical` that is not strictly beyond its `warning`.
- A configuration belongs to exactly one series, and a series has at most one.
- Saving changes what the hub judges by and nothing else: no measurement, no level and no
  event is written by a save.
- A page never shows a threshold it did not read from the store, so two readers see the
  same numbers.
- Nothing on this page reaches an agent.

## Edge cases

- **A value of zero** is legal and means what it says; the form states that a zero
  threshold has a margin of zero, so it cannot damp a flapping series
  ([evaluation.md](evaluation.md#hysteresis)).
- **A negative value** is legal too — a balance may be judged below −100 — and its margin
  is 20% of its magnitude, so it clears in the direction the comparison's negation points:
  upwards for `below`, downwards for `above`.
- **A percentage** is a number between 0 and 100 because that is what the series carries;
  nothing enforces the range, since a metric may report more than 100% of something.
- **The two series of one volume** are configured separately: a size on
  `disk.free_bytes` and a proportion on `disk.free_pct` fire whichever comes first, and
  that eagerness is the reader's choice ([0033](../decisions/0033-a-subject-is-a-series.md)).
- **A series that stops reporting** keeps its configuration: the subject freezes rather
  than losing what it was judged by ([evaluation.md](evaluation.md#freezing)).
- **A volume that reappears under different labels** is a different series and starts
  unconfigured; the old configuration stays on the old labels until it is removed.

## Out of scope

- What a level, an event or a message is made of → [evaluation.md](evaluation.md).
- Listing series and linking to this page → [history.md](history.md#page).
- Who may open the page at all → [0023](../decisions/0023-proxy-holds-the-web-perimeter.md)
  and [nginx-requirements.md](../nginx-requirements.md).
- Applying one number to many series at once, and copying a configuration between nodes →
  a convenience for later, not the model ([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)).
- Saying *why* a series has no threshold — a third state between watched and forgotten →
  later. Until then the counts on `/` and in the digest ([state.md](state.md#page),
  [evaluation.md](evaluation.md#digest)) are what keeps a forgotten volume visible.
- Exporting the stored thresholds to a file, or restoring them from one → the database is
  the record, and backing it up is [issue #19](https://github.com/pravbeseda/monitor/issues/19).
- Editing thresholds from a skin other than this page → the store is the contract
  ([0001](../decisions/0001-semantic-core-and-skins.md)); only this page writes it today.

## Open questions

None.
