# 0052. A sensor declares default thresholds, and the hub seeds each series with them once

- **Status:** accepted
- **Date:** 2026-11-03
- **Source:** [2026-11-03 default thresholds](../log/2026-11-03-default-thresholds.md),
  [0048](0048-a-sensor-names-its-metrics.md), [0051](0051-a-sensor-groups-its-family-by-a-label.md)
- **Amends:** [0032](0032-thresholds-are-set-in-the-interface.md) — nothing has a level by
  default, unless its sensor declares one; [0007](0007-public-repository.md) — such a
  default ships

## Context

[0032](0032-thresholds-are-set-in-the-interface.md) gave every series no level until a
reader sets one on its page. For a backup mirror that means a failed run is never coloured
and never reported until someone opens each job's page — six pages for three jobs of two
repositories, and six more for every new server. The colour that tells a failure apart is
exactly what the reader asked the backup panel for.

0032 rejected shipped defaults for two reasons, and both still stand where they were aimed:

- **A default layered under the form** makes the number that alerts you a merge of two
  sources nobody sees at once.
- **A default tied to an installation** — what "nearly full" means for one volume — says
  something about that installation, and a fresh install cries wolf on volumes it knows
  nothing about.

Some metrics escape both. "The last run failed" means the same on every machine, and a
backup that has not succeeded for two days is late wherever it runs. The sensor that
produces such a metric knows that much about it.

## Decision

- **A sensor may declare a default threshold for a metric it produces**: a direction and
  up to two values in the series' unit, as the form takes them. The declaration travels in
  the manifest beside the metric's name ([0048](0048-a-sensor-names-its-metrics.md)).
- **The hub seeds a series with it once.** When a request stores a measurement of the
  series and its manifest declares the default, and the series has never been seeded, the
  hub stores the default as the series' threshold — unless one is already stored — and
  marks the series seeded, in the same transaction. The hub does not keep the declaration,
  as it keeps a name: only a request carrying both seeds. From then on the threshold is an
  ordinary one: the form shows it, edits it and clears it.
- **A save that changes a series' threshold marks it seeded too.** A threshold the reader
  removed is never put back, and a reader's own threshold is never replaced. A save that
  only turns the anomaly switch leaves an unwatched series to be seeded.
- **The form may apply one threshold to every series of its metric**, on every node, after
  a page that lists them asks to confirm. It replaces each series' threshold; it is a
  one-off write, not a rule that later series follow.
- A sensor declares a default only where the value means the same on every installation.
  A threshold on a size or a share of a volume stays the reader's.

## Consequences

- There is still one source of truth: the store. A default is a value written once, not a
  layer read on every tick; changing a sensor's default in a later release reaches only
  series not yet seeded.
- An installation that upgrades seeds its existing series too: every series without a
  threshold gets the default at its next measurement. A threshold removed before the
  upgrade left no record, so it comes back once. A series already past the default is
  judged like one whose threshold was just set, so a critical is announced at once
  ([thresholds](../specs/thresholds.md#effects)).
- The hub writes thresholds from two places, the form and ingest. Ingest only ever inserts
  where nothing is stored and never after the series was seeded, so it cannot overwrite
  what the reader set. It writes a threshold, never a level or an event: the tick stays the
  event log's only writer ([0015](0015-evaluation-on-a-tick.md)).
- The manifest grows by one map. An agent from an older release declares no defaults; its
  series stay unwatched until it updates, then are seeded at their next measurement.
- 0032's "a convenience of the form, not a model underneath it" holds for applying to every
  series: it writes each series' own threshold and leaves no rule behind.

## Alternatives

- **A default read on every tick when a series has no threshold**: rejected. It is the layer
  0032 rejected — a cleared threshold would silently fall back to the default, and the form
  could not say what the hub is judging by without reading the sensor's declaration too.
- **Seeding only a series stored for the first time**: rejected. The series that prompted
  this decision already exist; they would stay uncoloured until their labels changed.
- **Defaults in the hub's configuration file**: rejected for the reason 0032 gave: thresholds
  left the file, and a default per metric is the sensor's knowledge, not the installation's.
- **A "watch every series of this metric" rule, kept and applied to series that appear
  later**: rejected. It is a layer again, and the sensor's default already covers series
  that appear later.
