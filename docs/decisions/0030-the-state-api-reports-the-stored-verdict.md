# 0030. The State API reports the verdict evaluation stored

- **Status:** accepted; amended by [0033](0033-a-subject-is-a-series.md) — `subjects` and
  `readings` became one list, because every series is now a subject; never evaluating on
  read, staleness decided at read, the node rollup and the refusal of `at=` stand; amended by
  [0036](0036-an-anomaly-is-a-value-outside-its-weeks-band.md) — each series' anomaly is
  computed on read, and is no level
- **Date:** 2026-09-19
- **Source:** [state spec](../specs/state.md), [design notes](../log/2026-09-19-state-api.md)

## Context

[0001](0001-semantic-core-and-skins.md) makes one State API the input of every skin, and
[0015](0015-evaluation-on-a-tick.md) makes the evaluation tick the only writer of levels.
A read endpoint could either report what the tick stored or judge the values again when it is
asked. Skins will be written against whatever shape ships first, so the answer and the shape
are hard to change later.

## Decision

- `GET /api/v1/state` reports the level and `since` evaluation last stored for each subject.
  It never re-evaluates. `null` means there is no stored level this build can read.
- Staleness is decided at request time, by evaluation's own freezing code.
- A node's level is the most severe level among its subjects that are not stale. The
  response's level is the most severe among its nodes.
- The contract has two flat lists. `subjects` holds what has a level. `readings` holds the
  newest value of every series no subject reads.
- `at=` is refused until time travel is designed. Later fields are only ever added.

## Consequences

- A level lags the values by at most one tick, and the page never disagrees with the alerts
  or the event log.
- A request during a tick can mix subjects from two ticks, because evaluation stores
  subject by subject.
- The index page `/` is a consumer of the same model, so it cannot show a level or a
  freshness the endpoint would not.
- Health, trend and anomaly fields, a `domain`, and subjects with no node arrive as
  additions to the same lists.

## Alternatives

- **Recompute on each request.** Rejected. It creates a second judge. Without the stored
  previous level it loses hysteresis. And it can show a level no alert was sent for.
- **Count stale subjects in a node's level.** Rejected. An unplugged removable drive would
  keep its node red until the drive returns. It would also disagree with the digest, which
  leaves frozen subjects out.
- **Hide stale removable volumes in the core.** Rejected. It would stop any skin from ever
  showing a detached backup drive. Hiding stays a rule of the `/` page.
- **One list, with readings as subjects that have `rule: null`.** Rejected. It would give
  `level: null` a fourth meaning, and `series` already means something else in
  `/api/v1/history`.
