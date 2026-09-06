# 0010. The agent's configuration lives on the hub and arrives in the ingest response

- **Status:** accepted; the "no second endpoint" clause is amended by
  [0022](0022-updates-are-pulled.md), which has the updater ask the hub for the target
  version over an endpoint of its own, because only a running agent makes an ingest request
- **Date:** 2026-08-28
- **Source:** [POC](../poc.md) questions 3 and 5

## Context

Intervals, thresholds and the set of active sensors have to be set centrally: editing files
on four machines guarantees they diverge within a month. But the hub cannot reach an agent —
a node may sit behind NAT, a VPN or a firewall, and by [0002](0002-push-not-pull.md) the
agent is always the side that opens the connection.

Sensors will also grow to dozens. A server has no battery, a laptop has no RAID; naming
every sensor for every node by hand does not scale, and switching them all on by default
means a new agent version starts sending metrics nobody asked for.

## Decision

**The agent holds nothing but the hub address and its token.** Everything else — which
sensors run, how often, with which thresholds — comes from the hub.

**Delivery rides on the existing request.** The agent posts measurements together with the
version of the configuration it currently holds; when that differs from the hub's, the
response carries the new configuration. No second endpoint, no second credential, nothing
for the hub to initiate.

**Configuration is layered**, most specific winning:

```
sensor default → node class → single node → single sensor or volume
```

**The set of sensors is chosen by profile.** A node class carries one (`server` = disk, cpu,
memory, load; `laptop` = disk, battery, memory); each sensor reports whether it is applicable
on this machine; the configuration overrides per node and per sensor. A new sensor enters a
profile deliberately — it never switches itself on across the fleet.

**The agent sends a manifest** of the sensors it was built with and their applicability, so
the hub knows what can be enabled rather than trusting names typed by hand.

**A cheap base tick** (5 minutes by default) carries the heartbeat and whatever the sensors
have collected; each sensor collects on its own schedule above that tick. A configuration
change therefore takes effect within one tick, not within the slowest sensor's interval.

## Consequences

- The ingest response is part of the contract and versions with `/api/v1/`; its shape
  belongs in the ingest spec.
- The hub must serve a configuration for a node it has never seen, so an unknown node and an
  unknown volume both fall back to defaults rather than failing.
- The web page that edits this configuration comes at stage 3, once the panel has
  authentication; until then the hub reads it from YAML on the server. The model does not
  change — only who writes the file.
- The layering is the same for intervals, thresholds and sensor selection: one mechanism to
  learn, one place to implement.

## Alternatives

- **A separate `GET /api/v1/config` endpoint** — rejected: a second channel and a second
  credential, and the two drift apart in time, for a gain measured in minutes.
- **Configuration files on each node** — rejected: four files on four machines diverge, which
  is the problem this decision exists to solve.
- **All sensors on by default, configuration only switching them off** — rejected: an agent
  upgrade would start collecting metrics nobody chose.
