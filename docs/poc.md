# POC — Free Disk Space

## Goal

See one end-to-end scenario working on real data and fix the architectural skeleton that
everything later grows from without a rewrite:

**agent on a node → ingest on the hub → storage → web page → Telegram alert.**

Scope: laptop-class nodes (macOS) and server-class nodes (Debian). Windows comes later,
but cross-platform support is a stack requirement from day one.

Metrics: free space (bytes and percent) per mounted volume per node. The agent's
liveness needs no metric of its own: the authenticated ingest request itself is the
heartbeat — every accepted request advances the node's last-seen
([ingest spec](specs/ingest.md)).

## Terminology

- **Agent** — the single application on a node that collects and sends measurements.
- **Sensor** — a module inside the agent that takes one kind of reading (disk, cpu,
  battery). Chosen over "driver": an agent has a set of sensors, like a robot.
- **Hub** — the server application: ingest, storage, evaluation, API, web.
- **Notifier** — a delivery channel for notifications (Telegram in the POC).

## Decisions this POC builds on

The reasoning and the rejected alternatives are in the ADRs; the POC only applies them.

- [0002](decisions/0002-push-not-pull.md) — push, not pull
- [0003](decisions/0003-sensors-are-modules.md) — sensors are modules
- [0004](decisions/0004-two-binaries-monorepo.md) — two artifacts, one repository
- [0005](decisions/0005-poc-stack.md) — the stack
- [0006](decisions/0006-alerting-rules.md) — alerting rules
- [0007](decisions/0007-public-repository.md) — what may not enter this repository
- [0010](decisions/0010-agent-configuration.md) — where the agent's configuration comes from
- [0011](decisions/0011-quality-gates.md) — the quality gates
- [0012](decisions/0012-threshold-model.md) — the disk threshold model
- [0013](decisions/0013-relative-hysteresis.md) — how a state recovers
- [0014](decisions/0014-macos-available-space.md) — what "free space" means
- [0015](decisions/0015-evaluation-on-a-tick.md) — evaluation runs on its own tick
- [0016](decisions/0016-leaving-critical-is-instant.md) — leaving critical is instant
- [0018](decisions/0018-history-through-the-api.md) — history is served by the API, not by a second tool

## Wire format

The ingest payload is the seed of the general measurement schema, so it is versioned from
the first commit.

```json
POST /api/v1/ingest
{
  "node": "laptop-a",
  "agent_version": "0.1.0",
  "config_version": "7",
  "ts": "2026-08-28T10:00:00Z",
  "measurements": [
    { "metric": "disk.free_bytes", "labels": {"mount": "/", "fs": "apfs", "removable": "false"},
      "value": 123456789 },
    { "metric": "disk.free_pct",   "labels": {"mount": "/", "fs": "apfs", "removable": "false"},
      "value": 34.2 }
  ]
}
```

`config_version` is the configuration the agent currently holds; when it differs from the
hub's, the response carries the newer one ([0010](decisions/0010-agent-configuration.md)).
The shape of that response belongs in the ingest spec.

Thresholds and node classes are declared in the hub's YAML configuration, never hard-coded
in the evaluation logic; product defaults apply where that file says nothing
([0007](decisions/0007-public-repository.md)). The deployment file itself lives on the server
and is not part of this repository, which ships only `config.example.yaml`. The full key
set lives in [hub-config](specs/hub-config.md) and [evaluation](specs/evaluation.md):

```yaml
rules:
  disk:
    warning:  { floor: 10GB, ratio: 15, ceiling: 100GB }
    critical: { floor: 4GB,  ratio: 7,  ceiling: 40GB }

classes:
  laptop: { silence_after: 48h }
  server: { silence_after: 10m }

nodes:
  laptop-a: { class: laptop, token_env: MONITOR_TOKEN_LAPTOP_A }
  server-b: { class: server, token_env: MONITOR_TOKEN_SERVER_B }
```

## Work plan

**Stage 1 — skeleton (one end-to-end thread)**
- [x] Monorepo: `cmd/agent`, `cmd/hub`, `internal/...`, with the quality gates of
      [0011](decisions/0011-quality-gates.md) in CI and the pre-commit hook
- [x] Agent: disk sensor, local configuration limited to the hub url and its token, push loop
- [x] Hub: `/api/v1/ingest`, write to SQLite, page `/` with the latest values
- [x] Collection intervals travel in the ingest response, keyed off `config_version`
      ([0010](decisions/0010-agent-configuration.md))
