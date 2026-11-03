# 2026-11-03 — Family panels

The item page of a backup mirror showed 26 charts: three jobs for each of two
repositories, each job reporting up to six series. The reader asked for one panel per
repository with a text summary, where each figure opens the chart it comes from, and for
an approach other families can reuse.

## What was weighed

- **Where the grouping is known.** There were three candidates: the sensor, a hub table, or
  the reader. The sensor was chosen ([0051](../decisions/0051-a-sensor-groups-its-family-by-a-label.md)).
  It is the same argument that made the sensor name its metrics
  ([0048](../decisions/0048-a-sensor-names-its-metrics.md)): the hub must not have to know
  each family.
- **"A generic panel limits us."** This objection was raised against grouping only. It
  was answered by keeping two things apart: *what* a panel holds is declared by the
  sensor, and *how* it looks is decided by the skin. The skin has a generic panel and may
  register a panel of its own per family. Putting a layout into the manifest was rejected,
  because the agent's contract would then carry fields for one skin.
- **One panel per `backup`, not per `backup` / `job`.** The reader thinks of repositories.
  A job is one line inside its repository's panel.
- **Every series stays on its panel.** One proposal was to show a single "figures" line
  per repository, taking each figure from the freshest job. It was rejected because it
  would hide the other jobs' series, and a level held on a hidden series must not
  disappear from the page.
- **No colour for "last run failed" by itself.** The overview judges nothing. A failure
  flag is coloured only once a threshold gives it a level. A threshold on its age is the
  better watch, because a single network reset would otherwise raise a critical.
- **A term other than "subject".** [0033](../decisions/0033-a-subject-is-a-series.md)
  already uses "subject" for a series, so the declared label is called the *group label*
  and its series a *group*.
