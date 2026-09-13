# 0023. The reverse proxy authenticates the web; the hub authenticates only ingest

- **Status:** accepted
- **Date:** 2026-09-12
- **Source:** [POC](../poc.md) stage 3,
  [proxy requirements](../nginx-requirements.md), [ADR 0007](0007-public-repository.md)

## Context

The hub binds to loopback, so no node other than the hub host can reach it and the POC cannot
be rolled out. Giving it a public address means TLS and a proxy in front, and
[0007](0007-public-repository.md) requires authentication on the web page before that address
exists. Today the hub authenticates one endpoint: `/api/v1/ingest`, by a per-node bearer token.
The pages and the read API have no authentication at all.

The hub host's nginx is already provisioned by Ansible from another repository, which owns
TLS and the vhost regardless of what is decided here.

## Decision

- **Everything except `/api/v1/ingest` is authenticated by the proxy**, with generic HTTP
  authentication over TLS: a browser prompt for a person, one request header for a program.
  The hub gains no login page, no session and no cookie.
- **`/api/v1/ingest` stays the hub's own**, unchallenged by the proxy, so the per-node token
  keeps being the single thing that admits a node.
- **The requirement, not the configuration, lives in this repository** —
  [nginx-requirements.md](../nginx-requirements.md) states what the proxy must do, and the
  vhost itself stays in the Ansible repository. A copy here would drift, and a real name in
  it would break [0007](0007-public-repository.md).

## Consequences

- The security perimeter now spans two repositories. The requirements document is what keeps
  them honest, and its check list is the acceptance test for the hub host.
- The hub is only safe behind the proxy. It must stay on loopback, and a deployment that
  exposes its port directly publishes an unauthenticated read API.
- Credentials for people and programs are an Ansible concern: adding, rotating or revoking one
  never touches this repository, and none of them is a node token.
- A skin, a chart renderer or an uptime check must send the program credential. The hub has no
  unauthenticated health path, and adding one later is a code change here.
- Nothing about this survives a second hub host or a hub reachable without nginx. Both would
  reopen the question, which is what an ADR is for.

## Alternatives

- **Authentication in the hub — a bilingual login page, a session cookie, a read token** —
  rejected for now: it is a day of code and a new spec to reach what one proxy directive
  already does, and [0008](0008-english-repo-bilingual-ui.md) would make the login page a
  translated surface of its own. It is the right answer once the web has more than one real
  user, and this ADR is the one to supersede then.
- **A single shared credential for people and programs** — rejected: a script's credential
  ends up in more places than a person's, and rotating it would lock the browser out too.
- **An IP allow-list instead of a credential** — rejected: nodes are laptops on changing
  networks, and the read API would be open to anyone sharing an address with them.
- **Authenticating ingest at the proxy as well** — rejected: it duplicates the node token with
  a second secret per node, and every agent would have to learn to send both.
- **Keeping the vhost in this repository as an example file** — rejected: it either carries
  the real name, which [0007](0007-public-repository.md) forbids, or it is a synthetic copy
  nobody applies and everybody edits.
