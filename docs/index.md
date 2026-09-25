# Monitor — Documentation Map

A personal control panel for infrastructure, finance and health metrics. This file is the
entry point to every document in the project.

## Product and plan

| Document | Contents |
|---|---|
| [concept.md](concept.md) | Idea, architectural principles, planned skins, domains, roadmap |
| [poc.md](poc.md) | POC spec: scope, terminology, wire format, work plan, answered questions |
| [install.md](install.md) | Install guide: one command, then the manual path — the hub host, install and verify a node, upgrade by hand or from a timer, uninstall |
| [nginx-requirements.md](nginx-requirements.md) | What the hub needs from the reverse proxy in front of it, handed to the Ansible repository that owns that host |
| [hub-host-node-requirements.md](hub-host-node-requirements.md) | What the Ansible play must do to install the agent on the hub host too and keep it on the hub's target, and how to check it |
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
| [0005](decisions/0005-poc-stack.md) | POC stack: Go, SQLite, server-side HTML | accepted, amended by [0035](decisions/0035-mission-control-is-rendered-by-the-hub.md) |
| [0006](decisions/0006-alerting-rules.md) | Alert on transitions; only critical is instant | accepted, amended by [0032](decisions/0032-thresholds-are-set-in-the-interface.md) |
| [0007](decisions/0007-public-repository.md) | Public repository from the first commit; nothing personal in it | accepted, amended by [0032](decisions/0032-thresholds-are-set-in-the-interface.md) |
| [0008](decisions/0008-english-repo-bilingual-ui.md) | The repository is English; the interface is bilingual | accepted |
| [0009](decisions/0009-development-process.md) | Specs for behaviour, plans for work, ADRs for decisions | accepted |
| [0010](decisions/0010-agent-configuration.md) | The agent's configuration lives on the hub and arrives in the ingest response | accepted, amended by [0032](decisions/0032-thresholds-are-set-in-the-interface.md) and [0033](decisions/0033-a-subject-is-a-series.md) |
| [0011](decisions/0011-quality-gates.md) | Quality is enforced by tooling, not by attention | accepted |
| [0012](decisions/0012-threshold-model.md) | Disk thresholds are a floor plus a proportional band | superseded by [0033](decisions/0033-a-subject-is-a-series.md) |
| [0013](decisions/0013-relative-hysteresis.md) | Hysteresis is a relative margin, not a fixed number of points | accepted, amended by [0033](decisions/0033-a-subject-is-a-series.md) |
| [0014](decisions/0014-macos-available-space.md) | Free space is what the system calls available; cgo in the darwin sensor only | accepted |
| [0015](decisions/0015-evaluation-on-a-tick.md) | Evaluation runs on its own tick, never inside a request | accepted |
| [0016](decisions/0016-leaving-critical-is-instant.md) | Leaving critical is announced as instantly as entering it | accepted |
| [0017](decisions/0017-one-spec-and-decision-gates.md) | One document per unit of work; gates on decisions, not documents | accepted |
| [0018](decisions/0018-history-through-the-api.md) | History is served by the hub's API; a chart is one of its consumers | accepted |
| [0019](decisions/0019-deployment-layout.md) | Install into system paths, with one environment file per binary | accepted |
| [0020](decisions/0020-agent-reads-its-environment-file.md) | The agent reads its own environment file; no shell sources it | accepted |
| [0021](decisions/0021-shell-is-linted-too.md) | The shell the project ships is linted like its Go | accepted |
| [0022](decisions/0022-updates-are-pulled.md) | Updates are pulled by an updater of their own; the hub names the version | accepted |
| [0023](decisions/0023-proxy-holds-the-web-perimeter.md) | The proxy authenticates the web; the hub authenticates only ingest | accepted |
| [0024](decisions/0024-the-hub-follows-a-target-with-a-kept-install-script.md) | The hub follows a target file with a kept copy of the install script | accepted |
| [0025](decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md) | The hub checks hourly and downloads a binary only to install it | accepted |
| [0026](decisions/0026-reader-time-zone-from-the-browser.md) | The reader's time zone comes from the browser in a cookie; pages stay server-rendered | accepted, amended by [0037](decisions/0037-skins-are-tabs-and-the-root-opens-the-last-one.md) |
| [0027](decisions/0027-the-hub-installer-reuses-the-binary-in-place.md) | The hub's installer reuses a binary in place that is already its release | accepted |
| [0028](decisions/0028-agents-follow-a-target-the-hub-serves.md) | Agents follow a target the hub serves under `/api/v1/agent/` | accepted |
| [0029](decisions/0029-pages-refresh-by-fetching-their-own-address.md) | Pages refresh themselves by fetching their own address | accepted |
| [0030](decisions/0030-the-state-api-reports-the-stored-verdict.md) | The State API reports the verdict evaluation stored | accepted, amended by [0033](decisions/0033-a-subject-is-a-series.md) and [0036](decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md) |
| [0031](decisions/0031-a-table-of-series.md) | A table of series beside the measurements | accepted, amended by [0033](decisions/0033-a-subject-is-a-series.md) |
| [0032](decisions/0032-thresholds-are-set-in-the-interface.md) | Thresholds are set in the interface and stored with the data | accepted |
| [0033](decisions/0033-a-subject-is-a-series.md) | A subject is a series, and a threshold is one comparison per level | accepted, amended by [0034](decisions/0034-a-series-without-a-sensor-still-ages.md) |
| [0034](decisions/0034-a-series-without-a-sensor-still-ages.md) | A series that names no sensor ages by its node's longest interval | accepted |
| [0035](decisions/0035-mission-control-is-rendered-by-the-hub.md) | Mission control is rendered by the hub; TypeScript waits for a skin that needs a client | accepted, amended by [0037](decisions/0037-skins-are-tabs-and-the-root-opens-the-last-one.md) |
| [0036](decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md) | An anomaly is a value outside the band its series kept the week before | accepted |
| [0037](decisions/0037-skins-are-tabs-and-the-root-opens-the-last-one.md) | Skins are tabs, each at its own address; `/` opens the one last chosen | accepted |
| [0038](decisions/0038-a-lane-is-summarised-on-read.md) | A timeline lane is summarised on read from the event log and the stored points | accepted |

