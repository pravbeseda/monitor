# Spec: Thresholds

- **Status:** approved
- **Owns:** the page that sets what a series is judged by — `GET /thresholds` and the
  posts it accepts: a save, a request to apply to every series, a confirmation — what may
  be stored as a threshold, whether the series may be shown as unusual, and the defaults a
  sensor declares and how they are seeded. What a stored threshold *means*, and when it
  produces a level, an event or a message, stays with [evaluation.md](evaluation.md); what an anomaly is stays with
  [anomaly.md](anomaly.md); the shell every page shares, its zone and its refresh stay
  with [web.md](web.md); the series a page can be opened for come from
  [history.md](history.md).
- **Decisions:** [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0023](../decisions/0023-proxy-holds-the-web-perimeter.md),
  [0029](../decisions/0029-pages-refresh-by-fetching-their-own-address.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md),
  [0042](../decisions/0042-an-anomalys-start-and-end-are-recorded.md),
  [0052](../decisions/0052-a-sensor-declares-default-thresholds-seeded-once.md),
  [0059](../decisions/0059-all-series-is-two-panes-and-settings-a-dialog.md)

## Purpose

Nothing alerts until someone says what "bad" means for one particular series
([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)), or its sensor has said
it once for every installation
([0052](../decisions/0052-a-sensor-declares-default-thresholds-seeded-once.md)). This is where they say
it: one page per series, two numbers and a direction, saved into the hub's own store and
applied by the next evaluation tick without a restart.

The same page is where a series is excluded from anomalies: a series that is unusual by
nature — a laptop's uptime after a weekend asleep ([anomaly](anomaly.md#edge-cases)) — is
told once not to be ranked.

The page is deliberately the whole configuration surface for alerting. There is no file to
edit and no layer above it: what the form shows is what the hub judges by. A sensor may
declare a default for a metric whose meaning is the same everywhere — a failed run — and
the hub writes it into each series once; from then on it is the page's to change
([0052](../decisions/0052-a-sensor-declares-default-thresholds-seeded-once.md)).

## Model