- [x] Run by hand on one laptop and one server

**Stage 2 — evaluation and alerts**
- [x] Evaluation on its own tick ([0015](decisions/0015-evaluation-on-a-tick.md)): `rules`
      in the hub's YAML, levels with the relative hysteresis of
      [0013](decisions/0013-relative-hysteresis.md), stale subjects frozen
      ([evaluation spec](specs/evaluation.md))
- [x] `states` and `events` tables: one event per transition, read back after a restart
- [x] Notifier behind an interface (log and Telegram), silence detector, the instant rule
      of [0016](decisions/0016-leaving-critical-is-instant.md), daily digest

**Stage 3 — operation**
- [x] systemd and launchd units, agent install script
      ([deployment spec](specs/deployment.md), [install guide](install.md))
- [x] Signed releases published by CI on a tag, and a verifier for what they publish
      ([release spec](specs/release.md))
- [x] One command that installs or upgrades a hub or an agent from a signed release
      ([installer spec](specs/installer.md)); the timer that would do it unattended is
      [0022](decisions/0022-updates-are-pulled.md) and is not built
- [ ] nginx vhost for the hub, per-node tokens, authentication on the web page and a
      credential a program can send to the read endpoints. The proxy holds that credential
      ([0023](decisions/0023-proxy-holds-the-web-perimeter.md)) and what it has to do is
      [nginx-requirements.md](nginx-requirements.md); the vhost itself is applied from the
      repository that provisions the hub host
- [x] Decide how history graphs are served, before any charting is built by hand:
      the hub serves the series and a renderer consumes them
      ([0018](decisions/0018-history-through-the-api.md)), against the
      [munin hub-plugin proposal](log/2026-08-29-munin-hub-plugin.md)
- [x] `/api/v1/series`, `/api/v1/history` and the drill-down page
      ([history spec](specs/history.md))
- [ ] Roll out to every node, observe for a week, tune thresholds

**Done when**: filling a disk on a test node produces a Telegram alert within one interval,
the node turns red on the web page, and the value history is visible for the whole
observation period.

## Questions, answered

All eight are settled. The architectural ones are owned by the ADR named against them; the
rest are recorded here, which is where they belong.

1. **Language** — Go for the agent and the hub; skins stay TypeScript over the State API.
   → [0005](decisions/0005-poc-stack.md), with the quality gates that made the trade sound
   in [0011](decisions/0011-quality-gates.md).
2. **Where the hub lives** — on one of the Debian servers, behind the nginx and certificate
   already on that host; the hub binds to localhost. The host name is a deployment setting
   and stays out of this repository ([0007](decisions/0007-public-repository.md)).
3. **Intervals** — per sensor, configured centrally, layered, delivered in the ingest
   response. → [0010](decisions/0010-agent-configuration.md). Starting values: disk every 15m
   on servers and 1h on laptops, above a 5m base tick.
4. **Thresholds** — a floor plus a proportional band, with backup volumes declared by role.
   → [0012](decisions/0012-threshold-model.md).
5. **Volumes and sensors** — an allow-list of filesystem types (`apfs`, `ext4`, `xfs`,
   `btrfs`, `zfs`, `ntfs`) delivered with the configuration; external drives are collected
   and flagged removable so an unplugged one is not read as a volume that vanished. Which
   sensors run is chosen by profile plus applicability.
   → [0010](decisions/0010-agent-configuration.md).
6. **Telegram** — a private chat, one recipient. The bot works both ways (manual input,
   summaries on request) and a channel has no dialogue. The recipient is one configuration
   value behind the `Notifier` interface.
7. **Retention** — keep every raw point; no downsampling is written. Tens of megabytes a year
   grows slower than storage gets cheaper, aggregation is irreversible, and a long series is
   what makes trends work. When it does become a problem it gets its own ADR. A daily
   `sqlite3 monitor.db ".backup /path/backup.db"` to the second server guards the history
   against the likelier accident — a plain `cp` is not equivalent, because in WAL mode the
   most recent transactions live in the `-wal` file and copying the database alone can lose
   them or corrupt the copy.
8. **Storage** — SQLite behind the `Storage` interface, not the MySQL already on that host.
   → [0005](decisions/0005-poc-stack.md).
