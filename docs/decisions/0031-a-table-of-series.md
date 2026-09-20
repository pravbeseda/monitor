# 0031. Keep a table of series beside the measurements

- **Status:** accepted
- **Date:** 2026-09-20
- **Source:** [issue #15](https://github.com/pravbeseda/monitor/issues/15),
  [issue #45](https://github.com/pravbeseda/monitor/issues/45),
  [2026-09-20 design note](../log/2026-09-20-history-streaming.md)

## Context

Every read the hub does starts by asking a question about *series*, not about points: which
series exist, and when did each last report. `measurements` answers both by ranking points —
`GROUP BY node, labels` for a window, `DISTINCT node, labels` for a listing, a window
function for the latest value — so the cost follows the stored history rather than the
handful of rows the answer carries.

Measured on a synthetic year: 10 nodes, 50 series, one point a quarter of an hour, 1.75M
points.

| read | over `measurements` |
|---|---|
| the window's newest point per series, 24h | 63 ms |
| the same, 365d | 162 ms |
| the series listing | 89 ms |
| the latest value of every series ([#45](https://github.com/pravbeseda/monitor/issues/45)) | 330 ms, on every evaluation tick and every render of `/` |

Indexing cannot fix the shape. An index leading with `(metric, node, labels, ts)` makes the
cost follow the whole history; one leading with `(metric, ts)` makes it follow the window and
adds a sort — measured at 781 ms for a 365-day window, more than reading every point the
answer returns. Both were tried, and one index cannot serve both orders.

## Decision

Store the series as a table of its own, written in the same transaction as the measurements
that imply it:

```
series(metric, node, labels, last_ts)   PRIMARY KEY (metric, node, labels)   WITHOUT ROWID
```

`last_ts` is the newest timestamp stored for that series, raised by an ingest that carries a
newer one and never lowered. Series selection, the window's end and the latest-value snapshot
all read this table; `measurements` is read only by the primary key, one series at a time,
for the points themselves.

The indexes over `measurements` go with it: no read scans the table any more, so the
measurements keep their primary key alone.

## Consequences

- One row per series is the unit of every question about series; reads that used to follow
  the stored history now follow the number of series, whatever the window.
- `SaveIngest` owns the invariant: it writes the measurement and raises `last_ts` in one
  transaction, so the table cannot drift from what it describes. Nothing else may write it.
- A migration backfills the table from the measurements once, and drops the history index.
  The entry is new rather than an edit of a shipped one, so no database re-runs a statement
  that changed under it; a database that already applied the shipped entries only gains this
  one. Backfilling again is harmless: a conflict raises `last_ts` to what the measurements
  say rather than duplicating a row.
- Dropping every index over `measurements` makes the table its primary key alone: measured
  on a synthetic year, the database goes from 238 MB to 131 MB. What it costs is a second
  statement per measurement and a b-tree of its own, both small: one row per series against
  one row per point.
- **Deleting measurements is now a two-table operation.** Retention keeps every raw point
  today ([poc.md](../poc.md)), so nothing deletes; the first thing that does must repair or
  delete the series row, and it has no index on `ts` to find old points with. A row left
  ahead of its measurements does not raise an error: the latest-value snapshot joins on the
  timestamp the row names, so the series disappears from `/` while history still returns it.
- A series that stops reporting keeps its row for ever, which is what the listing promises
  ([history.md](../specs/history.md#selection)) — the row is a fact about what was stored,
  not about what is arriving.

## Alternatives

- **An index leading with `(metric, ts)`** — measured above: it wins on windows that are a
  small part of the history (24h over a year: 63 ms → 1.3 ms) and loses badly where the
  window approaches it (365d: 162 ms → 781 ms), because the group has to be sorted. It also
  doubles the cost of the listing and does nothing for the latest-value snapshot, which is
  the most expensive read of the three and runs on every tick.
- **Keeping both indexes** — the planner takes the new one for the window regardless, so the
  only gain is the listing, for half the database again.
- **Accepting the scan** — it is linear in everything the hub has ever stored, on the hub's
  own tick. A year of a 50-series installation already costs hundreds of milliseconds per
  tick; the machine this runs on is a home server, not a database host.
- **A materialised view** — SQLite has none, and a trigger maintaining one would put the
  invariant in the schema where the tests cannot reach it as easily as they reach
  `SaveIngest`.
