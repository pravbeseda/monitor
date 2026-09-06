# Monitor — Documentation Map

A personal control panel for infrastructure, finance and health metrics. This file is the
entry point to every document in the project.

## Product and plan

| Document | Contents |
|---|---|
| [concept.md](concept.md) | Idea, architectural principles, planned skins, domains, roadmap |
| [poc.md](poc.md) | POC spec: scope, terminology, wire format, work plan, answered questions |
| [install.md](install.md) | Install guide: one command, then the manual path — the hub host, install and verify a node, upgrade, uninstall |
| [plans/stage-1-skeleton.md](plans/stage-1-skeleton.md) | Step plan for POC stage 1, kept as a record; plans now live in the task and the pull request ([0017](decisions/0017-one-spec-and-decision-gates.md)) |

## Architecture decisions

One decision per file, each stating what was rejected and why. New records start from
[TEMPLATE.md](decisions/TEMPLATE.md) with the next free number.

| # | Decision | Status |
|---|---|---|
| [0001](decisions/0001-semantic-core-and-skins.md) | Meaning lives in the core; skins are dumb renderers | accepted |
| [0002](decisions/0002-push-not-pull.md) | Agents push; the server never polls nodes | accepted |
| [0003](decisions/0003-sensors-are-modules.md) | Sensors are in-process modules of the agent | accepted |
| [0004](decisions/0004-two-binaries-monorepo.md) | Two artifacts — agent and monolithic hub — in one repository | accepted |
| [0005](decisions/0005-poc-stack.md) | POC stack: Go, SQLite, server-side HTML | accepted |
| [0006](decisions/0006-alerting-rules.md) | Alert on transitions; only critical is instant | accepted |
| [0007](decisions/0007-public-repository.md) | Public repository from the first commit; nothing personal in it | accepted |
| [0008](decisions/0008-english-repo-bilingual-ui.md) | The repository is English; the interface is bilingual | accepted |
| [0009](decisions/0009-development-process.md) | Specs for behaviour, plans for work, ADRs for decisions | accepted |
| [0010](decisions/0010-agent-configuration.md) | The agent's configuration lives on the hub and arrives in the ingest response | accepted |
| [0011](decisions/0011-quality-gates.md) | Quality is enforced by tooling, not by attention | accepted |
| [0012](decisions/0012-threshold-model.md) | Disk thresholds are a floor plus a proportional band | accepted |
| [0013](decisions/0013-relative-hysteresis.md) | Hysteresis is a relative margin, not a fixed number of points | accepted |
| [0014](decisions/0014-macos-available-space.md) | Free space is what the system calls available; cgo in the darwin sensor only | accepted |
| [0015](decisions/0015-evaluation-on-a-tick.md) | Evaluation runs on its own tick, never inside a request | accepted |
| [0016](decisions/0016-leaving-critical-is-instant.md) | Leaving critical is announced as instantly as entering it | accepted |
| [0017](decisions/0017-one-spec-and-decision-gates.md) | One document per unit of work; gates on decisions, not documents | accepted |
| [0018](decisions/0018-history-through-the-api.md) | History is served by the hub's API; a chart is one of its consumers | accepted |
| [0019](decisions/0019-deployment-layout.md) | Install into system paths, with one environment file per binary | accepted |
| [0020](decisions/0020-agent-reads-its-environment-file.md) | The agent reads its own environment file; no shell sources it | accepted |
| [0021](decisions/0021-shell-is-linted-too.md) | The shell the project ships is linted like its Go | accepted |
| [0022](decisions/0022-updates-are-pulled.md) | Updates are pulled by an updater of their own; the hub names the version | accepted |

## Behaviour specs

How subsystems behave, one file per subsystem, each built around a behaviour table that
tests are derived from. Required for contracts, stateful algorithms and multi-session work
(see [ADR 0009](decisions/0009-development-process.md)); rows state only what an observer
can see ([ADR 0017](decisions/0017-one-spec-and-decision-gates.md)). New specs start from
[TEMPLATE.md](specs/TEMPLATE.md), and "approved" below means reviewed and in force.

| Spec | Owns | Status |
|---|---|---|
| [ingest.md](specs/ingest.md) | `/api/v1/ingest` contract: request, response, config delivery | approved |
| [hub-config.md](specs/hub-config.md) | The hub's YAML file: validation, layering, per-node configuration and its version | approved |
| [disk-sensor.md](specs/disk-sensor.md) | The disk sensor: enumeration, filtering and the label contract of its metrics | approved |
| [agent.md](specs/agent.md) | The agent: local configuration, tick loop, delivery and configuration application | approved |
| [evaluation.md](specs/evaluation.md) | Levels, hysteresis, the event log, silence, digests and the notifier boundary | approved |
| [history.md](specs/history.md) | The history series, `/api/v1/series`, `/api/v1/history` and the drill-down page | approved |
| [release.md](specs/release.md) | What a tag publishes, how a release is signed, and how an artifact is checked | approved |
| [installer.md](specs/installer.md) | One command that installs or upgrades a hub or an agent from a signed release | approved |
| [deployment.md](specs/deployment.md) | The install layout, the units and what `install-agent.sh` does to a node | approved |

## Design notes

Reasoning from working sessions, including options that were rejected.

| Date | Topic |
|---|---|
| [2026-08-28](log/2026-08-28-concept.md) | Project start: visual concepts, architecture, naming |
| [2026-08-29](log/2026-08-29-stage-1-decisions.md) | Stage 1: locale negotiation, one wire package, what the live run caught |
| [2026-08-29](log/2026-08-29-munin-hub-plugin.md) | Proposal: a munin plugin on the hub host for off-the-shelf history graphs |
| [2026-08-29](log/2026-08-29-stage-2-decisions.md) | Stage 2: what the evaluation design rejected, and what three review rounds changed |
| [2026-09-06](log/2026-09-06-release-signing.md) | Release signing: the tools, the manifest and the key placement that lost |

## Not written yet

- `docs/architecture/` — subsystem documents arrive with the code. Until then the
  architecture fits in [concept.md](concept.md), the ADRs and the specs, and duplicating it
  would create a second source of truth.

## Where a new open question goes

It is written into the document it belongs to — a POC question into [poc.md](poc.md), a
subsystem question into its spec — and answered in the same session it is raised, not
collected for later. An answer that settles how the system is built becomes an ADR; the rest
stays where the question was asked. The questions this POC started with are answered at the
end of [poc.md](poc.md).
