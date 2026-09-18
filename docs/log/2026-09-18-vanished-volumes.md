# 2026-09-18 — Vanished volumes: why the page hides by `removable` and ages by last-seen

The index page showed the latest value of every series ever stored, so a Mac's page filled
with Time Machine snapshot mounts and an ejected installer image, each frozen at the moment
it was unmounted. Two separate causes, two fixes: the
[disk sensor](../specs/disk-sensor.md#edge-cases) now groups a mounted snapshot with the
container it was taken from, and [`/`](../specs/history.md#page) leaves out what stopped
arriving.

## What the page does with a series that stopped

Three options were weighed by the operator:

- **Hide everything past the bound.** Simplest, but an internal disk that vanished would leave
  the page as silently as a stick pulled out, which is the one disappearance worth seeing.
- **Collapse stale series into a block of their own.** Nothing is lost, but the clutter only
  moves: every backup and every installer adds to it for ever.
- **Hide removable, mark the rest — chosen.** It uses `removable` for what the sensor spec
  introduced it for: telling an unplugged drive from a vanished internal disk.

## What the spec reviews changed

- **The age is measured against the node's last-seen time**, not its newest point. Measuring
  against the newest point was skew-free, but a node sending only heartbeats would never age
  its rows, and a node whose only volumes are removable would keep the last one unplugged for
  ever — the bug being fixed. Last-seen also stops when the node stops, so a silent node keeps
  its rows instead of losing every removable one.
- **Snapshots are grouped, not dropped.** A rule dropping every `@` source would also drop a
  root mounted from a snapshot; grouping lets the container's shorter mount point win and
  still reports a snapshot that is its container's only watched member.
- **A series with no interval** — a metric in no rule, a disabled sensor, a node gone from the
  configuration — is never aged, matching how history refuses to break its line.

## Left as is

Series already stored for snapshots carry `removable: "false"`, so after the fix they show as
marked rows rather than vanish. They are removed once, on the server, by deleting their
measurements; no retention or "forget" action was built for a one-off cleanup. Evaluation
builds its subjects from stored series alone, so the state rows those subjects leave behind
are never matched again: no repeat, no digest entry, no error.
