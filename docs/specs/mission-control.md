# Spec: Mission control

- **Status:** approved
- **Owns:** the page `GET /` in `internal/hub` — mission control, the hub's primary view —
  which items it shows, in what order and with which links; and the address `/debug`, where
  the table of every series now lives. What that table contains stays with
  [state](state.md#page) and [history](history.md#page); what a level, a staleness or an
  anomaly *is* stays with [evaluation](evaluation.md), [state](state.md) and
  [anomaly](anomaly.md); every user-facing string comes from `internal/i18n`; the zone, the
  shell and the refresh come from [web](web.md).
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0002](../decisions/0002-push-not-pull.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md),
  [0035](../decisions/0035-mission-control-is-rendered-by-the-hub.md),
  [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)

## Purpose

Mission control answers the first two questions of the [concept](../concept.md): is
everything fine, and if not, where to look. It is empty while all is well, and what needs
attention surfaces on it by itself, most urgent first. Everything else — every series, its
value and its link to a threshold — is one click away on `/debug`.

It is a skin ([0001](../decisions/0001-semantic-core-and-skins.md)): it reads the state the
[State API](state.md) answers and judges nothing. Rendering it in the hub rather than in a
browser is [0035](../decisions/0035-mission-control-is-rendered-by-the-hub.md).

## Model

Only nodes the configuration names take part: a node it no longer names, and its series,
are left to `/debug`.

**An item** is one thing that needs attention, of four kinds:

