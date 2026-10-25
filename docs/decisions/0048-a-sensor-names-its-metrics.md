# 0048. A sensor names its metrics; the hub shows the name wherever a person reads one

- **Status:** accepted
- **Date:** 2026-10-25
- **Source:** [2026-10-25 metric names](../log/2026-10-25-metric-names.md),
  [0003](0003-sensors-are-modules.md), [0032](0032-thresholds-are-set-in-the-interface.md)
- **Amends:** [0008](0008-english-repo-bilingual-ui.md) — an external sensor's names come
  from its own declaration, not from the hub's catalogue

## Context

Every page names a series by its metric id — `disk.free_pct /srv`,
`timemachine.backup_age_seconds destination=backup-a`. That was kept on purpose when the
overview was specified: names for some metrics only would cost code per metric, against
"a metric costs no code" ([0032](0032-thresholds-are-set-in-the-interface.md)). The
overview made the cost of ids visible — a card of four ids reads as a debug table — and the
reader asked for readable names everywhere.

The interface is bilingual ([0008](0008-english-repo-bilingual-ui.md)), so a name is a pair
of strings, and some names need a label in them: "Free on /srv", not "Free on" beside a
separate `/srv`.

## Decision

**The sensor that produces a metric names it**, in every locale of the interface, beside
the metric id it already declares:

- The name is a template that may refer to the series' labels by key — `Free on {mount}` /
  `Свободно {mount}` — and is the same for every series of the metric. A sensor names the
  family of its metrics too — `disk` reads "Disk" — since pages head groups by it.
- A built-in sensor takes its names from the interface's catalogue, as every other
  user-facing string ([0008](0008-english-repo-bilingual-ui.md)); an external sensor
  carries its own.
- An agent declares its sensors' names in the `manifest` it sends with every request; a
  sensor the hub runs itself declares them in process, the same way
  ([0039](0039-the-hub-collects-a-service-node.md)).
- The hub stores the newest declaration of each metric id, so a series keeps its name while
  its node is silent and across restarts. It keeps a name only for a metric the same request
  carries a measurement of, so a token names nothing it does not report.
- **Wherever a person reads a series' name** — every page, notifications and the digest —
  it is the declared name with its labels filled in, followed by the labels the name does
  not use, as ids are followed today. A metric no sensor has named is shown by its id.
- Ids stay where a machine reads them: URLs, the State and history APIs, configuration.

## Consequences

- A metric costs no hub code still: a new sensor arrives with its names, built in or
  external ([0003](0003-sensors-are-modules.md)), and the hub names no metric itself.
- Each sensor carries user-facing strings, in both languages, next to its metric ids; a
  sensor without them is valid and shows ids.
- The manifest grows to one or two kilobytes per request; it is sent every time already,
  and statelessness still beats saving them ([ingest](../specs/ingest.md)).
- Until an agent is updated to a release that declares names, its metrics show ids. Agents
  update themselves where the hub's file sets `agent_target`
  ([0028](0028-agents-follow-a-target-the-hub-serves.md)); elsewhere by hand.
- Two agents of different releases may declare different names for one metric; the newest
  declaration wins, and the hub's page shows one name at a time.
- Any node can rename a metric it reports for every node that reports it. A token already
  lets a node report any value; a name is no more than that.

## Alternatives

- **A catalogue of names in the hub, by metric id** — rejected: cheaper today, but the hub
  would know every metric, a new sensor would cost a hub change, and an external sensor
  would have no name until a second mechanism arrived.
- **Names set by the reader in the interface, stored like thresholds** — rejected: zero
  code per metric, but every page shows ids out of the box until someone names each series.
  It stays possible later as an override on top of the declared name.
- **A name in every measurement** — rejected: the same string repeated in every point of a
  request, where the manifest says it once.
