# 0044. A sensor takes its parameters from the hub, in its entry of the configuration

- **Status:** accepted
- **Date:** 2026-10-03
- **Source:** [site traffic spec](../specs/site-traffic.md),
  [design notes](../log/2026-10-03-site-traffic.md),
  [#51](https://github.com/pravbeseda/monitor/issues/51)

## Context

Every sensor so far finds what it reads on its own: the disk sensor lists mounted volumes,
Time Machine keeps its own list of destinations. Reading a web site's access log needs a
path, and which logs belong to which site says something about an installation, so by
[0007](0007-public-repository.md) it has no default and lives in the hub's file. By
[0010](0010-agent-configuration.md) the agent holds nothing but the hub address and its
token, and a sensor's entry in the delivered configuration carries `enabled` and `interval`
only.

## Decision

- **A sensor's parameters ride in its own entry** of the configuration the ingest response
  delivers, beside `enabled` and `interval`. The hub resolves them, and the agent hands each
  sensor its entry without reading the parameters.
- **Each parameter is the sensor's own key**, documented in that sensor's spec and validated
  by the hub at startup; there is no free-form map. The first is `access_log`'s `sites`
  ([0045](0045-sites-are-a-node-their-hosts-report-for.md)).
- **A parameter changes the configuration version** of the nodes it reaches, and of no
  other, as any delivered value does.

## Consequences

- [0010](0010-agent-configuration.md) holds: the agent still merges nothing and keeps no
  file. Only the shape of a sensor's entry grows.
- A sensor reads its parameters from the agent that holds the delivered configuration, as
  the disk sensor already reads the filesystem allow-list; the `Sensor` interface does not
  change. The agent's own loop reads one key of the entry beside `enabled` and
  `interval`: `node`, the node the sensor's measurements belong to
  ([0045](0045-sites-are-a-node-their-hosts-report-for.md)).
- A backup sensor that names a path ([#51](https://github.com/pravbeseda/monitor/issues/51))
  can take the same route.

## Alternatives

- **A parameters file on the node** — rejected: it is the second file
  [0010](0010-agent-configuration.md) rules out, and editing it on every server is the drift
  that decision exists to prevent.
- **A free-form `params` map per sensor** — rejected: the hub could not validate it, and a
  typo in a path would surface only as a sensor error on one node, long after startup.
- **Top-level keys of the delivered configuration**, as `filesystems` and `skip_mounts` are
  — rejected: they belong to the disk sensor yet sit beside the tick, and every further
  sensor would add keys every agent receives.
