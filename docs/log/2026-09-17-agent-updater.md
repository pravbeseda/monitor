# 2026-09-17 — Agents follow the hub's target: what the spec reviews changed

The decision is [ADR 0028](../decisions/0028-agents-follow-a-target-the-hub-serves.md), and its
alternatives record the two options the operator chose between — one exempt path against a
prefix, one signing key against two. This note keeps what three independent reviews of the
behaviour tables changed before any code was written, because none of it is visible in the
final documents as a change.

## What the first draft got wrong

- **The token could go out in clear.** Nothing limited the scheme of `MONITOR_HUB`, and an
  installer running as root would have sent the token to `http://` and installed whatever
  answered — any signed release above the floor, chosen by whoever sat on the café network.
  The rule became https, or plain http to `127.0.0.1`, `[::1]` or `localhost`, which is what an
  agent on the hub's own host uses. A wider loopback range was left out: nothing uses it, and
  every address admitted is one more to reason about.
- **The updater would have kept a dead agent alive.** Ingest's invariant said every 200 advances
  last-seen, and the target endpoint was specified in the same document. An hourly updater
  would have hidden a crash-looping agent from silence detection for ever. The invariant is now
  ingest's alone, with a row saying a target request stores nothing.
- **The prefix exemption made every future path public by accident.** A handler registered
  under `/api/v1/agent/` without authentication would be reachable from the internet with no
  test failing. The hub now authenticates the prefix before routing, and a test asks an
  unserved path with no token for a 401.
- **A proxy's 401 would have sent the operator after the token.** Until the provisioning
  repository passes the prefix through, the proxy answers 401; the installer tells it from the
  hub's by the `WWW-Authenticate` header, which the hub never sends.
- **Two nodes could share a token value.** The target is answered for the token's node, and two
  variables holding one value made that whichever node sorted first. The hub now refuses it at
  startup — a check that also closes the same ambiguity ingest always had.
- **A host that is both hub and node** would have failed every hour: its kept script predates
  the agent role. It replaces the script before placing the agent's units.

## Rejected during implementation

- **Copying the follow checks into `install-agent.sh`**, as `is_version` had been copied between
  the kept script and `install-hub.sh`. That copy exists because the kept script cannot share a
  file with a release; the two installers can, so they source `install-follow.sh` instead, and
  only a follow run sources it — an operator's copy of `install-agent.sh` alone still installs.
- **`ls -i` for the running agent's inode on macOS** — `find -inum` says the same without
  parsing `ls`.
