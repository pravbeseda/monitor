# 0045. Sites are a node of their own, which the agent of each site's host reports for

- **Status:** accepted
- **Date:** 2026-10-03
- **Source:** [site traffic spec](../specs/site-traffic.md),
  [design notes](../log/2026-10-03-site-traffic.md)

## Context

A web site's traffic and errors are read from its access log, which lies on the server that
serves it. One server may serve many sites and another none, and a site may move between
servers. Everything the hub shows is arranged by node, as
[0039](0039-the-hub-collects-a-service-node.md) recounts: under its server's name, a site's
burst of errors would turn the whole server's lane red, and a site that moved would start a
new series and lose its history.

The hub never reaches a node ([0002](0002-push-not-pull.md)), so it cannot read the logs
itself the way it collects a service node. The agent on the site's host is the only reader.
Today a token speaks for exactly one node: ingest refuses a request naming another.

## Decision

- **The sites are one node of the compiled-in, reserved class `sites`.** Its entry in the
  hub's file lists each site with the node that hosts it and the path of its log. Like a
  service node, it has no token, and at most one node is of that class.
- **The host's agent reads the logs and reports for that node.** Each site reaches its
  host's `access_log` sensor as a parameter
  ([0044](0044-a-sensor-takes-its-parameters-from-the-hub.md)), and its measurements are
  marked as belonging to the sites node.
- **Ingest stores such a measurement only from the site's host**: one naming the sites
  node, labelled with a site the file gives to the requesting node. A measurement of the
  sites node for any other site is dropped and logged while the rest of the request is
  stored, so that a host still holding an old list of sites receives the new one. Naming any
  other node refuses the whole request, as today.
- **The sites node is seen when it stores a measurement**, and only then: unlike a service
  node it is not recorded at startup, since no loop of the hub's runs it. A host that stops
  reporting falls silent itself, and its sites' series age.

## Consequences

- A site is addressed by the sites node and its `site` label wherever it is shown, whatever
  server serves it: moving it to another host is one edited line, and its history goes on.
- The rule "one token, one node" becomes "one token, one node, and the sites it hosts". The
  check stays in ingest, before anything is stored.
- The sites node has no agent of its own, so its agent version and manifest stay empty.
- A dead host shows as its own silence, not as the sites node's, unless it hosted every
  site.

## Alternatives

- **The series under the host's node, labelled by site** — rejected: a site's errors would
  colour its server, and moving it would split its history.
- **nginx sending its log to the hub over syslog**, so that the hub collects the sites node
  itself like a service node — rejected: the hub listens on loopback behind the proxy
  ([0023](0023-proxy-holds-the-web-perimeter.md)), so it would need an open port taking
  plain text from the internet, and lines sent while the hub restarts are lost.
- **A lane per site, grouped from the hosts' series when shown** — rejected: the series
  would still belong to the host, so a move would still split history, and a lane would
  stop meaning a node.
- **A node per site** — rejected: each site would need a silence window, and a lane per
  site crowds the timeline with readings that rarely change.
- **A web analytics service's API, read as a service node** — rejected: ad blockers cut its
  counter from a share of visits, and it sees no server errors.
- **Refusing the whole request for a site the host does not hold** — rejected: the
  configuration rides on a 200, so a host whose site was moved away would never learn it,
  and would lose its own readings and fall silent until its agent restarted.
- **Several sites nodes** — rejected until wanted: one is enough to group every site, and a
  second would need its own interval on the same host's sensor.
