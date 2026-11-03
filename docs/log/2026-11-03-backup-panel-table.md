# 2026-11-03 — The backup panel becomes a table

The first backup panel wrote each job as one line of dot-separated phrases ("copy · last
run ok · succeeded 1.7 h ago · …"). On a real page with three jobs per repository that
read as a mass of text: the same kind of figure sat at a different place on every line,
and every figure was underlined as a link. Seven mockups were compared (a table, a
transposed table, a headline with details folded, a column per job, tiles, two-tier
lines, one panel for both repositories); the reader chose the table, rows by job and
columns by figure.

## What was weighed

- **The last-run column.** It was dropped. The job's name stands for the failure flag
  instead: it links to the flag's chart and takes the flag's mark as its colour. A failure
  nobody watches is not coloured ([0033](../decisions/0033-a-subject-is-a-series.md)), so
  without more it would look the same as a success. The words "last run failed" were
  therefore kept on hover and for a screen reader. Making a failure red without a
  threshold was rejected as a first exception to 0033; default thresholds declared by
  the sensor are the way to get the colour from the start, and are a separate change.
- **Marks as colour, not words.** "critical" or "unusual" printed beside a value doubled
  the noise. The value's own colour carries the mark, and the word stays on hover and for
  a screen reader, as the overview's cards already do.
- **Column headings.** Hub words such as "succeeded" were drafted first and rejected: the
  hub names no metric ([metric-names.md](../specs/metric-names.md)). The headings are the
  names the sensor declares, with the labels left out, as on `/debug`. The cells keep the
  panel's own reading: "4.0 h ago", and the count of archives as a whole number.
- **One panel for both repositories.** Rejected: one colour would stand for two
  repositories, and it would undo the panel per value of the group label
  ([0051](../decisions/0051-a-sensor-groups-its-family-by-a-label.md)).
