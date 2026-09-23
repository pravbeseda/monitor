# Spec: History

- **Status:** approved
- **Owns:** `internal/history` (series: selection, window, reduction, gaps) and its consumers
  in `internal/hub` — `GET /api/v1/series`, `GET /api/v1/history` and the drill-down page
  `GET /history`, plus the link the debug view `/debug` grows to reach it and which series `/debug`
  hides or marks as no longer arriving, by the staleness [state](state.md#staleness) reports.
  Reading stored points stays with `internal/storage`; the expected interval of a series
  comes from the resolved configuration `internal/config` already computes; every
  user-facing string comes from `internal/i18n`.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0005](../decisions/0005-poc-stack.md),
  [0007](../decisions/0007-public-repository.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0026](../decisions/0026-reader-time-zone-from-the-browser.md),
  [0034](../decisions/0034-a-series-without-a-sensor-still-ages.md)

## Purpose

History answers one question — what did this metric do over the last window — and answers
it as data. `/api/v1/series` says what exists, `/api/v1/history` returns the points, and the
drill-down page draws one series. Every renderer reads the same series
([0018](../decisions/0018-history-through-the-api.md)), so a chart cannot show something the
endpoint would not.

It does not decide meaning: no level, no threshold and no anomaly appears here, those stay
with [evaluation](evaluation.md) and [anomaly](anomaly.md). Nothing here judges a value good or bad, and the reduction
below is deliberately direction-free for that reason. Reading history never writes.

## Model

**A series is `(node, metric, labels)`** — `server-b`, `disk.free_pct`,
`{mount: /, fs: ext4, removable: false}` — carrying the points stored for it inside the
window, oldest first. An [evaluation](evaluation.md) subject is the same triple: a series
is what carries a level, so a volume contributes two of them, `disk.free_bytes` and
`disk.free_pct` ([0033](../decisions/0033-a-subject-is-a-series.md)).

