# 2026-09-28 — Mission control folds into the timeline

Using the hub, the user found mission control to be one phrase: on a quiet hub it said
"All is well" and nothing else, and the timeline's "Now" panel said the same above the
day's lanes and changes. The question was what the board is for.

Three answers were weighed:

- **Remove the board, make the timeline primary** — chosen. The timeline already contains
  the board whole; removing it costs a tab, a handler and a template, and leaves the list
  itself, which the timeline consumes.
- **Give the board quiet-time content** — the series closest to its threshold, how many
  nodes reported and when. It is the one thing no page shows, and it serves the
  prioritisation the project exists for; rejected for now because it needs its own spec and
  tends towards a second table. It can join the timeline's "Now" later.
- **Keep it as a glance-only view** — rejected: the timeline stacks "Now" first on a narrow
  screen, so a phone gets the glance anyway.

`/board` is left unanswered rather than redirected: the hub has one reader, and a redirect
would be a route with no behaviour of its own. The list's spec was renamed from
mission-control to attention, since it outlives the skin.

Decision: [0041](../decisions/0041-mission-control-folds-into-the-timeline.md).
