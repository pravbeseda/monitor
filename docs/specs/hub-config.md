# Spec: Hub configuration

- **Status:** approved
- **Owns:** `internal/config` (hub): the YAML file, its validation, and the per-node
  configuration the ingest response delivers
- **Decisions:** [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0011](../decisions/0011-quality-gates.md),
  [0028](../decisions/0028-agents-follow-a-target-the-hub-serves.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md)

## Purpose

The hub reads one YAML file at startup and turns it into two things: the tokens ingest
authenticates with, and a flat per-node configuration with a version, which
[ingest](ingest.md) hands to the agent. It resolves the layering of
[0010](../decisions/0010-agent-configuration.md) — sensor default → node class → node —
so that nothing downstream merges anything.

It does not evaluate: the meaning of `silence_after` belongs to
[evaluation](evaluation.md), which owns its validation too. It parses `digest` and `notify`
too, but what they mean and what refuses them belongs to that spec. This one owns the
tokens, what reaches an agent, and the version each node's agent is told to follow.

## The file

The deployment file lives on the server and never in this repository; the repository
ships `config.example.yaml` with synthetic names
([0007](../decisions/0007-public-repository.md)).

```yaml
# Product defaults are compiled in; every key below is optional except `nodes`.
base_tick: 5m
agent_target: latest
filesystems: [apfs, ext4, xfs, btrfs, zfs, ntfs]
skip_mounts: ["/System/Volumes/", "/Library/Developer/CoreSimulator/"]

sensors:
  disk: { interval: 15m }

classes:
  laptop:
    # A written profile replaces the compiled-in one, so the class misses the sensors
    # later releases add to it; leave it out to follow them.
    profile: [disk, load, memory, uptime]
    silence_after: 48h
    agent_target: 1.4.0
    sensors:
      disk: { interval: 1h }
  server:
    profile: [disk, load, memory, uptime, systemd]
    silence_after: 10m

nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
    sensors:
      disk: { interval: 5m }
```

**Product defaults** (compiled in, overridable at every layer): `base_tick` 5m, the
filesystem allow-list and the skip list above, `disk` every 15m, the
[host sensors](host-sensors.md) `load` and `memory` every 5m and `uptime` and `systemd` every
15m, and classes
`laptop` (profile `[disk, load, memory, uptime]`, disk every 1h) and `server` (profile
`[disk, load, memory, uptime, systemd]`). A compiled-in interval never stops the hub
starting: where it is shorter than the tick a node resolves to, the sensor collects every
tick instead. A hub upgrades itself unattended
([0025](../decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md)),
so a release that brings a sensor must not turn a tick the file chose into a crash loop.
The skip list names mount points no one watches — the system volumes of a Mac and the
simulator images — and says nothing about any installation.

**Deployment settings** (no defaults, absent means a startup error): the `nodes` map, each
node's `class` and `token_env`. Tokens themselves live in the environment, never in the
file.

Each node names one environment variable holding its token: a handful of nodes needs no
second secrets file, and `EnvironmentFile=` with mode 600 is what systemd already does.