**A series carries its expected interval** — the one the node resolves for the sensor the
series names ([ingest](ingest.md#wire-format)), the same input [evaluation](evaluation.md)
ages a subject against. It is what makes a gap in the series distinguishable from a series
that is simply sparse, and it is stated rather than guessed so that every renderer breaks a
line in the same place. A series that names no sensor, or whose node resolves no interval
for it, has no interval and no gaps.

**A query selects series and a window.** `metric` is required; `node` and label filters
narrow the selection by exact equality, and what is not named is not constrained, so a query
can select many series. A filter cannot demand that a series carry *no other* label, so a
link meant to reach exactly one series names every label that series has.

**The window ends now** — the instant the request is handled, on the hub's clock — and
reaches back by a duration written as a whole number and a unit, `^[0-9]+[mhd]$`, resolving
to between one minute and 365 days inclusive. A written `from`/`to` range and `at=` time
travel belong to a later stage and are deliberately absent. When the newest selected point
is stamped after now, that point is the end of the window instead: an agent whose clock runs
ahead produces a value [evaluation](evaluation.md) judges and `/debug` displays, and the chart
shows what they acted on. That reach is bounded by the window's own length, because the
window is shared by every series in one answer: a single node stamped a century out would
otherwise carry every other series off the chart, permanently.

## Wire format

Both endpoints answer `application/json; charset=utf-8` with `Cache-Control: no-store`; a
window ending now makes every response unique. No CORS header is sent: a browser skin reads
the hub through the same origin. A method other than `GET` answers `405`. A refused query
answers `400` with `{"error": "..."}` in English, the way [ingest](ingest.md) refuses one; a
failed read answers `500` the same way.

```json
GET /api/v1/series?metric=disk.free_pct

200 OK
{
  "series": [
    { "node": "server-b", "metric": "disk.free_pct",
      "labels": { "mount": "/", "fs": "ext4", "removable": "false" },
      "unit": "percent", "interval": "15m" }
  ]
}
```

```json
GET /api/v1/history?metric=disk.free_pct&node=server-b&label.mount=/&window=7d

200 OK
{
  "window": { "from": "2026-08-24T09:00:00.000Z", "to": "2026-08-31T09:00:00.000Z" },
  "series": [
    {
      "node": "server-b", "metric": "disk.free_pct",
      "labels": { "mount": "/", "fs": "ext4", "removable": "false" },
      "unit": "percent", "interval": "15m",
      "reduced": false, "stored": 672,
      "points": [
        { "ts": "2026-08-24T09:03:00.000Z", "value": 34.2 },
        { "ts": "2026-08-24T09:18:00.000Z", "value": 34.1 }
      ]
    }
  ]
}
```

Every timestamp is RFC 3339 in UTC with milliseconds, the resolution measurements are stored
at ([ingest](ingest.md)). `unit` is read from the metric id — `_bytes` is `bytes`, `_pct` is
`percent`, `_seconds` is `duration`, anything else is `number`, meaning the id declares no
unit — and the set is open:
a consumer treats a unit it does not know as opaque rather than as a number. `interval` is
empty when the series names no sensor, or its node resolves no interval for it. `stored` is how many points the window actually
holds, so a consumer can see what a reduction cost it.

## Behaviour

### Selection {#selection}

| Query | Response |
|---|---|
| `metric=disk.free_pct` | every series of that metric holding points in the window, ordered by node, then by labels |
| `metric=disk.free_pct&node=server-b` | only that node's series |
| `metric=disk.free_pct&label.mount=/` | only series whose `mount` label is exactly `/` |
| `metric=disk.free_pct&label.mount=/&label.fs=ext4` | only series matching both labels |
| `metric=disk.free_pct&label.mount=/nowhere` | 200, no series |
| `metric=nothing.reported` | 200, no series |
| a series whose every stored point is outside the window | not returned |
| a selection matching more than 50 series | 400 |

Series are ordered by node name, then by their labels rendered as `key=value` pairs sorted
by key and joined with `,`, compared byte by byte. Points come oldest first.

`GET /api/v1/series` takes the same selection, refuses the same queries, and answers with
the same series objects carrying no window and no points — including series whose last point
is older than any window a caller might ask for. It takes no `window` of its own: one given
is refused rather than ignored, for the reason the refusals below give. It is how a consumer
learns which nodes, metrics and label sets exist without being configured with them.

### Refused queries {#refusals}

| Query | Response |
|---|---|
| no `metric`, or `metric=` | 400 |
| `metric` not matching `[a-z0-9_.]+` | 400 |
| `node=` | 400 |
| `label.mount=` or `label.=/` | 400 |
| the same parameter given twice | 400 |
| `window` given to `/api/v1/series` | 400 |
| a parameter outside `metric`, `node`, `window`, `label.*` (`lang` too, on the page) | 400 |
| `window=0h`, `window=-1d`, `window=soon`, `window=2w`, `window=1h30m`, `window=90s`, `window=24H` | 400 |
| `window=366d`, and a count so large it would overflow a duration | 400 |
| the stored points cannot be read | 500 |

Refusing an unknown parameter is what keeps `?windwo=7d` from silently answering with a
different window than the caller asked for, and what makes a future `at=` a refusal rather
than a lie.

### The window {#window}

| Query | Window |
|---|---|
| no `window` | the 24 hours ending now |
| `window=7d` | the 7 days ending now |
| `window=30m` | the 30 minutes ending now |
| `window=1m`, `window=365d` | accepted; the bounds are inclusive |
| a point stamped exactly at `from` or at `to` | inside the window, returned |
| a point stamped after now, by no more than the window is long | the window ends at that point instead, and it is returned |
| a point stamped further ahead than that | the window still ends now, and the point is outside it |

### Reduction {#reduction}

| Points stored in the window | Points returned |
|---|---|
| 1000 or fewer | all of them, as stored; `reduced` is false |
| more than 1000 | the window is cut into 500 equal buckets, `[start, end)` and the last closed at `to`; each bucket that holds points contributes its lowest and its highest point, in timestamp order, and once when they are the same point; `reduced` is true |
| more than 1000, with a bucket holding no points | that bucket contributes nothing; no value is invented for it |
| more than 1000 | the newest point of the window is returned whatever bucketing does, so the chart's right edge and `/debug` show the same value |

Both extremes are kept because which of them matters is a property of the metric, and this
subsystem decides no such thing: a rule that kept the minimum would hide the peak of the
first metric whose high values are the bad ones. Values that tie resolve to the earliest
timestamp. A reduced series therefore holds at most 1001 points — two per bucket, plus the
newest when bucketing would not have kept it. The limit is fixed; a parameter letting a
caller ask for fewer or more can be added later without breaking a caller that never sent
one.

### Gaps {#gaps}

| Series | What a renderer draws |
|---|---|
| consecutive points no further apart than three times the interval | one continuous line |
| consecutive points further apart than that | the line is broken between them |
| a series with no interval — it names no sensor, or its node resolves none for it | one continuous line, never broken |

Three times the interval is the same age at which [evaluation](evaluation.md) calls a
subject's values stale; one definition of "this node was not reporting", not two. A series
with no interval is the one place the two questions part: a gap is a collection that was
due and did not arrive, and nothing here says when a point of such a series was due, while
staleness still has an answer — the node's longest sensor interval
([state](state.md#staleness)).

### The page {#page}

| Request | What the reader sees |
|---|---|
| `/history?node=server-b&metric=disk.free_pct&label.mount=/` | an SVG chart of that series over 24 hours, a time axis, and a value axis running from zero to above the highest value in the window |
| any chart | its node, metric and volume above it, and the newest value in the window with the time it was collected |
| the window the page is showing | marked among the links rather than offered as one, whatever the query spelled it as; a query naming no window is showing the first |
| the same with `&window=7d` | the same chart over 7 days |
| any of the above | window links 24h / 7d / 30d, each keeping node, metric, labels and language |
| a query selecting more than one series | no chart; one link per selected series, each carrying that series' full label set |
| a query selecting no series | a "no data for this window" message and no chart |
| a series with one point in the window | that point drawn on its own, no line |
| a series with a two-day silence inside a seven-day window | the line broken across the gap, not drawn straight through it |
| a query the endpoint refuses, or a read that fails | the same status the endpoint answers, as a translated page |
| a value on `/debug` | a link to the history page of its series, carrying the node, the metric and every label |
| any series on `/debug` | one row of its own: its metric id, the volume its labels name if they name one, its newest value and the time it was collected |
| a volume | two rows, one per series, since each is judged on its own ([0033](../decisions/0033-a-subject-is-a-series.md)) |
| any row | a link to the page that sets what that series is judged by ([thresholds.md](thresholds.md)) |
| the series of one volume collected at different times | each row its own time, and each left out or marked by its own age: no series is aged by another series' newest point |
| the rows of one node | grouped so that the series of one volume sit together, and ordered by metric inside the group; the grouping is the page's own, since the [State API](state.md#ordering) privileges no label |
| a row on `/debug` that the [state](state.md#staleness) calls stale when the page is read — its newest point older than three times the interval its series is aged by, or its node silent past its `silence_after` — and that carries `removable: "true"` | not shown: an unplugged drive or an ejected disk image is not a reading |
| the same, without `removable: "true"` | shown, its collected time marked, in the reader's language, as holding no fresh data |
| a row on `/debug` exactly three intervals old | shown unmarked: the bound is inclusive, as for gaps |
| that row reporting again | shown as before, unmarked |
| a series whose newest value names no sensor | aged by the longest interval among the sensors its node runs ([state](state.md#staleness)): marked, or hidden if it is removable, once past that bound |
| the same series once its node is silent past its `silence_after` | marked with the rest of that node's series: silence is the node's, not the series' |
| a series whose node runs its sensor no longer — resolved `enabled: false`, or a node the file no longer names | marked as holding no fresh data, or hidden if it is removable: nothing will refresh it |
| a node silent past its `silence_after`, its series not yet three intervals old | its series hidden or marked already: evaluation freezes a silent node's subjects in the tick it falls silent |
| a node whose every series is left out | "no current measurements" in place of its table, rather than the "no measurements yet" of a node that never sent one |
| a node still reporting but sending no measurements — its mount table unreadable | its series age like any other and are hidden or marked once past the bound |
| any chart page | a link to `/debug`, the table of every series |
| `&lang=ru` | axis labels, dates, byte sizes and percentages in Russian |
| any time on the page, and every axis label | the reader's time zone, named once on the chart ([web.md](web.md#zone)); which day a tick is labelled with follows that zone, where the ticks sit does not |

The value axis starts at zero: a free-space chart scaled to its own minimum turns a quiet
week into a cliff. Each axis carries at most six labelled ticks, spaced so the window's own
unit reads naturally.

## Invariants

- No value is invented. Every value returned equals a stored measurement, and every
  timestamp returned is the timestamp that measurement was stored under, reduced or not.
- The newest stored point of a series inside the window is always returned.
- Points come oldest first and no two share a timestamp.
- Each series says whether its own points were reduced; a reduced series is never presented
  as raw, and `stored` always states how many points the window held.
- Two identical queries a moment apart differ only by the window sliding.
- A read holds what it answers, not what its window holds: each series returns at most 1001
  points, and a window holding millions of them is answered in the memory those take.
- The page shows nothing the endpoint would not return for the same query, and breaks a line
  only where the interval the endpoint reports says there is a gap.

## Edge cases

- **A node that has never reported** selects no series; that is a 200 with an empty list, not
  an error. Nothing matched is an answer.
- **A volume that disappeared** — an unplugged removable disk — keeps its stored points and
  keeps being returned while they are inside the window. Only `/debug` leaves it out
  ([the page](#page)); the endpoints and the drill-down still reach it.
- **A row is hidden or marked by [evaluation](evaluation.md#freezing)'s own freezing rule** —
  node silence, or a value older than three intervals, on the hub's clock — applied by the
  same code, so `/debug` and evaluation cannot disagree about which series are fresh. A laptop
  asleep overnight therefore shows its internal volume marked and its external drive gone
  until it reports again. An agent clock running behind by more than the bound hides and
  marks what evaluation freezes, and points stamped ahead by a clock since corrected stay
  shown until real time passes them. `/debug` reads that verdict from the
  [State API](state.md#staleness) rather than applying the rule itself. Each series ages on
  its own, so one row of a volume can be marked while the other is not.
- **A removable volume plugged back under another mount point** is a new series; the old one
  stays hidden.
- **A sensor interval lowered while the agent still holds the old one** can hide a removable
  volume that is still plugged in, until the agent picks up the new interval — the same
  window in which [evaluation](evaluation.md#freezing) may freeze it.
- **A series that names no sensor** — pushed by something other than an agent, or by an
  agent too old to name it — is stored by [ingest](ingest.md) and served here with no
  interval, so its line is never broken, while its freshness is judged by its node's
  longest sensor interval ([evaluation](evaluation.md#freezing)). A pusher on its own
  cadence is therefore aged by a bound that is not its own, and naming a sensor in the
  measurement is how it gets one. Its unit still comes from its id, which is where the unit
  lives until metrics are declared.
- **A label filter naming a label no series carries** matches nothing rather than being
  ignored.
- **A node whose interval was changed** ages by the interval it resolves now, so a line drawn
  today may break where yesterday's did not. Evaluation has the same property.

## Out of scope

- **Authentication and rate limiting.** These endpoints publish every measurement the hub
  holds, so they must not be exposed before the stage 3 authentication item
  ([poc.md](../poc.md)), and that item has to issue a credential a program can send —
  a page session alone would lock out the very consumers
  [0018](../decisions/0018-history-through-the-api.md) exists to enable. Ingest's per-node
  limiter has no counterpart here yet.
- **Thresholds drawn on the chart** — a series' own warning and critical values
  ([thresholds.md](thresholds.md)) are meaning, and drawing them as lines on its chart is a
  later step.
- **Events on the chart.** An event and a series now key alike — both on
  `(node, metric, labels)` ([0033](../decisions/0033-a-subject-is-a-series.md)) — so the
  overlay is a design question of what to draw, not of how to join it.
- **`at=` time travel, arbitrary `from`/`to` ranges and several metrics in one query** —
  later extensions of the same endpoints ([0001](../decisions/0001-semantic-core-and-skins.md)).
- **A metric whose values fall below zero.** The value axis starts at zero, so such a series
  would be drawn outside the chart. Nothing the hub collects can be negative; the first
  metric that can needs a row here of its own.
- **Downsampling into storage** — retention keeps every raw point ([poc.md](../poc.md));
  reduction happens on read and is never written back.

## Open questions

None.
