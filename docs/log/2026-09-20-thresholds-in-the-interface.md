# 2026-09-20 — Thresholds move into the interface

The POC is finished, and the question was where the MVP starts. What follows is the
reasoning that did not fit into [0032](../decisions/0032-thresholds-are-set-in-the-interface.md)
and [0033](../decisions/0033-a-subject-is-a-series.md), and the options that lost.

## Where the MVP starts

Three ways in were weighed: more metrics first (load, uptime, backup age), the semantic
engine first (health, trend, anomaly rank), or a skin first. The roadmap's order — metrics,
then engine, then skins — won for one reason: both the engine and a skin need more than one
family of metrics before their algorithms and their screens have anything to say.

Then the ground turned out to be softer than the plan assumed. A sensor that only collects
and displays costs about eight files; the same sensor *with a level and an alert* costs
twenty, because a rule was two joined series compared with `<`. Generalising the engine had
to come before the second metric, or the second metric would pay for it.

## The engine's shape

- **Rule kinds in Go, numbers in the file** — the smallest change. Rejected: a metric with
  an alert still costs a code change, which is the opposite of what the concept promises.
- **Rules declared in the file** — an expression vocabulary: series, operator, levels.
  Rejected as the most expensive option and the one most likely to grow a half-language.
- **A hybrid** — simple comparisons declared, the composite disk rule kept in code. This is
  what was recommended, and it was overtaken by the next section.

## Setting a threshold where you see it

The proposal that settled it: a metric has no levels by default, because disks differ by
purpose, and each *instance* gets its own warning and critical on its own page in the web
interface, stored by the hub.

That removes the reason for an expression language altogether. If the numbers are hand-set
per volume, one fixed shape — a direction and two values — covers every metric the roadmap
names, and the vocabulary that would have expressed the floor-plus-band is not needed,
because [0012](../decisions/0012-threshold-model.md)'s band existed only to make a single
shipped default fit volumes of every size.

Rejected along the way:

- **The interface rewriting the YAML file**: two writers of one file.
- **Defaults in the file, overrides in the interface**: the number that alerts you becomes
  a merge of two places you cannot see at once.
- **Keeping the layering (class → node → volume) inside the store**: layering is a machine
  for spreading a default, and there is no default left.
- **`role: backup`**: a disk-shaped enum in a core that is supposed to know nothing about
  disks. What is genuinely lost is the record of *why* a volume has no threshold; the
  counts of unwatched series are the cheap stand-in, and a "deliberately not watched" state
  is left for later.

## What a threshold is set on

- **The subject stays a volume**, with the form choosing bytes or percent — rejected: the
  join stays in code, and every future metric has to declare which series it pairs with.
- **The subject becomes the series** — chosen. "Absolute or percent" stops being a switch
  and becomes *which series you put the number on*. The cost is two rows per volume on `/`,
  which is a grouping problem on one page, not a model.

Levels were kept at one comparison each rather than two combined: with `warning: 20GB` on
bytes and `warning: 10%` on percent, whichever fires first, the operator can still build the
old band's behaviour by hand, and they choose the eagerness instead of inheriting it.

## What three reviews changed before any code

Independent reviews of the behaviour tables and of the document set moved these:

- **A held level survives a direction flip.** The negation of the other direction holds the
  old level in a band where the value is fine. Decided: a change of direction discards the
  level, and the tables say so with numbers.
- **The upgrade would have bricked the running hub.** The live `hub.yaml` carries `rules`
  and `volumes`; an unknown key is a startup error; the hub upgrades itself hourly. Decided:
  for one release those two keys are accepted and ignored with a warning. A silenced hub can
  be configured, a crash-looping one cannot.
- **Silence had to be made visible.** No defaults means a fresh install, or a database
  restored without its thresholds, watches nothing and looks healthy. The state now counts
  watched and unwatched subjects, `/` says so above its tables, and a hub with nothing
  watched says it in the daily digest.
- **The CSRF token lost to an origin check.** A token the form was served with implies
  server-side state, and [0023](../decisions/0023-proxy-holds-the-web-perimeter.md) gives
  the hub no session and no cookie. Refusing a save whose origin is not the hub needs
  neither.
- **The write path is the person's credential.** The program credential exists so scripts
  can read; it travels further, and it must not be able to silence every alert.
- **One rule for staleness.** "No interval" meant three different things across three specs.
  Now: a series whose newest value names no sensor has no freshness rule at all; a series
  whose node no longer runs that sensor is stale, because nothing will refresh it.
- **`configured` meant two things** in one response — a node named in the file, and a series
  with a threshold. The second became `watched`.
