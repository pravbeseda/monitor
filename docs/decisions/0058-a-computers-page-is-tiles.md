# 0058. Lay a computer's page out as tiles, a line only where a trend says something

- **Status:** accepted; amends [0046](0046-an-overview-of-items-rendered-by-the-hub.md) —
  a computer's page no longer charts every series
- **Date:** 2026-11-07
- **Source:** [overview.md](../specs/overview.md#computer-page)

## Context

A computer's page drew every series as a small chart over a chosen window. In use most of
them said nothing: a volume took two charts of one shape, its free bytes and its free
percentage; the three load averages drew three near copies; the uptime drew a saw and the
failed units a flat zero. The charts took the page while the figures that mattered — how
much is free of how much — were split across them. The reader looked at three layouts on a
synthetic computer — bars without lines, tiles with a line where a trend matters, tiles with
one large chart of the chosen tile — and chose the second, with a volume's line over ten
days and every other over a day.

## Decision

- A computer's page lays its series out as **tiles**, not small charts. The skin knows the
  families a computer reports and gives each a tile of its own: a volume's free bytes and
  percentage are one tile with the total they imply and a bar of the share used; the
  memory likewise; the three load averages one tile; the uptime and the failed units a
  tile each without a line. Any other series that no panel gathers is a generic tile of
  its own value.
- A tile draws a **line** where a trend says something, over a window fixed per tile: ten
  days for a volume, which fills slowly, and a day for every other. The page offers no
  window links; the history page, one click away, has them.
- A computer's page has no row of numbers above the tiles: the tiles are its numbers.
- The page of a site or a service keeps its numbers, charts and window links.
- A panel is as wide as its table needs instead of stretching across the row.

## Consequences

- What a tile is, which series it holds and what its line draws is the skin's knowledge of
  a family, as a panel of its own is ([0051](0051-a-sensor-groups-its-family-by-a-label.md));
  a family it does not know falls to the generic tile, so a new sensor shows without code.
- A volume's total is derived on the page from its free bytes and percentage; no sensor
  reports it and the State API gains nothing.
- A `window` in a computer page's address still reaches the panels' links and nothing
  else, so an old link keeps working.
- Each tile's line is one history read per refresh; a volume's covers ten days, reduced to
  at most a thousand points as every history read is.

## Alternatives

- **Bars without lines.** The densest page, but whether a volume is filling would be one
  click away, and that is the question a server's page is opened for.
- **Tiles and one large chart of the chosen tile.** A real chart with axes, but only one
  series at a time and the history page repeated inside the item's page.
- **A line on every tile.** A saw of the uptime and a flat line of the failed units are the
  noise this decision removes.
- **The sensor declares which series earn a line.** It would put a skin's choice into the
  agent's declaration, which describes data and not how a page draws it.
- **Every item's page as tiles.** The sites' traffic and the services' figures were not
  what the reader found wanting; changing them waits for a reason of their own.