## Behaviour specs

How subsystems behave, one file per subsystem, each built around a behaviour table that
tests are derived from. Required for contracts, stateful algorithms and multi-session work
(see [ADR 0009](decisions/0009-development-process.md)); rows state only what an observer
can see ([ADR 0017](decisions/0017-one-spec-and-decision-gates.md)). New specs start from
[TEMPLATE.md](specs/TEMPLATE.md), and "approved" below means reviewed and in force.

| Spec | Owns | Status |
|---|---|---|
| [ingest.md](specs/ingest.md) | `/api/v1/ingest` contract: request, response, config delivery; the node's target under `/api/v1/agent/` | approved |
| [hub-config.md](specs/hub-config.md) | The hub's YAML file: validation, layering, per-node configuration and its version, and each node's agent target | approved |
| [disk-sensor.md](specs/disk-sensor.md) | The disk sensor: enumeration, filtering and the label contract of its metrics | approved |
| [host-sensors.md](specs/host-sensors.md) | The load, memory, uptime and failed-units sensors: what each reads on Linux and macOS, and the metrics it reports | approved |
| [agent.md](specs/agent.md) | The agent: local configuration, tick loop, delivery and configuration application | approved |
| [evaluation.md](specs/evaluation.md) | Levels, hysteresis, the event log, silence, digests and the notifier boundary | approved |
| [history.md](specs/history.md) | The history series, `/api/v1/series`, `/api/v1/history` and the drill-down page | approved |
| [state.md](specs/state.md) | `/api/v1/state`: subjects, stored levels, staleness and anomalies, and the levels on `/debug` | approved |
| [anomaly.md](specs/anomaly.md) | Each series' norm, how far its newest value lies from it, and the anomaly rank | approved |
| [mission-control.md](specs/mission-control.md) | `/board`, mission control: what needs attention, in what order, and the move of the table to `/debug` | approved |
| [timeline.md](specs/timeline.md) | `/timeline`: what needs attention now, each node's last 24 hours, and the levels that changed | approved |
| [thresholds.md](specs/thresholds.md) | The page that sets what a series is judged by — what may be stored as a threshold, and whether it may be shown as unusual | approved |
| [release.md](specs/release.md) | How a merge tags itself, what a tag publishes, how a release is signed, and how an artifact is checked | approved |
| [installer.md](specs/installer.md) | One command that installs or upgrades a hub or an agent from a signed release, the hub following the version its host names, and an agent following the one the hub names | approved |
| [deployment.md](specs/deployment.md) | The install layout, the units — the hub's and the agents' update timers among them — and what `install-agent.sh` does to a node | approved |
| [web.md](specs/web.md) | What every hub page shares: the tabs between skins and the one `/` opens, the reader's time zone, how a page says which zone it used, how an open page keeps itself current, and the shell | approved |

## Design notes

Reasoning from working sessions, including options that were rejected.

| Date | Topic |
|---|---|
| [2026-08-28](log/2026-08-28-concept.md) | Project start: visual concepts, architecture, naming |
| [2026-08-29](log/2026-08-29-stage-1-decisions.md) | Stage 1: locale negotiation, one wire package, what the live run caught |
| [2026-08-29](log/2026-08-29-munin-hub-plugin.md) | Proposal: a munin plugin on the hub host for off-the-shelf history graphs |
| [2026-08-29](log/2026-08-29-stage-2-decisions.md) | Stage 2: what the evaluation design rejected, and what three review rounds changed |
| [2026-09-06](log/2026-09-06-release-signing.md) | Release signing: the tools, the manifest and the key placement that lost |
| [2026-09-13](log/2026-09-13-hub-host-node.md) | The agent on the hub host: loopback, and why every play restarts rather than detecting a change |
| [2026-09-14](log/2026-09-14-auto-tag.md) | A merge tags itself: how the tag reaches the release, and the tokens and tools that lost |
| [2026-09-17](log/2026-09-17-agent-updater.md) | Agents follow the hub's target: what three spec reviews changed before the code |
| [2026-09-18](log/2026-09-18-vanished-volumes.md) | Vanished volumes: why `/` hides by `removable`, ages by the hub's clock, and groups snapshots |
| [2026-09-19](log/2026-09-19-state-api.md) | State API: why it comes before the MVP, and what the spec reviews changed |
| [2026-09-20](log/2026-09-20-history-streaming.md) | History holds its answer: reducing points as they stream, and why the series became a table |
| [2026-09-20](log/2026-09-20-thresholds-in-the-interface.md) | Thresholds move into the interface: why the rule engine was not generalised, and what three reviews changed |
| [2026-09-22](log/2026-09-22-mvp-scope.md) | MVP scope: why it is infra only, why backups wait, and whose memory figure a Mac reports |
| [2026-09-23](log/2026-09-23-mission-control.md) | Mission control: the engine slice before the board, no health number, server-rendered, and which deviation |
| [2026-09-24](log/2026-09-24-timeline.md) | The timeline and tabs between skins: anomalies left to the log, the choice made by a tab click, and past freshness |

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