**The agents' target** is `agent_target`: `latest` or one `MAJOR.MINOR.PATCH`, at the top
level, in a class or in a node, the most specific winning
([0028](../decisions/0028-agents-follow-a-target-the-hub-serves.md)). It has no default — a
file naming none has every node's updater install nothing — and it never reaches the ingest
response: [ingest](ingest.md#the-agents-target) serves it to a node that asks.

**What reaches the agent** is only the flat result — base tick, filesystem allow-list,
skip list, and the enabled sensors with their intervals, in the shape [ingest](ingest.md)
documents. `silence_after` stays on the hub.

**Thresholds are not in the file at all.** What a series is judged by is entered on the
page and stored beside the measurements ([thresholds.md](thresholds.md),
[0032](../decisions/0032-thresholds-are-set-in-the-interface.md)), so no key here carries a
number, a direction or a level.

## Behaviour

One row = one test. Anchors: `spec: hub-config.md#<heading>`.

### Startup

| Configuration | Result |
|---|---|
| `--version` given | the version on stdout and exit 0, before any setting is required ([release.md](release.md#the-version-a-binary-reports)) |
| `--config` not given | startup error: the path is a deployment setting and has no default |
| `--db` not given | startup error: the database path is a deployment setting too |
| neither `--listen` nor `MONITOR_LISTEN` given, or `MONITOR_LISTEN` set empty | the hub binds to `127.0.0.1:8080`, a product default that names no installation |
| `MONITOR_LISTEN` set, `--listen` not given | the hub binds to that address, and the line it logs at startup names the address it acquired |
| the address is already taken | startup error naming it, and no line claiming the hub is listening: a journal that announces an address it never got is what an operator reads while hunting the port |
| both given | `--listen` wins, so a run by hand can reach a port the service does not use |
| either of them names an address that is not loopback | startup error naming the address: the hub is reached through a reverse proxy ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)), and a hub on a public interface serves the pages and the read API to anyone |
| file missing or unreadable | startup error naming the path |
| not valid YAML | startup error naming the path and the position |
| a key the hub does not know, at any level | startup error naming the key |
| `rules` at any level, or a node's `volumes` — the two keys thresholds used to live in | the hub starts; one warning per key names it and says thresholds are now set on the page, and no number inside it is used ([thresholds.md](thresholds.md)) |
| `nodes` missing or empty | startup error: a hub with no nodes serves nobody |
| a node without `token_env` | startup error naming the node |
| `token_env` names a variable that is unset or empty | startup error naming the variable |
| a token shorter than 32 characters | startup error naming the variable |
| two nodes sharing one `token_env` | startup error naming both nodes |
| two variables holding the same token | startup error naming both nodes and no value: a token is what tells the hub which node is asking |
| a node whose `class` is neither compiled in nor in the file | startup error naming node and class |
| a class the file introduces without `silence_after` | startup error naming the class: a silence window is a deployment setting |
| a duration that Go cannot parse, or that is zero or negative | startup error naming the key |
| an interval the file sets below the `base_tick` a node resolves to | startup error naming the sensor: a sensor collects above the tick |
| `filesystems` present and empty | startup error: no volume would ever be collected |
| a sensor in a profile with no interval at any layer | startup error naming the sensor, whether or not a node uses that class |
| `agent_target` at the top level, in `classes.<name>` or in `nodes.<name>`, that is neither `latest` nor one `MAJOR.MINOR.PATCH` — `1.4`, `v1.4.0`, `01.4.0`, a component of ten digits or more, `""`, or present with no value | startup error naming the key and the class or node it is in |
| a valid file | the hub starts and every listed node resolves |

**The `rules` and `volumes` exception is transitional.** It exists because a hub that
upgrades itself hourly
([0025](../decisions/0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md))
meets the file it already has, and a startup error there is a crash loop rather than a
message anyone reads. A later release removes the exception and both keys are unknown keys
again, so the two are deleted from the file at the upgrade
([install.md](../install.md#upgrading-past-the-threshold-change)), not left to rot in it.

### Resolution

The node is listed in `nodes`; the layers apply most-specific-last.

| Configuration | Resolved for that node |
|---|---|
| the node entry sets nothing but `class` and `token_env` | product defaults, plus the class profile and its intervals |
| top-level `sensors.disk.interval` | wins over the compiled-in default |
| the class sets `sensors.disk.interval` | wins over the top-level sensor default |
| the node sets `sensors.disk.interval` | wins over the class |
| the node sets `sensors.<s>.enabled: false` for a sensor in its profile | that sensor is delivered as `enabled: false`, so the agent stops running it |
| the node sets `sensors.<s>.enabled: true` for a sensor outside its profile | that sensor is delivered, at its resolved interval |
| the class sets `base_tick`, `filesystems` or `skip_mounts` | wins over the top level; a node entry wins over the class |
| `skip_mounts` set to an empty list | nothing is skipped: an empty list is a value, not an omission |
| a sensor no layer mentions | absent from the delivered configuration |
| a node of the compiled-in `server` class, the file setting no `profile` for it | `load` and `memory` every 5m; `disk`, `uptime` and `systemd` every 15m |
| a node of the compiled-in `laptop` class, the file setting no `profile` for it | `load` and `memory` every 5m, `uptime` every 15m, `disk` every 1h |
| the file sets `profile: [disk]` for a compiled-in class | only `disk`: a profile in the file replaces the compiled-in one, it does not add to it |
| the file sets `base_tick: 1h` for `laptop`, and no interval for the host sensors | the hub starts, and `load`, `memory` and `uptime` collect every 1h: a compiled-in interval below the tick is raised to it |
| top-level `agent_target` | the node's target |
| the class sets `agent_target` | wins over the top level |
| the node sets `agent_target` | wins over the class |
| no layer sets `agent_target` | the node has no target |

### Configuration version

The version is the first 12 hex characters of the SHA-256 of the resolved configuration
in canonical JSON: nothing to bump by hand, and it changes exactly when what the agent
receives changes. It is opaque to the agent, which only compares it for equality; the hub
logs both versions when it delivers a new one.

| Situation | Version |
|---|---|
| the same file and environment, hub restarted | unchanged: the version is derived, never stored |
| a value that reaches this node changes | a different version |
| another node's settings change | unchanged for this node |
| a hub-only value changes (`silence_after`, `agent_target`) | unchanged: the agent is never sent it |
| a threshold is edited on the page | unchanged: no threshold is in the file, and none reaches an agent ([thresholds.md](thresholds.md)) |
| two nodes resolve to an identical configuration | the same version — it identifies the configuration, not the node |

### Tokens

| Situation | Result |
|---|---|
| every `token_env` is set at startup | the tokens are held in memory for ingest to compare |
| a token is rotated on the server | the new value takes effect when the hub restarts |
| a token appears anywhere in a log line, an error or a response | never happens; errors name the variable, never its value |

## Invariants

- Resolution is pure: the same file and environment yield the same configuration and the
  same versions, in any order and after any restart.
- The configuration version is a function of the delivered configuration alone, so a value
  the agent never sees cannot make it re-fetch.
- A resolved configuration carries only what the agent acts on — base tick, filesystem
  allow-list, skip list, sensors and intervals.
- No deployment setting has a fallback: the hub either starts fully configured or does not
  start ([0007](../decisions/0007-public-repository.md)).
- The whole file is validated, not only the parts a node references: every layer's
  durations and allow-lists, and every class down to the sensors its profile promises — a
  class nobody uses yet is resolved as if a node did, so a typo surfaces at startup rather
  than on the day the class is wired to a node.
- No check compares one layer with another. A more specific layer may lower the base tick
  or replace a list, so an intermediate layer is not required to stand alone; the one
  comparison that needs a final tick — a sensor collecting faster than it — is made on the
  resolved node, and refuses only an interval the file wrote.
- The file is read once, at startup: nothing re-reads it while the hub runs.

## Edge cases

- **A node the hub has never seen**: it is a node listed with a token and nothing else; it
  resolves from its class and the product defaults. A node absent from `nodes` has no
  token, so ingest rejects it before resolution runs
  ([ingest](ingest.md#authentication)).
- **A class named both in code and in the file**: the file wins key by key, so a file that
  sets only `silence_after` keeps the compiled-in profile.
- **A sensor in the agent's manifest that no layer enables**: it stays off. The manifest
  records what the agent could run; the configuration decides what it does run.
- **A sensor enabled for a node whose manifest lacks it**: it is delivered anyway and the
  agent ignores what it cannot run; the hub does not filter by manifest in stage 1.
- **A hub upgraded to a release whose compiled-in profiles changed**: every node that takes
  its profile from code resolves differently, so each is delivered a new
  [configuration version](#configuration-version) on its next request, and its
  sensorless series age by a bound that follows its new sensors
  ([evaluation](evaluation.md#freezing)). A node whose file sets its own `profile` is
  untouched.
- **The file edited while the hub runs**: no effect until a restart.
- **One node held back while its class follows**: it is pinned to the version it runs. No
  value takes a target away at a more specific layer; a node that must not change at all has
  its update timer stopped.

## Out of scope

- `silence_after` semantics → [evaluation](evaluation.md).
- What a series is judged by, and where it is set → [thresholds.md](thresholds.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md).
- How the resolved configuration is encoded and when it is sent → [ingest](ingest.md).
- Applying the configuration on the agent → agent spec.
- Editing the configuration from the web page → stage 3.

## Open questions

None.
