# 0022. Updates are pulled by an updater of their own; the hub names the version

- **Status:** accepted
- **Amends:** the "no second endpoint" clause of [0010](0010-agent-configuration.md), for the
  target version alone; everything that ADR says about the agent's own configuration, about
  the second credential it refuses, and about the hub never initiating, stands
- **Date:** 2026-09-06
- **Source:** [POC](../poc.md) stage 3, the manual upgrade path in
  [install.md](../install.md), and [issue #16](https://github.com/pravbeseda/monitor/issues/16)

## Context

A binary reaches a machine today by hand: build it, `scp` it, re-run `install-agent.sh` for
a node or `install` plus a restart for the hub ([install.md](../install.md)). That is one
command per machine per change, and during development the change comes several times a
day. The macOS agent cannot even be built from the same machine as the others: its disk
sensor is cgo ([0014](0014-macos-available-space.md)), so it needs a Mac.

Three facts constrain how that can be automated.

**Nothing can reach a node.** [0002](0002-push-not-pull.md) has the agent connect to the
hub because a laptop sits behind NAT, sleeps, and changes networks. That reasoning does not
stop at measurements: an update pushed from CI over SSH, or from the hub over SSH, reaches
the servers and silently never reaches the laptops. A scheme that works for half the fleet
is not a scheme.

**An agent that updates itself can brick its own node.** The agent runs as a supervised
system service ([0019](0019-deployment-layout.md)): if a released version fails at startup,
systemd and launchd restart it forever. Were the update logic inside that binary, the only
code able to fetch the fix is the code that cannot run, and the node is recoverable by hand
only — which is exactly the cost this decision exists to remove.

**That argument does not stop at the agent.** Whatever installs the agent has the same
problem one level up, and there it is worse: a broken agent is repaired by the next release,
while a broken installer is repaired by nothing, because it is the thing that fetches
repairs. The regress ends only where something on the machine is small enough, and changes
rarely enough, that recovering it by hand is an event rather than a routine.

**An update is root fetching code from the internet.** Today the only path to
`/usr/local/bin` on a node is the operator's SSH session. Automation opens a second path
and leaves it open. A checksum published beside the artifact does not defend it: whoever
can replace the binary can replace the sums next to it.

## Decision

**Every machine pulls its own updates, an updater separate from the agent installs them,
and the hub names the version the fleet should be running.**

1. **CI publishes releases.** A tag builds every target — `linux/amd64`, `linux/arm64` and
   `darwin` on a macOS runner, because of [0014](0014-macos-available-space.md) — and
   publishes them as GitHub Release assets. This is
   [issue #16](https://github.com/pravbeseda/monitor/issues/16), and it is the prerequisite
   for everything below.
2. **A release is trusted by its signature**, verified against a public key that ships in
   the resident part of the updater — the half that was not just downloaded, since a release
   cannot vouch for itself. Which signing tool does that is an implementation choice for the
   spec, not for this ADR; publishing checksums alone is not one of the options.
3. **The updater is its own unit** — its own timer on Debian and macOS, its own binary or
   script, installed beside the agent and supervised independently of it. It survives an
   agent that will not start, which is the whole reason it is separate.
4. **The hub names the target version**, in its configuration, and the node asks the hub for
   it. Deliberately not through the ingest response that carries the rest of the node's
   configuration ([0010](0010-agent-configuration.md)): only a running agent makes that
   request, and the node that most needs a corrected target is the one whose agent will not
   start. Asking is the installer's job rather than the stub's — point 6 keeps the choice of
   a version inside the release, so the hub's address, the token and the shape of that answer
   stay out of the frozen half. The hub already receives every node's `agent_version` in the
   ingest payload, so it can report who is behind without the updater saying anything.
5. **The hub's own target is set on the hub's host**, not by the hub itself, and it is
   upgraded first. The hub must accept measurements from an agent older than itself
   regardless — a laptop can be asleep for a week — so the fleet is never required to move
   in step.
6. **Nothing on the machine updates itself.** What lives there permanently is a stub, and it
   is the only thing that downloads and verifies: fetch the newest release, check its
   signature, hand over to the installer *inside that release*. That installer asks the hub
   for the target and either installs itself, when the target is the version it came from, or
   names the version it needs and exits — whereupon the stub fetches that release, checks its
   signature the same way, and hands over to *its* installer. An installer that names nothing,
   because the hub did not answer, installs nothing: a hub that is down or unreachable leaves
   the machine exactly as it was. **The hub's own installer asks nothing** — it reads the
   target point 5 puts on that host. Asking would mean asking the service it is upgrading,
   and the day that matters is the day a released hub crashes at startup, when nothing would
   answer and nothing would ever be repaired. The branch is drawn per binary and not per
   host: on a machine that runs both, the agent's installer asks the hub like any other
   node's, at whatever address that node is configured with, and only the hub's installer
   reads the local target. **A target is a release that carries an
   installer**, which puts a floor under how far back the hub may point: the releases
   [#16](https://github.com/pravbeseda/monitor/issues/16) produces before the updater exists
   carry binaries alone, and naming one is not a rollback but a stop. A stub that fetches a
   release with nothing to hand over to installs nothing and says so in its log; going below
   the floor is the manual path of [install.md](../install.md), which is what that path is
   for.
   Everything that changes — how a version is chosen, where files go, how a service is
   restarted — travels in the signed release and is therefore current at every run. The stub
   is deliberately frozen, and when it does have to change it changes over the manual path
   of [install.md](../install.md), which this decision keeps supported for exactly this.
7. **The stub runs on a timer**, daily, catching up after a machine was asleep, and spread
   over a window so a fleet does not arrive at the hub in one second. Which unit expresses
   that on each supervisor belongs to [0019](0019-deployment-layout.md) and the spec.

## Consequences

- The rollout is controllable without cutting a release: pinning one node to a version,
  canarying a new one, and rolling back are edits to the hub's configuration.
- The hub gains a fleet view it did not have: which nodes run which version, and which
  have not taken the target yet.
- Nodes still need no inbound port, no public address and no account CI can log into.
  Nothing in this decision reaches toward a node.
- CI gains publishing rights on the repository's releases. It gains no credential to any
  host, and none of the secrets in `hub.env` or `agent.env`
  ([0007](0007-public-repository.md)).
- Every run downloads the newest release even when the target is an older version, because
  the code that knows the target travels in that download; a node held on an older target
  then downloads that one too, two releases a day rather than one. It is a few megabytes
  against keeping the frozen half unable to choose anything, and against a downloaded release
  vouching for the next one.
- The agent's installer reads the token from `agent.env` when it asks the hub for the
  target; it gets no environment file of its own. The hub's installer needs neither, since it
  makes no request. So a hub-only host needs no `agent.env` and a node-only host needs no
  local target, and a machine that is both — the case [install.md](../install.md) supports —
  simply has both, one per binary, as it already has two environment files. One copy of the secret means one rotation procedure
  — re-running `install-agent.sh`, which [0019](0019-deployment-layout.md) already defines —
  and nothing that can drift out of step with it. The one-file-per-binary rule of 0019 is
  untouched: what reads that file here is a transient root script, not a resident service.
- Reading that file means reading it by [0020](0020-agent-reads-its-environment-file.md)'s
  rules and never sourcing it — the installer is a third reader after the agent and
  `install-agent.sh`, and it runs as root out of a downloaded archive, which is precisely the
  shape 0020 was written against. "One file, one parser" is therefore one file and one set of
  rules: a value the agent reads as `abc=` cannot be a value the updater reads as `"abc="`,
  or a rotated token leaves the updater authenticating nowhere while the node looks healthy.
  The parser travels in the release like the rest of the installer.
- The private half of the signing key becomes a secret of the project — the one secret whose
  loss would let someone else's binary install itself as root on every node.
- The updater is a third thing to build, ship and test, on two supervisors
  ([0019](0019-deployment-layout.md) owns where it lands). Until it exists, the manual path
  in [install.md](../install.md) stays the only one, and it stays supported afterwards: it
  is what recovers a machine the updater cannot.
- The stub's interface — where it looks for a release, what it executes out of one, and how
  an installer names a version instead of installing — becomes a contract that every future
  release has to keep, because old stubs stay in the field. Breaking it is the one change
  that costs hands on every machine.
- The release carries an installer, not only binaries, and that installer runs as root from
  a downloaded archive. It is the same trust as running a downloaded binary and rests on the
  same signature; it is not an additional one.
- Rolling out is no longer the same act as building, so a release that was never installed
  anywhere becomes a normal state. "Latest release" stops being a synonym for "what is
  running".

## Alternatives

- **GitHub Actions deploys over SSH** — rejected: it reaches the hub's host and no laptop,
  for the reason [0002](0002-push-not-pull.md) already recorded, so the nodes would need a
  second mechanism anyway. It would also give a public repository's CI a standing root path
  onto the host that holds the database and every node token.
- **The hub orchestrates its nodes over SSH** — rejected: it inverts
  [0002](0002-push-not-pull.md) and needs every node to be reachable and to trust a key the
  hub holds. The hub would become the one machine whose compromise is the whole fleet's.
- **The agent updates itself in process** — rejected: a bad release that crashes at startup
  takes the update path down with it, and every node it reached needs hands.
- **The updater updates itself** — rejected: it reproduces the failure it was introduced to
  prevent, one level up and without a remedy. A bad agent is repaired by the next release; a
  bad updater that cannot start is repaired only on site.
- **The agent updates the updater and the updater updates the agent** — rejected, though it
  does answer the regress: each repairs the other, and only two bad releases at once are
  fatal. It gives the agent installation logic and write access to `/usr/local/bin`, which
  doubles the code trusted with root and puts back what point 3 took out.
- **A frozen updater, changed by hand alone** — not rejected so much as absorbed. It is the
  right answer for something small enough, which is why point 6 shrinks the resident part
  until it qualifies instead of freezing the whole updater and hoping it never has to move.
- **The target rides the ingest response** — rejected: it is one channel fewer, but only a
  running agent makes that request, so a release that crashes the agent at startup cuts the
  node off from the corrected target and from the rollback under Consequences. That is the
  case point 3 exists to remove, so the mechanism that answers it cannot depend on the agent.
- **The stub asks the hub for the target itself** — rejected: it saves downloading a release
  the node may not install, and costs the hub's address, the node's token and the shape of
  that answer their place in the frozen half, plus a second contract old stubs depend on — on
  the side that point 5 upgrades first. Point 6 exists to keep the resident part unable to
  choose anything.
- **Each node follows the latest release on its own, with no hub involvement** — the same
  shape as the decision, minus point 4. Rejected because it gives away rollout control for
  nothing: a bad version reaches the whole fleet at once and the only remedy is another
  release, while the channel that would have held it back already exists
  ([0010](0010-agent-configuration.md)).
- **Distribution as OS packages** — an apt repository for Debian, Homebrew for macOS, with
  the system's own unattended upgrades doing the work. Rejected for now: it is two
  mechanisms rather than one, it needs a signed repository to be hosted somewhere, and the
  macOS half fits a per-user package manager badly when the thing being updated is a root
  daemon. It stays the sane destination if this project ever ships to machines that are not
  the author's.
