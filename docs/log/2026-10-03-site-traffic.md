# 2026-10-03 — Site traffic

The wish was to watch the traffic of a handful of web sites. Four questions shaped
[site-traffic.md](../specs/site-traffic.md) and ADRs
[0044](../decisions/0044-a-sensor-takes-its-parameters-from-the-hub.md) and
[0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md).

## Where the figures come from

- **A web analytics service's API, read as a service node — rejected.** It is the cheapest
  path and fits [0039](../decisions/0039-the-hub-collects-a-service-node.md) exactly, but ad
  blockers drop its counter from a share of visits, and it never sees a server error.
- **A counter of our own, a pixel or a beacon posting to the hub — rejected.** It rebuilds
  web analytics: a public endpoint, bot filtering, abuse limits — low-level work off-the-shelf
  tools already do.
- **The access log, read by the agent on the site's host — chosen.** Nothing hides from it,
  and the status code makes errors as visible as visits. It needs the sites to be served by
  machines that run an agent.
- **GoAccess on the host, read through its JSON report — rejected** in favour
  of our own parser: it ties the sensor to a tool on the host and to its output format, and
  its totals cover a period rather than an interval. GoAccess keeps the drill-down role.

## Which figures

Requests, 4xx and 5xx shares, the 95th percentile of the response time, and page views.
Visitors were the obvious ask, and three shapes were weighed: unique (address, user agent)
pairs over a sliding day, per calendar day, or page views instead. The first two hold a day
of addresses that a restart loses or that must be written to disk, and the calendar day is a
sawtooth the anomaly band reads badly. **Page views over a sliding 24 hours** need neither:
the window is rebuilt from the log itself, and a sliding day has no daily rhythm to mistake
for an anomaly. A one-hour window was rejected: requests per interval already react within
minutes, and the hour shows the night as a dip.

The analyzer is a package of its own whose metrics are aggregators over parsed requests, so
unique visitors can still come later as one more aggregator.

## Whose node a site is

- **The host's node, a `site` label on each series — rejected.** One site's errors would
  colour the whole server, and a site moved to another server would start new series.
- **A node per site — rejected.** A lane per site and a silence window per site, for
  readings that rarely change.
- **A lane per site grouped when shown — rejected.** The series would still be the host's.
- **One sites node, like `cloud` — chosen.** Unlike `cloud`, the hub cannot collect it: the
  logs lie on the hosts, and the hub never reaches a node. Shipping the logs to the hub over
  syslog was weighed and dropped — an open port taking plain text, and lines lost while the
  hub restarts. So the host's agent reports for the sites node, and ingest admits that only
  for the sites the file gives the host.

## Parameters

The sensor is the first to need one: which log belongs to which site is a deployment
setting. It rides in the sensor's entry of the delivered configuration rather than in a
file on the node or a free-form map, which the hub could not validate — the same route
[#51](https://github.com/pravbeseda/monitor/issues/51) needs for a backup's path.

## What the spec reviews changed

- **A moved site no longer locks its old host out.** The first draft refused a whole request
  carrying a site the host no longer held; since configuration rides on a 200, the host
  would never have learnt the site had moved. Such a measurement is now dropped alone.
- **A sensor may return measurements and an error together**, so one unreadable log does
  not cost the other sites theirs; the agent used to discard both.
- **The response time spans the last hour**, not the interval: a quiet site at night would
  otherwise go stale every night.
- **Reading back follows rotated files until it covers the day**, compressed ones included,
  and searches the oldest instead of reading it; a collection still reading when the next
  falls due makes that one fail instead of starting a second read.
- **Rotation follows the file, not its name**: a renamed log is drained until nginx stops
  writing to it, and a half-written last line waits for its end.
- **Startup refuses a host whose tick is longer than the sites interval**, and a sites
  `silence_after` one missed collection would trip.
