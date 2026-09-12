# What the hub needs from the reverse proxy

The hub host's nginx is provisioned by Ansible, in the repository that owns that host. This
file is the requirement handed to it: **what the hub needs**, not how nginx should be written.
No vhost lives here — a second copy would drift out of sync with the real one — and per
[ADR 0007](decisions/0007-public-repository.md) no host name, domain or credential may enter
this repository at all.

`hub.example.com` below stands for the public name of the hub host, the same placeholder
[install.md](install.md) uses. The real one is a deployment setting and lives on the server.

Related: [install.md](install.md) covers everything up to the point the proxy takes over,
[ADR 0023](decisions/0023-proxy-holds-the-web-perimeter.md) records why the browser-facing
credential lives in the proxy rather than in the hub.

## What is being proxied

One Go process, one systemd unit, one port:

| What | Value |
|---|---|
| unit | `monitor-hub.service` |
| listens on | a loopback address, plain HTTP — `127.0.0.1:8080` unless the host says otherwise |
| account | `monitor`, unprivileged |

**The port is a per-host setting, not a constant.** It is `MONITOR_LISTEN` in the hub's
environment file, and a host where 8080 is taken runs the hub elsewhere — 8090, say. Take the
upstream address as a variable of the role and agree its value with whoever installs the hub;
the hub refuses to start on anything but a loopback address, and logs the one it bound to.

Its whole HTTP surface:

| Path | Method | Who calls it | Authenticated by the hub |
|---|---|---|---|
| `/api/v1/ingest` | POST | the agent on every node | yes — a per-node bearer token |
| `/` | GET | a person in a browser | no |
| `/history` | GET | a person in a browser | no |
| `/api/v1/series` | GET | a program, a chart renderer | no |
| `/api/v1/history` | GET | a program, a chart renderer | no |

The version prefix `/api/v1/` is part of the contract and later endpoints keep it. Assume the
path list grows; do not enumerate paths where a prefix rule will do.

## Requirements

1. **One HTTPS name, and nothing served over plain HTTP.** The public name answers over TLS
   with a certificate that renews itself. Port 80 exists only to redirect to the same URL over
   HTTPS — except on `/api/v1/ingest`, which is better refused there outright: an agent
   misconfigured with `http://` sends its node token in clear *before* it ever sees the
   redirect, and a redirect makes that look like it worked. A token that has crossed port 80
   is rotated, not reused.

2. **Every request goes to the hub, unchanged.** Method, path, query string, body **and
   headers** arrive as they were sent. Two headers matter by name: `Authorization`, which
   carries a node's token, and `Accept-Language`, which is how the page picks English or
   Russian ([ADR 0008](decisions/0008-english-repo-bilingual-ui.md)) — a role that normalises
   or strips headers silently makes the interface English-only. No path rewriting, no
   trailing-slash normalisation, no static file served from disk, no directory listing, no
   default vhost answering for this name.

3. **The hub stays unreachable except through the proxy.** It binds to loopback, and the
   host's firewall keeps its port closed from outside. This is the one requirement whose
   failure publishes the read API to anyone, and no debugging shortcut should end up undoing
   it.

4. **Ingest needs no credential from the proxy.** `POST /api/v1/ingest` passes through with its
   `Authorization` header delivered to the hub byte for byte — the hub authenticates the node
   itself, and a proxy-level challenge there would lock out every agent. No other method on
   that path needs to work.

5. **Everything except ingest needs a credential the proxy checks.** Missing or wrong gets
   `401` and never reaches the hub. Two things must both work: a person opening
   `https://hub.example.com/` gets a prompt in the browser, and a program reaches
   `/api/v1/series` by sending one request header. The credential is generic HTTP
   authentication over TLS — no login page, no cookie, no redirect to a form.

6. **Credentials are several, separate and individually revocable.** At least one for a
   person and one for a program, so a script's credential can be replaced without locking the
   browser out, and adding or removing one is an Ansible change rather than a rewrite. None of
   them may be a node's ingest token, and none of them belongs in this repository.

7. **A 1 MiB request body passes; a larger one is rejected with `413`.** That is the hub's own
   cap on an ingest body, so the two agree on the size. Refusing it *by size* is something
   only the proxy can do: the hub authenticates before it looks at a body, so an oversized
   body carrying a bad token is answered `401`, never `413`.

