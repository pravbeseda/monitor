# 2026-11-03 — Default thresholds

The backup panel became a table whose job names take the colour of their failure flag.
With no threshold on the flag, a failed run stays uncoloured
([0033](../decisions/0033-a-subject-is-a-series.md)), and setting one per job and per server
is six pages a mirror. The reader asked for a default that a sensor defines, written into
each new series once and changed on its page afterwards, and for a button that writes one
threshold into every series of a metric after a confirmation.

## What was weighed

- **Colouring a failure without a threshold.** Rejected: it would be the first exception
  to "colour is a level", and a single network reset that the next run recovers from would
  look like an outage.
- **A default read on every tick under the form.** Rejected for the reason 0032 gave: the
  number that alerts would come from two places, and clearing a threshold would bring the
  default back unseen. A default is written once instead, and the store stays the only
  source.
- **Seeding only a series seen for the first time.** Rejected: the backup series that
  prompted this already exist. Every series not yet seeded is seeded at its next
  measurement, and a save of the form marks a series seeded, so what the reader set or
  removed is never overwritten.
- **Which defaults for backups.** Three sets were offered; the reader chose the mild one: a
  failed run is a warning, so it reaches the digest and not the phone; no success for 30
  hours is a warning and for two days critical, which is what catches a job that stopped
  running at all. A failure as critical was rejected because two transient rsync resets in
  one week would have woken the reader twice.
- **Applying to every series as a kept rule.** Rejected: later series would follow a layer
  again. The button writes each series' own threshold and leaves nothing behind; later
  series are covered by the sensor's default.
- **Which series "every" means.** Every series of the metric, on every node and with any
  labels — for a backup flag that is every job of every repository on every server. The
  confirmation page lists them, so the reach is visible before anything is written.
- **A save that only turns the anomaly switch.** It does not opt a series out of its
  default: only a save that stores or removes a threshold does, since that is the reader
  answering the question the default would answer.
- **"Confirm" reaches every series held when it is pressed**, not only those listed. Carrying
  the list through the confirmation would cost hidden fields for every series, and a series
  that appears in the seconds between is one more series of the same metric, which is what
  "every" was asked for.
