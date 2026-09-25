# Spec: Timeline

- **Status:** approved
- **Owns:** the page `GET /timeline` in `internal/hub` — the timeline skin: what needs
  attention now, how each node's last 24 hours looked, and the levels that changed. What
  a level, a staleness or a silence *is* stays with [evaluation](evaluation.md) and
  [state](state.md); the items of "now" are mission control's
  ([mission-control](mission-control.md#model)); the tabs, the zone, the shell and the
  refresh come from [web](web.md); every user-facing string comes from `internal/i18n`.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0002](../decisions/0002-push-not-pull.md),
  [0015](../decisions/0015-evaluation-on-a-tick.md),
  [0026](../decisions/0026-reader-time-zone-from-the-browser.md),
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md),
  [0035](../decisions/0035-mission-control-is-rendered-by-the-hub.md),
  [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md),
  [0037](../decisions/0037-skins-are-tabs-and-the-root-opens-the-last-one.md),
  [0038](../decisions/0038-a-lane-is-summarised-on-read.md)

## Purpose

Mission control answers "is everything fine now". The timeline adds "and what happened
while I was not looking": a strip per node over the last day, and the log of level changes
evaluation has kept all along ([evaluation](evaluation.md#persistence-and-restart)). It
judges nothing: every colour on it is a level evaluation stored, a silence it recorded, or
data that was or was not fresh. How a past hour is summarised from those records is
[0038](../decisions/0038-a-lane-is-summarised-on-read.md).

## Model

Only nodes the configuration names, and that have reported at least once, take part.

The page has three panels, headed "Now", "Last 24 hours" and "What changed".

**Now** is mission control's list of items: the same items, in the same order, with the
same headline, notice and links ([mission-control](mission-control.md#model)).

**A level's span.** A series' level begins at the `since` evaluation recorded for it —
the change that entered it, or its first evaluation, which records no change when it finds
`ok` — and runs to the change that left it, or to the present for the level it holds now.
A level whose leaving was never recorded — its threshold was removed, and evaluation
forgets a level without an event ([evaluation](evaluation.md#configuration-changes)) — is
known only at the instant it began, and a level that neither began nor ended with a
recorded change leaves nothing behind once it is forgotten. A series is *watched* at a
moment inside one of its spans.

**Fresh at a moment.** A series is fresh at an instant when the hub holds a point of it
stamped at or before that instant and no older than three of the interval of the sensor
its newest value names, as the configuration resolves it today — or of its node's longest
interval, for a series that names no sensor or whose sensor the configuration no longer
gives one. A node whose configuration runs no sensor at all has nothing fresh. It is the
bound [state](state.md#staleness) applies, without the rule that a sensor switched off is
stale outright: that rule is about now.

**A lane** is one node's last 24 hours, as 24 cells of one hour each, in the order of the
nodes' names so that no lane moves between refreshes. The node's name stands at its left.
Cells follow the reader's clock ([web](web.md#zone)): the last cell is the hour in progress,
up to now, each cell starts on the hour, and every fourth cell's hour is written under the
lanes. A cell takes the first of these that applies:

| Cell | Word | Drawn | Applies when, at some moment inside the cell |
|---|---|---|---|
| silent | "silent" | red, hatched | the node's silence subject stood at `critical` |
| no fresh data | "no fresh data" | empty | — none: no series of the node was fresh at any moment of the cell |
| critical | "critical" | red | a series of the node stood at `critical` while fresh |
| warning | "warning" | yellow | a series of the node stood at `warning` while fresh |
| ok | "ok" | green | a series of the node was watched and fresh |
| reporting | "reporting, nothing watched" | neutral grey | none of the above: fresh data, no threshold over any of it |

Hovering over a cell shows its hour and its word. A legend under the lanes names every
state with its colour.

**A change** is one transition from the event log: a series or a node's silence moving
between levels. It shows the time, a dot in the colour of the level it entered, the node,
the series named as mission control names it ([mission-control](mission-control.md#model)), the
level it left and the level it entered as the catalogue's level words, and the value that
produced it, formatted in its unit as `/debug` formats it. A node's silence reads "fell
silent" on entering `critical`, followed by how long the node had not reported — its
`silence_after` as the configuration resolves it today, since the log does not record the
one in force, written as the language writes a duration ("no report for 15.0 min") — and
"reporting again" on leaving it. The instant of a silence is when
the hub noticed it, one `silence_after` after the node's last report, which is why "now"
names an earlier time for the same silence. Changes are listed newest first, the newest 50
of the nodes that take part, grouped under the day they happened on in the reader's zone:
"Today", "Yesterday", or the day and month as the reader's language writes them
("21 September", "21 сентября"), with the year when it is not the current one.

## Behaviour

One row = one test. Anchors: `spec: timeline.md#<heading>`. Words quoted below are the
English catalogue's; every one of them has its Russian.

### Now {#now}

| State | What the reader sees |
|---|---|
| a volume at `critical`, a node silent, a series ranking | under "Now", the items mission control shows, in its order, with its headline |
| everything `ok`, nothing ranking | mission control's headline "All is well" and no item |
| nothing watched | mission control's headline and its nothing-judged notice |

### Lanes {#lanes}

Times are in the reader's zone; "reporting throughout" means some series of the node is
fresh at every moment.

| State | What the reader sees |
|---|---|
| two nodes that take part, the second in alphabetical order `critical` | one lane each, named, in the order of their names |
| a node the configuration no longer names | no lane |
| a node listed in the file that never reported | no lane |
| the hour 15:40 | 24 cells from 16:00 yesterday to 15:00 today, with 16:00, 20:00, 00:00, 04:00, 08:00 and 12:00 written under them |
| a node reporting throughout, nothing watched | 24 cells "reporting, nothing watched", none of them green |
| a node reporting throughout, one series watched and `ok` all day | 24 cells "ok" |
| a volume watched and `ok` from before the window, entering `warning` at 14:20 and `critical` at 16:05, reporting throughout | the cells before 14:00 "ok", 14:00 and 15:00 "warning", 16:00 on "critical" |
| a series at `warning` that recovered at 09:10, reporting throughout | the 09:00 cell "warning", the 10:00 cell "ok" |
| a series `critical` since 30 hours ago and still, reporting throughout | every cell "critical" |
| a series whose first evaluation found it `critical` at 11:30, reporting throughout | the cells before 11:00 "reporting, nothing watched" if nothing else was watched, 11:00 on "critical" |
| a laptop with `silence_after` 48h, whose every series was last fresh at 01:50 and fresh again from 07:40, nothing watched | the 02:00 to 06:00 cells "no fresh data"; the 01:00 and 07:00 cells "reporting, nothing watched" |
| a node silent from 09:10 until now | from the 09:00 cell on "silent" |
| a node silent from 09:10 whose series was `critical` from before | the 09:00 cell on "silent", the cells before it "critical" |
| a volume collected every 5m, at `critical`, its last point at 12:00, other series of the node fresh and unwatched | the 12:00 cell "critical", from 13:00 on "reporting, nothing watched" |
| a node that first reported at 12:30 | the cells before 12:00 "no fresh data" |
| a series entering `critical` at 10:00 whose threshold was removed at 12:00, nothing else watched, reporting throughout | the 10:00 cell "critical", from 11:00 on "reporting, nothing watched" |
| that threshold set again at 14:00 and the series found `ok` | the 10:00 cell "critical", 11:00 to 13:00 "reporting, nothing watched", 14:00 on "ok" |
| that threshold set again at 14:00 and the series found `critical` | the 10:00 cell "critical", 11:00 to 13:00 "reporting, nothing watched", 14:00 on "critical" |
| a series watched and `ok` from 08:00, its first evaluation, whose threshold was removed at 12:00, nothing else watched, reporting throughout | every cell "reporting, nothing watched": the level left no record |
| any cell | its hour and its word on hovering over it |
| the lanes | a legend naming "silent", "no fresh data", "critical", "warning", "ok" and "reporting, nothing watched" with their colours |
| the reader's zone `Asia/Kolkata`, 30 minutes off UTC | cells that start on the hour of the reader's clock |

### Changes {#changes}

| State | What the reader sees |
|---|---|
| a volume moving from `warning` to `critical` at 14:02 with 4% free | "14:02", a red dot, the node, the series named as mission control names it, "warning → critical" and its value formatted as `/debug` formats it |
| a series recovering to `ok` | the same line with a green dot, ending "→ ok" and its value |
| a series whose first evaluation found it `critical` | "ok → critical": evaluation records a first level as a change from `ok` |
| a node falling silent at 09:10, its `silence_after` 15m | "09:10", a red dot, the node, "fell silent", "no report for 15.0 min" |
| that node reporting again | a green dot, "reporting again" |
| a transition of a series | a link to that series' history page, carrying the node, the metric, every label and the language |
| a transition of a node's silence | a link to `/debug` |
| transitions yesterday and today | today's under "Today", yesterday's under "Yesterday", as the reader's zone counts days |
| a transition three days ago, in Russian | under its day and month, "21 сентября" |
| 60 transitions of nodes that take part | the newest 50 |
| 50 transitions of a node the configuration no longer names, and 10 of one it names | those 10, and none of the others |
| two transitions at the same instant, of `server-a` and `server-b` | `server-b`'s first: the reverse of the node, metric and labels order a tick records them in |
| a transition in September of last year, in English | under "21 September 2025" |
| a transition of a series whose threshold was since removed | still listed: it happened |
| no transition recorded | "No level has changed yet" |

### The page {#page}

| Request | What the reader sees |
|---|---|
| `/timeline` | the timeline, under the tabs with its own marked ([web](web.md#skins)) |
| `/timeline?lang=ru` | every word in Russian, the language kept on every link |
| a level change or a node falling silent while the page is open | on the page within 30 seconds, in now, its lane and the changes, without a reload ([web](web.md#live)) |
| the state, the log or the stored points cannot be read | the same failure mission control answers with |

## Invariants

- Every coloured cell and every change has a reason in what evaluation stored or in the
  points the hub holds: the page invents no level, and no level is shown over a moment
  when its series was not watched.
- A change is listed at most once.

## Edge cases

- **Past hours are judged by what the hub holds now.** An agent delivers what it buffered
  while it could not reach the hub ([agent](agent.md)), so a cell that read "no fresh data"
  can turn to a level once the points arrive; and switching a sensor's interval repaints
  the hours its series covers. Both follow from
  [0038](../decisions/0038-a-lane-is-summarised-on-read.md).
- **A threshold removed** leaves its level's end unknown, so the level shows only in the
  hour it began, as the rows above say.
- **A tick that lands while the page is being read** has its change counted: the change
  closes the span before it, so the level held until then is not cut short, and the next
  refresh shows the new one.
- **A fresh hub** has no log: its lanes show only the levels it holds now, from their
  `since`, and changes say none yet.
- **A threshold removed from a series at `ok`** that never changed level takes its green out
  of every past hour: nothing recorded it.
- **A daylight-saving change** inside the window labels one cell with a repeated or a
  skipped hour, as the reader's clock does; the lane stays 24 cells of one hour. Where the
  clocks shift by half an hour, the cell the shift cuts begins at the shift and lasts half
  an hour or an hour and a half, and every other cell stays on the hour.
- **A laptop that ticks hourly** stays fresh for three intervals after each report, so a
  sleep shorter than that shows no "no fresh data" cell.
- **A node that fell silent before the window** is "silent" from the first cell.

## Out of scope

- **Anomalies in the lanes and the changes** — an anomaly is computed on read and never
  stored ([0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)), so
  there is no history of one to show. Recording them is
  [#54](https://github.com/pravbeseda/monitor/issues/54). They appear in now, as on
  mission control.
- **A laptop falling asleep or waking** as a change: staleness writes no event, so the
  lanes show it and the changes do not. The prototype's "asleep" is "no fresh data" here,
  since the hub cannot tell sleep from a failure.
- **Ordering lanes by trouble**, as the prototype did: a lane that jumps on a refresh
  loses the reader's place.
- **A window other than 24 hours**, and paging through older changes.
- **Values in the lanes** — the history page draws them.

## Open questions

None.
