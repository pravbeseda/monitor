# Spec: State

- **Status:** approved
- **Owns:** `GET /api/v1/state` in `internal/hub` — which subjects exist now, the level last
  recorded for each, whether the values behind it are fresh, and the newest value itself —
  and the levels the index page `/` shows. The level itself is written by
  [evaluation](evaluation.md) alone and the threshold that produced it is set on
  [its own page](thresholds.md); which rows `/` shows, hides or marks, in what order and
  with which links stays with [history](history.md#page), reading freshness from here; every
  user-facing string comes from `internal/i18n`.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0007](../decisions/0007-public-repository.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0015](../decisions/0015-evaluation-on-a-tick.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0030](../decisions/0030-the-state-api-reports-the-stored-verdict.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0034](../decisions/0034-a-series-without-a-sensor-still-ages.md)

## Purpose

The State API answers one question — what is the state of everything right now — and is the
one input every skin renders from ([0001](../decisions/0001-semantic-core-and-skins.md)). It
carries meaning, never presentation: a level, how long it has held, whether the value behind
it is fresh, and the value itself. No colour, size or slot of any skin appears in it.

It judges nothing. The level it reports is the one evaluation last stored
([0015](../decisions/0015-evaluation-on-a-tick.md)); reading the state never evaluates and
never writes. The index page `/` is its first consumer, the debug view: it shows every
subject with its level, and nothing the endpoint would not return.

Health 0–100, trend, anomaly rank and forecasts belong to the semantic engine and are
absent; they arrive as new fields, which a consumer that does not know them ignores.

## Model

