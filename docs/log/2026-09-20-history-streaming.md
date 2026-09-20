# 2026-09-20 — History holds its answer: streaming the points, and the table of series behind them

`Reader.Read` asked storage for every point of the window and reduced the slice afterwards,
so the memory one request needed was set by the window rather than by the answer: a year of
a minute-interval metric across 50 series is 26M stored points, ~840 MB, for a response that
carries at most 1001 points per series ([issue #15](https://github.com/pravbeseda/monitor/issues/15)).

The fix the issue proposed was a single streaming pass: `Points` hands back an iterator and
the reduction keeps only the running extremes of its 500 buckets. One pass turned out not to
be enough, for a reason the proposal did not see.

## The window has to be settled before the first bucket

The end of the window is not `now` when a clock runs ahead: it is the newest point of the
*selected* series, capped at one window ahead ([window](../specs/history.md#window)). Bucket
edges are measured from `window.From`, which is that end minus the window. So a pass that
reduces as it reads would have to know, at its first point, something only its last point can
tell it.

Three ways out were weighed:

- **Read the rows newest first** — `ORDER BY ts DESC` over the whole selection — so the window
  ends at the first row. No index then served a global order by `ts` and none does now, so
  SQLite would sort every matching row in a temporary b-tree: the same unbounded memory,
  moved inside the database.
- **Bucket on a fixed grid and re-align afterwards.** A bucket of a finer grid can straddle
  two final buckets, and only its extremes survive, so the answer would no longer be made of
  the points a bucket actually holds. Rejected: the spec invents no value.
- **Settle the window first, then stream — chosen.** `Newest` returns one row per series,
  which is what the window and the 50-series bound are computed from; `Points` then streams
  one series at a time between the bounds that are now known. That row started as a
  `GROUP BY node, labels` over the points, and the section below is how it stopped being
  one.

## What the shape bought beyond the memory

Reading one series at a time names `node`, `metric` and `labels`, which is an exact prefix of
the primary key, so a label-filtered query no longer reads the points of the other series of
its metric — the old single query read every series the metric had and threw the unmatched
ones away in Go. The 50-series refusal also happens before any point is read rather than
after all of them.

The cost is the query count: one read of the series table, then one query per selected
series, so up to 51 where there was one. Each of them is a primary-key seek over exactly the
rows it returns, against a single query that read the whole window.

## Time needed the schema, not the read

Streaming fixed the memory and left the time. `measurements_series` was
`(metric, node, labels, ts)`, so `ts >= ?` could not be a seek constraint with `labels`
unconstrained before it, and settling the window read every row its metric ever stored,
whatever the window asked for.

Replacing that index with `(metric, ts)` was tried first, and measured on a synthetic year —
10 nodes, 50 series, a point a quarter of an hour, 1.75M points:

| read | `(metric, node, labels, ts)` | `(metric, ts)` |
|---|---|---|
| the window's newest point per series, 24h | 63 ms | 1.3 ms |
| the same, 365d | 162 ms | 781 ms |
| the same, one node named, 365d | 16 ms | 137 ms |
| the series listing | 89 ms | 184 ms |

The index wins where the window is a small part of the history and loses where it is not: it
turns the group into a sort over every row of the window, and on a 365-day window that costs
more than reading every point the answer returns (397 ms for all 1.75M). Keeping both
indexes does not help either — the planner takes the new one for the window regardless, so
the only gain is the listing, for half the database again.

What the measurements showed is that no index has the right shape. Every question the hub
asks first is about *series* — which exist, when each last reported, what each holds now —
and an index over points can only make that cost follow the points. So the series became a
table of their own ([ADR 0031](../decisions/0031-a-table-of-series.md)), written in the same
transaction as the measurements, and the index over `measurements` went away entirely: no
read scans the table any more, so its primary key is the whole of it.

| read | before | after |
|---|---|---|
| the window's newest point per series, 24h | 63 ms | 0.30 ms |
| the same, 365d | 162 ms | 0.06 ms |
| the same, one node named, 365d | 16 ms | 0.02 ms |
| the series listing | 89 ms | 0.05 ms |
| the latest value of every series ([#45](https://github.com/pravbeseda/monitor/issues/45)) | ~1.4 s | 0.98 ms |
| the database on disk | 238 MB | 131 MB |

The join that reads the latest values is written `CROSS JOIN` on purpose: with a plain join
SQLite was free to drive it from `measurements` and scan every point, which is what the plan
test in `internal/storage` now refuses.

## What stayed where it was

The reduction rules did not move into SQL: both extremes per bucket, ties to the earliest
timestamp and the newest point always returned are still one tested Go function
([reduction](../specs/history.md#reduction)), now fed point by point instead of a slice. The
exact window bounds are still applied in `internal/history`: stored timestamps have
millisecond resolution, so the lower bound handed to SQL is a millisecond looser than the
window the answer reports.