8. **Rate limiting on the public name**, since ingest is reachable unauthenticated by design.
   At least 120 requests a minute per source address with a burst of 20, and over that the
   proxy answers `429` **immediately** rather than queueing the request — a delayed request
   holds a connection and still arrives. The number is a variable of the role: nodes get
   added, and several of them can sit behind one NAT address. The hub's own limit of 60
   requests a minute per node is keyed by node name and applies only *after* authentication,
   so it does not overlap with this one.

9. **The proxy caches nothing.** Every page and every API response is live state; a cached one
   shows a healthy disk that filled up ten minutes ago. What a *browser* caches is the hub's
   own business — its JSON already says `no-store` and its pages will
   ([#23](https://github.com/pravbeseda/monitor/issues/23)); nothing in nginx can fix that.

10. **The proxy does not depend on the hub being up.** It starts, reloads and survives on its
    own. While the hub restarts — a version upgrade, a configuration change — the proxy
    answers `502` and recovers by itself; no nginx reload is needed after a hub restart, and
    the hub's own deploy never touches nginx.

11. **An access log that can explain a rejection**, with method, path, status and source
    address, kept at least 14 days — long enough to answer "why did that node get a 429 last
    week". It must not log the `Authorization` header, and nothing that carries a credential
    should end up in a query string.

12. **Generated credentials reach us out of band** — not in this repository, not in an issue,
    not in a pull request. What we need back is the public name, the credential for a person
    and the credential for a program.

## What we are not asking for

- No authentication in front of `/api/v1/ingest` (requirement 4).
- No IP allow-list: nodes are laptops on changing networks.
- No websockets, no HTTP/2 server push, no static hosting, no CDN.
- No unauthenticated health path. The hub has none today, so an external uptime check needs
  the program credential from requirement 6. If the role would rather probe something open,
  say so and the hub gets one — it is a code change on our side, not a proxy setting.

## How we check it is done

With the program credential in `$cred` (`user:password`) and the real name in place of
`hub.example.com`.

Requirement 1 — plain HTTP only redirects, and not on ingest:

```sh
curl -sI http://hub.example.com/ | head -1          # 30x to the same path over https
curl -s -o /dev/null -w '%{http_code}\n' -X POST -d '{}' \
     http://hub.example.com/api/v1/ingest           # refused, not redirected
```

Requirements 5 and 6 — the credential, and two of them working independently:

```sh
curl -sI https://hub.example.com/                   # 401 with a WWW-Authenticate header
curl -s -u "$cred" https://hub.example.com/ | head  # the page
curl -sI https://hub.example.com/api/v1/series      # 401 — no credential, no data
curl -s -u "$cred" "https://hub.example.com/api/v1/series?metric=disk.free_pct" | head -c 200
curl -s -u "$human_cred" https://hub.example.com/ -o /dev/null -w '%{http_code}\n'   # 200
```

Requirement 4 — the one that proves agents still get in:

```sh
curl -sS -i -X POST -H 'Authorization: Bearer wrong' -H 'Content-Type: application/json' \
     -d '{}' https://hub.example.com/api/v1/ingest
```

`401` with a JSON body from the hub is right. `401` with a `WWW-Authenticate` header means the
proxy answered and **every agent is locked out**.

Requirements 7 and 8 — the caps:

```sh
head -c 2097152 /dev/zero | tr '\0' x > 2mib.json
curl -s -o /dev/null -w '%{http_code}\n' -X POST --data-binary @2mib.json \
     https://hub.example.com/api/v1/ingest          # 413 from the proxy, not 401 from the hub
seq 200 | xargs -P 20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
     -X POST -d '{}' https://hub.example.com/api/v1/ingest | sort | uniq -c   # 429s appear
grep -c 'Bearer' /var/log/nginx/access.log          # 0 — requirement 11
```

Requirement 3 — from another machine, with the hub's own port:

```sh
curl -s --connect-timeout 5 http://hub-host:8090/   # no connection, not a page
```

Requirement 10 — while the hub is restarting. One status code cannot show a transition, so
poll from another machine and restart the service underneath it:

```sh
while :; do                                     # leave this running
  curl -s -o /dev/null -w '%{http_code} ' -u "$cred" https://hub.example.com/
  sleep 1
done

sudo systemctl restart monitor-hub              # meanwhile, on the hub host
```

The stream shows `200`, a `502` or two while the process is down, and `200` again — with no
nginx reload in between. A stream that never leaves `502` means the proxy needs one, which is
the half of the requirement a single request cannot see.

The end-to-end proof is a node: with an agent installed against `https://hub.example.com`,
the journal on that node shows an accepted push and the page shows its volumes with a fresh
timestamp.