| Kind | Taken from the state |
|---|---|
| a silent node | a node whose silence subject is `critical` |
| a level | a series at `warning` or `critical` that is not stale |
| an anomaly | a series with an anomaly rank ([anomaly](anomaly.md#model)) that is not a level item |
| no fresh data | a watched series that is stale on a node some of whose other series are not, and that does not carry `removable: "true"` |

A node whose every series is stale gets no item until its silence subject turns `critical`:
the state cannot tell a laptop asleep from an agent whose sensors all fail, and the first
is the everyday case.

A series is one item even when it qualifies twice: a series at `critical` that also ranks
is shown once, as a level, with its usual value beside it. A node whose series are all stale
is quiet, not broken: a laptop asleep inside its `silence_after` is the expected state of
[0002](../decisions/0002-push-not-pull.md), and once past it the node's silence is the one
item. An unwatched series that stopped arriving is not an item — nothing was asked of it —
and neither is a removable volume that was unplugged, as `/debug` hides one too
([history](history.md#page)).

**A series item names its series** the way a notification does
([evaluation](evaluation.md#messages)): the node, the metric, a mount point as itself and
every other label as the pair it is, `fs` and `removable` left out. It carries the newest
value, and, by kind: a level item its level's word and since when it holds, plus the usual
value when the series also ranks; an anomaly item its usual value — the norm; a no-fresh-data
item the time of its newest value, and no level.

**The order** is by how urgent an item is, not by node:

1. silent nodes and series at `critical`, by node;
2. series at `warning`, by node;
3. anomaly items by rank — the five highest-ranked, and then, when more anomaly items
   remain, one line saying how many more;
4. watched series with no fresh data, by node.

Inside a node, series come in the order `/debug` lists them: a volume's series together
([history](history.md#page)).

**The headline** states how things stand, above the items, in the reader's language. The
first line that applies is the one shown:

| State | Headline |
|---|---|
| no node has reported | no node has reported yet |
| the state's `level` is `critical` or `warning` | that level's word, marked as that level |
| `level` is `null`, or nothing is watched | nothing is judged yet |
| there are items | nothing past a threshold |
| otherwise | all is well |

Below the headline, whenever the state counts nothing watched and lists any node — one the
configuration no longer names included — the notice
`/debug` shows for the same case: nothing here is judged by a threshold, and where to set
one ([state](state.md#page)).

## Behaviour

One row = one test. Anchors: `spec: mission-control.md#<heading>`. Words quoted below are
the English catalogue's; every one of them has its Russian.

### Items {#items}

| State | What the reader sees |
|---|---|
| every watched series `ok`, no anomaly, no node silent | no item |
| `server-b`'s silence subject `critical` | an item naming `server-b` as silent, with the time it was last seen, linking to `/debug` |
| the same node's series, stale because of the silence | no item of their own |
| a laptop asleep inside its `silence_after`, every series stale | no item |
| a node still reporting whose every series is stale — its sensors all failing | no item; `/debug` marks each series |
| a volume at `critical` | an item naming the node, the metric and the mount point, with the newest value, the level's word and since when it holds |
| a series at `critical` that also ranks | one item, the level's, with the usual value beside it |
| a watched series at `ok` that ranks | an anomaly item with the newest value and the usual value, in the unit of the series |
| an unwatched series that ranks, labelled `{queue: payments}` | an anomaly item naming it `queue=payments` |
| a series at `warning` whose value is stale, its node reporting other series | a no-fresh-data item with the time of its newest value and no level word |
| a watched series whose sensor the configuration switched off, on a node still reporting | a no-fresh-data item, for as long as the threshold is kept |
| an unwatched series whose values stopped arriving | no item |
| a watched removable volume, unplugged | no item |
| a watched series whose stored threshold this build cannot read, fresh and not ranking | no item: it has no level ([state](state.md#listing)) |
| a node the configuration no longer names, its series watched and stale | no item for it or its series |
| a level item | a link to the history page of that series, carrying the node, the metric, every label and the language |
| an anomaly item or a no-fresh-data item | a link to that history page and one to the page that sets what the series is judged by ([thresholds](thresholds.md)), where it can be excluded from anomalies or rid of its threshold |

### Order {#order}

| State | Order of the items |
|---|---|
| `server-c` silent, a `warning` on `server-a`, a `critical` on `server-b` | `server-b`'s critical, `server-c`'s silence, then `server-a`'s warning |
| a node silent and one of its series `critical` from before it fell silent | the silence only: the series is stale |
| two volumes of one node at `critical`, both series of each | `/a`'s two series, then `/b`'s two |
| anomalies ranked 1, 2 and 3 on three nodes | in rank order, whatever their nodes |
| five series ranking | all five, and no line after them |
| six series ranking | ranks 1 to 5, then "more unusual series: 1", linking to `/debug` |
| seven series ranking, ranks 1 and 2 at `critical` | ranks 1 and 2 as level items; ranks 3 to 7 as anomaly items; no line |
| a `warning` and an anomaly on the same node | the warning first |
| a series with no fresh data and an anomaly elsewhere | the anomaly first |

### The headline {#headline}

| State | Headline |
|---|---|
| an empty hub | "no node has reported yet" |
| one series `critical` | "critical", marked as critical |
| one series `warning`, nothing critical | "warning", marked as warning |
| nothing watched, one node silent | "critical", and the nothing-judged notice below it |
| nothing watched, every node reporting, one anomaly | "nothing is judged yet", the nothing-judged notice, and the anomaly |
| nothing watched, every node reporting, no item | "nothing is judged yet" and the nothing-judged notice: a hub that judges nothing must not read as well |
| every node configured-out | "nothing is judged yet" |
| the only node reported for the first time, before the next tick | "nothing is judged yet" |
| everything `ok` and one anomaly | "nothing past a threshold", and the anomaly below it |
| everything `ok` and a watched series with no fresh data | "nothing past a threshold", and that item below it |
| everything `ok`, nothing else | "all is well" |

### The page {#page}

| Request | What the reader sees |
|---|---|
| `/` | mission control, and a link to `/debug` reading "all series" |
| `/debug` | the debug view of [state](state.md#page) and [history](history.md#page), and a link to `/` |
| `/?lang=ru`, `/debug?lang=ru` | every word on the page in Russian, the language kept on every link |
| any time on the page | in the reader's zone ([web](web.md#zone)) |
| any value and any usual value | formatted in the unit its metric id declares, as `/debug` formats it |
| a new anomaly, a level change or a node falling silent while the page is open | on the page within 30 seconds, without a reload ([web](web.md#live)) |
| the state cannot be read | the same failure `/debug` answers with |

## Invariants

- Mission control shows nothing the [State API](state.md) would not return at the same
  instant: no level, staleness, value or anomaly of its own.
- A series appears at most once.
- Every item on the page has a reason in the state; with no reason, the page holds none.
- Every series that ranks is on the board or marked on `/debug`, which the line after the
  fifth anomaly links to.

## Edge cases

- **A hub that watches nothing** still ranks anomalies: the notice says nothing is judged
  by a threshold, and the board still shows what is unusual.
- **A fresh hub** has no norms for about two days ([anomaly](anomaly.md#norm-period)), so
  in its first days only levels, silence and stale series reach the board.
- **A sensor switched off in the configuration** leaves its watched series stale for good,
  and a no-fresh-data item with it. The item links to the thresholds page: removing the
  threshold is how the reader says the series is no longer wanted.
- **A laptop's uptime** ranks for about a day after each weekend asleep
  ([anomaly](anomaly.md#edge-cases)) until the reader excludes it, from the item's own link
  to its thresholds page ([thresholds](thresholds.md#saving)).
- **A node that has just fallen silent** has its series stale at once, while its silence
  subject turns `critical` only on the next evaluation tick: for up to a minute the board
  shows neither ([evaluation](evaluation.md#the-tick)).
- **Many anomalies at once** — a hub back from a long outage whose nodes all changed — are
  capped at five on the board; the rest are marked on `/debug`.
- **Both series of one volume** can rank together, a byte count and a percentage moving as
  one; they are two items, side by side, because each is a series of its own
  ([0033](../decisions/0033-a-subject-is-a-series.md)).

## Out of scope

- **Health 0–100, trend and forecast** — not built; the headline is the state's level.
- **Notifying an anomaly** — [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md).
- **Dismissing or acknowledging an item** — the board shows the state; an item leaves when
  its reason does.
- **A count of unwatched series on the board** — every hub has some, so it would never
  leave; `/debug` counts them per node, and anomalies judge them meanwhile.
- **Other skins** — advisors, the city, the organism, the Telegram bot as a skin.

## Open questions

None.
