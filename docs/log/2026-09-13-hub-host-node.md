# 2026-09-13 — The agent on the hub host: why every play restarts

Context: [hub-host-node-requirements.md](../hub-host-node-requirements.md), the requirement
handed to the Ansible repository so the play that installs the hub also installs an agent on
the same host.

## Loopback, not the public name

The agent on the hub host reports to the hub's listen address over plain HTTP rather than
through the proxy. Its token then never leaves the host, it does not spend the proxy's rate
limit, and a proxy outage cannot make it silent. Rejected: the public HTTPS name, which would
exercise the proxy end to end but ties the host's own disk reports to nginx.

An earlier draft claimed a silent hub-host node would mean the hub is down. It would not:
silence is computed by the hub's own tick, so a hub that is down reports nothing at all.
Watching the hub itself is an external check, and not part of this requirement.

## Restart every play, not on change

Four review rounds tried to make a second play report no change. Each fix moved the problem:

- **A handler on the templating task** loses its "changed" flag when a play stops before
  handlers run, and the next play leaves the hub on the old configuration for good.
- **Comparing a service's start time with file modification times** survives a stopped play,
  but `systemctl show --timestamp=unix` gives whole seconds while `stat` gives fractions, so
  a restart within the same second as the write looks stale on every run and restarts again.
  Microsecond times through `busctl` fix that and are too obscure to hand to a role written
  without this context.
- **Comparing content** — the binary's version, the service being enabled and active, the
  lines of `agent.env` checked through a `no_log` grep with the token on stdin — leaves one
  gap: a binary replaced under a running service that was never restarted. Closing it needs
  `/proc/<pid>/exe` reading "(deleted)", and adds more tasks that carry the token.

Chosen: every play enables and restarts the hub, waits until the hub's own process holds its
listen address, and runs the agent's installer. Waiting for the address to accept a
connection was rejected in review: on an address another process already holds, the hub
restart-loops while that process answers. The play is run by hand and rarely; a hub restart is a moment of `502` the
proxy recovers from, and the agent's first tick runs as soon as it starts. The cost is a play
that always reports a change for both services.
