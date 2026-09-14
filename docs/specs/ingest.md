# Spec: Ingest

- **Status:** approved
- **Owns:** `internal/ingest` (hub), the `/api/v1/ingest` contract both binaries implement
- **Decisions:** [0002](../decisions/0002-push-not-pull.md),
  [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md)

## Purpose

The ingest endpoint receives measurements from agents, stores them, and delivers the
agent's configuration in the response. It is the only channel between an agent and the
hub. It validates shape, not meaning: whether a value crosses a threshold is the
evaluation engine's business, not ingest's.

## Wire format

### Request

```json
POST /api/v1/ingest
Authorization: Bearer <per-node token>

{
  "node": "laptop-a",
  "agent_version": "0.1.0",
  "config_version": "7",
  "ts": "2026-08-28T10:00:00Z",
  "manifest": [
    { "sensor": "disk", "applicable": true },
    { "sensor": "battery", "applicable": false }
  ],
  "measurements": [
    { "metric": "disk.free_bytes", "labels": {"mount": "/", "fs": "apfs", "removable": "false"},
      "value": 123456789 },
    { "metric": "disk.free_pct",   "labels": {"mount": "/", "fs": "apfs", "removable": "false"},
      "value": 34.2, "ts": "2026-08-28T09:55:00Z" }
  ]
}
```

- `node` — must match the node the token belongs to.
- `agent_version` — the version the agent binary reports; the hub keeps the one from the
  node's latest request and shows it on its page.
- `config_version` — the configuration the agent currently holds; empty string on first
  run. Opaque to the agent: it compares for equality, never for order.
- `ts` — request time by the agent's clock, RFC 3339 UTC.
- `manifest` — the sensors this agent build contains and whether each is applicable on
  this machine ([0010](../decisions/0010-agent-configuration.md)). Sent on every request;
  it is a few hundred bytes and statelessness beats saving them.
- `measurements` — may be empty: the base tick is a valid request even when no sensor
  had anything new. A measurement's optional `ts` is its collection time, for sensors
  that collect above the base tick; absent, the request `ts` applies.
- `metric` ids match `[a-z0-9_.]+`; `value` is a finite JSON number; `labels` is a flat
  string-to-string map.
- Unknown JSON fields are ignored, so an older hub accepts a newer agent's request.

### Response

```json
200 OK
{
  "config_version": "8",
  "config": {
    "base_tick": "5m",
    "filesystems": ["apfs", "ext4", "xfs", "btrfs", "zfs", "ntfs"],
    "skip_mounts": ["/System/Volumes/", "/Library/Developer/CoreSimulator/"],
    "sensors": {
      "disk": { "enabled": true, "interval": "15m" }
    }
  }
}
```

- `config` and `config_version` are present only when the agent's `config_version`
  differs from the hub's; otherwise the body is `{}`.
- The config is the **flat, resolved** result of the layering
  (sensor default → node class → node → sensor): the hub resolves layers, the agent
  applies what it receives and never merges anything.
- The config carries only what the agent acts on: tick, sensor selection, intervals,
  filesystem allow-list, and the mount prefixes to skip. Thresholds stay on the hub — evaluation is hub-side
  ([0012](../decisions/0012-threshold-model.md)) and the agent has no use for them.
- Errors: `{ "error": "<english message>" }` with the status codes below.

## Behaviour

One row = one test. Anchors: `spec: ingest.md#<heading>`.

### Authentication

| Request | Response | Side effect |
|---|---|---|
| no `Authorization` header | 401 | nothing stored |
| token unknown to the hub | 401 | nothing stored |
| valid token, `node` ≠ token's node | 403 | nothing stored |
| valid token, matching `node` | proceeds to validation | — |

### Validation

| Request | Response | Side effect |
|---|---|---|
| body is not valid JSON | 400 | nothing stored |
| `node`, `ts` or `measurements` missing | 400 | nothing stored |
| `ts` or a measurement `ts` not RFC 3339 | 400 | nothing stored |
| a measurement missing `metric` or `value`, or `value` not a finite number | 400 | nothing stored |
| `metric` id not matching `[a-z0-9_.]+` | 400 | nothing stored |
| body larger than 1 MiB | 413 | nothing stored |
| one invalid measurement in a batch | 400 | **whole request** rejected, nothing stored |

### Storage

| Request | Response | Side effect |
|---|---|---|
| valid request | 200 | all measurements stored; node's last-seen set to hub receipt time; node's agent version replaced by the request's |
| valid request, `measurements` empty | 200 | no measurements stored; the node updated as for any valid request |
| measurement with a metric id the hub's config does not declare | 200 | stored; evaluation ignores it (out of scope here) |
| measurement identical to a stored one (same node, metric, labels, ts to the millisecond) | 200 | duplicate silently skipped |
| `manifest` differs from the stored one | 200 | stored manifest replaced |

### Configuration delivery

| Request | Response | Side effect |
|---|---|---|
| `config_version` equals the hub's for this node | 200, body `{}` | — |
| `config_version` differs | 200 with `config` + `config_version` | — |
| `config_version` empty or missing (first run) | 200 with `config` + `config_version` | — |
| node has no node-specific config on the hub | 200, config resolved from sensor defaults and node class | — |

### Limits

| Request | Response | Side effect |
|---|---|---|
| more than 60 requests per minute from one node | 429 | nothing stored |

## Invariants

- Nothing is stored unless the response is 200: a request is atomic.
- Every 200 advances the node's last-seen, measurements or not — arrival of an
  authenticated request is what "the agent is alive" means.
- Re-sending an identical batch (agent retry) changes nothing: ingest is idempotent
  over (node, metric, labels, ts), with `ts` taken to the millisecond.
- A response contains only the requesting node's configuration, never another node's
  ([0007](../decisions/0007-public-repository.md)).
- The agent's clock never sets last-seen; silence detection runs on hub receipt time.

## Edge cases

- **Malformed and from the wrong node at once**: shape is checked before ownership, so
  such a request answers 400 rather than 403. The 403 row describes a request that is
  otherwise valid.
- **Sub-millisecond timestamps**: storage keeps `ts` to the millisecond, so two readings
  of one series inside the same millisecond are one reading. Nothing collects that often —
  the finest interval the hub configures is a minute — and a fixed-width timestamp is what
  keeps the stored series sortable and readable.
- **Clock skew**: measurement `ts` is stored as sent; the hub separately records receipt
  time. A skewed agent clock corrupts history placement, never silence detection.
- **Config changed while the agent was offline**: the next request carries the old
  version and the response delivers the new config — within one base tick of the agent
  returning ([0010](../decisions/0010-agent-configuration.md)).
- **Hub restart**: all state that ingest depends on (tokens, config versions, manifests,
  last-seen) is in storage or the config file, never only in memory.
- **Newer agent, older hub**: unknown request fields are ignored; the agent must likewise
  ignore unknown response fields.

## Out of scope

- Threshold evaluation, health states, hysteresis → [evaluation](evaluation.md).
- Silence detection beyond recording last-seen → [evaluation](evaluation.md#node-silence).
- Agent-side scheduling, retry and buffering policy → agent spec.
- How sensors collect and label measurements (removable flag, mount discovery) → disk
  sensor spec.
- Editing the configuration → stage 3, [0010](../decisions/0010-agent-configuration.md).

## Open questions

None.
