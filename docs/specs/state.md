# Spec: State

- **Status:** approved
- **Owns:** `GET /api/v1/state` in `internal/hub` — which subjects and readings exist now,
  the level last recorded for each, and whether the values behind it are fresh — and the
  levels the index page `/` shows. The level itself is written by
  [evaluation](evaluation.md) alone; which rows `/` shows, hides or marks, in what order and
  with which links stays with [history](history.md#page), reading freshness from here; every
  user-facing string comes from `internal/i18n`.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0007](../decisions/0007-public-repository.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0015](../decisions/0015-evaluation-on-a-tick.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0030](../decisions/0030-the-state-api-reports-the-stored-verdict.md)

## Purpose

The State API answers one question — what is the state of everything right now — and is the
one input every skin renders from ([0001](../decisions/0001-semantic-core-and-skins.md)). It
carries meaning, never presentation: a level, how long it has held, whether the values behind
it are fresh, and the values themselves. No colour, size or slot of any skin appears in it.

It judges nothing. The level it reports is the one evaluation last stored
([0015](../decisions/0015-evaluation-on-a-tick.md)); reading the state never evaluates and
never writes. The index page `/` is its first consumer, the debug view: it shows every
subject with its level, and nothing the endpoint would not return.

Health 0–100, trend, anomaly rank and forecasts belong to the semantic engine and are
absent; they arrive as new fields, which a consumer that does not know them ignores.

## Model

**A subject** is what has a level, as [evaluation](evaluation.md#model) defines it:
`(node, rule, labels)`; a node's own silence is the subject `(node, silence, {})`. A subject
is listed whenever a tick run now would build it, frozen or not: a configured node that has
reported, a rule whose sensor that node runs and whose thresholds resolve for the volume, a
complete join of the rule's series.

**A reading** is the newest value of a series no listed subject reads: a metric no rule
declares, one half of a join whose other half has not arrived, a sensor the node does not
run, a node the configuration no longer names. It has no level.

**Stale** is evaluation's *frozen* ([evaluation](evaluation.md#freezing)), decided by the
same code at the instant of the request: a subject is stale when its older series is older
than three intervals of its sensor, or its node is silent past its `silence_after`. The
silence subject is never stale. A reading is stale by the same rule when a rule reads its
metric and its node resolves an interval for that rule's sensor; otherwise no freshness rule
applies to it and `stale` is `null`.

**A level is recorded or absent.** `null` means evaluation holds no level for the subject
that this build can read: its values have not yet been found fresh by a tick, or the stored
level is one this build does not know. A node's level is the most severe level among its
subjects that are not stale — what is wrong now by fresh values, which is also what the
digest reports — and the response's level the most severe among its nodes; either is `null`
when nothing under it has a level. A stale subject keeps its own level and says it is stale;
it does not raise its node's.

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
  "nodes": [
    { "node": "server-b", "configured": true, "agent_version": "v0.9.0",
      "last_seen": "2026-09-19T09:58:00.000Z", "level": "warning" }
  ],
  "subjects": [
    { "node": "server-b", "rule": "silence", "labels": {},
      "level": "ok", "since": "2026-09-01T08:00:00.000Z", "stale": false, "values": [] },
    { "node": "server-b", "rule": "disk",
      "labels": { "mount": "/data", "fs": "ext4", "removable": "false" },
      "level": "warning", "since": "2026-09-18T22:10:00.000Z", "stale": false,
      "values": [
        { "metric": "disk.free_bytes", "unit": "bytes", "value": 12000000000,
          "ts": "2026-09-19T09:45:00.000Z" },
        { "metric": "disk.free_pct", "unit": "percent", "value": 12,
          "ts": "2026-09-19T09:45:00.000Z" }
      ] }
  ],
  "readings": [
    { "node": "server-b", "metric": "load.one", "labels": {}, "unit": "number",
      "value": 0.4, "ts": "2026-09-19T09:55:00.000Z", "stale": null }
  ]
}
```

Every timestamp is RFC 3339 in UTC with milliseconds. `at` is the hub's clock when the
request was handled, the instant staleness is decided at. `unit` is read from the metric id
exactly as [history](history.md#wire-format) reads it. `level` is `ok`, `warning`,
`critical` or `null`, and `since` is `null` exactly when `level` is. A consumer treats a
level or a unit it does not know as opaque. Lists and label maps are never `null`, only
empty. `node` names a node today; a subject that belongs to none, which manual input will
bring, will carry `node: null` and have no entry in `nodes`.

## Behaviour

One row = one test. Anchors: `spec: state.md#<heading>`.

### The endpoint {#endpoint}

