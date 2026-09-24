# 0038. A timeline lane is summarised on read from the event log and the stored points

- **Status:** accepted
- **Date:** 2026-09-24
- **Source:** [timeline spec](../specs/timeline.md#model),
  [design notes](../log/2026-09-24-timeline.md)

## Context

The timeline shows each node's last 24 hours as hourly cells coloured by the worst level a
series held while fresh, or by silence, or by the absence of fresh data. The state answers
only for the present ([state](../specs/state.md)); reconstructing a whole past state at
any instant is `at=` time travel, which [0001](0001-semantic-core-and-skins.md) places in a
shared service and which is not built.

What a lane needs is narrower: for each hour, whether any series was fresh, and the worst
level held while it was. The hub already keeps what answers that: the event log, whose
entries chain — each change records when the level it left began — and every point a node
ever sent, and the configuration that says how long a point stays fresh.

## Decision

- **A lane is computed on each read** from the event log, the current levels and the stored
  points — those of the window, and before it as far back as a point can still be fresh
  inside it — and stored nowhere.
- **A level spans from the `since` evaluation recorded for it to the change that left it**,
  or to the present for the level held now. A span whose end was never recorded — a removed threshold, which evaluation forgets without an event —
  is shown only at the instant it began, rather than guessed at.
- **Freshness in the past uses today's bounds**: a point stays fresh, from its own stamp, for
  three of the interval the configuration gives the series' sensor now, or of its node's
  longest when it gives none — the state's bound, without its rule that a sensor switched
  off is stale outright.
- This is a summary for one skin, not time travel: it answers "what was the worst of this
  hour", never "what did the state say at 14:03".

## Consequences

- No schema change and no new writer: the evaluation tick stays the log's one writer
  ([0015](0015-evaluation-on-a-tick.md)).
- A past hour can change: points an agent delivers late, from its buffer, and a changed
  interval repaint it. The timeline spec says so.
- Cost follows the window, not the history: one read per series of the nodes on the page,
  of the window's points and the few before it, on every refresh.
- When time travel is built, a lane may be re-expressed as 24 reads of it; the cells would
  not change meaning.

## Alternatives

- **Store an hourly summary per node, written by the tick.** Rejected: late points make a
  stored hour wrong, and a second record of the same facts drifts from the log.
- **Wait for `at=` time travel.** Rejected: a general past state is a design of its own
  ([state](../specs/state.md#out-of-scope)), and a lane needs a small part of it.
- **Freshness as the history chart draws gaps** ([history](../specs/history.md#gaps)).
  The two agree but for a series with no interval of its own — it names no sensor, or its
  sensor resolves none — whose line a chart never breaks. Rejected for that case: such a
  series would then never go stale in a lane and could hold a level over hours its node was
  asleep, while the state ages it by the node's longest interval.
- **Extend a level with an unrecorded end to the present or to the series' next record.**
  Rejected: it paints a level over hours in which nothing was judged.
