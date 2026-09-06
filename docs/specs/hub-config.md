# Spec: Hub configuration

- **Status:** approved
- **Owns:** `internal/config` (hub): the YAML file, its validation, and the per-node
  configuration the ingest response delivers
- **Decisions:** [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0011](../decisions/0011-quality-gates.md)

## Purpose

The hub reads one YAML file at startup and turns it into two things: the tokens ingest
authenticates with, and a flat per-node configuration with a version, which
[ingest](ingest.md) hands to the agent. It resolves the layering of
[0010](../decisions/0010-agent-configuration.md) — sensor default → node class → node —
so that nothing downstream merges anything.

It does not evaluate: the `rules` and `volumes` keys, volume roles and the meaning of
`silence_after` belong to [evaluation](evaluation.md), which owns their validation too.
It parses `digest` and `notify` too, but what they mean and what refuses them belongs to
that spec. This one owns the tokens and what reaches an agent.

## The file

The deployment file lives on the server and never in this repository; the repository
ships `config.example.yaml` with synthetic names
([0007](../decisions/0007-public-repository.md)).

```yaml
# Product defaults are compiled in; every key below is optional except `nodes`.
base_tick: 5m
filesystems: [apfs, ext4, xfs, btrfs, zfs, ntfs]
skip_mounts: ["/System/Volumes/", "/Library/Developer/CoreSimulator/"]

sensors:
  disk: { interval: 15m }

classes:
  laptop:
    profile: [disk]
    silence_after: 48h
    sensors:
      disk: { interval: 1h }
  server:
    profile: [disk]
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
filesystem allow-list and the skip list above, `disk` every 15m, classes `laptop` (profile
`[disk]`, disk every 1h) and `server` (profile `[disk]`). The skip list names mount points
no one watches — the system volumes of a Mac and the simulator images — and says nothing
about any installation.

**Deployment settings** (no defaults, absent means a startup error): the `nodes` map, each
node's `class` and `token_env`. Tokens themselves live in the environment, never in the
file.

Each node names one environment variable holding its token: a handful of nodes needs no
second secrets file, and `EnvironmentFile=` with mode 600 is what systemd already does.

**What reaches the agent** is only the flat result — base tick, filesystem allow-list,
skip list, and the enabled sensors with their intervals, in the shape [ingest](ingest.md)
documents.
`silence_after` and thresholds stay on the hub.

## Behaviour

One row = one test. Anchors: `spec: hub-config.md#<heading>`.

### Startup

| Configuration | Result |
|---|---|
| `--version` given | the version on stdout and exit 0, before any setting is required ([release.md](release.md#the-version-a-binary-reports)) |
| `--config` not given | startup error: the path is a deployment setting and has no default |
| `--db` not given | startup error: the database path is a deployment setting too |
| `--listen` not given | the hub binds to `127.0.0.1:8080`, a product default that names no installation |
| file missing or unreadable | startup error naming the path |
| not valid YAML | startup error naming the path and the position |
| a key the hub does not know, at any level | startup error naming the key |
| `nodes` missing or empty | startup error: a hub with no nodes serves nobody |
| a node without `token_env` | startup error naming the node |
| `token_env` names a variable that is unset or empty | startup error naming the variable |
| a token shorter than 32 characters | startup error naming the variable |
| two nodes sharing one `token_env` | startup error naming both nodes |
| a node whose `class` is neither compiled in nor in the file | startup error naming node and class |
| a class the file introduces without `silence_after` | startup error naming the class: a silence window is a deployment setting |
| a duration that Go cannot parse, or that is zero or negative | startup error naming the key |
| a sensor interval below the `base_tick` a node resolves to | startup error: a sensor collects above the tick |
| `filesystems` present and empty | startup error: no volume would ever be collected |
| a sensor in a profile with no interval at any layer | startup error naming the sensor, whether or not a node uses that class |
| a valid file | the hub starts and every listed node resolves |

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
| a hub-only value changes (`silence_after`, thresholds) | unchanged: the agent is never sent it |
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
  resolved node.
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
- **The file edited while the hub runs**: no effect until a restart.

## Out of scope

- Thresholds, volume roles and `silence_after` semantics → [evaluation](evaluation.md),
  [0012](../decisions/0012-threshold-model.md).
- How the resolved configuration is encoded and when it is sent → [ingest](ingest.md).
- Applying the configuration on the agent → agent spec.
- Editing the configuration from the web page → stage 3.

## Open questions

None.
