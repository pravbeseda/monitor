# 0046. An overview of items, rendered by the hub, opens a page per item

- **Status:** accepted
- **Date:** 2026-10-25
- **Source:** [overview spec](../specs/overview.md),
  [design notes](../log/2026-10-25-overview.md)

## Context

The timeline answers "is everything fine" and "what happened while I was not looking", in
words and coloured hours. Nothing on the hub answers "how do things stand, in numbers": the
values are on `/debug`, one table of every series, and one series at a time on its chart.
The reader asked for a view that gives the whole picture in figures and small charts at a
glance, and a page per server, site or service with all of its statistics behind it.

Two decisions frame it. [0041](0041-mission-control-folds-into-the-timeline.md) removed a
board that was a list of what needs attention, empty while all is well, and rejected giving
it quiet-time numbers for now because it "risks growing into a second table". And
[0035](0035-mission-control-is-rendered-by-the-hub.md) keeps skins server-rendered until one
needs a client.

The things a reader thinks of are not always nodes: the sites are one node whose series a
`site` label tells apart ([0045](0045-sites-are-a-node-their-hosts-report-for.md)).

## Decision

- **A third skin, the overview, at `/overview`**, rendered by the hub like the timeline,
  its tab between the timeline's and the table's. `/` still opens the timeline when no skin
  is remembered.
- **An item is a node, and the sites node is one item per site.** Each item has a card on
  the overview and a page at `/item`, which the card links to.
- **The overview groups items by who collects them**: nodes with an agent of their own as
  "Computers", the `sites` node's sites as "Sites", `service` nodes as "Services". The
  sections keep a fixed order; inside one, cards go from the worst state to the best, with
  no fresh data last — a laptop asleep is the everyday case.
- **A card shows a few numbers, not every series**: the series of the item that need
  attention by the kinds of [attention](../specs/attention.md), then a product default per
  metric, and when neither exists the item's first two series — so a metric no default
  names still shows. The first two carry a 24-hour line. A series is named by its metric id,
  as on every other page.
- **A card's state is the worst among its item's series**, in the words the timeline's lanes
  already use — silent, no fresh data, critical, warning, unusual, ok, reporting with
  nothing watched. A site is silent when its node or its host is. No health score.
- **The item's page draws every series of the item** as a small chart with its thresholds,
  grouped by family, under the card's numbers, beside the item's changes from the event
  log, with the window links of the history page.

## Consequences

- The tabs become three, and the web spec's skins table grows a row per address.
- The "second table" [0041](0041-mission-control-folds-into-the-timeline.md) feared is held
  off by the card's six rows at most; every series of an item is on its page, one item at a
  time, and the whole table stays `/debug`.
- A default per metric is the one place a sensor's metric needs code to reach a card first;
  the fallback and the attention rows keep a new metric from being invisible.
- The overview reads the state, the stored points and the event log, as the timeline and
  the history page do, and the stored thresholds, as the thresholds page does, to draw them
  on its charts; it adds nothing to the State API, which keeps the threshold out
  ([0030](0030-the-state-api-reports-the-stored-verdict.md)).
- When finance and health arrive, most likely as service nodes, "Services" will need
  splitting; the cards and the addresses stay as they are.
- A forecast ("full in five days") and a norm band drawn on the charts are left for later,
  each a field of the semantic engine first.

## Alternatives

- **A TypeScript client with a charting library.** Rejected for now: the toolchain and a
  second copy of the zone, catalogue and refresh logic that
  [0035](0035-mission-control-is-rendered-by-the-hub.md) avoided, for charts the hub already
  draws. An interactive chart can later replace one consumer of `/api/v1/history`.
- **Grafana over the history endpoint.** Rejected: a second surface where good and bad are
  declared, which [0001](0001-semantic-core-and-skins.md) reserves for the core, and a heavy
  dependency on the hub's host.
- **Numbers on the timeline's lanes instead of a new view.** Rejected: the timeline answers
  what changed; figures on every lane make it answer two questions badly.
- **An item per node only, the sites as one card.** Rejected: [0045](0045-sites-are-a-node-their-hosts-report-for.md)
  makes a site a thing of its own, addressed by its label wherever it is shown.
- **A general "item label" declared per metric family.** Rejected: one case exists; the
  rule for the reserved class is one check, and the general form can grow from it.
- **Series pinned to a card from the interface.** Deferred: a new stored setting before the
  defaults have shown they are wrong.
- **A health score per card.** Rejected, as in the [mission control notes](../log/2026-09-23-mission-control.md):
  no formula anyone could defend; the worst level already answers in one word, and a count
  of `ok` series reads well exactly when one is critical.
- **Sections per node class, or a `domain` key in the configuration.** Rejected: per class
  splits servers from laptops, which the reader wanted together; a key is a setting for one
  skin while only one domain exists.
- **Cards in a fixed order, as the timeline's lanes are.** Rejected for the cards: a section
  holds a handful, and the glance is for what is wrong; the sections themselves stay put.
- **Seven layouts were drawn** — tiles, sections, dense rows, rings, a map of every series,
  a briefing and a bento grid — and three item pages. The reader chose the sections and the
  grid of small charts; the rest are in the [design notes](../log/2026-10-25-overview.md).
