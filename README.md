# Monitor

[![CI](https://github.com/pravbeseda/monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/pravbeseda/monitor/actions/workflows/ci.yml)

A personal control panel for life. Metrics from infrastructure, finance and health flow into
one semantic core that computes what they *mean* — status, anomaly, freshness — and a set of
interchangeable skins renders that meaning.

Monitor does not replace the apps and devices that already show these numbers. It turns them
into a briefing: one glance to see whether everything is fine, attention drawn to what is
off, history behind every value, and silence until something needs you.

## What it does

- **Agents** on Linux and macOS nodes report free disk space, load, memory, uptime,
  failed systemd units and the age of the latest Time Machine backup to the hub.
- **The hub** collects online services itself — free space on Google Drive is the first.
- **Thresholds** are set per series on the hub's `/thresholds` page; no series alerts until
  its threshold is set.
- **A silent node** is noticed without any threshold: each node class says how long it may
  stay quiet.
- **Alerts** fire on transitions with hysteresis: entering or leaving critical is sent at
  once, everything else waits for a daily digest; Telegram delivers them once configured.
- **Anomalies** — a value outside the band its series kept the week before — are shown,
  never notified.
- **The timeline** at `/timeline` shows what needs attention now, each node's last
  24 hours, and what changed; `/debug` is the full table of every series.
- **The interface** speaks English and Russian.

Finance and health come once they have sources that report on their own; the roadmap is in
[docs/concept.md](docs/concept.md#roadmap).

## How it fits together

```
 node (agent) ──┐
 node (agent) ──┼── POST /api/v1/ingest ──▶ hub ──▶ SQLite
 node (agent) ──┘                            │
                                             ├── collects online services
                                             ├── evaluation tick ──▶ alerts, daily digest
                                             └── web pages: /timeline, /debug, /thresholds
```

- **Two binaries in one repository:** `cmd/agent` runs on every node, `cmd/hub` is one
  process holding ingest, storage, evaluation, the web pages and notifications
  ([ADR 0004](docs/decisions/0004-two-binaries-monorepo.md)).
- **Agents push; the hub never polls them**
  ([ADR 0002](docs/decisions/0002-push-not-pull.md)). An agent knows only the hub's address,
  its node name and its token; which sensors run and how often arrives in the ingest
  response ([ADR 0010](docs/decisions/0010-agent-configuration.md)).
- **Go and SQLite, pages rendered by the hub**
  ([ADR 0005](docs/decisions/0005-poc-stack.md)).
- **Signed releases**, pulled and installed by an updater of their own on each machine once
  a version to follow is named — the hub's on its host, an agent's by the hub
  ([ADR 0022](docs/decisions/0022-updates-are-pulled.md)).

Every decision is recorded in [docs/decisions/](docs/decisions/), each saying what was
rejected and why.

## Install

One command installs or upgrades a hub or an agent from the newest signed release:

```sh
curl -fsSL https://raw.githubusercontent.com/pravbeseda/monitor/main/deploy/monitor-install.sh \
    | sudo sh -s -- hub
```

A node's first install needs its token, and a hub's first run needs its configuration;
[docs/install.md](docs/install.md) walks through both, the key fingerprint to check, and the
manual path.

## Develop

Go (the version in `go.mod`), [golangci-lint](https://golangci-lint.run) and
[gitleaks](https://github.com/gitleaks/gitleaks):

```sh
git config core.hooksPath .githooks   # once per clone: the privacy and format checks
go test -race -cover ./...
golangci-lint run
```

To run both binaries locally, write a `config.yaml` holding just the `laptop-a` node from
[config.example.yaml](config.example.yaml) — every other node there needs its own token or
credentials — and use one token of at least 32 characters for both:

```sh
MONITOR_TOKEN_LAPTOP_A=<token> go run ./cmd/hub --config config.yaml --db monitor.db
MONITOR_TOKEN=<token> go run ./cmd/agent --hub http://127.0.0.1:8080 --node laptop-a
```

Behaviour is specified before it is coded: the tables in [docs/specs/](docs/specs/) are what
the tests cite.

## Documentation

[docs/index.md](docs/index.md) maps every document: the concept, the specs, the decisions and
the design notes behind them.

## This repository is public

It holds tooling only. Node names, addresses, thresholds, tokens and measurements live on
the machines that run Monitor, never here; examples use invented names such as `laptop-a`
and `hub.example.com`. The rules and the checks that enforce them are in
[ADR 0007](docs/decisions/0007-public-repository.md).

## License

[MIT](LICENSE).
