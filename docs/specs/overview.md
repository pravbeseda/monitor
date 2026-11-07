# Spec: Overview

- **Status:** approved
- **Owns:** the pages `GET /overview` and `GET /item` in `internal/hub` — the overview skin:
  a card per computer and service with a few numbers, a sortable table of the sites, and a
  page per item with every series of it — on a tile for a computer, charted for a site or
  a service — or gathered in its group's panel, and the changes it went through. What a level, a staleness or a
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
  [0046](../decisions/0046-an-overview-of-items-rendered-by-the-hub.md),
  [0051](../decisions/0051-a-sensor-groups-its-family-by-a-label.md),
  [0053](../decisions/0053-a-computer-card-is-lines-on-a-shared-scale.md),
  [0054](../decisions/0054-the-backup-line-gathers-the-backup-families.md),
  [0055](../decisions/0055-a-computer-card-line-is-its-values.md),
  [0056](../decisions/0056-cards-in-a-section-are-ordered-by-name.md),
  [0057](../decisions/0057-sites-are-a-sortable-table.md),
  [0058](../decisions/0058-a-computers-page-is-tiles.md)

## Purpose

The overview answers "how do things stand, in numbers": every computer and service as a
card with the few figures that say most about it — a computer's in the places every
computer card keeps them, so one reads against the next; a service's with how it moved over
the last day. The sites, many and alike, are one table that sorts by any of its figures. A
card or a site's row opens the item's page, where every series of it is on a tile for a
computer, charted over a chosen window for a site or a service, or gathered with its group
in a panel, beside what changed. It judges nothing: a card's colour is a level evaluation stored, an anomaly, a
silence or data that is or is not fresh, and every number is one the hub holds or, on a
computer's tile, the arithmetic of two it holds.

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
how many — in the order silent, critical, warning, unusual, no fresh data: "Computers · 4 ·
1 silent · 1 warning · 1 no fresh data".

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

**Cards in a section** stand in two runs
([0056](../decisions/0056-cards-in-a-section-are-ordered-by-name.md)): first the red ones —
critical or silent — then every other, each run by the item's name, so a card keeps its
place as it turns unusual or warning and back. By name means
character by character in the order of their code points, a capital letter read as its
small one in any alphabet and a digit as a character, not as part of a number; names equal
but for case by their bytes.
A site's name is its `site` label; a node's is the node.