**A subject is a series** — `(node, metric, labels)`, the same triple
[history](history.md#model) serves points for ([0033](../decisions/0033-a-subject-is-a-series.md))
— plus one subject per node for its own silence, `(node, silence, {})`. Every series the hub
has stored is listed, whatever produced it and whether or not anything judges it.

**A subject is watched when a threshold is stored for it**
([thresholds.md](thresholds.md)). Only a watched subject can carry a level: an unwatched
series is observed and displayed, never judged
([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)). `watched` says which it
is, so a consumer can tell "nobody set a number" from "set, not yet evaluated" — both of
which report `level: null`. A node's silence subject is always watched: it is judged on its
class's `silence_after` rather than on anything stored
([evaluation](evaluation.md#node-silence)). `configured` on a *node* is a different
statement — that the file still names it — and the two never mean the same thing.

**The response counts what is watched**, per node and in total, because a hub that watches
nothing must not look like a hub where nothing is wrong: that is the fresh installation, and
that is a database restored without its thresholds. The counts are of series — the things
somebody has to configure — so a node's silence, judged without a threshold, counts in
neither.

**Stale** is evaluation's *frozen* ([evaluation](evaluation.md#freezing)), decided by the
same code at the instant of the request: a subject is stale when its newest value is older
than three intervals of the sensor that value names — or, when it names none, three of the
longest interval among the sensors its node runs — when its node is silent past its
`silence_after`, or when its node no longer runs that sensor — resolved `enabled: false`, or
no interval resolved for it, which is where a node the file no longer names ends up too. The
silence subject is never stale. Every subject of a node therefore carries a verdict: `stale`
is a boolean, never absent.

**A level is recorded or absent.** `null` means no level this build can read is held for the
subject: nothing is watched there, its values have not yet been found fresh by a tick, or
the stored level is one this build does not know. A node's level is the most severe level
among its subjects that are not stale — what is wrong now by fresh values — and the
response's level the most severe among its nodes; either is `null` when nothing under it has
a level. A stale subject keeps its own level and says it
is stale; it does not raise its node's.

## Wire format

`application/json; charset=utf-8` with `Cache-Control: no-store`, like
[history](history.md#wire-format): no CORS header, `405` for a method other than `GET` (a
`HEAD` is answered as a `GET` is),
`400` or `500` with `{"error": "..."}` in English.

```json
GET /api/v1/state

200 OK
{
  "at": "2026-09-19T10:00:00.000Z",
  "level": "warning",
  "watched": 2,
  "unwatched": 2,
  "nodes": [
    { "node": "server-b", "configured": true, "agent_version": "v0.9.0",
      "last_seen": "2026-09-19T09:58:00.000Z", "level": "warning",
      "watched": 2, "unwatched": 2 }
  ],
  "subjects": [
    { "node": "server-b", "metric": "silence", "labels": {}, "watched": true,
      "level": "ok", "since": "2026-09-01T08:00:00.000Z", "stale": false,
      "unit": null, "value": null, "ts": null },
    { "node": "server-b", "metric": "disk.free_bytes",
      "labels": { "mount": "/data", "fs": "ext4", "removable": "false" },
      "watched": true, "level": "warning", "since": "2026-09-18T22:10:00.000Z",
      "stale": false, "unit": "bytes", "value": 9000000000,
      "ts": "2026-09-19T09:45:00.000Z" },
    { "node": "server-b", "metric": "disk.free_pct",
      "labels": { "mount": "/data", "fs": "ext4", "removable": "false" },
      "watched": false, "level": null, "since": null, "stale": false,
      "unit": "percent", "value": 12, "ts": "2026-09-19T09:45:00.000Z" },
    { "node": "server-b", "metric": "load.one", "labels": {}, "watched": false,
      "level": null, "since": null, "stale": false, "unit": "number",
      "value": 0.4, "ts": "2026-09-19T09:55:00.000Z" }
  ]
}
```

Every timestamp is RFC 3339 in UTC with milliseconds. `at` is the hub's clock when the
request was handled, the instant staleness is decided at. `unit` is read from the metric id
exactly as [history](history.md#wire-format) reads it. `level` is `ok`, `warning`,
`critical` or `null`, and `since` is `null` exactly when `level` is. A consumer treats a
level or a unit it does not know as opaque. Lists and label maps are never `null`, only
empty. The silence subject carries no value of its own — `unit`, `value` and `ts` are
`null` — because its input is the node's last-seen time, which `nodes` already states.
`watched` and `unwatched` count that node's series, and at the top level every node's; a
silence subject is watched and counted in neither. `node` names a node today; a subject that belongs to
none, which manual input will bring, will carry `node: null` and have no entry in `nodes`
— what ages such a subject is a question for whoever adds it, since every bound here is
read from a node's configuration.

## Behaviour

One row = one test. Anchors: `spec: state.md#<heading>`.

### The endpoint {#endpoint}

| Request | Response |
|---|---|
| `GET /api/v1/state` on a hub no node has reported to | 200, `level: null`, `nodes` and `subjects` empty lists |
| `GET /api/v1/state?at=2026-09-01T00:00:00Z` | 400: time travel is a later stage, and refusing it keeps a caller from reading the present as the past |
| `?at=` empty, or any other query parameter | 400 |
| `POST /api/v1/state` | 405 |
| the stored state cannot be read | 500 |
| two requests with nothing reported or evaluated between them, and no staleness verdict changing | bodies equal but for `at` |

### Listing {#listing}

| Situation | Where it appears |
|---|---|
| a configured node that has reported | in `nodes` with `configured: true`, and its silence subject in `subjects` |
| a configured node that has never reported | nowhere: an uninstalled agent is not a node yet |
| a node the configuration no longer names | in `nodes` with `configured: false` and `level: null`, which says the file dropped it, not that anything is fresh; its series listed, `stale: true`, since nothing will refresh them, and no silence subject: silence is judged against a window the file no longer gives |
| a volume both of whose series are stored | two subjects, one per series, each with its own newest value |
| a series with a threshold stored for it | `watched: true` |
| a series with no threshold | `watched: false`, `level: null`, `since: null` |
| a node's silence subject | `watched: true` always: it is judged without a stored threshold, and counted in neither `watched` nor `unwatched` |
| a series whose threshold was removed while it held a level | `watched: false`, `level: null`: the level was forgotten with the threshold ([evaluation](evaluation.md#configuration-changes)) |
| a series whose stored threshold this build cannot read | `watched: true`, `level: null`: something is set, and nothing is judged by it |
| a hub where nothing is watched | `watched: 0` at the top level and on every node |
| a sensor the node resolves as `enabled: false` | its series listed, `stale: true`, keeping whatever level evaluation stored before |
| a series with no labels | `labels: {}` |
| `last_seen` of a node | when the hub last accepted a request from it, not any time an agent stamped |
| `agent_version` of a node that never reported one | `null` |

### Levels {#levels}

| Situation | What the state says |
|---|---|
| a subject evaluation stored as `warning` since 22:10 | `level: warning`, `since` 22:10 |
| a value that crossed a threshold after the last tick | the level already stored |
| the same, after the next tick | the level and `since` that tick stored |
| a subject given a threshold after the last tick | listed, `watched: true`, `level: null`, `since: null` |
| a node whose only `critical` subject names no sensor, its newest value not yet past three of the node's longest interval | node `level: critical`: the subject is not stale, and its level counts |
| a subject stale since it first appeared | `level: null` for as long as it stays stale |
| a subject whose stored level this build does not know | `level: null`, `since: null` |
| a node whose watched subjects are `ok` and whose silence subject is `critical` | node `level: critical` |
| a node with one `warning` subject and the rest `ok` | node `level: warning` |
| a node none of whose subjects has a level | node `level: null` |
| a node whose only `critical` subject is stale — an unplugged removable volume, or a series whose sensor stopped reporting | node `level` the most severe of its other subjects; that subject still `level: critical`, `stale: true` |
| a node silent past its `silence_after` whose subjects were `warning` | node `level: critical`, from its silence subject alone |
| one node `critical`, another `ok` | response `level: critical` |

### Staleness {#staleness}

| Situation | What the state says |
|---|---|
| a series whose newest value is exactly three intervals old | `stale: false`: the bound is inclusive |
| the same a moment later | `stale: true`, `level` and `since` still the ones stored |
| a node silent past its `silence_after` | each of its subjects `stale: true` but its silence subject, which is `stale: false` |
| a series whose newest value names no sensor, exactly three of its node's longest sensor interval old | `stale: false`: the bound is inclusive here too, and nothing else says when the value was due |
| the same series a moment later | `stale: true`, so a series no agent will ever name again still ages off the page |
| a series whose newest value names no sensor, on a node that runs no sensor at all | `stale: true`: nothing will refresh it |
| a series whose node resolves no interval for its sensor | `stale: true`: nothing will refresh it |
| a stale volume with `removable: "true"` | listed, `stale: true`; hiding it is `/`'s rule ([history](history.md#page)) |
| values stamped an hour ahead of the hub's clock | `stale: false` |

### Ordering {#ordering}

Names and labels are compared byte by byte; labels are rendered as history renders them
([selection](history.md#selection)).

| List | Order |
|---|---|
| `nodes` | by node |
| `subjects` | by node, then metric, then labels; a node's silence subject comes first. No label is privileged: the core does not know `mount` is about disks ([0033](../decisions/0033-a-subject-is-a-series.md)), and grouping a volume's two rows together is `/`'s own rendering ([history](history.md#page)) |

### The debug view {#page}

| Request | What the reader sees |
|---|---|
| `/` | beside every series row, its level in the reader's language |
| a hub where nothing is watched | a line above the tables, in the reader's language, saying that nothing here is being judged and where to set a threshold |
| a node some of whose series are unwatched | how many, beside the node's name |
| series at `ok`, `warning` and `critical` | each of the three marked differently from the other two |
| a series with no level | a dash in place of the level |
| a node whose level is `warning` or `critical` | that level beside the node's name, marked as a row at that level is |
| a node whose level is `ok` or `null` | nothing beside its name |
| a node whose silence subject is `critical` | the node marked silent in the reader's language, beside its last-seen time |
| a stale series still shown | its level beside the "no fresh data" mark |
| `/?lang=ru` | every level word and the silent mark in Russian |

## Invariants

- Every `level` and `since` equal the ones evaluation last stored for that subject, or are
  `null` when no stored level is readable.
- Only a `watched` subject ever carries a level.
- `watched` and `unwatched` sum to the series listed, per node and in total; a node's
  silence is in neither.
- A node's `level` is the most severe `level` of its subjects that are not stale in the same
  response, and the response's `level` the most severe of its nodes'.
- A subject is stale exactly when evaluation's own freezing code says so at `at`, and every
  subject of a node carries that verdict: `stale` is never `null`.
- `/` shows no level, subject or staleness the endpoint would not return at the same instant.
- Reading the state writes nothing.

## Edge cases

- **A restart** changes nothing: levels are read back from storage, so with the same
  configuration and at the same instant the state after a restart is the state before it.
- **A request during a tick** can see some subjects at the level this tick stored and others
  at the previous one: evaluation stores subject by subject. Every level shown is one a tick
  stored; the next request after the tick is consistent.
- **A request between a save and the next tick** shows the subject already `watched` and
  still without a level: a threshold is stored the moment it is saved, and judged on the
  next tick ([thresholds.md](thresholds.md#effects)).
- **An agent clock running behind** makes a new subject stale from its first appearance, so
  it stays `null` until its values are fresh — the same subject evaluation never judges.

## Out of scope

- **`at=` time travel** — reconstructing a past state from the event log and the stored
  points is its own design; the parameter is refused until then.
- **The event stream** (SSE or WebSocket) that [0001](../decisions/0001-semantic-core-and-skins.md)
  names as a shared service, and the stable subject identifier it will need.
- **Health, trend, anomaly rank, forecasts, a numeric severity** — the semantic engine.
- **The threshold itself on the wire** — what a subject is judged by is read and written on
  [its own page](thresholds.md); the State API reports the verdict, not the rule behind it
  ([0030](../decisions/0030-the-state-api-reports-the-stored-verdict.md)).
- **`domain` on a subject** — metrics declare no domain yet; it arrives with the first
  metric outside infra.
- **A "detached" verdict for an unplugged removable volume** — listed as stale today; a
  semantic field for it is additive.
- **Authentication** — the proxy holds it for every path under `/api/v1/` that is not ingest
  or `/api/v1/agent/` ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)).

## Open questions

None.
