# 0041. Mission control folds into the timeline, which becomes the primary view

- **Status:** accepted
- **Date:** 2026-09-28
- **Amends:** [0035](0035-mission-control-is-rendered-by-the-hub.md), which made mission
  control the primary view; and [0037](0037-skins-are-tabs-and-the-root-opens-the-last-one.md),
  which put it at `/board` and sent `/` there when no skin is remembered
- **Source:** [attention spec](../specs/attention.md), [timeline spec](../specs/timeline.md),
  [web spec](../specs/web.md#skins), [design notes](../log/2026-09-28-mission-control-folds.md)

## Context

Mission control was a list of what needs attention under a headline, empty while all is
well. The timeline, added a day later, shows that same list under "Now", in the same order
with the same headline, and adds each node's last 24 hours and the levels that changed. On
a quiet hub mission control was one phrase, "All is well", and the timeline said the same
phrase with the day behind it: the board answered nothing the timeline did not.

## Decision

- **Mission control is no longer a skin.** Its list stays, as what the timeline shows
  under "Now" ([attention](../specs/attention.md)).
- **The timeline is the primary view**: its tab comes first, and `/` opens it when no skin
  is remembered, or when the remembered one is gone.
- **`/board` answers nothing.** No redirect is kept for it.

## Consequences

- Two skins remain as tabs: the timeline and the table. A reader who remembered mission
  control lands on the timeline.
- A bookmark of `/board` is dead; a page left open there stops refreshing, and a reload
  says the address is gone.
- The list of what needs attention keeps its own spec, since the timeline consumes it and
  later skins may too.

## Alternatives

- **Keep mission control and give it quiet-time content** — what is closest to a threshold,
  how many nodes reported and when. Rejected for now: it needs a spec of its own and risks
  growing into a second table; the same content can later join the timeline's "Now".
- **Keep it as it is**, as a glance-only view for a phone or a wall. Rejected: the
  timeline's "Now" panel already opens first on a narrow screen, and two tabs showing the
  same answer cost a choice every visit.
- **Redirect `/board` to `/timeline`.** Rejected: one reader, and a route kept for a page
  that no longer exists is code with no behaviour of its own.
