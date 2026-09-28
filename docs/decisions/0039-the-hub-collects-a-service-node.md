# 0039. The hub collects a service node itself, through the same path an agent reports on

- **Status:** accepted
- **Date:** 2026-09-27
- **Source:** [services spec](../specs/services.md),
  [design notes](../log/2026-09-27-cloud-storage.md)

## Context

Watching how much space a cloud storage account has left is a reading that belongs to no
machine. Everything the hub shows is arranged by node: a lane per node on the timeline, a
node's name on every item of the board, silence judged per node. A reading collected by the
agent of some server would stand under that server's name, and a full Drive would turn the
server's lane red although nothing is wrong with the server — the meaning the core exists to
compute would be wrong.

Reading the quota needs an OAuth refresh token for the account, which is a secret. By
[0010](0010-agent-configuration.md) an agent holds nothing but the hub address and its token,
and by [0007](0007-public-repository.md) a secret lives in an environment file.

Such a token dies quietly — revoked, unused for six months, or issued by a client left in
Testing — and a sensor that fails leaves its series stale, which by
[attention](../specs/attention.md) raises nothing while every series of the node
is stale.

## Decision

- **A reading of an online service belongs to a service node**: a node of the compiled-in,
  now reserved class `service`, listed in the hub's file with the sensors it enables, and collected by the
  hub itself. It has no token, since no agent speaks for it.
- **The hub runs the agent's own loop for it**, with the sensors the hub carries, and hands
  each report to ingest in process instead of over HTTP. The checks a measurement passes,
  staleness and the configuration it runs are therefore the ones every node has.
- **A service node is seen only when it stores a measurement**, and when the hub first
  records it, so that one refused from the start falls silent too. It cannot sleep, so a
  silence means its service stopped answering, and the silence message is the alert that the
  credentials need attention.
- **The service's credentials live in the hub's environment file**, and are required at
  startup only while a service node enables the sensor that needs them.
- **A sensor is either the hub's or an agent's.** The hub carries only service sensors, and
  a service sensor runs only on a service node; the file is refused otherwise.
- **The service is read by our own code over its HTTP API**, with `golang.org/x/oauth2`
  refreshing the access token. The refresh token is obtained once, outside the monitor.

## Consequences

- [0002](0002-push-not-pull.md) is untouched: the hub still never reaches a node. It reaches
  an online service, the way an agent's sensor would.
- [0003](0003-sensors-are-modules.md) holds with a second host: a sensor is an in-process
  module of whichever loop runs it, the agent's on a node or the same loop inside the hub.
- The hub grows a collector: one agent loop per service node, stopped with the hub.
- Deploying a service node changes nothing on any machine: the hub's environment file,
  which the deployment already writes, gains the service's variables.
- Each further service — Yandex Disk, Dropbox — is a sensor of its own, a registration with
  that service, and its variables in the same file. A second account of the same service
  would name its variables per node, as `token_env` names a node's token; one account needs
  no such indirection yet.

## Alternatives

- **The series under the node whose agent collects it** — rejected: the reading would be
  attributed to a machine it says nothing about, and moving it to a node of its own later
  would change the series' identity and split its history.
- **A subject with `node: null`**, as [state](../specs/state.md) foresaw for manual input —
  rejected: every bound that ages a subject is read from a node's configuration, and the
  timeline, the board and silence are all per node; a service node gets all of them for free.
- **A second agent on some host, reporting under a node name of its own** — rejected: the
  install layout holds one agent per host, and parameterising its paths, units and updater
  costs more than the collector, and adds an agent to every deployment.
- **`rclone about` as the sensor** — rejected: one sensor for some forty services, but it
  adds a package to the host and a second secrets file that rclone rewrites on every refresh,
  which the deployment's file management then reports as drift. Its shared Google client is
  announced for retirement, so it saves no registration with Google either.
- **Silence by heartbeat, as for an agent** — rejected: the hub would keep the node alive
  while its only sensor failed, and a dead token would show nowhere but `/debug`.
- **An authorization command of our own** — rejected for now: it runs once per account's
  lifetime, and Google's OAuth Playground issues the same refresh token for the deployment's
  own client with no code to maintain.
