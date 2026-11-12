# 2026-11-12 — Naming the failed units

Issue #104: the failed-units alert says "2" and not which two. Outcome:
[ADR 0060](../decisions/0060-a-measurement-may-carry-notes.md).

## Transport

Two candidates were weighed:

1. **Notes on the count's measurement**, kept with the series' newest value only. This was
   chosen. It is the one way that meets "a recovered unit leaves no row behind" without
   bending staleness, and it adds no join between series.
2. **A labelled series per unit.** Rejected. The cost is everything a series carries: up to
   three intervals of afterlife on the page (ADR 0034), a row in the series table and in
   history for good, a place in the digest's count of unwatched series, and a message on
   the count that would have to read other series. ADR 0033 deleted exactly that join.

Reading the notes from the series when a message is sent was rejected as well. A message
retried on a later tick would put the old value beside the new list.

## The journal line

Left out and filed as #107. Three things settled it:

- journal text can carry secrets, and a note travels to the hub and to Telegram;
- the unit's last journal line is usually systemd's own "Failed with result" line, so it
  says nothing new;
- reading it costs one call per failed unit.

## Smaller choices

- **Bounds**: 20 notes of 128 characters at most. One notification of plain text then fits
  inside Telegram's 4 096 UTF-16 units, and the longest template-instance unit names seen in
  practice still fit. A digest of several such entries does not fit, which is why the
  Telegram channel now parts a long text into several messages. Before this change a digest
  of many entries could already be refused for being too long, and nothing parted it.
  A unit name longer than a note allows is cut and ends in `…`, not dropped.
  When ingest drops a note it keeps the measurement: a note must never cost the value the
  threshold works on.
- **What a note says**: the unit's name and its `Result`, as systemd prints them, for
  example `monitor-demo-fail.service (exit-code)`. A note is shown verbatim, so there is
  nothing in it to translate.
- **Which calls are made**: the count still comes from `NFailedUnits`. Names and results
  are read only when that count is above zero, with one `list-units` and one `show` for
  all the failed units together. Collection stays a fixed number of calls, not one per unit.
- **Where notes show**: the State API, messages, the computer page's tiles, the changes on
  the timeline and on an item's page, and the timeline's list of what needs attention. That
  covers every place a person reads the count's value next to something that needs action.
  The card head, `/debug` and the history pages still show the value alone.
- **Storage**: the series and the event log gain a column. Both tables are rebuilt by
  copying their rows, so the step can be replayed and sensor names are kept, which the
  earlier series rebuilds lost. A replay does lose the notes already stored: a series gets
  its notes back from its next value, but the event log's notes are gone for good. A
  separate notes table keyed by series and timestamp was rejected: it would mean a second
  join on every read of the newest values.
