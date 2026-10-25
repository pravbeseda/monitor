# Spec: Overview

- **Status:** approved
- **Owns:** the pages `GET /overview` and `GET /item` in `internal/hub` — the overview skin:
  a card per item with a few numbers and a line of its last day, and a page per item with
  every series of it charted and the changes it went through. What a level, a staleness or a
  silence *is* stays with [evaluation](evaluation.md) and [state](state.md), what an anomaly
  is with [anomaly](anomaly.md), and what needs attention with [attention](attention.md); the
  points, the windows and the chart's axes are [history](history.md)'s; a threshold is
  [thresholds](thresholds.md)'; the change lines are the timeline's
  ([timeline](timeline.md#changes)); the tabs, the zone, the shell and the refresh come from
  [web](web.md); every user-facing string comes from `internal/i18n`.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0018](../decisions/0018-history-through-the-api.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0035](../decisions/0035-mission-control-is-rendered-by-the-hub.md),
  [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md),
  [0037](../decisions/0037-skins-are-tabs-and-the-root-opens-the-last-one.md),
  [0039](../decisions/0039-the-hub-collects-a-service-node.md),
  [0042](../decisions/0042-an-anomalys-start-and-end-are-recorded.md),
  [0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md),
  [0046](../decisions/0046-an-overview-of-items-rendered-by-the-hub.md)

## Purpose

The overview answers "how do things stand, in numbers": every computer, site and service as
a card with the few figures that say most about it and how they moved over the last day. A
card opens the item's page, where every series of it is charted over a chosen window beside
what changed. It judges nothing: a card's colour is a level evaluation stored, an anomaly, a
silence or data that is or is not fresh, and every number is one the hub holds.

## Model

**An item** is a node the State API lists with `configured: true` — one that has reported,
or a service node the hub recorded at startup ([state](state.md#listing)) — except the node
of class `sites`, which is not an item itself but one item per site: a site the file lists
for it ([site traffic](site-traffic.md)) of which the hub holds at least one series. Below,
`sites` names that node; its name is whatever the file gives it.

**An item's series** are, for a node, every series the State API lists for it; for a site,
the `sites` node's series labelled with that site. A node's silence subject is never one of
them: it decides "silent", below, and nothing else.

**A section** groups items by who collects them, in this fixed order:

| Section | Its items |
|---|---|
| "Computers" | nodes of any class other than `sites` and `service` — every node with an agent of its own |
| "Sites" | the sites of the `sites` node |
| "Services" | nodes of class `service` |

A section with no item is not shown. Its heading is its name, how many items it holds, and
for each state that items of it are in, other than "ok" and "reporting, nothing watched",
how many — in the order cards are ordered: "Computers · 4 · 1 silent · 1 warning · 1 no
fresh data".

**The state of an item** is the first of these that holds now, in the words the timeline's
lanes use ([timeline](timeline.md#lanes)):

| State | Word | Holds when |
|---|---|---|
| silent | "silent" | the silence subject of the item's node is `critical` — for a site, that of the `sites` node or of the node the file names as the site's host |
| no fresh data | "no fresh data" | none of the item's series is fresh by [state](state.md#staleness), or it has none |
| critical | "critical" | a series of the item is at `critical` and not stale |
| warning | "warning" | a series of the item is at `warning` and not stale |
| unusual | "unusual" | a series of the item has an anomaly rank and is not stale |
| ok | "ok" | a series of the item is at `ok` and not stale |
| reporting | "reporting, nothing watched" | none of the above: fresh data, and no level over any of it |

**When an item last reported** is its node's `last_seen` ([state](state.md#listing)) — for
a service node that has stored nothing, when the hub recorded it — or, for a site, the
newest point among its series. A card in the silent or the no-fresh-data state says it.

**Cards in a section** are ordered silent, critical, warning, unusual, ok, reporting, then no
fresh data — a laptop asleep is the everyday case and goes last — and by the item's name
within one state. A site's name is its `site` label; a node's is the node.

**The page order of an item's series** — the order its page charts them in — is by family,
the part of a metric id before its first dot as on `/debug` ([history](history.md#page));
inside a family, the series without labels first, by metric id, then the labelled ones as
`/debug` orders the rows of a matrix and its columns.

**A card** shows the item's name, its state's word marked in its colour, and up to six
**rows**, each one series of the item: its name, its newest value formatted in its unit as
`/debug` formats it, and at most one mark, the first that applies: no fresh data when the
series is stale, whatever level it stored; critical; warning; unusual. The rows are taken in
this order, a series at most once:

1. every series of the item that would be an item of the list of what needs attention by
   that list's kinds — a level, an anomaly, no fresh data ([attention](attention.md#model))
   — in that list's order, without its cap of five anomalies;
2. the item's **default series**, in the order of the table below;
3. when neither gives a row, the first two series the item's page charts.

When more series qualify by the first two steps than six rows hold, the card ends with "N
more", N being how many of them are not shown. The first two rows carry a line of the last
24 hours of their series, drawn as the history page draws a series — broken across a gap, a
lone point drawn on its own — with no axes; a series with no point in that day has no line.
An item with no series reads "No series yet" in place of rows. The whole card links to the
item's page.

**Default series**, the product's choice of what says most about a kind of item:

| Metric | Which series, when the item has several |
|---|---|
| `disk.free_pct` | the volume with the least free among those not marked `removable: "true"` and not stale; among those not removable when every one is stale; none when every volume is removable |
| `memory.available_pct` | — |
| `load.avg_15m` | — |
| `systemd.failed_units` | — |
| `timemachine.backup_age_seconds` | the destination whose backup is oldest among those not stale; among all when every one is stale |
| `site.requests_per_min` | — |
| `site.server_error_pct` | — |
| `site.response_p95_seconds` | — |
| `gdrive.free_pct` | — |

A tie goes to the series first in the page order. A metric the item does not have gives no
row.

**A row's name** is its series' name ([metric names](metric-names.md#naming)) — `fs` and
`removable` left out — and **a small chart's name** is the same with a volume's filesystem
and its being removable; either leaves out the `site` label on a site's own card and page.
A family is headed by its family's name.

On a narrow screen the cards flow one to a line, each section's heading above its own.

**The item's page** has, from the top:

- the item's name, its section's singular ("Computer", "Site", "Service"), its state's word,
  and when it last reported, whatever its state;
- the card's rows, as numbers, without lines;
- window links 24h / 7d / 30d, as the history page offers them and marks the open one
  ([history](history.md#page));
- every series of the item that `/debug` would show — a removable volume that is stale left
  out ([history](history.md#page)) — as a small chart over the window, grouped under its
  family's name, in the page order; "No series yet" when there is none;
- the changes of the item: the newest 20 of its series' transitions and anomaly starts and
  ends, and the silence transitions of its node — for a site, of the `sites` node and of its
  host — written and grouped by day as the timeline writes them
  ([timeline](timeline.md#changes)).

**A small chart** is the history page's chart of one series over the window — a time axis,
a value axis from zero — reduced in size, under the series' name, its newest value with its
mark, and the time it was collected; each chart reads its own series, so no item has too
many for one page. A series that holds a readable threshold ([state](state.md#listing)) has
it drawn across the chart: a line at its `warning` value and one at its `critical` value,
each in its level's colour and named by its level's word, the value axis reaching above the
highest of its values and those lines. An unusual series says its usual value — its norm —
as "now" does. A stale series' value is marked as holding no fresh data. The chart links to
the series' history page with the same window, and carries the link to the page that sets
what the series is judged by ([thresholds](thresholds.md)).

## Behaviour

One row = one test. Anchors: `spec: overview.md#<heading>`. Words quoted below are the
English catalogue's; every one of them has its Russian. `sites` is the node of class
`sites`.

### Items {#items}

| State | What the reader sees |
|---|---|
| nodes `server-b` (class `server`) and `laptop-a` (class `laptop`) that have reported | two cards, both under "Computers" |
| a node of a class the file introduces | its card under "Computers" |
| `sites` whose file entry lists `blog-a` and `shop-c`, both with series | two cards under "Sites", named `blog-a` and `shop-c`, and none named after `sites` |
| a site the file lists that has no series yet | no card |
| a series of `sites` labelled with a site the file does not list | on no card; on `/debug` |
| a `service` node recorded at startup that has stored no series | its card under "Services", "No series yet" in place of rows |
| a node the configuration no longer names | no card |
| a node the file lists that never reported, not of class `service` | no card |
| no item at all | "No node has reported yet", and no section |

### Sections {#sections}

| State | What the reader sees |
|---|---|
| items in all three sections | "Computers", then "Sites", then "Services", whatever their states |
| no `service` node | no "Services" heading |
| four computers: one `warning`, one silent, one with no fresh data, one `ok` | "Computers · 4 · 1 silent · 1 warning · 1 no fresh data" |
| two computers, one `ok`, one reporting with nothing watched | "Computers · 2" |

### The state of an item {#state}

| State | Card's word |
|---|---|
| the node's silence subject `critical`, a series of it `critical` from before | "silent" |
| a node reporting whose every series is stale | "no fresh data" |
| a laptop asleep inside its `silence_after`, every series stale | "no fresh data", and when it last reported |
| a `service` node recorded at startup that has stored no series | "no fresh data" |
| a volume at `critical`, fresh | "critical" |
| a volume at `critical` that is stale, another series fresh and at `ok` | "ok": a stale level does not raise the item, as it does not raise its node ([state](state.md#model)) |
| one series at `warning`, another unusual | "warning" |
| a series at `ok` and an unwatched series ranking | "unusual" |
| fresh series, none watched, none ranking | "reporting, nothing watched", not marked as ok |
| a series whose threshold was saved after the last tick, nothing else watched | "reporting, nothing watched": it holds no level yet |
| a series whose stored threshold this build cannot read, nothing else watched | "reporting, nothing watched" |
| `shop-c`'s `site.server_error_pct` at `critical`, `blog-a`'s series all at `ok` | `shop-c` "critical", `blog-a` "ok": a site is judged by its own series |
| `sites` silent | every site's card "silent" |
| `shop-c`'s host `server-b` silent, `blog-a` served by `server-a` and fresh | `shop-c` "silent", `blog-a` as its own series say |
| `blog-a`'s series all stale while `shop-c`'s are fresh and both hosts report | `blog-a` "no fresh data" |

### Order of cards {#order}

| State | Order inside the section |
|---|---|
| `server-a` `ok`, `server-b` `critical`, `server-c` silent | `server-c`, `server-b`, `server-a` |
| `laptop-a` with no fresh data, `server-d` reporting with nothing watched, `server-e` `ok` | `server-e`, `server-d`, `laptop-a` |
| two items in the same state | by name |

### Rows {#rows}

| State | Rows of the card |
|---|---|
| a Linux computer with `disk`, `memory`, `load` and `systemd` series, `/` its least-free volume, nothing needing attention | `disk.free_pct /`, `memory.available_pct`, `load.avg_15m`, `systemd.failed_units` |
| the same computer, `load.avg_15m` unusual | `load.avg_15m`, `disk.free_pct /`, `memory.available_pct`, `systemd.failed_units` |
| volumes `/` at 40% free and `/srv` at 12%, the threshold on `disk.free_bytes` of `/` at `warning` | `disk.free_bytes /` first, then `disk.free_pct /srv` among the defaults |
| volumes `/` at 40% and `/srv` at 12%, both fresh | the default row is `disk.free_pct /srv` |
| a fresh removable volume at 3%, `/` at 40% | the default row is `disk.free_pct /`: a removable volume is never a default, plugged in or not |
| every volume removable | no disk default row |
| volumes `/a` and `/b`, both at 12.0% | the default row is `/a`'s, the first in the page order |
| a Mac with Time Machine destinations backed up 2 days and 9 days ago, both fresh | the 9-day one |
| the 9-day destination stale, the 2-day one fresh | the 2-day one |
| a site | `site.requests_per_min`, `site.server_error_pct`, `site.response_p95_seconds` |
| a Google Drive service node | `gdrive.free_pct` |
| a Mac with no `systemd` series | no `systemd.failed_units` row |
| a node of a class the file introduces, reporting `uptime.boot_seconds`, `queue.depth` and `net.rx_bytes`, nothing needing attention | `net.rx_bytes`, then `queue.depth`: the first two in the page order |
| an item whose only ranking series is sixth by rank on the hub | that series as its first row |
| exactly six series qualifying | six rows, no "more" line |
| four series needing attention and four defaults besides them | six rows and "2 more" |
| a series needing attention that is also a default | one row, in the place attention gives it |
| a series at `critical` | its row marked critical |
| a series at `critical` that also ranks | its row marked critical |
| a series at `critical` that is stale | its row marked as holding no fresh data, not critical |
| the first two rows | each with a line of its series' last 24 hours |
| the third row on | no line |
| a series with a six-hour gap in its last day | its line broken across the gap |
| a series with a single point in its last day | that point on its own |
| a series with no point in its last day | its row without a line |
| any value | formatted in its unit as `/debug` formats it |
| a row of `disk.free_pct` labelled `{mount: /srv, fs: ext4, removable: "false"}` | `disk.free_pct /srv`; no `ext4` |
| a row of `queue.depth` labelled `{queue: payments}` | `queue.depth queue=payments` |
| a row on a site's card | without `site=blog-a`: the card already names the site |
| any card | a link to that item's page, carrying the language |

### The card {#card}

How a card lays out what the rows above choose
([2026-10-25 metric names](../log/2026-10-25-metric-names.md#the-card) records the style
chosen). Its state's word is a pill, and a stripe runs along its left edge, both in the
colour the timeline's lanes draw that state ([timeline](timeline.md#lanes)). The first two
rows are its main numbers, side by side however narrow the screen: each its name, its value
large with its mark before it, and its line of the day under it, the area under the line
filled. The rows after them follow in a run that wraps. A reason closes the card.

| State | The card |
|---|---|
| any card | its state's word as a pill, and a stripe of the same colour along its left edge |
| two rows or more | the first two as the main numbers, side by side, each with its name, value, mark and line |
| one row | that row alone as the main number, at the card's full width |
| a main number whose series holds a readable threshold | each of its `warning` and `critical` values at zero or above drawn across the line as a dashed line in its level's colour, the line's scale reaching it |
| a main number with no point in its last day | no line and no threshold lines |
| the third row on | in the run after the main numbers, each its name, its value and its mark, separated by a dot; "N more" at its end |
| the card's last report | at its foot, and nowhere else on the card |
| a card `critical` or `warning`, its first row at that level with a readable threshold holding a value for it | the reason: the row's name, "below" or "above" by the threshold's direction, and that value — "Free on /srv below 10.0%" — whatever the newest value, since a level holds until its value recovers past the band |
| the same, the threshold unreadable or holding no value for that level | the reason: the row's name and the level's word |
| a card `unusual` | the reason: the first row marked unusual, its name and its usual value, as "now" says it ([attention](attention.md#model)) |
| a card `ok` whose rows carry no mark | the reason "All within thresholds" |
| a card `ok` with a row marked as holding no fresh data | the reason: the first such row's name and that it holds no fresh data since its newest value |
| a card silent or with no fresh data | its last report as the reason |
| a card "reporting, nothing watched" | no reason: its pill says it |

### The overview page {#overview-page}

| Request | What the reader sees |
|---|---|
| `/overview` | the sections and their cards, under the tabs with "Overview" marked ([web](web.md#skins)) |
| `/overview?lang=ru` | every word in Russian, the language kept on every link |
| `/overview?x=1` | the overview: like the other skins, it reads `lang` and nothing else |
| a level change, a new value or a node falling silent while the page is open | on the page within 30 seconds without a reload ([web](web.md#live)), cards reordered as their states say |
| the state or the stored points cannot be read | the same failure `/debug` answers with |

### The item's page {#item-page}

| Request | What the reader sees |
|---|---|
| `/item?node=server-b` | `server-b`'s page: its name, "Computer", its state's word and when it last reported |
| `/item?node=sites&label.site=blog-a` | `blog-a`'s page: its name, "Site", and the time of its newest point |
| `/item?node=gdrive-a`, a `service` node that has stored nothing | its page, "Service", "no fresh data", last reported at the time the hub recorded it, and "No series yet" |
| any item's page | the card's rows as numbers, without lines, above the charts |
| any item's page | the tabs, none marked, as on `/history` |
| no `window` | every chart over the 24 hours ending now, "24h" marked among the window links |
| `&window=7d` | every chart over 7 days, "7d" marked |
| `&window=168h` | the same as `7d`, "7d" marked: the window is marked whatever the query spelled it as |
| `&window=12h` | every chart over 12 hours, no window link marked |
| any window link | the same item, that window and the language |
| a computer with `disk.free_pct /`, `load.avg_1m`, `memory.available_pct` and `uptime.boot_seconds` | four families headed by their names — "Disk", "Load", "Memory", "Uptime" in English — in the order of their ids `disk`, `load`, `memory`, `uptime` |
| two volumes, each with `free_bytes` and `free_pct` | four charts under the family `disk`: `/a`'s two, then `/b`'s two |
| `load.avg_1m`, `load.avg_15m` and `load.avg_5m` | under the family `load` in metric id order: `avg_15m`, `avg_1m`, `avg_5m` |
| 60 `queue.depth` series of one node | 60 charts |
| a removable volume that is stale | no chart for it |
| a series holding `warning: 10GB` and `critical: 5GB` | its chart with a yellow line at 10 GB named "warning" and a red one at 5 GB named "critical" |
| a series holding a `warning` value only | one line |
| `load.avg_15m` holding `above`, `warning: 4`, its values under 1 | the value axis reaching above 4, the line on the chart |
| a series holding `warning: -5` | no line for it: the value axis starts at zero |
| an unwatched series | no threshold line |
| a series whose stored threshold this build cannot read | no threshold line |
| an unusual series | its usual value beside its newest, as "now" writes it |
| a stale series | its chart drawn, its value marked as holding no fresh data |
| a series with no point in the window | its chart saying there is no data for this window |
| a chart | a link to the series' history page with the same window and the language, and a link to the page that sets what the series is judged by |
| any value, time or axis label | formatted as the history page formats it, in the reader's zone |
| `/item?node=server-b&lang=ru` | every word in Russian |
| a new value while the page is open | on the page within 30 seconds without a reload, the window kept |
| the state, the stored points, the thresholds or the event log cannot be read | the same failure `/debug` answers with |
| a `disk.free_pct` chart of `/srv` on `ext4` | named as the history page names it, `ext4` included |

### The item's changes {#changes}

| State | What the reader sees |
|---|---|
| `disk.free_pct /srv` of `server-c` moving from `ok` to `warning` at 10:40 | on `server-c`'s page, the line the timeline writes for it, under "Today" |
| a transition of `server-b` | not on `server-c`'s page |
| `server-c` falling silent and reporting again | both lines on `server-c`'s page |
| an anomaly of `server-c`'s load starting and ending | both lines |
| a transition of `shop-c`'s `site.server_error_pct` | on `shop-c`'s page, not on `blog-a`'s |
| `sites` falling silent | the line on every site's page |
| `shop-c`'s host `server-b` falling silent | the line on `shop-c`'s page and on `server-b`'s |
| 25 changes of the item's series and 5 of its node's silence | the newest 20 of the 30 |
| no change recorded for the item | "Nothing has changed yet" |
| a change of a series whose threshold was since removed | still listed: it happened |
| a level change while the page is open | in the changes within 90 seconds: the tick records it within a minute ([timeline](timeline.md#page)) |

### Addresses {#addresses}

| Request | Answer |
|---|---|
| `/item` without `node` | `400`, as a translated page |
| `/item?node=sites` without `label.site` | `400`: the sites are items one by one |
| `label.site` on a node that is not of class `sites` | `400` |
| `label.site` given twice, or empty | `400` |
| any other `label.*`, or a parameter outside `node`, `label.site`, `window` and `lang` | `400` |
| a `window` the history endpoint refuses | `400`, as it refuses it ([history](history.md#refusals)) |
| a node that is not an item | `404`, as a translated page |
| a site the file does not list for `sites`, or one without series | `404` |
| an item's page left open after its item is gone | the page as it was, under the notice that it is not being refreshed ([web](web.md#live)); a reload answers `404` |
| `HEAD` on `/overview` or an item's page | what `GET` answers, without the body |

## Invariants

- The overview and the item's pages show nothing the [State API](state.md), the history
  endpoint, the stored thresholds and the event log would not return at the same instant: no
  level, staleness, value, anomaly or change of their own.
- Every item is on the overview exactly once, and every series of an item is on its page at
  most once.
- A card in a state other than "ok" or "reporting, nothing watched" names its reason among
  its rows or by its silence: a series at that level, unusual or with no fresh data heads its
  rows, or the card reads "No series yet".

## Edge cases

- **A node whose sensors all fail** while its agent reports reads "no fresh data", as a
  laptop asleep does: the state cannot tell them apart ([attention](attention.md#model)),
  and its silence turns the card "silent" once past `silence_after`.
- **A sensor switched off in the configuration** leaves a watched series stale; it is a row
  marked as holding no fresh data, first on its card, for as long as its threshold is kept,
  as it is an item under "now".
- **A site removed from the file** loses its card at once. A threshold on its series keeps
  it under the timeline's "now" as no fresh data until the threshold is cleared; the
  overview shows the sites the file lists, and leaves that one to the timeline and `/debug`.
- **The site's host moved**: its series keep their label, so its card and its page carry on
  ([0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md)); its silence is
  judged by the host the file names now.
- **A volume's byte and percentage series** both needing attention take the first two rows,
  and their two lines have the same shape: they are two series
  ([0033](../decisions/0033-a-subject-is-a-series.md)).
- **A fresh hub** has no norms for about two days, so no card reads "unusual" yet.
- **A long window on a large item** reads every point of every series in it on each refresh,
  as that many history pages would; the page stays correct and grows slow, not wrong.

## Out of scope

- **A health score, a trend or a forecast** on a card — not built
  ([0046](../decisions/0046-an-overview-of-items-rendered-by-the-hub.md)).
- **A norm band drawn on a chart** — the State API carries the norm, not the band, and the
  band of the current hour drawn over a month would mislead.
- **Choosing a card's rows in the interface** — the defaults stand until they prove wrong.
- **Figures across items** — "6 of 7 reporting", "least free space anywhere": new fields of
  the core first.
- **Links to an item's page from the timeline or `/debug`** — the card is the way in for now.
- **Thresholds on the history page's own chart** — drawn here first; the history page keeps
  its own spec's step for it.

## Open questions

None.
