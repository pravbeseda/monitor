# 0060. A measurement may carry notes, kept with its series' newest value and its events, never in history

- **Status:** accepted; amends [0031](0031-a-table-of-series.md) — the series row also
  carries the notes its newest value came with
- **Date:** 2026-11-12
- **Source:** issue #104; [host-sensors.md](../specs/host-sensors.md#failed-units) on why
  failed units are a count; [design note](../log/2026-11-12-failed-unit-names.md)

## Context

`systemd.failed_units` says how many units failed and not which. When three nodes alerted
at once, only logging in to each showed that the cause was one and harmless: two updater
timers that had hit an error from the same upstream.

The obvious transport, a series per unit, is what [host-sensors.md](../specs/host-sensors.md)
rejected, and the rejection holds up. A series leaves the page only three of its interval
after its last value ([0034](0034-a-series-without-a-sensor-still-ages.md)), its row in the
series table and its history stay for good, the digest counts it among the series nobody
watches, and an alert on the count that named the units would have to read other series —
the join [0033](0033-a-subject-is-a-series.md) removed.

What the units are is not a value that changes over time. It is a fact about one value of
the count: these two are the two.

## Decision

- **A measurement may carry notes**: a short list of text lines about that one value. Notes
  are not a metric: they form no series, keep no history and are judged by no threshold.
- **Notes belong to the value they came with.** The hub keeps them with its series' newest
  value only: a newer value replaces them, notes or none, and a value arriving late changes
  nothing.
- **An event keeps the notes of the value that produced it**, so a message names what was
  true when the change was recorded, whether it goes out at once, is retried on a later tick
  or is listed in the digest.
- **They are shown with the value they came with**: on the subject in the State API, after
  the value in a message, on the changes of the timeline and of an item's page, in the
  timeline's list of what needs attention, and under the values of a tile on a computer's
  page. Other surfaces — a card, `/debug`, a history page — show the value alone until a
  need says otherwise.
- **They are bounded at ingest**: at most 20 notes, each 1 to 128 characters with no
  control, format or line-separating character. What breaks a bound is dropped and the
  measurement is kept: the value is what the threshold works on, and a note it cannot keep
  must not cost it.
- **A note is data, not a sentence.** It is shown verbatim and never translated, as a label
  value is ([0008](0008-english-repo-bilingual-ui.md)), and the hub knows nothing about what
  one says.

## Consequences

- The failed-units count names its units. A unit that recovers disappears at the next
  collection and leaves nothing behind.
- The wire format, the series and the event log, the State API and the message each gain a
  field. An older hub ignores notes, since unknown fields are ignored, and an older agent
  sends none.
- Notes leave the node and reach every channel the hub delivers to, Telegram among them, so
  a sensor puts in a note only what may travel that far. A unit's name and its result may.
  Its journal is left out for now, see #107.
- Twenty notes of 128 plain characters keep one notification inside Telegram's 4 096
  UTF-16 units, but not a digest of several such entries, nor notes of characters that
  count twice there or need quoting: the Telegram channel parts a text it cannot send as one
  message into several ([evaluation](../specs/evaluation.md#messages)).
- A unit that fails while another recovers leaves the count where it was, so it moves no
  level and sends nothing; the page shows the new names at once. The threshold judges the
  count, and notes are judged by nothing.
- A batch an agent keeps while the hub is away carries notes on each series' newest value
  only, since the hub keeps no other.
- A series stored before the hub knew notes carries none until its sensor next reports.
- History holds no notes. Which units had failed yesterday is on record only where the count
  moved a level and wrote an event.

## Alternatives

- **One series per unit** (`systemd.failed_unit{unit, result}` = 1). Rejected for the
  reasons in Context: a recovered unit stays listed for up to three intervals and stays in
  the series table and history for good, each one counts as unwatched in the digest, and
  naming them in the count's message means one series' message reads another's values.
- **The list as a label on the count.** Rejected: a label identifies a series, so every
  change in the list would start a new series and leave the threshold on the old one.
- **The unit's last journal line in its note.** Deferred to #107. Journal text can carry
  secrets and would go to the hub and to Telegram. Its last line is usually systemd's own
  "Failed with result", so picking the useful line is a problem of its own. If it comes, it
  comes as an opt-in.
- **Reading the notes from the series when a message is sent**, instead of keeping them in
  the event. Rejected: a retried or digested message would pair the value that changed the
  level with a list taken later, such as "2" beside three names.
