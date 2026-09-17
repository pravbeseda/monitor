# 0028. Agents follow a target the hub serves under `/api/v1/agent/`

- **Status:** accepted
- **Amends:** [0022](0022-updates-are-pulled.md) point 7 (the stub runs daily), for the
  agent; [0023](0023-proxy-holds-the-web-perimeter.md) (the hub authenticates only ingest);
  answers [0024](0024-the-hub-follows-a-target-with-a-kept-install-script.md) point 7 for a
  kept script on a node
- **Date:** 2026-09-17
- **Source:** [issue #32](https://github.com/pravbeseda/monitor/issues/32), the half of
  [0022](0022-updates-are-pulled.md) that [0024](0024-the-hub-follows-a-target-with-a-kept-install-script.md)
  left unbuilt

## Context

The hub follows a target its host names; an agent stays on the version it was installed with
until someone re-runs the install command on that node. [0022](0022-updates-are-pulled.md)
settled the shape of the rest — the hub names the agents' version, the node asks for it
through the release's installer rather than through ingest or the frozen stub, and nothing on
the machine updates itself. Three things were left to the unit that builds it.

**Where the node asks.** [0023](0023-proxy-holds-the-web-perimeter.md) put every path but
`/api/v1/ingest` behind the proxy's credential, and refused a second secret per node. A node
asking for its target presents only its token, so the proxy has to let a second path through.

**How often.** 0022 said daily; [0025](0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md)
moved the hub to hourly because a merge should be visible within the hour, and a run with
nothing to change costs kilobytes.

**How many keys a node's stub carries.** [0024](0024-the-hub-follows-a-target-with-a-kept-install-script.md)
point 7 accepted one key for the hub host, where replacing the kept script is one play, and
required the question to be answered again before a stub reaches a node touched by hand.

## Decision

1. **The hub's configuration names the agents' target** as `agent_target`, `latest` or one
   `MAJOR.MINOR.PATCH`, at the top level, per class and per node, resolved most-specific-last
   like the rest of [0010](0010-agent-configuration.md). No layer naming one means the hub
   names none, and a node that asks installs nothing: following is switched on by the
   configuration, never by a default.
2. **The hub serves it at `GET /api/v1/agent/target`**, authenticated by the node's token and
   answering for the token's node alone, as plain text in the grammar of `hub.target`. `latest`
   is passed through unresolved: the hub does not know the releases, the installer does.
3. **Every path under `/api/v1/agent/` is the hub's to authenticate**, like `/api/v1/ingest`:
   the proxy lets the prefix through without its credential. What a node calls lives there
   from now on; ingest keeps its path, because agents in the field post to it.
4. **The kept script and its units reach nodes.** The same `monitor-install.sh`, at the same
   path on Debian and macOS, runs as `monitor-install.sh agent --follow-target` from
   `monitor-agent-update.service` and `.timer` on Debian and a launchd daemon on macOS, placed
   by hand or by provisioning and never by a release. The hand-over is 0024's and 0025's with
   the role `agent`; the agent's installer reads the hub's address, the node's name and the
   token from `agent.env` by [0020](0020-agent-reads-its-environment-file.md)'s rules.
5. **The agent's update runs hourly**, spread over the fleet: systemd's timer as the hub's,
   launchd's interval counted from when the daemon was loaded.
6. **A node's kept script carries one key**, as the hub host's does. A lost or rotated key is
   a kept script replaced by hand on every node.

## Consequences

- Pinning a node, canarying a version on one class and rolling the fleet back are edits to
  `hub.yaml` and a hub restart. The hub's own version is still its host's to name.
- The proxy's requirements change: the prefix passes through unchallenged, which the
  provisioning repository has to apply before a node outside the hub host can follow.
- A second hub-authenticated path means a second path an unauthenticated client reaches.
  It reads nothing but the token's own node's target, and a wrong token is answered `401`
  before anything under the prefix is routed.
- The hub, an unprivileged service facing the internet through the proxy, now chooses what
  root installs on every node that follows it. It can choose nothing unsigned and nothing
  below the floor, but a compromised hub can roll the fleet back to an older signed release.
  Pinning back is the feature; the trust it takes is stated here rather than discovered.
- The token is sent only over https, or in clear to a loopback address: an updater that sent
  it anywhere else would let whoever answers in clear choose what root installs.
- The token now leaves the node from a root script as well as from the agent. It travels in a
  header read from a pipe, never in an argument.
- The agent's rollback floor is the first release whose installer answers an agent follow run;
  an older one refuses the hand-over, as the hub's did.
- A hub older than this answers the request `404`, and a node following it installs nothing
  and reports a failed run until the hub is upgraded — which [0022](0022-updates-are-pulled.md)
  point 5 already orders first.
- A key rotation touches every node that keeps a script, laptops included. With a handful of
  machines that is minutes of hands, accepted against a second secret to keep offline that
  would not help the case that matters more, a leaked key.
- On an Intel Homebrew Mac the kept script's directory sits under `/usr/local`, which an admin
  account owns: the exposure of [issue #17](https://github.com/pravbeseda/monitor/issues/17),
  shared with the agent's binary and answered with it.
- A Mac asleep when its interval fires skips that run, as launchd documents; the next one comes
  within the hour it is awake.

## Alternatives

- **One path, `/api/v1/agent-target`, exempted beside ingest** — rejected by the operator:
  every later endpoint a node calls would be one more exemption in another repository.
- **`GET /api/v1/ingest` answering the target** — rejected: ingest stops meaning "measurements
  arrive", and a proxy that passes only `POST` there is correct today.
- **The node sends the proxy's credential as well** — rejected by
  [0023](0023-proxy-holds-the-web-perimeter.md) already: a second secret per node.
- **The target as JSON** — rejected: the reader is a shell script with no JSON parser, and the
  grammar of `hub.target` already exists on both sides.
- **The hub resolves `latest`** — rejected: the hub would have to ask GitHub, and a hub that
  cannot would answer nothing useful; the installer already knows the newest release.
- **`latest` as the product default** — rejected: placing a timer on a node would install
  whatever was merged last with no line in any configuration saying so.
- **Daily, as 0022 said** — rejected for the reason 0025 gives the hub.
- **launchd's calendar interval**, which catches a missed run up on wake — rejected: every Mac
  would reach GitHub and the hub in the same minute, and the plist is a constant, so the minute
  cannot differ per machine.
- **Two keys, a backup's private half kept offline** — rejected by the operator: it spares the
  hands after a lost key, not after a leaked one, and adds a secret to keep; it would also
  change the signature check every kept script freezes.
