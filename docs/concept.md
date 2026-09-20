# Monitor — Concept

*Working title: Monitor. Status: concept draft, August 2026.*

## Idea

A personal control panel for one's own life: a single place where indicators from
different areas — infrastructure (servers, workstations), finance (investments,
savings), health and activity (steps, sleep, weight) — converge into one picture
of how things stand.

Every one of those values is already visible in some app or device. Monitor does not
replace those sources; it turns them into a **briefing**:

1. **One glance** — is everything fine or not.
2. **Attention where it is due** — the system raises what is off, ranked by anomaly
   rather than grouped by category.
3. **Depth and history** — click into any indicator and see its dynamics over any horizon.
4. **Notifications** — active control: silence while all is well, a signal when a
   threshold breaks or a metric goes quiet.

The pull is the one strategy games like SimCity exploit — not their mechanics, but the
urge to keep one's holdings in order, and the aesthetics of watching and steering.

## Architectural principles

Each principle below is recorded in full, with its alternatives, in the linked ADR.

1. **The semantic core is the single source of truth.** It computes meaning — health
   (0–100), status, trend, anomaly rank, freshness, forecasts — once; skins render it and
   never judge values themselves. → [ADR 0001](decisions/0001-semantic-core-and-skins.md)
2. **A metric costs no code.** A measurement declares itself: its id carries its unit, its
   labels make it a series, and what that series is judged by — a direction and up to two
   values — is set on its own page and stored with the data, never compiled in and never a
   file to edit. What the agent collects and how often stays configuration; health scoring,
   anomaly detection and *silence* detection follow from the series and its thresholds.
   → [ADR 0032](decisions/0032-thresholds-are-set-in-the-interface.md),
   [ADR 0033](decisions/0033-a-subject-is-a-series.md),
   [ADR 0010](decisions/0010-agent-configuration.md)
3. **Skins are interchangeable consumers of one State API.** Adding a skin touches no core
   code. → [ADR 0001](decisions/0001-semantic-core-and-skins.md)
4. **Cross-cutting services live outside skins**: drill-down (`openMetric(id)`), time
   travel (`at=` on every State API call, which makes timelapses free), and an event stream
   of state transitions that skins subscribe to.
5. **Notifications are rationed.** Instant delivery belongs to entering and leaving
   critical; everything else accumulates into a daily digest, and thresholds have
   hysteresis. → [ADR 0006](decisions/0006-alerting-rules.md),
   [ADR 0016](decisions/0016-leaving-critical-is-instant.md)
6. **Nothing personal enters the repository.** Code is public; schemas, thresholds, node
   names, secrets and measurements are not. → [ADR 0007](decisions/0007-public-repository.md)

## Planned skins

| Skin | What it is |
|---|---|
| Debug view | A table of every metric. The first skin; it proves the State API is sufficient and stays as the debugging mode. |
| Mission control | Dark board, empty while all is well; anomalies surface on their own. The primary view. |
| Advisors | Per-domain summaries in plain language (an LLM over aggregates). |
| City | Isometric city: districts are life areas, buildings are metrics. |
| Organism | Metrics as body systems, with one overall "pulse" number. |
| Telegram bot | A skin without a screen: alerts, digests, manual input. |

A skin is a manifest (what it can display) + a mapping (metric → slot, defaulted from
metric metadata) + a lazy-loaded renderer.

## Domains (first wave)

| Domain | Example metrics | Source |
|---|---|---|
| infra | free space, load, uptime, backups | own agents on nodes |
| finance | invested total, savings, contributions | manual input via bot, CSV statements |
| health | steps, sleep, weight | Google Fit / Health Connect export |

Manual input is a feature, not a crutch: a weekly ritual, like a turn in a turn-based game.

## Roadmap

1. **POC** — free disk space across a handful of nodes: agent, hub, one web page,
   Telegram alerts. Details: [poc.md](poc.md).
2. **MVP** — 5–7 metrics from different domains, declarative config, digests.
3. **Semantic engine** — health, anomalies, trends, forecasts, time travel.
4. **Skins** — mission control first, then advisors, then the visual metaphors.
