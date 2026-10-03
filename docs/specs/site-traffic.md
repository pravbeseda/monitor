# Spec: Site traffic

- **Status:** approved
- **Owns:** `internal/weblog` (the analyzer), `internal/sensor/accesslog` (agent), the
  `sites` class in the hub's file, and what ingest accepts for the sites node
- **Decisions:** [0002](../decisions/0002-push-not-pull.md),
  [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0044](../decisions/0044-a-sensor-takes-its-parameters-from-the-hub.md),
  [0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md)

## Purpose

Shows how a set of web sites is doing: how busy each one is, how often it fails, how slowly
it answers, and how many pages people view. The figures come from each site's own access log
on the server that serves it, so an ad blocker hides nothing and a server error is seen as
readily as a visit.

All sites are series of one **sites node**, whichever server serves them
([0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md)). The agent on each
site's host reads the log with its `access_log` sensor and reports for that node. Like every
sensor it decides nothing: the series have no level until someone sets one
([thresholds.md](thresholds.md)), and a fall in page views shows up as an anomaly
([anomaly.md](anomaly.md)).

The analyzer is a package of its own: it turns a log line into a request, and each metric is
an aggregator over requests. A further metric is a further aggregator; neither the parsing
nor the sensor changes for it. This is not a web analytics tool: referrers, paths and
visitors per day stay with tools built for them.

## The file

```yaml
nodes:
  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
  sites:
    class: sites
    sites:
      blog-a: { host: server-b, log: /var/log/nginx/blog-a.access.log }
      shop-c: { host: server-b, log: /var/log/nginx/shop-c.access.log }
```

