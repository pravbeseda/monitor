# 2026-10-08 — The series table as matrices

Once the sites reported, `/debug` became hard to read: five series a site, each a row of its
own, the site's name repeated five times, and no way to compare one figure across sites. The
answer is in [history.md](../specs/history.md#page) and [state.md](../specs/state.md#page).

## What was weighed

Four shapes were drawn as mockups on synthetic data.

- **Collapsible nodes with an index above them — rejected as the fix.** It shortens the page
  but leaves the sites' table as long as before once opened. It stays an option if the page
  grows long again.
- **Only what needs attention, the rest behind a link — rejected.** It hides what a debug
  view is for, and series without a threshold drop out of sight, which
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md) wants visible.
- **Matrices and collapsible nodes together — deferred**: the most comfortable, and the most
  work; the second half can follow on its own.
- **A matrix: a row per label set, a column per metric — chosen.** Thirty rows of six sites
  become six, and a column compares one figure across them. The same rule gathers a host's
  volumes, so the sites need no special case.

## How the matrix is cut

- **Only when two label sets share their keys — rejected.** Unplugging one of two drives
  would flip the node between a matrix and rows. Every labelled series is in a matrix, even
  of one row.
- **By label keys alone — rejected.** Two unrelated sensors using the same key would share a
  matrix of dashes. A matrix is one label key set within one metric family, the part of the
  id before its first dot, which also names the corner and leaves the headers short.
- **Columns from every series, shown or not — rejected**: an unplugged volume would leave a
  column of dashes.
- **The thresholds link in every cell — rejected**: it doubles the width of every cell. It
  moved to the chart a cell's value opens; rows of their own keep theirs.
- **A row ordered by its storage key — rejected**: that key sorts a volume's filesystem
  before its mount point. Rows go by what their labels name, and the list of what needs
  attention follows the same order.
