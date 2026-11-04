# 2026-11-04 — The computer card

The overview's computer cards were noisy. Each card showed two large numbers with a line of
their last day, a run of further values, and a sentence at its foot ("All within
thresholds", or the usual value of an unusual series). The lines were flat on a scale from
zero and said nothing. Counters printed as `0.00`. Free memory appeared twice, in bytes and
in percent. Two cards could not be compared, because each put different series in the same
place. The reader wanted two things: the cause of a card's state at a glance, and computers
that compare.

## What was weighed

Eight layouts were mocked up side by side, with the same four computers in each:

- **The cause instead of the lines.** The marked series came first and large, and the rest
  was one muted run of text. It was quiet and named the cause, but it gave no comparison
  and no sense of scale.
- **Bars.** The free percent was drawn as a bar with threshold ticks. That worked only for
  percentages and could not say "unusual".
- **Chips.** Every value was a pill, coloured when marked. It was compact, but every value
  had the same weight, so nothing compared.
- **A table, one row per computer.** It compared best, but it broke on a phone and left
  gaps where computers differ.
- **A line for the cause only, around its band.** It explained an anomaly well, but cards
  of uneven height came back, and the healthy cards said little.
- **Bullets: a band of the usual range, a dot for now.** One picture read the same for
  bytes, percent, hours and load. **This was chosen**
  ([0053](../decisions/0053-a-computer-card-is-lines-on-a-shared-scale.md)).
- **Exceptions only.** Healthy computers folded into one line. The layout jumped as states
  changed.
- **A sentence.** It needed a template per metric, in two languages, and it scanned worse
  than numbers.

Within bullets, each question was settled in turn:

- **The scale.** Each slot runs from zero, and every computer card of a section shares it,
  so the disk line of one card reads against the disk line of the next. A scale measured in
  band widths from the norm made "unusual" loudest, but it made a disk 18% free look like
  one 75% free. A scale fitted to each card had the same flaw. Both were rejected. The cost
  is that a stable series' band shrinks to a sliver, which honestly says "it does not move".
- **Beside the value.** The value alone left the cause to be read from where the dot sits.
  The norm on every line brought the noise back. The norm ("usually X"), or the threshold
  that holds ("threshold X"), is written only on a marked line.
- **The same lines on every card, a dash where one is missing.** Lines sit at the same
  height across cards. Listing only what a computer has would shift them.
- **One backup line, the oldest fresh one.** A line per backup would make cards uneven
  again, and the oldest is the one a reader worries about.
- **Failed units go to the card's head**, and only when above zero. A zero counter on every
  card is noise, and a counter is not a scale.
- **Load shows 1, 5 and 15 minutes.** The 15-minute value is on the scale, as it was the
  card's default. The other two are muted, for reference. The reader first asked for 5, 10
  and 15 minutes. The sensor reports 1, 5 and 15, so those were used. **Memory and disk**
  show their bytes muted beside the percent on the scale. The rule is the same as for load:
  the rightmost value is the dot.
- **The last report closes every card.** It was first written under the head, and only on a
  card that stopped. A review found that this pushed the lines of that card below its
  neighbours' lines. Writing it into the state's pill truncated the name. Now it sits at
  the foot of every card, small and muted, and brighter once the computer stops.
- **Values never wrap.** The value column is fixed and the scale gives up the width. A
  column sized to its text would move each line's scale, and the dots would stop lining up.

## Left for later

Sites and services keep their cards. Their series share no scale from one item to the next:
requests per minute differ by orders of magnitude between sites.