**The `sites` class is compiled in**: a profile of `access_log` alone, `access_log` every
5m, `silence_after` 30m. Like `service`, its node has no `token_env`, and the name is
reserved in the same way ([services.md](services.md#the-file-and-the-environment)). The file
may override the class key by key, and the class or the sites node — no other layer — may
set `sensors.access_log.interval`. A compiled-in interval shorter than the longest
`base_tick` among the hosts is raised to it, as [hub-config.md](hub-config.md#the-file)
raises one below a node's own tick, and the compiled-in `silence_after` is raised to the
bound [startup](#startup) sets for a written one.

**Each site names its host and its log.** The host is a node of the file that runs an agent;
the log is an absolute path on that host. A site's name is the `site` label of its series.

**A host receives its own sites only**, as parameters of `access_log`
([0044](../decisions/0044-a-sensor-takes-its-parameters-from-the-hub.md)), at the interval
the sites node resolves, beside the sensors of its own layers. A node that hosts no site
receives no `access_log`. The entry's `node` tells the agent whose measurements the sensor
takes ([agent.md](agent.md#ticking)):

```json
"sensors": {
  "access_log": { "enabled": true, "interval": "5m", "node": "sites",
    "sites": [ { "name": "blog-a", "log": "/var/log/nginx/blog-a.access.log" },
               { "name": "shop-c", "log": "/var/log/nginx/shop-c.access.log" } ] }
}
```

and the agent posts each of them with that `node` ([ingest.md](ingest.md#wire-format)):

```json
{ "node": "sites", "metric": "site.pageviews_24h", "labels": { "site": "blog-a" },
  "sensor": "access_log", "value": 1200 }
```

## The log

A line is nginx's `combined` format — Apache's too — and anything may follow it, as nginx's
stock `main` format appends `"$http_x_forwarded_for"`. A space-separated token after it
that reads `rt=<seconds>`, a non-negative number, is the request time — nginx's
`$request_time`; a line without one, or with `rt=-`, carries none. The time is the one the
line carries, with its offset:

```
203.0.113.7 - - [03/Oct/2026:10:00:00 +0300] "GET /about/ HTTP/1.1" 200 5120 "-" "Mozilla/5.0 (X11; Linux x86_64)" rt=0.120
```

A line whose request field is not a method, a target and a protocol — `"-"`, or the bytes of
a TLS handshake sent to a plain port — is still a request with its status, and never a page
view.

**A page view** is a request that

- uses `GET`,
- was answered 2xx or 304,
- asks for a path whose last segment, without the query, has no extension or ends in
  `.html`, `.htm` or `.php`, regardless of case — an extension being what follows the last
  dot that is not the segment's first character,
- and comes from a user agent that is neither empty nor `-`, and does not contain,
  regardless of case, any of `bot`, `crawl`, `spider`, `slurp`, `curl`, `wget`, `python`,
  `go-http-client`, `java/`, `headless`, `scrapy`, `httpclient`, `facebookexternalhit`.

The list is a product default and is not configurable; a substring list errs both ways, and
a phone whose model name contains `bot` is not counted.

**Reading back.** A site the sensor has not read yet — at the agent's start, or when a site
is added or moved to its host, or its missing log appears — is read back over the last 24
hours: the current log, then `<log>.1` or `<log>.1.gz`, then `<log>.2.gz` and so on, until a
file begins before that point. That last file, when it is not compressed, is searched for
the point rather than read whole; a compressed one is read through. From there on the
sensor reads only what is appended. Reading back runs beside the collections rather than in
one, so it is not bound by the half tick a collection is given
([agent.md](agent.md#ticking)); until it ends, the collections after the one that starts it
report the site's interval metrics only.

## Measurements

Every metric carries the label `site`. "The interval" is the time between this collection
and the previous one, by the agent's clock, and "its requests" are the complete lines
appended to the site's log in it. Values are rounded to two decimals, half away from zero.

| Metric | Value |
|---|---|
| `site.requests_per_min` | the interval's requests ÷ its minutes |
| `site.client_error_pct` | 100 × the interval's 4xx requests ÷ its requests; 0 when it has none |
| `site.server_error_pct` | the same for 5xx |
| `site.response_p95_seconds` | the nearest-rank 95th percentile of `rt` among the requests that carry it and whose time is within the hour before the agent's clock; not reported when none does |
| `site.pageviews_24h` | the page views whose time, cut to the minute, is within the 1440 minutes ending with the agent's clock's minute |

Every count includes bots and static files, except page views. The response time spans an
hour rather than the interval so that a quiet site at night still has a reading.

## Behaviour

One row = one test. Anchors: `spec: site-traffic.md#<heading>`.

### Startup

| Configuration | Result |
|---|---|
| one node of class `sites` listing sites on agent nodes | the hub starts; each host is delivered its own sites |
| a node of class `sites` with `token_env` | startup error naming the node and saying the class is reserved |
| two nodes of class `sites` | startup error naming both |
| a sites node with no `sites`, or an empty map | startup error naming the node: it would never store a measurement |
| a site without `host` or `log` | startup error naming the site and the key |
| a site whose `host` is not a node of the file, or is a `service` or `sites` node | startup error naming the site and the host |
| a site whose `log` is not an absolute path | startup error naming the site |
| two sites with the same `host` and `log` | startup error naming both: each line would count twice |
| `sites` on a node of another class | startup error naming the node and the key |
| the top level, or any class or node other than the `sites` class and node, naming `access_log`, even `enabled: false` | startup error naming it: the sensor reaches a host only through its sites |
| the `sites` class or node enabling any other sensor, or leaving `access_log` out of its profile or disabling it | startup error naming it |
| a top-level `sensors.<s>.enabled: true` | it never reaches the `sites` class, as it never reaches `service` ([services.md](services.md#startup)) |
| an `access_log` interval the file writes for the sites class or node, shorter than the `base_tick` a host resolves | startup error naming the host: its sites would collect once a tick, later than the interval promises |
| no interval and no `silence_after` written, a host resolving `base_tick: 15m` | the hub starts; the sites node's `access_log` runs every 15m and its `silence_after` is 75m |
| a `silence_after` the file writes for the sites class, shorter than twice its `access_log` interval plus three of the longest `base_tick` among its hosts | startup error naming the node: one missed collection would make it fall silent |

### Configuration

| Configuration | Delivered |
|---|---|
| `server-b` hosts `blog-a` and `shop-c`, `server-d` hosts none | `server-b` receives `access_log` with both sites, `node: sites`, interval 5m; `server-d` receives no `access_log` |
| the sites node sets `sensors.access_log.interval: 10m` | every host receives interval 10m |
| a site is added on `server-b` | `server-b`'s configuration version changes; `server-d`'s does not |
| a site moves from `server-b` to `server-d` | both versions change; the site's series stay those of the sites node |

### Ingest

| Request from `server-b`'s token | Response | Side effect |
|---|---|---|
| a measurement with `node: sites` and `site: blog-a`, which `server-b` hosts | 200 | stored under the sites node; `server-b`'s last-seen advances, and the sites node is seen |
| an empty batch | 200 | `server-b` is seen; the sites node is not |
| a measurement with `node: sites` and a site `server-b` does not host — moved away, removed, or unknown — or no `site` label | 200 | that measurement dropped and the request logged naming the node and the site, or saying it had none; the rest stored, and the configuration delivered, so a host still holding an old list receives the new one |
| a measurement whose `node` names another agent's node, a service node, or no node of the file | 403 | nothing stored |
| a measurement whose `node` is `server-b` | 200 | as without the field |
| a measurement whose `node` is not a string, or is empty | 400 | nothing stored |

### Reading

The site `blog-a`; the collection 5 minutes after the previous one.

| Log since the previous collection | Collected for `blog-a` |
|---|---|
| 10 lines: 7 answered 200, 2 answered 404, 1 answered 502 | `requests_per_min` 2, `client_error_pct` 20, `server_error_pct` 10 |
| 3 lines: 1 answered 200, 2 answered 404 | `requests_per_min` 0.6, `client_error_pct` 66.67 |
| a `GET /` from `Googlebot/2.1` and a `GET /style.css` from a browser, both 200 | `requests_per_min` 0.4; neither is a page view |
| no line, and no request in the last hour | `requests_per_min` 0, both error shares 0, no `response_p95_seconds`, `pageviews_24h` holding the page views still in the window |
| 20 lines carrying `rt` 0.01, 0.02 … 0.20 | `response_p95_seconds` 0.19 |
| lines carrying `rt` 0.1, 0.2 and 0.3, and two carrying none | `response_p95_seconds` 0.3 |
| one line carrying `rt=0.125` | `response_p95_seconds` 0.13 |
| no line, a request carrying `rt=0.4` 40 minutes ago | `response_p95_seconds` 0.4 |
| lines carrying `rt=-` or no `rt` at all | no `response_p95_seconds`; the other metrics as usual |
| a line with `"-"` as its request, answered 400 | counted as a 4xx request |
| a line in no recognised format among 9 valid ones | the 9 counted, the line skipped, no error |
| new complete lines, none in a recognised format | no interval metric for the site and an error naming it: the log is not in `combined` format; `pageviews_24h` and `response_p95_seconds` from what the windows hold |
| the last line written only in part | not read until it ends; nothing reported about it |
| two sites, the log of one missing or unreadable | the other's metrics, and an error naming the missing site and its path |
| the previous collection still reading when this one is due | nothing started, and an error saying the previous collection is still running |
| reading back still running at a collection | the interval metrics only, counting from the end of the log as reading back found it |

### Page views

| Request | A page view |
|---|---|
| `GET /` 200, a browser's user agent | yes |
| `GET /blog/post`, `GET /index.html`, `GET /INDEX.HTM`, `GET /page.php?id=3`, `GET /index.php/blog/post` — each 200 | yes |
| `GET /about/` 304, `GET /video` 206 | yes |
| `GET /.well-known/x` 200 | yes: a leading dot is not an extension |
| `GET /style.css` 200, `GET /LOGO.PNG` 200, `GET /api/v1.2` 200 | no |
| `HEAD /` 200, `POST /` 200 | no |
| `GET /` 301, `GET /` 404, `GET /` 500 | no |
| `GET /` 200 from `Googlebot/2.1`, `curl/8.0`, `python-requests/2.31`, or a phone whose user agent names the model `CUBOT` | no |
| `GET /` 200 with the user agent `-` or empty | no |

### The 24-hour window

The agent's clock reads 12:00:00, in UTC.

| Situation | `site.pageviews_24h` |
|---|---|
| 300 page views from 12:01 yesterday on, and 50 before | 300 |
| a page view at 12:00:30 yesterday | not counted: its minute is the one the window has just left |
| a page view at 12:01:00 yesterday | counted |
| a page view logged at `13:30:00 +0300` today | counted: it is 10:30 UTC |
| a page view whose minute is ahead of the agent's clock's minute | counted once the clock reaches that minute |
| the agent restarts at 12:00 | once reading back ends, the page views in the window found in the current log and the rotated ones |
| the rotated files reach back only to 14:00 yesterday — `dateext` names, or rotation by size | the page views they hold, and a warning naming the site and how far back the window reaches |
| no file holds a request yet — a new site | 0, and the same warning |
| the log renamed while reading back runs | each line counted once: the file just read is not read again as `<log>.1` |
| the clock set back since the previous collection | `pageviews_24h` as usual; no interval metric this collection |

### Rotation

| Situation | Collected |
|---|---|
| the first collection of a site the sensor has not read yet | nothing for the site; reading back starts, and the interval metrics start with the next collection, counting from the end of the log as it was found |
| the first collection after reading back ends | `pageviews_24h` and `response_p95_seconds` join the interval metrics |
| the log renamed and a new one created (logrotate's default) | the new file from its start, and beside it the lines appended to the renamed file, which the sensor keeps open until a collection finds it unchanged |
| the renamed file compressed and deleted while held open | the rest of it still read through the open file |
| two rotations between collections | the file the sensor held, followed by what it is rather than by its name, and the newest; the lines of the file between them are not counted |
| the log truncated in place (`copytruncate`), and `<log>.1` holding at least what was read | the lines `<log>.1` holds past the point read, then the file from its start |
| the log truncated in place and grown back past the point read before the next collection — a burst after a quiet day | the same: the file no longer begins as it did |
| a site moved away from this host | its position dropped; nothing more is read for it, reading back included |

## Invariants

- A host stores for the sites node only measurements of the sites the file gives it.
- A site's series belong to the sites node and its `site` label, whichever host reads it.
- The sensor never writes: it holds its positions and its windows in memory and nothing on
  disk. A window holds a count per minute, and the hour's response times, never a line.
- One site's unreadable log never costs the other sites their measurements.
- At most one collection of the sensor reads at a time.
- Every measurement names the sensor, so the series ages by the sites node's interval.

## Edge cases

- **A host that stops reporting** falls silent as itself. Its sites' watched series go stale
  and each is listed as holding no fresh data while other hosts' sites are fresh
  ([attention.md](attention.md)); the sites node falls silent only when no host stores a
  measurement for it.
- **One web server** carrying every site: its outage is announced twice — the server's
  silence, and `silence_after` later the sites node's. Each is true, and folding one into
  the other would make evaluation know which node reports for which.
- **Every log of the only host unreadable** — a wrong path, a wrong format: the host is
  healthy, the sites node falls silent, and the agent's log names the cause.
- **A sites node that never stored a measurement** — no host's agent installed yet — is,
  like any node that never reported, absent from the timeline and from `/debug`. It is not
  recorded at startup, as a service node is, since no loop of the hub's runs it.
- **The sites node's agent version and manifest** stay empty: no agent is its own.
- **A site removed from the file** stops producing its series. Its rows stay, marked as
  holding no fresh data, and a watched one stays listed as such until its threshold is
  cleared — as for a Time Machine destination removed
  ([timemachine-sensor.md](timemachine-sensor.md#edge-cases)).
- **A site moved to another host** keeps its series, but its new host's log reaches back
  only to the move: `pageviews_24h` dips for up to a day and may read as an anomaly. The
  old host's log is not read for it, since one series has one reader.
- **A collection the agent abandons** after half a tick keeps reading and finishes; what it
  counted is lost to the interval metrics, and the page views stay in the window.
- **A big rotated log**: reading back searches the oldest file it needs instead of reading
  it whole; a collection still reading when the next falls due makes that one report an
  error rather than start a second read.

## Out of scope

- Unique visitors: a further aggregator once it is wanted.
- Rotated logs named by date (`dateext`): the window holds what the numbered ones reach.
- Paths, referrers, countries and daily visitor counts: GoAccess and its kin.
- Formats other than `combined`, and JSON logs.
- Telling a host's silence from the sites node's in one message.

## Open questions

None.
