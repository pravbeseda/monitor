# 0059. Show all series in two panes and open a series' settings in a dialog

- **Status:** accepted, amends [0029](0029-pages-refresh-by-fetching-their-own-address.md)
- **Date:** 2026-11-09
- **Source:** [history.md](../specs/history.md#table), [web.md](../specs/web.md#dialog),
  [design note](../log/2026-11-09-all-series-and-settings-ux.md)

## Context

`/debug` drew every node's every series on one long page: a date on every row, every value a
link, labels spelled `key=value`, and a word of explanation under some values that changed
the height of its row. Setting a threshold left the page for a form that could only be left
by the browser's back button and said nothing when it had saved.

The form is opened from four pages, so where it opens is one decision for all of them. Every
open hub page replaces its whole body when its data changes
([0029](0029-pages-refresh-by-fetching-their-own-address.md)), so whatever holds a half-typed
form has to survive that.

## Decision

- `/debug` is two panes: every node and its groups on the left, one group on the right. The
  group, the filter and the search live in the address, so a refresh, a bookmark and a shared
  link keep them.
- A filter or a search shows what it matches across every node on the right; "All" shows
  everything. Without one, the first group holding a problem opens, a problem being what the
  timeline would list for a series, so the two pages cannot disagree.
- A page holds back its refresh for what only the reader has: a search not yet applied, and
  a reload after an upgrade while a dialog or its notice is up. This amends 0029's promise
  that an open page is never quietly behind.
- A value's whole row or cell is one link, to the series' settings. The chart is one link
  further on.
- A link to the settings opens them in a modal dialog from any hub page, fetching the
  settings page and showing its form. No endpoint or fragment exists for it: the dialog is
  that page, so what it accepts and refuses is decided once. The link stays a link, and with
  scripting off it is the page.
- The dialog and its notice live outside `<body>`, so the page behind goes on refreshing and
  cannot touch what is being typed.

## Consequences

- The reader can no longer see every node at once by default; "All" is one click.
- Every string the dialog shows comes from the page it fetched, so the script carries no
  catalogue.
- The shell's stylesheet carries the form, because the form appears on every page.
- A search applies on Enter: it is a plain `GET`, and the page holds no list to filter.

## Alternatives

- **A side panel.** Keeps the table visible and suits walking through many series. Rejected
  by the reader, who preferred the form alone in front of them.
- **A row that expands.** A matrix cell has no row of its own, and the rows below jump.
- **The separate page, repaired.** The least code, but a round trip per series with dozens
  of series to configure. It remains as what a link is with scripting off.
- **A flat list of series.** A family of backups takes five rows per job where a matrix takes
  one.
- **Filtering inside the selected group.** A problem in another group would read as "nothing
  found" while the left pane showed it.
- **Pausing the refresh while the dialog is open.** Needs a guard on the timer and another on
  a fetch already in flight, and freezes the numbers behind the dialog; a dialog outside the
  body needs neither.
- **A search that filters as the reader types.** Needs every series in the page or a fetch
  per keystroke that keeps the focus across a body swap.