**A configuration belongs to one series** — `(node, metric, labels)`, the subject of
[evaluation](evaluation.md#model). It carries a **direction**, `below` or `above`, and up
to two **values**, `warning` and `critical`, written in the unit of the series. Either
value may be left empty; a configuration with both empty is no configuration at all and is
removed.

**Consistency is per direction.** With `below`, `critical` must be strictly below
`warning`; with `above`, strictly above it. Equal values would make one of the two levels
unreachable, so they are refused rather than silently ordered.

**An exclusion belongs to one series too**, and is independent of its threshold: a series
can be excluded with no threshold, and keeps its exclusion when its threshold is removed.
An excluded series carries no anomaly ([anomaly](anomaly.md#subjects)); its levels are
untouched, and it counts as watched or unwatched by its threshold alone.

**A series is addressed the way [history](history.md#selection) addresses one**:
`?node=…&metric=…&label.<name>=…`, naming every label the series carries, because a
threshold belongs to one series and not to a family of them.

**The unit is the series' own**, derived from the metric id as everywhere else
([history.md](history.md#wire-format)): a `_bytes` metric takes a size, a `_pct` metric a
percentage number, a `_seconds` metric a duration in seconds, anything else a plain number.
The form says which it is asking for, and names no unit for a plain number. A size may be written with a decimal unit — `10GB` is
10 000 000 000, as sizes are written everywhere in this project — and a duration with one of
`s`, `m` (minutes), `h` or `d` — `36h` is 129 600; everything else is a plain number. A size
unit or a duration unit is read regardless of case and of a space before it, and is written
in the Latin letters listed here in both languages — not as pages render a duration, `90 min`
or `36 ч`. A value takes one unit, never a sum of them, and what is stored is
always the number in the series' own unit.

**A default** is a direction and up to two values that a sensor declares for a metric it
produces, in the manifest beside the metric's name
([metric names](metric-names.md#declaration)), under `thresholds`: an id, then an object
of `direction`, `warning` and `critical`, the values plain numbers in the series' own unit
— `{"backup.success_age_seconds": {"direction": "above", "warning": 108000, "critical": 172800}}`.
A default is held to the rules the store holds a threshold to ([invariants](#invariants)).
It belongs to the metric, whichever entry of the manifest declares it.

**A series is seeded once.** When a request stores a measurement of a series and its
manifest declares a default for the series' metric, the default becomes the series'
threshold unless one is stored already; a series seeded before, or whose threshold a save
of this build or a later one changed, is not seeded again. The hub does not
remember a declaration between requests, as it remembers a name: a series is seeded only
by a request that carries both. From then on the threshold is the page's like any other.

A save changes a series' threshold when it stores one or removes one that was stored; a
save that only turns the switch, on a series with no threshold, changes nothing and leaves
the series to be seeded. A confirmation (below) changes the threshold of every series it
reaches, whatever was stored. A threshold removed before this build left no record, so such a
series is seeded once like one never configured.

**One threshold can be applied to every series of a metric** — on every node, whatever its
labels, the nodes the file no longer names included — from the form of any of them. The
button redraws the form with what was typed and lists the series it will change, each
with its current threshold; "Confirm" then replaces each series' threshold with the
form's, or removes it when both values are blank. The series whose form it is takes the
switch as well; the others keep theirs. "Confirm" reaches every series of the metric the
hub holds when it is pressed, and leaves no rule behind: a series that appears later is
seeded, if its sensor declares a default, like any other.

**A save is refused unless the browser says it came from this page.** The hub has no
session and no login of its own ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)),
so the guard against another site posting through a reader's cached credentials is the
request's own origin, not a stored token: a save whose `Origin` names another host than the
one it was sent to is refused before anything is read. The host is what is compared — the
hub answers the proxy over plain HTTP, so its own scheme is not the reader's.

## Behaviour

One row = one test. Anchors: `spec: thresholds.md#<heading>`.

### The form {#form}

| Request | What the reader sees |
|---|---|
| any series | the series' name above its node, a link to its history, and "Cancel", which leaves what was typed unsaved |
| the page on its own | "Cancel" and a link above the name both lead to "All series", in the reader's language |
| a series with points in the last 24 hours | its newest value and a chart of those hours with the stored values drawn across it; with scripting on, a value typed into either field that a save would read — its unit or comma included — moves its line at once, on the page and in the [dialog](web.md#dialog), and a blank or anything else takes its line away |
| a typed value past the top of the chart | the chart grows to hold it |
| scripting off | the stored lines only |
| a series with no point in the last 24 hours, or whose points cannot be read | the same form with no chart: reading them never costs the form |
| a series with nothing configured | an empty form: direction `below`, both values blank, and a line saying the series has no level until a value is set |
| a series with a configuration | its direction and values as stored, in the unit the metric implies |
| a `_seconds` series | the field asks for seconds and names the units it also takes, saying that `m` is minutes, with an example: `36h` |
| any series | one switch saying the series is marked as unusual when it is, on unless the series is excluded; off, the form says `exclude`, not nothing |
| a series whose stored threshold this build cannot read | the switch as stored: an exclusion is read on its own |
| any series the hub has values for | the same form, whatever the metric: nothing about it is particular to disks |
| a series the hub has never stored | `404` as a page: a threshold is set on something that reports |
| a series of a node the file no longer names | the form as ever, saying the series is not judged while its node is not configured |
| a series whose stored threshold this build cannot read | the form, with the unreadable values blank and a line saying the stored configuration could not be read and saving will replace it |
| the page at any time | never refreshed under the reader, typing or not: nothing on a form changes by itself, and a refresh would replace what they typed — or the refusal they are reading — with the stored configuration ([web.md](web.md#live)) |
| a query naming fewer labels than the series carries, or an extra one | `404`: the address names one series or none |
| a series `{queue: ""}` addressed with `label.queue=` | its form; the same address without `label.queue=` answers `404`, and a save from the form is kept for `{queue: ""}`, not for `{}` |
| a metric or label value that is not a valid query at all | `400` as a page, in the manner of [history](history.md#refusals) |
| the page in Russian | every label, unit and message from the Russian catalogue ([0008](../decisions/0008-english-repo-bilingual-ui.md)) |
| a method other than `GET` or `POST` | `405` |

### Saving {#saving}

| Save | Result |
|---|---|
| direction and both values, consistent | stored; the reader is sent back to the form, which says it was saved and that it applies from the next check, and shows what is now stored; the next tick judges by it |
| the form reached by reloading that answer | the same form, saying so again, its address marked `saved=1`; nothing stored |
| the form opened with `saved=1` | as without it, saying it was saved; a save from it goes to the address without the mark |
| a value written with a decimal comma — `6,5`, `1,5h` | read as the decimal point it stands for: 6.5, 5 400; a single comma is always decimal, so `1,000` is 1 |
| a value with more than one separator — `1,000.5` | refused, naming the field; nothing is stored |
| one value filled, the other blank | stored: a subject may have only a warning or only a critical |
| both values blank | the configuration is removed; the subject loses its level without an event or a recovery message ([evaluation.md](evaluation.md#configuration-changes)) |
| a value that is not a finite number | refused, naming the field; nothing is stored, and what the reader typed is still in the form |
| a `_bytes` value written `10GB` | accepted and stored as 10 000 000 000; the form shows it back as a size |
| a `_bytes` value written as a bare number | accepted as that many bytes, which is what the field says it is asking for |
| a `_bytes` value no round size names — 20 123 456 789 | shown back as that number, not rounded to a size: redrawing the form must not rewrite the threshold |
| a `_seconds` value written `36h`, `36H` or `36 h` | accepted and stored as 129 600; the form shows it back as `36h` |
| a `_seconds` value written as a bare number | accepted as that many seconds, which is what the field says it is asking for |
| a `_seconds` value written `1.5h` | accepted and stored as 5 400; the form shows it back as `90m` |
| a `_seconds` value a whole number of days, hours or minutes | shown back in the largest of them that names it whole: 86 400 as `1d`, −3 600 as `-1h` |
| a `_seconds` value no whole minute names — 129 601, or 30 — or zero | shown back as the bare number: `129601`, `30`, `0` |
| a duration written as a sum — `1d12h` — in a unit the form does not list — `2w`, `90min` — or in Cyrillic — `36ч` | refused, naming the field; nothing is stored |
| a value that is finite only before its unit is applied — `1e300GB`, `1e305d` | refused as not a finite number; nothing is stored |
| a `_pct` value written `12%`, a `_bytes` value written `36h`, a `_seconds` value written `10GB`, or any value with a unit the metric does not take | refused, naming the field; nothing is stored |
| `critical` not strictly beyond `warning` in the chosen direction | refused, naming both fields; nothing is stored |
| a direction that is neither `below` nor `above` | refused; nothing is stored |
| a save for a series the hub has never stored | `404`; nothing is stored |
| a save whose `Origin` is another site, or a cross-site form post | refused as a page before anything is read, whatever address it names and whether or not that series exists; nothing is stored |
| a save with no `Origin` at all — an old browser, a hand-made request | refused the same way: the form's own saves always carry one |
| the switch turned off | the series is excluded: from the next answer on it carries no anomaly, and from the next tick the timeline shows it as unusual no more; the hours it already was keep their colour ([anomaly](anomaly.md#record)) |
| the switch turned off, both values blank | the threshold removed and the exclusion stored: the two are separate |
| the switch turned on again | the exclusion removed: the next answer ranks the series again if it is unusual |
| the switch turned off in a save refused for its values | nothing stored, the exclusion included; the form shows the switch as the reader left it |
| a save carrying no value for the switch, or one that is neither on nor off — a form drawn before the switch existed, a hand-made request | refused, naming the switch; nothing is stored: a form is the whole configuration, and a missing switch is not an answer |
| two saves for one series | the later one is what is stored: a form is a statement of the whole configuration, not a patch |
| a save reloaded by the browser afterwards | nothing is saved twice: the answer to a save is a redirect, not a page |
| a save while the proxy no longer accepts the reader's credentials | the proxy refuses it and nothing reaches the hub ([0023](../decisions/0023-proxy-holds-the-web-perimeter.md)) |

### Defaults {#defaults}

`server-b` below reports `backup.failed` and `backup.success_age_seconds` for `web` / `copy`
and `vpn` / `copy`; its backup sensor declares `backup.failed` above, warning 0, and
`backup.success_age_seconds` above, warning 108 000, critical 172 800.

| Request or state | Result |
|---|---|
| the first measurements of `backup.failed` of `web` / `copy` and `vpn` / `copy`, in a request declaring the default | both judged by above, warning 0 from the next tick; their forms show those values |
| `backup.success_age_seconds` seeded | its form shows `30h` and `2d` |
| a series stored before this build with no threshold | seeded at its next measurement in a request declaring the default |
| a series whose threshold was removed before this build | seeded the same way: the removal left no record |
| a series with a threshold stored before this build | its threshold unchanged by its next measurement; if the reader then clears it, later measurements leave its form empty |
| a series whose threshold the reader removes on the form, then a measurement with the default | its form stays empty |
| a series the reader saves with a threshold before its agent declares a default, then a request declaring it | its form stays as saved |
| a seeded series whose threshold the reader edits | the edit stays through later measurements |
| a series with no threshold whose switch the reader turns off, then a measurement with the default | seeded, and still excluded: a default sets a threshold, never the switch |
| a series whose stored threshold this build cannot read | left as stored |
| a seeded series, then a release declaring another default | the series keeps what was seeded; a series not yet seeded takes the new one |
| a release declaring a default for a metric that had none | its series with no threshold, none changed by a save, seeded at their next measurement |
| a default for a metric the request holds no measurement of | nothing seeded; the request is accepted |
| a default declared by one manifest entry for a metric whose measurements name another sensor | seeded: a default belongs to the metric |
| two entries of one request declaring a default for one metric | the later entry wins |
| a manifest entry with no `thresholds` (an older agent) | nothing seeded; a later request declaring the default seeds the series |
| `thresholds` that is not an object of objects — `{"backup.failed": {…}, "x": 5}` among them | every default of that entry ignored, its names kept; the request is accepted |
| a default with `critical` only | seeded with a critical and no warning |
| a default with `null` for a value | that value absent |
| a default whose direction is missing or neither `below` nor `above`; a value that is not a number — `"30h"` among them — or not finite; both values absent; `critical` not strictly beyond `warning`; or a key other than `direction`, `warning` and `critical` | that default ignored, the entry's others kept; the request is accepted |
| `server-b`'s request carrying the sites node's measurement of a site it hosts, declaring a default for its metric | the sites node's series seeded |
| a sensor the hub runs itself declares a default | seeded the same way as an agent's |
| a series seeded | no event, no message and no anomaly change by the seeding itself; the next tick judges it as one whose threshold was just set ([effects](#effects)) |

### Applying to every series {#all}

`backup.failed` below has four series: `web` / `copy` and `vpn` / `copy` on `server-b`,
`web` / `copy` and `web` / `verify` on `server-c`. Every request is a post to the form's
own address.

| Request | Result |
|---|---|
| the form of any series | below "Save", a button "Apply to every series of this metric" |
| that button, with consistent values | the form again, with the values typed, and above "Confirm" the four series by node, then by name (`node · ` the series' name, [metric names](metric-names.md#naming)), each with its current threshold or "none", and a line saying their thresholds will be replaced and the switch is this series' only; nothing stored |
| that button, both values blank | the same, saying their thresholds will be removed |
| that button, a value refused by a save | the form, refused as a save is; nothing stored |
| a metric with one series | the button, and that one series listed |
| a series of a node the file no longer names | listed and changed like the others |
| "Confirm" | all four judged by the form's values from the next tick; the reader is sent back to the form, which says it was saved |
| "Confirm" with both values blank | all four thresholds removed, without an event or a recovery message ([evaluation.md](evaluation.md#configuration-changes)); later measurements with a default leave them empty |
| "Confirm" covering a series whose stored threshold cannot be read | replaced like the others |
| "Confirm" from `server-b`'s `web` / `copy` with the switch off | that series excluded; the other three keep their switches |
| "Confirm" with a value changed after the list was drawn | the values in the form when it is pressed are applied, held to the rules a save is |
| "Confirm" with a value a save refuses | the form, refused as a save is; nothing stored |
| a fifth series stored between the list and "Confirm" | changed too |
| a series appearing after "Confirm" | not changed: seeded by its sensor's default, if it declares one |
| either post from another site, or with no `Origin` | refused as a save is, before anything is read |
| either post for a series the hub has never stored | `404`; nothing stored |
| the list reloaded — the browser sends the post again | the list again; nothing stored |
| the page after "Confirm" reloaded | nothing stored twice: the answer is a redirect |
| the page with the list, left open | never refreshed under the reader ([web.md](web.md#live)) |
| the page in Russian | every word from the Russian catalogue |

### What changes elsewhere {#effects}

| After a save, a confirmation, or a seeding | Result |
|---|---|
| a threshold set on a series that is already past it | the next tick transitions it, and a critical is announced at once ([evaluation.md](evaluation.md#notifications)) |
| a threshold set or cleared | no agent is affected and no `config_version` changes ([hub-config.md](hub-config.md#configuration-version)) |
| a threshold cleared for a subject standing in `critical` | no recovery message: the question was withdrawn, not answered ([evaluation.md](evaluation.md#configuration-changes)) |
| an exclusion set or cleared | no level, event or message changes, and the series stays watched or unwatched as it was; an anomaly still open is withdrawn by the next tick, with no end listed ([anomaly](anomaly.md#record)) |
| any save | the hub restarts nothing and rereads no file |
| a save made in the dialog ([web](web.md#dialog)) | the same save: a dialog is this page, shown over another |

## Invariants

- The store never holds what the form would refuse: a direction outside `below`/`above`, a
  value that is not finite, neither value, or a `critical` that is not strictly beyond its
  `warning`.
- A configuration belongs to exactly one series, and a series has at most one; so does an
  exclusion. Applying to every series writes one configuration per series, and leaves no
  rule behind it.
- Saving changes what the hub judges by and nothing else: no measurement, no level and no
  event is written by a save, by a confirmation, or by seeding.
- A threshold is written by the form, by a confirmation, or by seeding, and by nothing
  else; seeding never replaces a stored threshold, and never touches a series seeded
  before or whose threshold a save of this build changed.
- A page never shows a threshold it did not read from the store, so two readers see the
  same numbers — the values a reader typed, redrawn with a refusal or above "Confirm" or
  drawn as a line while typing, apart.
- Nothing on this page reaches an agent.

## Edge cases

- **A value of zero** is legal and means what it says; the form states that a zero
  threshold has a margin of zero, so it cannot damp a flapping series
  ([evaluation.md](evaluation.md#hysteresis)). A value small enough that 20% of it is
  indistinguishable from zero behaves the same way.
- **A negative value** is legal too — a balance may be judged below −100 — and its margin
  is 20% of its magnitude, so it clears in the direction the comparison's negation points:
  upwards for `below`, downwards for `above`.
- **A percentage** is a number between 0 and 100 because that is what the series carries;
  nothing enforces the range, since a metric may report more than 100% of something.
- **The two series of one volume** are configured separately: a size on
  `disk.free_bytes` and a proportion on `disk.free_pct` fire whichever comes first, and
  that eagerness is the reader's choice ([0033](../decisions/0033-a-subject-is-a-series.md)).
- **A series that stops reporting** keeps its configuration: the subject freezes rather
  than losing what it was judged by ([evaluation.md](evaluation.md#freezing)).
- **A volume that reappears under different labels** is a different series and starts
  unconfigured; the old configuration stays on the old labels until it is removed.

## Out of scope

- What a level, an event or a message is made of → [evaluation.md](evaluation.md).
- Listing series and linking to this page → [history.md](history.md#page).
- Who may open the page at all → [0023](../decisions/0023-proxy-holds-the-web-perimeter.md)
  and [nginx-requirements.md](../nginx-requirements.md).
- Copying one configuration to a chosen few series, or between metrics → later; applying
  to every series of one metric is [above](#all).
- Saying on the form that a threshold came from a default → later: once seeded it is the
  reader's like any other.
- Saying *why* a series has no threshold — a third state between watched and forgotten →
  later. Until then the counts on `/debug` and in the digest ([state.md](state.md#page),
  [evaluation.md](evaluation.md#digest)) are what keeps a forgotten volume visible.
- Exporting the stored thresholds to a file, or restoring them from one → the database is
  the record, and backing it up is [hub-backup-requirements.md](../hub-backup-requirements.md).
- Editing thresholds from a skin other than this page → the store is the contract
  ([0001](../decisions/0001-semantic-core-and-skins.md)); only this page, whether opened on
  its own or in the [dialog](web.md#dialog), and seeding write it today.

## Open questions

None.