**The page order of an item's series** — the order its page shows them in — is by family,
the part of a metric id before its first dot as on `/debug` ([history](history.md#page));
inside a family, the series without labels first, by metric id, then the labelled ones as
`/debug` orders the rows of a matrix and its columns.

**A card** shows the item's name and its state's word marked in its colour, and links as a
whole to the item's page. A computer's card lays its series out as
[below](#computer-card); a service's shows the item's rows, the first two with a line of
their last day. Sites are not cards but the rows of a table ([below](#site-table)). An item with no series reads "No series
yet" in place of them.

**An item's rows** are up to six of its series, each with its name, its newest value
formatted in its unit as `/debug` formats it, and at most one **mark**, the first that
applies: no fresh data when the series is stale, whatever level it stored; critical;
warning; unusual. The rows are taken in this order, a series at most once:

1. every series of the item that would be an item of the list of what needs attention by
   that list's kinds — a level, an anomaly, no fresh data ([attention](attention.md#model))
   — in that list's order, without its cap of five anomalies;
2. the item's **default series**, in the order of the table below;
3. when neither gives a row, the first two series in the page order.

When more series qualify by the first two steps than six rows hold, a service's card ends
with "N more", N being how many of them are not shown. On a service's card the first two
rows carry a line of the last 24 hours of their series, drawn
as the history page draws a series — broken across a gap, a lone point drawn on its own —
with no axes; a series with no point in that day has no line.

**Default series**, the product's choice of what says most about a kind of item:

| Metric | Which series, when the item has several |
|---|---|
| `disk.free_pct` | the volume with the least free among those not marked `removable: "true"` and not stale; among those not removable when every one is stale; none when every volume is removable |
| `memory.available_pct` | — |
| `load.avg_15m` | — |
| `systemd.failed_units` | — |
| `timemachine.backup_age_seconds` | the destination whose backup is oldest among those not stale; among all when every one is stale |
| `backup.success_age_seconds` | the job whose last success is oldest among those not stale; among all when every one is stale |
| `site.requests_per_min` | — |
| `site.server_error_pct` | — |
| `site.response_p95_seconds` | — |
| `gdrive.free_pct` | — |

A tie goes to the series first in the page order. A metric the item does not have gives no
row.

**A row's name** is its series' name ([metric names](metric-names.md#naming)) — `fs` and
`removable` left out — and **a small chart's name** is the same with a volume's filesystem
and its being removable; either leaves out the `site` label on a site's own row and page.
A family is headed by its family's name.

**A computer's card** — a card under "Computers" — puts each of its series where every
other computer card puts it, so one card reads against the next
([0053](../decisions/0053-a-computer-card-is-lines-on-a-shared-scale.md),
[0055](../decisions/0055-a-computer-card-line-is-its-values.md)). It holds:

- in its head, before the state's word, "failed units: N" when its `systemd.failed_units`
  holds a value above zero, carries a mark, or is all the card would show, N a whole number,
  in the colour of that series' mark — the muted colour when it carries none. That series is
  never a line;
- its **slots**, each one **line**, in this order:

  | Slot | Name | Its value | Beside it, muted |
  |---|---|---|---|
  | load | "Load" | `load.avg_15m` | `load.avg_1m`, `load.avg_5m` |
  | memory | "Free RAM" | `memory.available_pct` | `memory.available_bytes` |
  | disk | "Disk *mount* free" | `disk.free_pct` of the volume the default picks | `disk.free_bytes` of that volume |
  | backup | "Backup" | of the `timemachine.backup_age_seconds` and the `backup.success_age_seconds` the defaults pick, the older; a fresh one before a stale one | — |

  A slot with no series for its value — a disk slot with no volume the default picks is
  named "Disk free" — reads a dash where its values stand, so the lines below it keep their
  place; its muted series are not shown. A computer with none of the four values has no
  slots.

  The backup slot **gathers** every other series of the families `backup` and
  `timemachine` the computer has
  ([0054](../decisions/0054-the-backup-line-gathers-the-backup-families.md)): none of them
  is a line, a value, or counted in "N more", and their marks speak on the backup
  line, as a line says why it is marked below, so the backup's many series say one thing on
  the card. Gathering changes where a series is shown, not the card's state. A backup slot
  that reads a dash gathers none: they stay what they would be without it;
- then its **extra lines**: the series its rows' first step takes — what needs attention —
  that no line shows, in that order, at most two, and "N more" when more qualify. A
  computer with no slot and no such series has the first two series in the page order as its
  extra lines;
- at its foot, small and to the right, when it last reported: muted, and brighter when it is
  silent or holds no fresh data. It closes every computer card, so a card that stopped
  reporting keeps its lines where its neighbours keep theirs.

**A line** is a name at its left and the values at its right, nothing drawn between them
([0055](../decisions/0055-a-computer-card-line-is-its-values.md)). The values never wrap; a
name too long for the line is cut short. Lines are parted by a faint rule, and the name and
the values stand centred between the rules, however many rows the values and what the line
says under them take. Of the values, the muted ones come first, in the slot's order, then
the line's own value, each formatted as `/debug` formats it and in the colour of its
own mark — a muted one in the muted colour when it carries none. The line is faded when the
series of its own value holds no fresh data — all but the values that carry the mark of a
level or an anomaly. Under
them the line says why it is marked, by the most urgent mark among its series — critical,
warning, unusual: "threshold X" when that series' threshold is readable and holds a value
for its level, otherwise the level's word; "usually X" with its norm when it is unusual.
Among equals the line's own comes first, then the muted ones, then those the slot
gathers in the page order. A gathered series speaks by the same marks, and by one more below
unusual: holding no fresh data while watched, as attention lists it
([attention](attention.md#model)). Only the most urgent speaks; the others are on the
item's page. One that speaks is said by its mark's word alone, in its mark's colour, never
faded with its line, as no value of it stands beside the word to say whose it is; the values
keep their own colours. On hover, and to a screen reader, the word names
that series as a row names it and says why as above, "Newest archive borg job-a: usually
4.0 h" — its mark's word when there is no more to say. A slot's line is named by its slot,
an extra line by its row name; on hover, either names the series of its own value as a row
names it.

**The sites** are a table rather than cards, a row per site
([0057](../decisions/0057-sites-are-a-sortable-table.md)), under the section's heading. Its
columns, in this order:

| Column | Header | A row's cell |
|---|---|---|
| site | "Site" | a dot in the colour of the site's state, then its name, linking to its page; the state's word on hover and to a screen reader |
| requests | "Requests/min" | `site.requests_per_min` |
| day | "Last 24 h" | a line of the last 24 hours of `site.requests_per_min`, drawn as a row's line is ([rows](#rows)) on the site's own scale, the area under it filled, with no threshold drawn on it and nothing for a screen reader; faded with the requests cell; empty when that day holds no point of it |
| views | "Views/day" | `site.pageviews_24h` |
| errors | "Server errors" | `site.server_error_pct` |
| latency | "Response p95" | `site.response_p95_seconds` |
| also | "Also" | what needs attention that no other column shows, in the order attention lists it, at most two, each its row name and its value in its mark's colour, then "N more"; after them, once the site is silent or holds no fresh data, when it last reported, which is not one of the two |

**A value cell** reads its series' newest value as `/debug` formats it, in the colour of its
mark, and under it, small, why it is marked as a computer's line says it; a dash when the
site has no such series. It is faded when its series holds no fresh data, and on hover names
its series as a row names it. A site's row carries no reason: its cells and their marks say
it.

**Sorting.** Every header but "Last 24 h" and "Also" is a link that sorts the rows by its
column, and the sort is the address's `sort`: a column's key — `site`, `requests`, `views`,
`errors`, `latency` — for ascending, the key after `-` for descending. A header links to its
column's first direction — "Site" ascending, a value column descending, from the largest
value down — but the header the rows are sorted by links to the other direction, so a
second click reverses it; nothing returns to the default but an address without `sort`.
The sorted header shows ▲ ascending or ▼ descending, and a screen reader is told the
direction. A chosen sort orders the rows by that column alone, the red ones no longer
first: "Site" by name as cards are ordered by name ([order](#order)), a value column by the
stored value, a stale one by its value too. Rows with no value in the column stand last,
and equal values stand by name, A to Z, whichever the direction. The sort is in the page's
address, so a refresh and a shared link keep it; a sort link keeps the language, while the
tabs and a site's link to its page drop the sort. A `sort` the table has no column for is
ignored. With no sort chosen the rows stand as cards do ([order](#order)), and the sort
orders nothing else on the page.

**Computer cards are narrower** than a service's, so at the page's full width four stand
in a row where three service cards do.

On a narrow screen the cards flow one to a line, each section's heading above its own, and
the sites table scrolls sideways, the site column staying in view.

**The item's page** has, from the top:

- the item's name, its section's singular ("Computer", "Site", "Service"), its state's word,
  and when it last reported, whatever its state;
- on a computer's page, its [tiles](#computer-page), then its panels; on a site's or a
  service's page, these two:
  - the item's rows, as numbers, without lines;
  - every series of the item that `/debug` would show — a removable volume that is stale left
    out ([history](history.md#page)) — under its family's name: in its group's panel, below,
    when its family has a group label and the series carries it, otherwise as a small chart
    over the window; "No series yet" when there is none. The panels of every family come
    first, then the window links 24h / 7d / 30d, as the history page offers them and marks
    the open one ([history](history.md#page)), shown whatever the page holds, then the charts
    of every family; the panels and the charts each in the page order. A panel shows the
    newest values, so the window does not change them. A family with both is headed above
    its panels and again above its charts;
- the changes of the item: the newest 20 of its series' transitions and anomaly starts and
  ends, and the silence transitions of its node — for a site, of the `sites` node and of its
  host — written and grouped by day as the timeline writes them
  ([timeline](timeline.md#changes)).

**A computer's page** — the page of an item under "Computers" — takes every series of the
item that `/debug` would show, a removable volume that is stale left out, and lays them out
as **tiles**, then the panels as every item's page draws them, each family's under its name ([0058](../decisions/0058-a-computers-page-is-tiles.md)). It has no rows as numbers,
no small chart and no window links; a `window` in its address is accepted and reaches the
panels' links alone. A series a panel gathers is on no tile; every other one is on exactly
one; "No series yet" when there is none. The tiles stand under no
family heading, in the page order of their first series, as many to a row as the page's
width holds, one to a row on a narrow screen.

| Tile | One for | Its name | Its values | Its bar | Its line, and its window |
|---|---|---|---|---|---|
| volume | each set of labels the item's `disk.free_bytes` and `disk.free_pct` series carry | the labels as a panel's generic row names them, filesystem and removable included — "/srv · ext4" — or the family's name when there are none | `disk.free_bytes`; then, muted, "of *total*" and `disk.free_pct` | the share used | `disk.free_pct`, 10 days |
| memory | the unlabelled `memory.available_bytes` and `memory.available_pct` | the family's name | as a volume's | as a volume's | `memory.available_pct`, 24 hours |
| load | the unlabelled `load.avg_1m`, `load.avg_5m` and `load.avg_15m` | the family's name, then, muted, the minutes of the averages it has — "1 · 5 · 15 min" | those averages, in that order, parted by " · " | — | `load.avg_15m`, 24 hours |
| uptime | the unlabelled `uptime.boot_seconds` | its series' name | its value | — | none, 24 hours |
| failed units | the unlabelled `systemd.failed_units` | its series' name | its value as a whole number | — | none, 24 hours |
| generic | each other series, a labelled one of the memory, load, uptime or failed-units metrics among them | its small chart's name | its value | — | the series, 24 hours |

A tile shows the series of its kind the item has, and no more: what a missing series would
show is left out. A volume or the memory without its free bytes leads with its free share,
not muted.

- **A value** is its series' newest value formatted as `/debug` formats it — the failed
  units as a whole number — coloured and underlined by its mark as a panel's figure is. On
  hover and to a screen reader it names its series as a row names it, and its mark's word
  after. It links to its series' history page over its tile's window, in the page's
  language, whether or not a line is drawn. The tile's name, the total and the parting
  words are plain text.
- **The total** is the free bytes divided by the free share, formatted as the free bytes
  are; it is shown when the tile has both and the share is from 1% to 100%, as a share
  rounded to two decimals says too little of a fuller volume.
- **The bar** is a track filled to the share used, `100 − free %`, drawn when the free share
  is from 0% to 100%: in the colour of the free share's mark when that mark is `warning`
  or `critical`, neutral otherwise; a tick across it, in its level's colour, at
  `100 − value` for each value the free share's readable threshold holds, a value outside
  0 to 100 drawing none. A screen reader is told "used" and the share used, formatted as a
  percentage is: "used 76.0%".
- **The line** is drawn as a service card's line is — no axes, broken across a gap, a lone
  point on its own, its series' readable threshold dashed across it — over its window
  ending now; it is not a link, and a screen reader skips it, as its values link to the
  same page. A window holding no point of it draws none.
- **The state of a tile** is its series' as a panel's state is, by the rules of an item's
  state — never "silent" — shown as the colour of the tile's edge, as a panel's edge shows
  it, faint for no fresh data and for reporting with nothing watched, and said by its word
  to a screen reader.
- **A fresh unusual series** of a tile says its usual value under the values, as "now"
  writes it: the first of them in the order of its values, when several are.
- A tile shows no time of its own and no link to the thresholds' form: the page's head says
  when the item last reported, and its history page links to the form.

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

**A panel** is as wide as its table needs, never stretched across its row. It is one group
of the item: its series of one family whose group label is
declared ([metric names](metric-names.md#groups)) that hold one value of that label
([0051](../decisions/0051-a-sensor-groups-its-family-by-a-label.md)). A family's panels are
ordered by that value, byte by byte. A panel shows:

- the value as its title, quoted as a label value is quoted in a series' name
  ([metric names](metric-names.md#naming)), and the group's state word marked in its colour — the first of
  "no fresh data", "critical", "warning", "unusual", "ok" and "reporting, nothing watched"
  that holds for its series by the rules of an item's state; a silence is a node's, so a
  panel is never "silent";
- every series of the group by its newest value, formatted in its unit, linking to the
  series' history page with the page's window and language, except where a panel of its
  own says otherwise below. The mark a card's row gets is the value's colour and a dotted
  underline, not a word beside it: the word stays for a screen reader and on hover. A
  panel draws no line: the chart is one click away.

The labels of a group's series that a panel names are those other than the group label —
and, on a site's page, other than `site`, as everywhere on it. A group whose every series
the page leaves out has no panel.

**The generic panel** is a table of the group: a row per set of the labels it names —
whatever their keys, so one panel holds what `/debug` splits across matrices — named by
those labels as `/debug` names a matrix row by its own, nameless when there is none, in
the page order; a column per metric, headed as a `/debug` matrix column is, the metric id
on hover, in metric id order; a cell its series' value and mark, a dash where the row has
no series of that metric, as on `/debug`.

**A panel of its own** replaces the generic one for a family the skin knows. The one there
is today is the backup panel, for the family `backup`. It is a table like the generic one,
with these differences:

- a row per job — a set of the labels it names that is `job` alone — in the order of the
  job's values, byte by byte, named by the job quoted as a panel's title is; then a row
  per other set of those labels, in the page order, named as a generic row is;
- a row's `backup.failed` series has no column: the row's name stands for it. The name
  links to that series' history page and takes its mark as a figure does, the word kept
  for a screen reader and on hover. The value is said on hover and to a screen reader
  too: "last run ok" for 0, "last run failed" for 1, otherwise "last run" and the
  value as `/debug` formats it. A nameless row's name is those words, shown. A row
  without that series has a plain name: no link, no colour, nothing on hover;
- the columns are those of the metrics below that any row has, in this order, then a
  column per other metric but `backup.failed`, in metric id order; every column is
  headed as a generic column is;
- the count of archives reads as a whole number, rounded half away from zero and not
  grouped, anything else as `/debug` formats it: an age is a duration, with no "ago".

| Series | Cell en | Cell ru |
|---|---|---|
| `backup.success_age_seconds` | 4.0 h | 4,0 ч |
| `backup.latest_archive_age_seconds` | 7.0 h | 7,0 ч |
| `backup.archives` | 10 | 10 |
| `backup.stored_bytes` | 5.1 GB | 5,1 ГБ |
| `backup.latest_archive_original_bytes` | 2.3 GB | 2,3 ГБ |

The name is coloured only when the flag carries a mark: unwatched, fresh and usual, it is
plain whatever its value ([0033](../decisions/0033-a-subject-is-a-series.md)).

## Behaviour

One row = one test. Anchors: `spec: overview.md#<heading>`. Words quoted below are the
English catalogue's; every one of them has its Russian. `sites` is the node of class
`sites`.

### Items {#items}

| State | What the reader sees |
|---|---|
| nodes `server-b` (class `server`) and `laptop-a` (class `laptop`) that have reported | two cards, both under "Computers" |
| a node of a class the file introduces | its card under "Computers" |
| `sites` whose file entry lists `blog-a` and `shop-c`, both with series | two rows of the sites table, `blog-a` and `shop-c`, and none named after `sites` |
| a site the file lists that has no series yet | no row |
| a series of `sites` labelled with a site the file does not list | in no row; on `/debug` |
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

| State | The item's word: a card's pill, a site's on hover over its dot |
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
| `sites` silent | every site's row "silent" |
| `shop-c`'s host `server-b` silent, `blog-a` served by `server-a` and fresh | `shop-c` "silent", `blog-a` as its own series say |
| `blog-a`'s series all stale while `shop-c`'s are fresh and both hosts report | `blog-a` "no fresh data" |

### Order of cards {#order}

| State | Order inside the section |
|---|---|
| `server-a` `ok`, `server-b` `critical`, `server-c` silent | `server-b`, `server-c`, `server-a`: the red ones first, each run by name |
| `server-a` `critical`, `server-b` silent | `server-a`, `server-b`: silent and critical are one run |
| `server-a` silent, `server-b` `critical` | `server-a`, `server-b` |
| `laptop-a` with no fresh data, `server-d` reporting with nothing watched, `server-e` `ok` | `laptop-a`, `server-d`, `server-e` |
| `server-a` `ok`, `server-b` `warning`, `server-c` unusual, `server-d` `ok` | `server-a`, `server-b`, `server-c`, `server-d`: by name |
| `Server-f` and `server-e` | `server-e`, `Server-f`: a capital as its small letter |
| `Сервер-б` and `сервер-а` | `сервер-а`, `Сервер-б`: in any alphabet |
| `ёж-а` and `яр-б` | `яр-б`, `ёж-а`: by code point, not by a language's alphabet |
| `server-a` and `Server-a` | `Server-a`, `server-a`: equal but for case, by their bytes |
| `server-10` and `server-9` | `server-10`, `server-9`: no number is read |
| sites `shop-c` `ok` and `blog-a` | `blog-a`, `shop-c` |
| sites `shop-c` `critical` and `blog-a` | `shop-c`, `blog-a` |
| `server-c` turning unusual or `warning` while the page is open | it keeps its place |
| `server-c` turning `critical` while the page is open | it moves to the red run at the top |

### Rows {#rows}

An item's rows: what a site's or a service's page lists as numbers and a service's card
shows; a computer card's extra lines take their first step. The item below is a service,
whatever series it reports, unless the row names another kind.

| State | Rows of the item |
|---|---|
| an item with `disk`, `memory`, `load` and `systemd` series, `/` its least-free volume, nothing needing attention | `disk.free_pct /`, `memory.available_pct`, `load.avg_15m`, `systemd.failed_units` |
| the same item, `load.avg_15m` unusual | `load.avg_15m`, `disk.free_pct /`, `memory.available_pct`, `systemd.failed_units` |
| volumes `/` at 40% free and `/srv` at 12%, the threshold on `disk.free_bytes` of `/` at `warning` | `disk.free_bytes /` first, then `disk.free_pct /srv` among the defaults |
| volumes `/` at 40% and `/srv` at 12%, both fresh | the default row is `disk.free_pct /srv` |
| a fresh removable volume at 3%, `/` at 40% | the default row is `disk.free_pct /`: a removable volume is never a default, plugged in or not |
| every volume removable | no disk default row |
| volumes `/a` and `/b`, both at 12.0% | the default row is `/a`'s, the first in the page order |
| an item whose `backup.latest_archive_age_seconds` is unusual | that series among the rows needing attention |
| Time Machine destinations backed up 2 days and 9 days ago, both fresh | the 9-day one |
| the 9-day destination stale, the 2-day one fresh | the 2-day one |
| backup jobs that last succeeded 2 days and 9 days ago, both fresh | the 9-day one |
| the 9-day job stale, the 2-day one fresh | the 2-day one |
| a site | `site.requests_per_min`, `site.server_error_pct`, `site.response_p95_seconds` |
| a Google Drive service node | `gdrive.free_pct` |
| an item with no `systemd` series | no `systemd.failed_units` row |
| an item reporting `uptime.boot_seconds`, `queue.depth` and `net.rx_bytes`, nothing needing attention | `net.rx_bytes`, then `queue.depth`: the first two in the page order |
| an item whose only ranking series is sixth by rank on the hub | that series as its first row |
| exactly six series qualifying | six rows, no "more" line |
| four series needing attention and four defaults besides them | six rows and "2 more" |
| a series needing attention that is also a default | one row, in the place attention gives it |
| a series at `critical` | its row marked critical |
| a series at `critical` that also ranks | its row marked critical |
| a series at `critical` that is stale | its row marked as holding no fresh data, not critical |
| the first two rows of a service's card | each with a line of its series' last 24 hours |
| the third row on | no line |
| a series with a six-hour gap in its last day | its line broken across the gap |
| a series with a single point in its last day | that point on its own |
| a series with no point in its last day | its row without a line |
| any value | formatted in its unit as `/debug` formats it |
| a row of `disk.free_pct` labelled `{mount: /srv, fs: ext4, removable: "false"}` | `disk.free_pct /srv`; no `ext4` |
| a row of `queue.depth` labelled `{queue: payments}` | `queue.depth queue=payments` |
| a row on a site's page, an entry under "Also" in its row | without `site=blog-a`: the row and the page already name the site |
| any card | a link to that item's page, carrying the language |

### The card {#card}

How a service's card lays out what the rows above choose
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

### A computer's card {#computer-card}

Unless a row says otherwise, the computer is a Linux server reporting `load`, `memory`,
`disk` with the one volume `/`, `systemd.failed_units` 0 and a backup job, every series
fresh, nothing needing attention and no threshold readable. Values are as the English
catalogue writes them. A row naming `job-a` or a gathered series has the backup job
labelled `backup: borg`, `job: job-a`, reporting `backup.success_age_seconds`,
`backup.latest_archive_age_seconds` and `backup.failed` 0.

| State | The card |
|---|---|
| any computer card | its state's word as a pill and a stripe of its colour, as [a card](#card) has; no line of the day, and no reason at its foot |
| the computer above | lines "Load", "Free RAM", "Disk / free", "Backup", in that order, and nothing after them |
| a Mac with Time Machine and no `systemd` | the same four lines |
| a computer with no backup series | "Backup" a dash, in fourth place |
| a computer with no `disk` series | "Disk free" a dash in third place, "Backup" still fourth |
| every volume removable | "Disk free" a dash |
| `load.avg_5m` and no `load.avg_15m` | "Load" a dash; `load.avg_5m` not shown |
| `load.avg_5m` at `critical` and no `load.avg_15m` | "Load" a dash; `load.avg_5m` an extra line |
| a computer reporting only `uptime.boot_seconds` and `queue.depth`, nothing needing attention | no slot; `queue.depth`, then `uptime.boot_seconds`, as its extra lines |
| a computer with no series | "No series yet", no slot |
| a computer reporting | when it last reported, at its foot, muted |
| a silent computer | when it last reported, at its foot, brighter |
| a computer with no fresh data | when it last reported, at its foot, brighter |
| `load.avg_1m` 0.52, `load.avg_5m` 0.61, `load.avg_15m` 0.67 | "0.52 0.61 0.67" on the load line, the first two muted |
| no `load.avg_1m` | "0.61 0.67" |
| `memory.available_pct` 38.5, `memory.available_bytes` 2 GB | the memory line reading the bytes muted, then "38.5%" |
| volumes `/` at 40% free and `/srv` at 12% | "Disk /srv free", `/srv`'s `free_bytes` beside it |
| a Time Machine destination backed up 2 days ago and a backup job that last succeeded 9 days ago, both fresh | "Backup" at the 9-day one |
| a Time Machine destination and a backup job of the same age | "Backup" at the job: the first in the page order |
| the 9-day job stale | "Backup" at the 2-day one |
| `backup.latest_archive_age_seconds` unusual with norm 4 h, the backup on the line unmarked | no extra line for it; "unusual" under the backup's value in the unusual colour, the value in the text colour; on hover and to a screen reader "Newest archive borg job-a: usually 4.0 h" |
| `backup.latest_archive_age_seconds` of `job-a` and of a second job `job-b` of `borg`, and `backup.archives` of `job-a`, all unusual, nothing else needing attention | the four slots and nothing after them: no extra line, no "N more" |
| three non-backup series needing attention and two gathered series unusual | two extra lines and "1 more" |
| `backup.failed` 1 at `critical`, the backup on the line unusual | "critical" under the backup's value in the critical colour: the most urgent mark speaks |
| `backup.failed` 1 at `warning` under its readable `above` `warning` of 0 | "warning" under the backup's value in the warning colour, and on hover "Failed borg job-a: threshold 0.00" |
| `backup.failed` 1 at `warning`, its threshold unreadable | "warning" under the backup's value, and on hover "Failed borg job-a: warning" |
| `backup.failed` of `job-a` and of `job-b` both at `critical` | on hover "Failed borg job-a: critical": the first in the page order |
| the backup on the line at `critical` under a readable `critical`, a gathered series at `warning` | the line's own threshold under the value; nothing names the gathered one |
| the backup on the line unusual, a gathered series unusual too | "usually X" with the norm of the line's own |
| a Time Machine destination on the line, the job's `backup.success_age_seconds` at `warning` | "warning" under the backup's value in the warning colour; no extra line |
| `backup.latest_archive_age_seconds` watched and stale, the backup on the line fresh and unmarked | "no fresh data" under the backup's value in the colour of no fresh data; no extra line for it |
| `backup.latest_archive_age_seconds` stale and unwatched | nothing under the backup's value |
| `backup.failed` 1 at `critical` and stale | "no fresh data" under the backup's value: a stale level is no level |
| the backup on the line stale, a gathered series watched and stale | the line faded; nothing under the values |
| the backup on the line stale, a gathered series unusual | the line faded but for "unusual" under the values, in the unusual colour |
| `backup.failed` 1 at `critical`, everything else `ok` | the pill "critical", the heading counting "1 critical" |
| a job that never succeeded — `backup.failed` 1 at `critical`, no `backup.success_age_seconds` and no Time Machine series | "Backup" a dash; `backup.failed` an extra line |
| `gdrive.free_pct` unusual | an extra line: `gdrive` is not a backup family |
| `systemd.failed_units` 2, unwatched and usual | "failed units: 2" in the head before the state's word, in the muted colour; no line for it |
| `systemd.failed_units` 2 at `critical` | "failed units: 2" in the critical colour; no extra line for it |
| `systemd.failed_units` 0 | nothing in the head |
| `systemd.failed_units` 2, stale | "failed units: 2" in the colour of no fresh data |
| `systemd.failed_units` 0, watched and stale | "failed units: 0" in the colour of no fresh data; no line for it |
| `systemd.failed_units` 0, unusual against a norm of 3 | "failed units: 0" in the unusual colour |
| a computer whose only series is `systemd.failed_units` 0 | "failed units: 0" in the head, no line |
| `memory.available_pct` unusual at 38.5, norm 46 | "38.5%" in the unusual colour, "usually 46.0%" under the values |
| `disk.free_pct /` at `warning` under a readable `warning` of 10 | its value in the warning colour, "threshold 10.0%" under the values |
| `disk.free_pct /` at `warning`, its threshold unreadable | "warning" under the values |
| `load.avg_15m` and `load.avg_1m` both at `warning`, under readable `warning`s of 0.5 and 0.4 | "threshold 0.50": the line's own first among equals |
| `load.avg_15m` unusual, `load.avg_1m` at `critical` under a readable `critical` of 8 | `load.avg_1m` in the critical colour, "threshold 8.00" under the values: the most urgent mark speaks |
| `memory.available_pct` usual, `memory.available_bytes` unusual with norm 3 GB | the bytes in the unusual colour, "usually 3.0 GB" under the values, "38.5%" in the text colour |
| `load.avg_15m` stale | the load line faded, nothing under the values |
| `load.avg_15m` stale, `load.avg_1m` fresh and unusual | the line faded but for `load.avg_1m`, in the unusual colour, and "usually X" under the values |
| `disk.free_bytes /` at `warning` while the disk line shows `/srv` | an extra line after the slots named as its row is, its values its own |
| three series needing attention that no slot shows | two extra lines and "1 more" |
| `memory.available_bytes` unusual | no extra line: the memory line shows it |
| a line's name on hover | the series of its own value, named as a row names it |
| a line's values on the narrowest card | on one line: the values never wrap, a long name is cut short |
| a state's word and failed units too long for the card's head beside the item's name | the name whole, the words beside it cut short |
| a line whose values say why under them | its name centred against both rows |
| a line, read by a screen reader | its name, its values and what it says under them: nothing is drawn to put in words |
| the overview at its full width | four computer cards in a row, three service cards |
| a service's card | as before: rows, the first two with a line of their day ([card](#card)) |

### The sites table {#site-table}

Unless a row says otherwise, the site `blog-a` reports `site.requests_per_min` 12.6,
`site.pageviews_24h` 88, `site.server_error_pct` 0.1 and `site.response_p95_seconds` 0.4,
and `shop-c` reports `site.requests_per_min` 3, `site.server_error_pct` 0 and nothing else,
every series fresh, nothing needing attention and no threshold readable. Values are as the
English catalogue writes them.

| State | The sites |
|---|---|
| any overview with sites | one table under the "Sites" heading, its headers "Site", "Requests/min", "Last 24 h", "Views/day", "Server errors", "Response p95", "Also"; no site card |
| the sites above | rows `blog-a`, `shop-c`; `blog-a` reading "12.60", its line of the day, "88.00", "0.1%", "0.4 s" and nothing under "Also" |
| `shop-c` with no `site.pageviews_24h` and no `site.response_p95_seconds` | a dash in those two cells |
| `site.requests_per_min` with no point in its last day | its value, and an empty "Last 24 h" cell |
| a site's row | its name a link to its page, carrying the language and not the sort; a dot in its state's colour, the word on hover and to a screen reader |
| `site.requests_per_min` unusual at 34.2, norm 9.5 | "34.20" in the unusual colour, "usually 9.50" under it |
| `site.requests_per_min` at `warning` under a readable `above` `warning` of 30 | its value in the warning colour, "threshold 30.00" under it, and no dashed line across its line of the day |
| `site.requests_per_min` at `warning`, its threshold unreadable | "warning" under its value |
| `site.requests_per_min` at `critical` and stale | its cell faded, its value in the colour of no fresh data, nothing under it: a stale level is no level |
| `site.server_error_pct` at `critical` | that cell's value in the critical colour, the dot in the critical colour, the row first while no sort is chosen |
| `site.server_error_pct` stale and unwatched | its cell faded, nothing under its value |
| any site's row | no reason: no "All within thresholds", no sentence |
| `site.requests_per_min` with a six-hour gap in its last day | its line broken across the gap |
| `site.requests_per_min` stale | its cell and its line of the day faded |
| a site with no `site.requests_per_min` | a dash under "Requests/min", an empty "Last 24 h" cell |
| a site whose only series is `uptime.x`, nothing needing attention | a dash in every value cell, nothing under "Also" |
| a site with no fresh data | its dot in the colour of no fresh data |
| `site.client_error_pct` at `warning` | its row name and its value in the warning colour under "Also" |
| `site.requests_per_min` unusual | nothing under "Also" for it: its column shows it |
| `site.client_error_pct` at `warning` and `uptime.x` unusual | under "Also", `site.client_error_pct` then `uptime.x` |
| three series needing attention that no column shows | two under "Also", then "1 more" |
| a silent site, or one with no fresh data | when it last reported, under "Also" |
| `shop-c`'s host silent, `site.client_error_pct` at `warning` | under "Also" its row name and value, then when it last reported |
| a site reporting | no last report in its row |
| a value cell on hover | the series of its value, named as a row names it |
| `/overview` | rows in the order of [cards](#order), no header marked, "Site" linking to `/overview?sort=site` and "Views/day" to `/overview?sort=-views` |
| the "Last 24 h" and "Also" headers | not links |
| a click on "Views/day" | `/overview?sort=-views`: rows from the most views down, the header marked ▼ |
| a click on "Views/day" while sorted by it from the most down | `/overview?sort=views`: from the fewest up, the header marked ▲ |
| `/overview?sort=site` | rows by name, the header marked ▲, linking to `/overview?sort=-site`; "Views/day" still linking to `/overview?sort=-views` |
| `/overview?sort=-site` | rows by name reversed, the header marked ▼ |
| `/overview?sort=site`, `shop-c` `critical` | `blog-a`, `shop-c`: a chosen sort keeps no red run |
| sorted by requests, `blog-a`'s stale | `blog-a` placed by its value |
| the sorted header, read by a screen reader | its name and its direction, ascending or descending |
| `/overview?sort=-views` | the computers' and services' cards in the order of [cards](#order) |
| sorted by views, `shop-c` with no views | `shop-c` last, whichever the direction |
| two sites with the same value in the sorted column | by name |
| `/overview?lang=ru&sort=-views` | the headers' links keep `lang=ru` |
| `/overview?sort=nonsense`, `sort=day`, `sort=also`, `sort=`, `sort=--views` or `sort=Views` | rows in the order of [cards](#order), no header marked |
| a narrow screen | the table scrolls sideways; the site column stays in view |
| a reader at `/overview?sort=-views` when the page refreshes itself | the rows still sorted by views |

### The overview page {#overview-page}

| Request | What the reader sees |
|---|---|
| `/overview` | the sections — their cards, and the sites as a table — under the tabs with "Overview" marked ([web](web.md#skins)) |
| `/overview?lang=ru` | every word in Russian, the language kept on every link |
| `/overview?x=1` | the overview: it reads `lang` and `sort` and nothing else |
| a level change, a new value or a node falling silent while the page is open | on the page within 30 seconds without a reload ([web](web.md#live)), the cards in the order of [Order of cards](#order), the sites' rows in the order the address chooses ([sites table](#site-table)) |
| the state or the stored points cannot be read | the same failure `/debug` answers with |

### The item's page {#item-page}

Rows of numbers, charts and window links are of a site's or a service's page — `gdrive-a`
below is a service; a computer's page is [its own](#computer-page).

| Request | What the reader sees |
|---|---|
| `/item?node=server-b` | `server-b`'s page: its name, "Computer", its state's word and when it last reported |
| `/item?node=sites&label.site=blog-a` | `blog-a`'s page: its name, "Site", and the time of its newest point |
| `/item?node=gdrive-a`, a `service` node that has stored nothing | its page, "Service", "no fresh data", last reported at the time the hub recorded it, "No series yet" and the window links |
| a site's or a service's page | the item's rows as numbers, without lines, above the panels and the charts |
| a family `q` grouped by `queue` with a panel `a`, beside `disk.free_pct /` | the panel `a`, then the window links, then the disk's chart: every panel comes before every chart, whatever the families' order |
| a page with panels and no chart | the window links below the panels |
| a page with charts and no panel | the window links above the charts |
| any item's page | the tabs, none marked, as on `/history` |
| no `window` | every chart over the 24 hours ending now, "24h" marked among the window links |
| `&window=7d` | every chart over 7 days, "7d" marked |
| `&window=168h` | the same as `7d`, "7d" marked: the window is marked whatever the query spelled it as |
| `&window=12h` | every chart over 12 hours, no window link marked |
| any window link | the same item, that window and the language |
| a service with `disk.free_pct /`, `load.avg_1m`, `memory.available_pct` and `uptime.boot_seconds` | four families headed by their names — "Disk", "Load", "Memory", "Uptime" in English — in the order of their ids `disk`, `load`, `memory`, `uptime` |
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
| `/item?node=gdrive-a&lang=ru` | every word in Russian |
| a new value while the page is open | on the page within 30 seconds without a reload, the window kept |
| the state, the stored points, the thresholds or the event log cannot be read | the same failure `/debug` answers with |
| a `disk.free_pct` chart of `/srv` on `ext4` | named as the history page names it, `ext4` included |

### A computer's page {#computer-page}

`server-b` is a computer. Words are the English catalogue's.

| State | What the reader sees |
|---|---|
| a volume `/srv` on `ext4` with 180 GB and 9.0% free | one tile, "/srv · ext4", reading "180.0 GB", then, muted, "of 2.0 TB" and "9.0%" |
| the same, in Russian | "180,0 ГБ", "из 2,0 ТБ", "9,0 %" |
| a tile's name | plain text, not a link |
| a volume with no labels | a tile named "Disk" |
| a fresh removable volume `/usb` on `exfat` | a tile "/usb · exfat · removable" |
| a stale removable volume | no tile for it |
| a volume with `disk.free_bytes` alone | "180.0 GB", no total, no bar, no line; the value linking with `window=10d` |
| a volume with `disk.free_pct` alone | "9.0%" leading, not muted; no total; its bar and its line |
| a volume at 0.5% free | no total |
| a volume at 105% free | no total and no bar |
| a volume at 24.0% free under `below`, `warning: 10`, `critical: 5` on its `disk.free_pct` | its bar filled to 76%, a warning tick at 90% and a critical tick at 95% |
| a threshold on `disk.free_bytes` alone | no tick |
| `disk.free_pct` unwatched, or its threshold unreadable | no tick |
| a `warning: 120` on `disk.free_pct` | no tick for it |
| the bar at 24.0% free, read by a screen reader | "used 76.0%"; in Russian "занято 76,0 %" |
| `disk.free_pct` at `warning` | "9.0%" in the warning colour, underlined; the bar in the warning colour; the tile's edge in the warning colour |
| `disk.free_pct` at `critical` and stale | the bar neutral; "9.0%" in the colour of no fresh data |
| `disk.free_pct` unusual | the bar neutral |
| a volume with points over the last 12 days | its line: the points of `disk.free_pct` in the 10 days ending now, none older |
| a volume with no point of `disk.free_pct` in 10 days | no line |
| a volume's value | a link to its series' history page with `window=10d` and the language |
| a tile's line | not a link, hidden from a screen reader |
| `memory.available_bytes` 5.2 GB, `memory.available_pct` 32.5% | a tile "Memory": "5.2 GB", "of 16.0 GB", "32.5%"; its bar filled to 67.5%; its line of `memory.available_pct` over the 24 hours ending now |
| load averages 0.42, 0.38 and 0.30 over 1, 5 and 15 min | a tile "Load", muted "1 · 5 · 15 min", reading "0.42 · 0.38 · 0.30"; its line of `load.avg_15m` over 24 hours |
| the load's 0.38, hovered | "Load 5 min" |
| `load.avg_15m` under `above`, `warning: 4` | its threshold dashed across the load tile's line |
| `load.avg_1m` and `load.avg_15m` | muted "1 · 15 min", reading "0.42 · 0.30" |
| `load.avg_1m` alone | muted "1 min", reading "0.42", no line |
| `uptime.boot_seconds` | a tile "Uptime" with its value, no line, the value linking with `window=24h` |
| `systemd.failed_units` at 2 | a tile "Failed units" reading "2", no line |
| `memory.available_pct` labelled `{zone: a}` | a generic tile of its own, not the memory tile |
| `timemachine.backup_age_seconds` of one destination | a generic tile named as its small chart would be, its value, its line over 24 hours |
| `memory.available_pct` unusual, norm 50.0% | "usually 50.0%" on the memory tile |
| `memory.available_pct` unusual and stale | no "usually" |
| `load.avg_1m` and `load.avg_15m` both unusual | the 1 min's usual value |
| a stale value | in the colour of no fresh data |
| a tile, read by a screen reader | its state's word: "warning" |
| `disk.free_pct` at `critical` and stale, `disk.free_bytes` fresh at `ok` | the tile's edge "ok": a stale level does not raise a tile |
| every series of a tile stale | the faint edge, "no fresh data" for a screen reader |
| a tile's series fresh and unwatched | the faint edge, "reporting, nothing watched" for a screen reader |
| the computer silent | no tile "silent"; each as its series say |
| a stale tile | no time of its own, no link to the thresholds' form |
| volumes `/b` and `/a`, the load, the memory and the uptime | the tiles `/a`, `/b`, "Load", "Memory", "Uptime", under no family heading |
| the family `backup` grouped by `backup`, and a volume `/` | the tile `/`, then the panels `vpn` and `web` under "Backups"; no tile of a backup series |
| a computer's page | no rows as numbers, no small chart, no window links |
| `/item?node=server-b&window=7d` | the tiles as without it; a panel's figures linking with `window=7d` |
| a computer with no series | "No series yet", no window links |
| `/item?node=server-b&lang=ru` | every word in Russian: "Память", "Нагрузка", "из", "1 · 5 · 15 мин" |
| a narrow screen | one tile to a row |
| a new value while the page is open | on the page within 30 seconds without a reload |
| the state, the stored points, the thresholds or the event log cannot be read | the same failure `/debug` answers with |

### Panels {#panels}

`server-b` below reports backup series of `web` / `copy` and `web` / `maintain`, and the
same for `vpn`, and its family `backup` is grouped by `backup`.

| State | What the reader sees |
|---|---|
| the family `backup` grouped by `backup` | under "Backups", a panel `vpn`, then a panel `web`, and no tile of a backup series |
| `server-b` declared that grouping; `server-c`, an older agent, reports backup series and no `groups` | panels on `server-c`'s page too: a group label belongs to the family |
| a group `backup: ""` beside `web` | a panel titled `""`, first |
| a group `backup: "my repo"` | a panel titled `"my repo"` |
| a group `backup: <b>x</b>`, its job `<b>y</b>` | both shown as those characters |
| `disk` grouped by `mount`, one group whose only series are of a stale removable volume | no panel for that group |
| the same family, no group label declared | no panel: each series on a tile |
| `disk` with no group label beside the grouped `backup` | the disk's tiles |
| on `gdrive-a`'s page, a family `q` grouped by `queue`, two series carrying `queue` and one without it | the panels first, then the window links, then the chart of the one without it; `q` headed above the panels and above the chart |
| a family grouped by a label none of its series carry | no panel: each series on a tile |
| a lone panel on a wide page | as wide as its table, not stretched across the row |
| every series of `web` fresh, none watched | `web` "reporting, nothing watched" |
| `web` / `copy`'s success age at `critical`, fresh | `web` "critical"; `vpn` as its own series say |
| `web` / `copy`'s success age at `critical` but stale, the rest of `web` fresh at `ok` | `web` "ok": a stale level does not raise a panel, as it does not raise an item |
| a series of `web` at `warning`, another unusual | `web` "warning" |
| every series of `web` stale | `web` "no fresh data" |
| the node silent past its `silence_after` | every panel "no fresh data", none "silent" |
| the backup panel of `web` | rows `copy`, then `maintain` |
| `copy` with `failed` 0, a success 4 hours ago, a newest archive 7 hours ago, 10 archives and 5.1 GB stored | columns `Since success`, `Newest archive`, `Archives`, `Stored`; `copy`'s cells `4.0 h`, `7.0 h`, `10`, `5.1 GB` |
| the same, in Russian | columns `С успеха`, `Свежий архив`, `Архивов`, `Занято`; cells `4,0 ч`, `7,0 ч`, `10`, `5,1 ГБ` |
| `maintain` beside it with `failed` 0, a success and 8 archives only | `maintain`'s cells `4.0 h`, a dash, `8`, a dash |
| `copy` with every known figure and `backup.queue_depth` named "Queue {backup} {job}" | columns `Since success`, `Newest archive`, `Archives`, `Stored`, `Newest archive data`, `Queue` |
| no job with newest archive data | no column `Newest archive data` |
| a column | its metric id on hover |
| 10.4 archives; 10.5; 1500 | `10`; `11`; `1500` |
| `copy` with `failed` alone | a row `copy` and no column |
| `failed` 0, no threshold on it | `copy` not coloured; "last run ok" on hover |
| `failed` 1, no threshold on it | `copy` not coloured; "last run failed" on hover |
| `failed` 2 | "last run 2.00" on hover |
| `failed` 1 at `critical` | `copy` coloured critical; "last run failed" and "critical" on hover and for a screen reader, neither shown |
| `failed` 0 at `critical`, stale | `copy` coloured as holding no fresh data |
| `copy`'s name | a link to its `backup.failed` history page with the page's window and the language |
| a row with no `backup.failed` series and its other series at `critical` | its name neither a link nor coloured, nothing on hover |
| a job `"my repo"`; a job `""` | rows `"my repo"` and `""` |
| the success age at `critical` | its value coloured critical, the word "critical" not shown but kept for a screen reader and on hover |
| a stale figure | coloured as holding no fresh data |
| an unusual figure | coloured unusual |
| a figure | a link to its series' history page with the page's window and the language |
| `copy` reporting `backup.queue_depth`, a metric the panel does not know, named "Queue {backup} {job}" | a column `Queue` after the known ones, its cell `2.00` |
| the same metric named "{backup} {job}" | headed `queue_depth` |
| `backup.b_depth` named "Alpha {backup} {job}" and `backup.a_depth` named "Zeta {backup} {job}" | columns `Zeta`, then `Alpha` |
| `backup.verify_age_seconds` 3600, a metric the panel does not know | its cell `1.0 h` |
| a series of `web` with `backup` alone | a nameless row after the job rows |
| a series of `web` with `backup` alone, its `failed` 1 | a row named "last run failed", a link to that series |
| a series `{backup: web, job: copy, host: h1}` beside `copy`'s and the nameless one | `copy`, then the nameless row, then `host=h1 job=copy` |
| a family `q` grouped by `queue`, with `q.depth {queue: a, shard: 1}` and `q.depth {queue: a, shard: 2}`, `q.depth` named "Depth {queue}" | a generic panel `a`: rows `shard=1`, `shard=2`, one column headed `Depth` |
| a generic panel whose series have no label besides the group label | one nameless row |
| a generic panel with `q.depth {queue: a}` and `q.depth {queue: a, shard: 1}` | the nameless row first, then `shard=1` |
| a generic panel with `q.depth` and `q.age_seconds` of `shard=1`, `q.depth` only of `shard=2` | a dash in `shard=2`'s `q.age_seconds` cell |
| a generic panel's cell at `critical` | its value coloured critical, the word not shown but kept for a screen reader and on hover |
| a family of `sites` grouped by `site` | on `blog-a`'s page, one panel titled `blog-a` |
| a family of `sites` grouped by `page`, on a site's page, its series labelled `site`, `page` and `path` | rows named by `path` alone |
| `/item?…&window=7d` | each figure's link carrying `window=7d` |
| a figure's new value, or a panel's state changing, while the page is open | on the page within 30 seconds without a reload |

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
  level, staleness, value, anomaly or change of their own — but for a computer tile's
  total and used share, arithmetic on two values they do return
  ([0058](../decisions/0058-a-computers-page-is-tiles.md)).
- Every item is on the overview exactly once, and every series of an item is on its page at
  most once.
- A card or a site's row in a state other than "ok" or "reporting, nothing watched" names
  its reason or its silence: a series at that level, unusual or with no fresh data heads a
  service's rows; is in a site's row, in its column or under "Also", in its mark's colour,
  a silent site's last report under "Also"; or is in
  a computer's head, on one of its slots in its mark's colour — said there by its word when
  the slot gathers it — or its first extra line; or the card reads "No series yet".

## Edge cases

- **A node whose sensors all fail** while its agent reports reads "no fresh data", as a
  laptop asleep does: the state cannot tell them apart ([attention](attention.md#model)),
  and its silence turns the card "silent" once past `silence_after`.
- **A sensor switched off in the configuration** leaves a watched series stale; it is a row
  marked as holding no fresh data, first on its card — in a site's row, its cell faded in
  its place, or under "Also" in the order attention lists it; on a computer's, its slot's line faded in its
  place, said on the backup line when the slot gathers it and the line does not already say
  it, faded or by a mark as loud, or an extra line — for as long as its threshold is kept, as it is an item under "now".
- **A site removed from the file** loses its row at once. A threshold on its series keeps
  it under the timeline's "now" as no fresh data until the threshold is cleared; the
  overview shows the sites the file lists, and leaves that one to the timeline and `/debug`.
- **The site's host moved**: its series keep their label, so its row and its page carry on
  ([0045](../decisions/0045-sites-are-a-node-their-hosts-report-for.md)); its silence is
  judged by the host the file names now.
- **A volume's byte and percentage series** both needing attention are two series
  ([0033](../decisions/0033-a-subject-is-a-series.md)): a computer's disk line shows both
  when the volume is the one it picks, and for another volume they are two extra lines.
- **A fresh hub** has no norms for about two days, so no card reads "unusual" yet.
- **A long window on a large item** reads every point of every series in it on each refresh,
  as that many history pages would; the page stays correct and grows slow, not wrong.

## Out of scope

- **A health score, a trend or a forecast** on a card — not built
  ([0046](../decisions/0046-an-overview-of-items-rendered-by-the-hub.md)).
- **A norm band drawn on a chart** — the band of the current hour drawn over a month would
  mislead; no skin draws the band ([0055](../decisions/0055-a-computer-card-line-is-its-values.md)).
- **Choosing a card's rows in the interface** — the defaults stand until they prove wrong.
- **Figures across items** — "6 of 7 reporting", "least free space anywhere": new fields of
  the core first.
- **Links to an item's page from the timeline or `/debug`** — the card is the way in for now.
- **Thresholds on the history page's own chart** — drawn here first; the history page keeps
  its own spec's step for it.

## Open questions

None.
