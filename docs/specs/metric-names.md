# Spec: Metric names

- **Status:** approved
- **Owns:** what a sensor declares about the names of its metrics, how the hub keeps the
  newest declaration, and how a series is named wherever a person reads it
- **Decisions:** [0048](../decisions/0048-a-sensor-names-its-metrics.md),
  [0008](../decisions/0008-english-repo-bilingual-ui.md),
  [0003](../decisions/0003-sensors-are-modules.md)

## Purpose

A person reads "Free on /srv", not `disk.free_pct /srv`. The sensor that produces a metric
says what it is called, in every language of the interface; the hub keeps what it was told
and uses it on every page and in every message. The hub names no metric itself: a metric
nobody named is shown by its id, exactly as before.

## Declaration

Each manifest entry may carry two maps, `names` keyed by metric id and `families` keyed by
**family** — the part of an id before its first dot, or the whole id when it has none
([history](history.md#page)). Each value is one string per locale of the interface:

```json
"manifest": [
  { "sensor": "disk", "applicable": true,
    "families": { "disk": { "en": "Disk", "ru": "Диск" } },
    "names": {
      "disk.free_pct":   { "en": "Free on {mount}", "ru": "Свободно {mount}" },
      "disk.free_bytes": { "en": "Free on {mount}", "ru": "Свободно {mount}" }
    } }
]
```

A metric's name may refer to a label of its series by key in braces: `{`, then one or more
lower-case letters, digits or underscores, then `}`, read from left to right; any other
character is literal text. A family's name is literal text throughout. A name's length is
counted in characters, not bytes.

The built-in sensors take their names from the interface's catalogue
([0008](../decisions/0008-english-repo-bilingual-ui.md)), and name every metric they produce
and its family:

| Id | en | ru |
|---|---|---|
| family `disk` | Disk | Диск |
| `disk.free_pct`, `disk.free_bytes` | Free on {mount} | Свободно {mount} |
| family `load` | Load | Нагрузка |
| `load.avg_1m` / `_5m` / `_15m` | Load 1 min / 5 min / 15 min | Нагрузка 1 мин / 5 мин / 15 мин |
| family `memory` | Memory | Память |
| `memory.available_pct`, `memory.available_bytes` | Memory available | Свободно памяти |
| family `uptime` | Uptime | Аптайм |
| `uptime.boot_seconds` | Uptime | Аптайм |
| family `systemd` | Services | Сервисы |
| `systemd.failed_units` | Failed units | Упавшие юниты |
| family `timemachine` | Time Machine | Time Machine |
| `timemachine.backup_age_seconds` | Backup {destination} | Бэкап {destination} |
| family `backup` | Backups | Бэкапы |
| `backup.failed` | Failed {backup} {job} | Сбой {backup} {job} |
| `backup.success_age_seconds` | Since success {backup} {job} | С успеха {backup} {job} |
| `backup.archives` | Archives {backup} {job} | Архивов {backup} {job} |
| `backup.latest_archive_age_seconds` | Newest archive {backup} {job} | Свежий архив {backup} {job} |
| `backup.stored_bytes` | Stored {backup} {job} | Занято {backup} {job} |
| `backup.latest_archive_original_bytes` | Newest archive data {backup} {job} | Данные архива {backup} {job} |
| family `gdrive` | Google Drive | Google Drive |
| `gdrive.free_pct`, `gdrive.free_bytes` | Free on Google Drive | Свободно на Google Drive |
| family `site` | Site | Сайт |
| `site.requests_per_min` | Requests a minute | Запросов в минуту |
| `site.client_error_pct` | Client errors | Ошибки клиента |
| `site.server_error_pct` | Server errors | Ошибки сервера |
| `site.response_p95_seconds` | Response time, p95 | Время ответа, p95 |
| `site.pageviews_24h` | Page views, 24 h | Просмотры за сутки |

Two metrics of one quantity in two units share a name: every place that shows a name
shows a value in its unit beside it, or asks for one in that unit, and that tells them
apart. The history page's list of several series shows none, but lists the series of one
metric only.

## Behaviour

One row = one test. Anchors: `spec: metric-names.md#<heading>`.

### Ingest {#ingest}

| Request | Hub |
|---|---|
| a manifest naming a metric the same request carries a measurement of | that metric's name, in each declared locale, is the one declared |
| a manifest naming a metric the same request carries no measurement of | that name is ignored; the request is accepted |
| a manifest naming a family that a measurement of the same request belongs to | that family's name is the one declared |
| a manifest naming a family no measurement of the same request belongs to | that name is ignored; the request is accepted |
| a manifest entry naming a metric whose measurements name another sensor | kept: a name belongs to the metric, whichever entry declares it |
| `names` or `families` that is not a map of maps of strings | that entry's names are ignored; the request is accepted |
| a later request declaring another name for the same metric, from any node | the later name replaces the earlier one on every page |
| a later request declaring the same metric in `ru` only | the `ru` name is replaced; the `en` one declared earlier stays |
| two manifest entries of one request naming the same metric | the later entry in the manifest wins |
| a manifest entry with no `names` or `families` (an older agent) | accepted; every name declared earlier stays |
| a name for a locale the interface does not have | ignored; the rest of the declaration is kept |
| an empty name, or one of spaces only | that name is ignored; the request is accepted |
| a name longer than 80 characters | that name is ignored; the request is accepted |
| a name carrying a control, format or line-separating character — a line break, U+2028, a bidirectional override | that name is ignored; the request is accepted |
| a sensor the hub runs itself declares names | kept the same way as an agent's |
| the hub restarts | the names declared before it stopped are still known |

A name is never removed: a sensor that stops declaring one leaves the last one in place.

### Naming a series {#naming}

A series' **name** is its metric's declared name in the reader's locale — in English when
that locale has none, the metric id when neither has one — with its labels filled in,
followed by what the labels it does not use name today
([evaluation](evaluation.md#messages)):

| Series | Locale | Name |
|---|---|---|
| `disk.free_pct {mount: /srv, fs: ext4, removable: "false"}` | ru | `Свободно /srv` |
| the same | en | `Free on /srv` |
| `queue.depth {queue: payments}`, named "Queue depth" | en | `Queue depth queue=payments` |
| `disk.free_pct {mount: "/Volumes/Backup Disk"}` | en | `Free on "/Volumes/Backup Disk"`: a value is quoted as a label value is quoted today |
| `disk.free_pct {mount: ""}` | en | `Free on ""`: an empty value still tells the series apart |
| a name referring to `{queue}`, on a series without that label | en | the name with `{queue}` left out and the spaces around it closed up |
| a name that is only `{queue}`, on a series without that label | en | the metric id followed by its labels: a series is never left without a name |
| a name referring to `{fs}` | en | the filesystem filled in, and not repeated after the name |
| a label value that itself contains `{site}` | en | filled in as it is; nothing is filled in twice |
| `Free {` | en | `Free {`: a brace that opens no reference is literal |
| `Free on {{mount}}` | en | `Free on {/srv}`: the inner `{mount}` is the reference, the outer braces literal |
| `Free on { mount }` | en | `Free on { mount }`: a space ends no key, so nothing is referred to |
| a metric declared in `ru` only | en | the metric id followed by its labels |
| a metric no sensor has named | any | the metric id followed by its labels, as today |
| a series on a site's own card or page | any | its name without the `site` label, as the card already names the site ([overview](overview.md)) |
| a name holding `<b>x</b>` | any | shown as those characters, on a page and in a message |

**A family's name** is its declared name in the reader's locale, in English when that locale
has none, and its id otherwise.

Where a page also shows a volume's filesystem and its being removable, they follow the
name as they follow the labels today:

| Series | Page name |
|---|---|
| `disk.free_pct {mount: /srv, fs: ext4, removable: "false"}`, ru | `Свободно /srv · ext4` |
| `disk.free_pct {mount: /Volumes/x-a, fs: apfs, removable: "true"}`, en | `Free on /Volumes/x-a · apfs · removable` |

### Where the name appears {#where}

Each row is the place, and what it shows where it showed the metric id, in the locale the
page or message is written in:

| Place | Shows |
|---|---|
| a row of an overview card | the series' name |
| the item page's numbers | the series' name |
| the item page's small chart | the series' name with filesystem and removable |
| the item page's family heading | the family's name |
| the history page's heading | `node · ` the series' name with filesystem and removable |
| the history page's list of several series | `node · ` the series' name with filesystem and removable |
| `/debug`'s table of unlabelled series | the series' name; the metric id on hover |
| a `/debug` matrix's corner | the family's name |
| a `/debug` matrix's column | its metric's name with the labels it refers to left out; the metric id on hover |
| a `/debug` matrix's column whose metric's name is only references to labels | headed as before names: its id less its family; the metric id on hover |
| the thresholds form | `node · ` the series' name with filesystem and removable; the metric id on hover |
| an item of the list of what needs attention | `node · ` the series' name |
| a change on the timeline or the item page | the series' name |
| a delivered notification or digest entry | `node · name: level (was …)` and the value alone after the dash: `laptop-a · Свободно /srv: критично (было предупреждение с 10:00) — 4,0 %` |
| a node's silence, delivered | as today, the node alone: `laptop-a: критично (…) — нет отчётов` |
| the `log` channel's line of one notification | the same text in English; the metric id stays among the line's attributes |
| the `log` channel's digest line | the entries' text in English; like today, no metric id per entry |

Every order stays the one it is — matrices, their rows and columns, the item page, the list
of what needs attention — by metric id and labels, never by name: a name depends on the
reader's language, an order must not.

The metric id stays in every URL, in the State and history APIs, in log attributes and in
the configuration: those are read by machines and in bug reports.

## Invariants

- The hub names no metric: every name it shows came from a declaration, or is the id.
- Two series of one metric on one node never read alike, except by `fs` and `removable` in a
  message, as today ([evaluation](evaluation.md#messages)).
- A series' name depends on the newest declaration of its metric and the reader's locale
  only, never on which node declared it.

## Edge cases

- **An agent older than this spec** sends no names; its metrics show ids until it updates,
  unless another node already named the same metric. Agents update themselves where the
  hub's file sets `agent_target` ([0028](../decisions/0028-agents-follow-a-target-the-hub-serves.md)).
- **Two releases declaring different names** for one metric: the name follows whichever
  agent reported last, until both run the same release.
- **Any node's token can rename a metric** that node reports, on every node — the sites
  node's too, when it hosts a site. A token already lets a node report any value; a name is
  no more than that, and ignoring names for metrics the request does not carry keeps a
  token from naming what it never reports.
- **A name that uses every label of its series** is followed by nothing in a message; a page
  still adds the filesystem and "removable".
- **The sites node's series** are reported by the sites' hosts; their names come from the
  host's manifest like any other.
- **`silence`**, the hub's own subject for a node that stopped reporting, keeps its
  catalogue words; it is never declared.

## Out of scope

- A name set by the reader in the interface — possible later on top of the declared one
  ([0048](../decisions/0048-a-sensor-names-its-metrics.md#alternatives)).
- Units: still derived from the metric id ([thresholds](thresholds.md)).
- The overview card's layout — [overview](overview.md).

## Open questions

None.