| Request | Response |
|---|---|
| `GET /api/v1/state` on a hub no node has reported to | 200, `level: null`, `nodes`, `subjects` and `readings` empty lists |
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
| a node the configuration no longer names | in `nodes` with `configured: false` and `level: null`; every value of it in `readings`, `stale: null` |
| a volume both of whose series are stored | one subject, `values` holding the newest value of each series, ordered by metric |
| a volume only one of whose series is stored | no subject; that series in `readings` |
| a metric no rule declares | in `readings`, `stale: null` |
| a sensor the node resolves as `enabled: false` | its series in `readings`, `stale: null`, whatever level evaluation stored before |
| a series with no labels | `labels: {}` |
| `last_seen` of a node | when the hub last accepted a request from it, not any time an agent stamped |
| `agent_version` of a node that never reported one | `null` |

### Levels {#levels}

| Situation | What the state says |
|---|---|
| a subject evaluation stored as `warning` since 22:10 | `level: warning`, `since` 22:10 |
| a value that crossed a threshold after the last tick | the level already stored |
| the same, after the next tick | the level and `since` that tick stored |
| a subject that first became complete after the last tick | listed, `level: null`, `since: null` |
| a subject stale since it first appeared | `level: null` for as long as it stays stale |
| a subject whose stored level this build does not know | `level: null`, `since: null` |
| a node whose volumes are `ok` and whose silence subject is `critical` | node `level: critical` |
| a node with one `warning` volume and the rest `ok` | node `level: warning` |
| a node none of whose subjects has a level | node `level: null` |
| a node whose only `critical` subject is stale — an unplugged removable volume, or a volume whose sensor stopped reporting | node `level` the most severe of its other subjects; that subject still `level: critical`, `stale: true` |
| a node silent past its `silence_after` whose volumes were `warning` | node `level: critical`, from its silence subject alone |
| one node `critical`, another `ok` | response `level: critical` |

### Staleness {#staleness}

| Situation | What the state says |
|---|---|
| a volume whose older series is exactly three intervals old | `stale: false`: the bound is inclusive |
| the same a moment later | `stale: true`, `level` and `since` still the ones stored |
| a node silent past its `silence_after` | each of its subjects `stale: true` but its silence subject, which is `stale: false` |
| a half-join reading on that node | `stale: true` |
| a stale volume with `removable: "true"` | listed, `stale: true`; hiding it is `/`'s rule ([history](history.md#page)) |
| values stamped an hour ahead of the hub's clock | `stale: false` |

### Ordering {#ordering}

Names and labels are compared byte by byte; labels are rendered as history renders them
([selection](history.md#selection)).

| List | Order |
|---|---|
| `nodes` | by node |
| `subjects` | by node, then `mount` label, then rule, then labels; a node's silence subject, which has no mount, comes first |
| `readings` | by node, then metric, then labels |

### The debug view {#page}

| Request | What the reader sees |
|---|---|
| `/` | beside every volume row, its level in the reader's language |
| volumes at `ok`, `warning` and `critical` | each of the three marked differently from the other two |
| a volume with no level | a dash in place of the level |
| a row of a reading | a dash in place of the level |
| a node whose level is `warning` or `critical` | that level beside the node's name, marked as a row at that level is |
| a node whose level is `ok` or `null` | nothing beside its name |
| a node whose silence subject is `critical` | the node marked silent in the reader's language, beside its last-seen time |
| a stale volume still shown | its level beside the "no fresh data" mark |
| `/?lang=ru` | every level word and the silent mark in Russian |

## Invariants

- Every `level` and `since` equal the ones evaluation last stored for that subject, or are
  `null` when no stored level is readable.
- A node's `level` is the most severe `level` of its subjects that are not stale in the same
  response, and the response's `level` the most severe of its nodes'.
- A subject or reading is stale exactly when evaluation's own freezing code says so at `at`.
- `/` shows no level, subject or staleness the endpoint would not return at the same instant.
- Reading the state writes nothing.

## Edge cases

- **A restart** changes nothing: levels are read back from storage, so with the same
  configuration and at the same instant the state after a restart is the state before it.
- **A request during a tick** can see some subjects at the level this tick stored and others
  at the previous one: evaluation stores subject by subject. Every level shown is one a tick
  stored; the next request after the tick is consistent.
- **An agent clock running behind** makes a new subject stale from its first appearance, so
  it stays `null` until its values are fresh — the same subject evaluation never judges.

## Out of scope

- **`at=` time travel** — reconstructing a past state from the event log and the stored
  points is its own design; the parameter is refused until then.
- **The event stream** (SSE or WebSocket) that [0001](../decisions/0001-semantic-core-and-skins.md)
  names as a shared service, and the stable subject identifier it will need.
- **Health, trend, anomaly rank, forecasts, a numeric severity** — the semantic engine.
- **`domain` on a subject** — metrics declare no domain yet; it arrives with the first
  metric outside infra.
- **A "detached" verdict for an unplugged removable volume** — listed as stale today; a
  semantic field for it is additive.
- **Authentication** — the proxy holds it for every path under `/api/v1/` that is not ingest
  or `/api/v1/agent/` ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)).

## Open questions

None.
