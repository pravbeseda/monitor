# 2026-09-28 — Silence after the hub restarts

[#56](https://github.com/pravbeseda/monitor/issues/56): a hub back from an outage longer
than a node's `silence_after` announced every such node as fallen silent, then as reporting
again. The node was fine; the hub was the one missing.

## Rejected: a grace window after every start

The first fix held a node's silence at its previous level until one `silence_after` had
passed since the start, for any node not heard from since. The review found two costs:

- every start restarted the window, so a hub restarted more often than `silence_after` —
  deploys, configuration changes, host reboots, all common inside a laptop's 48h — never
  announced a node that died; the delay had no bound;
- a thirty-second restart forgave a node a whole `silence_after`, so a node that died just
  before it was announced nearly twice as late.

A silent monitor is worse than the false alarm #56 was about, so the window was dropped.

## Chosen: leave out only the hub's own outages

The hub records the instant of every tick, and each start records the span from the last
tick to the start as an outage. A node's quiet is now − last_seen less every part of those
outages after its last report ([evaluation](../specs/evaluation.md#node-silence)). A short
restart forgives only itself, repeated restarts do not add up to a blind spot, and a node
silent before an outage stays silent without a rule of its own.

A second review found two gaps in a version that remembered the latest outage alone:

- a restart soon after coming back from a long outage — a deploy or a configuration fix —
  replaced that outage with a few seconds, and #56 came back for every node that had not
  reported in between; so every outage is kept, in a table of its own;
- the state judged staleness by the raw last_seen while the tick left the outage out, so
  after an outage a live node's series read "no fresh data"; so the outages come with the
  storage snapshot both read, and one function moves last_seen past them.

A tick lands every minute, so an outage can be overstated by up to a minute. The first
start of this build has no tick recorded and leaves nothing out, once. Outages are not
pruned: one row per restart.
