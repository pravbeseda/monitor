# Spec: Evaluation and alerts

- **Status:** approved
- **Owns:** `internal/evaluate` (hub): the tick, thresholds, hysteresis, silence detection
  and the notification boundary. The channels behind that boundary — the log line and the
  Telegram bot — are `internal/notify`, which formats and delivers but never decides.
  Persistence of thresholds, levels, events and the digest mark stays with
  `internal/storage`; the `digest`, `notify` and `silence_after` keys are parsed and
  validated by `internal/config`, which keeps owning the file. Editing a threshold is the
  page's business ([thresholds.md](thresholds.md)); this spec owns what a stored threshold
  means.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0006](../decisions/0006-alerting-rules.md),
  [0007](../decisions/0007-public-repository.md),
  [0013](../decisions/0013-relative-hysteresis.md),
  [0015](../decisions/0015-evaluation-on-a-tick.md),
  [0016](../decisions/0016-leaving-critical-is-instant.md),
  [0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
  [0033](../decisions/0033-a-subject-is-a-series.md)

## Purpose

Evaluation turns stored measurements into meaning: every configured subject gets a level
(`ok`, `warning`, `critical`), a change of level is written to an event log, and events
become notifications under the rules of [0006](../decisions/0006-alerting-rules.md) and
[0016](../decisions/0016-leaving-critical-is-instant.md). It runs on its own tick, never
inside a request ([0015](../decisions/0015-evaluation-on-a-tick.md)), so
[ingest](ingest.md) keeps checking shape and not meaning.

It does not collect, does not render, and does not decide what an agent runs: a threshold
never reaches an agent ([0010](../decisions/0010-agent-configuration.md)).

## Model

**A subject is a series**: the triple `(node, metric, labels)`
([0033](../decisions/0033-a-subject-is-a-series.md)). A volume contributes two of them —
`disk.free_bytes` and `disk.free_pct` on `server-b` with
`{mount: /, fs: ext4, removable: false}` — and each is judged on its own. Nothing here knows
that either is about a disk.

**A subject has a level only while a threshold is configured for it.** A series nobody has
configured is stored, listed and charted, and has no level, no event and no notification.
The thresholds come from the store, are entered in the interface
([0032](../decisions/0032-thresholds-are-set-in-the-interface.md),
[thresholds.md](thresholds.md#form)) and are read afresh by every tick.

**A configuration is a direction and up to two values.** The direction — `below` or
`above` — belongs to the subject; `warning` and `critical` are independent values in the
unit of the series, and either may be absent. Entry is the strict comparison; exit negates
it with the 20% margin of [0013](../decisions/0013-relative-hysteresis.md), measured on the
threshold's magnitude so that a negative threshold clears in the same direction a positive
one does:

```
below    enter L when v <  T(L)      leave L when v >= T(L) + 0.2·|T(L)|
above    enter L when v >  T(L)      leave L when v <= T(L) − 0.2·|T(L)|
```

The **margin** is `0.2·|T|`; the value on the far side of it — `T + margin` going up,
`T − margin` going down — is the level's **clearing value**. Comparisons are made on the
stored value, to a tolerance of one part in a billion of the threshold's magnitude, so that
a value sitting exactly on a clearing value counts as cleared however the two were
computed. The tolerance is far below anything a sensor can tell apart.

**A level with no value is never entered and never held**, so removing `critical` from a
subject standing in `critical` drops it to whatever `warning` and the value say on the next
tick. The subject's previous level is still whatever was last stored, even when that level
is no longer configured: it is what the remaining levels are held against.

**A change of direction discards the level it held.** Hysteresis holds a level against the
comparison that created it, and the negation of the other direction would hold it in a band
where the value is perfectly good.

**The level of a subject** is chosen from its previous level, most severe first; a subject
with no stored state has previous level `ok`:

```
for L in [critical, warning]:
    if enter(L)                     -> L
    if previous >= L and !leave(L)  -> L      # hysteresis holds the level
-> ok
```

The margin is 20% and nothing changes it.
[0013](../decisions/0013-relative-hysteresis.md) allows a per-subject override for an
inherently noisy metric; none has proved noisy yet, so the field is deferred rather than
shipped unused.

**Staleness needs an interval, and a measurement names its sensor**
([ingest](ingest.md#wire-format)). The series keeps the sensor its newest value named, and
`stale_after` is 3× the interval that node resolves for that sensor
([hub-config.md](hub-config.md#resolution)). A series whose newest value names no sensor has
no staleness at all; a series whose node runs that sensor no longer — resolved
`enabled: false`, or no interval for it — is frozen outright, because nothing will refresh
it.

## The tick

Evaluation runs on its own schedule ([0015](../decisions/0015-evaluation-on-a-tick.md)),
against one consistent view of the data taken at the instant it evaluates for: a
measurement that arrives while a tick is running is evaluated by the next tick, never by
half of this one. Within a tick a node's silence is decided before its other subjects, so a
node that has just fallen silent has its subjects frozen in that same tick rather than the
next. A level change is recorded before any message about it is sent. Messages and digest
entries come out in a stable order: by node name, then metric id, then labels.

Two ticks never run at once: a tick that would start while the previous one is still
running is skipped, and the skip is logged. A notifier that does not return cannot hold
evaluation open — the send is abandoned, counted as a failure, and the event is retried on
a later tick.

## Configuration

**Thresholds are not in the file.** What a subject is judged by lives in the store and is
edited on the page ([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)); the
file keeps only what is true of the installation as a whole. These keys extend
[hub-config.md](hub-config.md), and none of them reaches an agent, so none of them changes a
`config_version`.

```yaml
digest: { at: "09:00", timezone: UTC }   # product default
notify: { channel: log, locale: en }     # channel: log | telegram
```

- **The evaluation tick is 1m** ([0015](../decisions/0015-evaluation-on-a-tick.md)) and no
  key changes it. `silence_after` stays a property of a node class
  ([hub-config.md](hub-config.md)), because silence must work before anyone opens a page.
- **A threshold edit needs no restart**: the tick reads the store, so the next tick after a
  save judges by the new numbers.
- **Secrets are never in the file**: with `channel: telegram` the bot token and the chat id
  come from `MONITOR_TELEGRAM_TOKEN` and `MONITOR_TELEGRAM_CHAT_ID`
  ([0007](../decisions/0007-public-repository.md) rule 4).

## Behaviour

One row = one test. Anchors: `spec: evaluation.md#<heading>`. A row that asserts a message
asserts what is delivered on the configured channel.

### Levels

Unless a row says otherwise, the subject is `disk.free_bytes` on one volume, configured
`below` with `warning: 10GB` and `critical: 4GB`. Sizes are decimal, as the interface
renders them.

| Previous | Configuration | Value | Level | Why |
|---|---|---|---|---|
| ok | default | 40 GB | ok | neither comparison holds |
| ok | default | 10 GB | ok | entry is strict |
| ok | default | 9.99 GB | warning | below the warning value, above the critical one |
| ok | default | 4 GB | warning | the critical comparison is strict too |
| ok | default | 3 GB | critical | below the critical value |
| warning | default | 3 GB | critical | the more severe level is entered at once |
| ok | default | 0 | critical | an empty volume is a value, not an error |
| ok | `critical: 4GB` only | 9 GB | ok | a level with no value is never entered |
| ok | `warning: 10GB` only | 1 GB | warning | with no critical value, warning is the worst it can reach |
| — | nothing configured | 1 GB | none | no threshold, no level, and no previous level either: the series is listed and charted only |
| ok | `above`, `warning: 4`, `critical: 8` | 3.5 | ok | below both, and this subject is judged upwards |
| ok | `above`, `warning: 4`, `critical: 8` | 4 | ok | entry is strict in this direction as well |
| ok | `above`, `warning: 4`, `critical: 8` | 4.1 | warning | past the warning value |
| ok | `above`, `warning: 4`, `critical: 8` | 8 | warning | exactly the critical value: entry is strict in this direction too |
| ok | `above`, `warning: 4`, `critical: 8` | 8.2 | critical | past the critical value |

### Hysteresis

The same subject: `below`, `warning: 10GB`, `critical: 4GB`, so the margins are 12 GB and
4.8 GB.

| Previous | Configuration | Value | Level | Why |
|---|---|---|---|---|
| warning | default | 11 GB | warning | past entry, below the 12 GB clearing value |
| warning | default | 12 GB | ok | exactly the clearing value counts as cleared |
| warning | default | 9 GB | warning | entry still holds, so no exit is considered |
| ok | default | 11 GB | ok | hysteresis holds a level, it never creates one |
| warning | default | 4.5 GB | warning | the hold clause needs the previous level to be at least as severe, so it never raises one |
| critical | default | 4.5 GB | critical | inside the 4.8 GB clearing value, so the level is held although entry no longer applies |
| critical | default | 4 GB | critical | exactly the critical value: entry is strict, and 4 does not reach 4.8 either |
| critical | default | 4.9 GB | warning | clears the 4.8 GB critical clearing value, still below the warning value |
| critical | default | 11 GB | warning | critical clears, the 12 GB warning clearing value does not: a level steps down one band at a time |
| critical | default | 12 GB | ok | clears both levels in one tick |
| warning | `above`, `warning: 4` | 3.5 | warning | above the 3.2 clearing value |
| warning | `above`, `warning: 4` | 3.2 | ok | exactly the clearing value, cleared downwards |
| critical | `above`, `warning: 4`, `critical: 8` | 7 | critical | inside the 6.4 clearing value |
| warning | `below`, `warning: −100` | −90 | warning | the margin is 20% of the magnitude, so the clearing value is −80 |
| warning | `below`, `warning: −100` | −80 | ok | exactly the clearing value, cleared upwards |
| warning | `above`, `warning: −100` | −120 | ok | an `above` threshold clears downwards: −100 − 20 |
| ok | `below`, `warning: 0` | 0 | ok | entry is strict, so a zero threshold alerts below zero only |
| warning | `below`, `warning: 0` | 0 | ok | a margin of zero clears at the threshold itself |
| critical | `warning: 10GB` only | 11 GB | warning | critical cannot be held without a value, and the previous level still counts against warning's 12 GB |
| warning | `critical: 4GB` only | 9 GB | ok | warning has no value, so it is neither entered nor held, and the fall waits for the digest |
| warning | direction flipped to `above`, `warning: 10GB` | 9 GB | ok | the flip discards the held level: 9 GB is good under the new comparison |
| warning | `warning` edited from 10GB to 5GB | 9 GB | ok | a held level clears against the configuration of the tick that judges it: the clearing value is now 6 GB |
| warning | `warning` edited from 10GB to 20GB | 9 GB | warning | re-entered outright, not held |

### Freezing

Stale values are never re-evaluated: a frozen subject keeps its level and its `since`,
writes no event and sends no repeat, and its stale reading is not what the digest lists as
standing. What freezing withholds is judgement of stale values, not a record already
written: a message recorded from fresh values is still owed, and a transition recorded
while they were fresh is still part of the day's story.

Age is the tick time minus the measurement's own `ts`, against `stale_after` = 3× the
interval the node resolves for the series' sensor.

| Situation | Result |
|---|---|
| the node is silent (see below) | its subjects are frozen in that same tick, except the `silence` subject itself |
| a subject's newest value is older than `stale_after` | that subject is frozen; the others are evaluated |
| a subject's newest value is exactly `stale_after` old | evaluated: the bound is inclusive, as it is for a chart's gaps ([history](history.md#gaps)) |
| a removable volume is unplugged | frozen by the same rule; it neither recovers nor repeats |
| the node resolves the series' sensor as `enabled: false`, or resolves no interval for it | frozen: nothing will refresh those values |
| the series' newest value names no sensor | never frozen on age: with no interval, staleness has no meaning, and the value is judged as it stands |
| a series reported again under a different sensor name | `stale_after` follows the newest value's sensor from that tick on; the level is untouched |
| a series that reported with a sensor and now reports without one | never frozen on age from then on, by the row above it |
| a stored threshold naming a series that has never reported — a store written by hand | nothing is evaluated for it: there is no value to judge |
| a volume that reappears under different labels | a new subject, unconfigured until it is given a threshold; the old one freezes |
| the node reports again | evaluation resumes on the next tick, and a changed level writes one event |
| a subject freezes with an instant message still undelivered | the message is delivered anyway: it was recorded from values that were fresh at the time |

The `silence` subject is never frozen: its input is hub receipt time, which is always
fresh. A skewed agent clock can freeze that agent's own subjects; freezing only holds
state, so nothing is lost when the clock is corrected.

### Node silence

The node is a subject too: metric `silence`, empty labels, level `ok` or `critical`. It
needs no configuration — a node that goes quiet must be noticed on a fresh install
([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)) — and the window is the
`silence_after` its class resolves to ([0006](../decisions/0006-alerting-rules.md)).

| State | Event | New state | Side effect |
|---|---|---|---|
| ok | now − last_seen > `silence_after` | critical | notify immediately |
| critical | now − last_seen > `silence_after`, notified under 24h ago | critical | nothing |
| critical | now − last_seen > `silence_after`, notified 24h ago or more | critical | notify again |
| critical | now − last_seen within `silence_after` | ok | notify recovery |
| ok | now − last_seen within `silence_after` | ok | nothing |
| ok | now − last_seen exactly `silence_after` | ok | nothing: the window is inclusive, and only past it is silence |
| any | a node listed in the file that has never reported | no subject | nothing: an uninstalled agent is not an incident |

Recovery has no margin and no separate trigger: `last_seen` advances on every accepted
request ([ingest](ingest.md#storage)), and the next tick reads it. Silence cannot flap,
because a request either arrived inside the window or did not.

### Notifications

Leaving or entering `critical` is instant; everything else waits for the digest
([0016](../decisions/0016-leaving-critical-is-instant.md)). Delivery is driven by the
subject's newest event against its `last_notified_at`, not by what changed on this tick, so
a failed send is retried rather than lost.

| Situation | Delivery |
|---|---|
| ok or warning → critical | instant message |
| critical → warning | instant message |
| critical → ok | instant recovery message |
| ok → warning | queued for the digest |
| warning → ok | queued for the digest |
| an instant-delivery event newer than the subject's `last_notified_at`, from any earlier tick | delivered now, whatever the level has become since |
| `last_notified_at` empty | the subject has never been notified: any instant-delivery event is due |
| level unchanged, critical, last notified 24h ago or more | instant repeat |
| level unchanged, critical, last notified under 24h ago | nothing |
| level unchanged, warning or ok | nothing |
| the notifier returns an error or times out | the event stays written, `last_notified_at` is not advanced, the failure is logged, and the next tick delivers it again |
| one subject's send fails | the remaining subjects are still delivered: failure is per message |

### Messages

Every message carries the same fields, so a channel formats rather than decides: the node,
the metric id, the subject's labels, the level it left and the level it reached, the value
that produced it, and how long the subject has been in the level it left (`since`). A
digest carries a list of those, one entry per subject.

What a channel writes names the series, or two subjects of one metric on one node read
alike: the node, then a mount point as itself and every other label as the pair it is.
`fs` and `removable` are left out — they decorate a volume rather than identify it.

| Configuration | Result |
|---|---|
| `channel: log` | one English log line per message; nothing is sent, so the default channel needs no secret |
| `channel: telegram` | one message per notification to the configured chat |
| a value whose metric id ends in `_bytes`, `_pct` or `_seconds` | rendered in that unit; any other metric is rendered as a plain number ([history.md](history.md#wire-format)) |
| two subjects of one metric on one node, differing only by a label | two messages a reader can tell apart: each names its own labels |
| a volume's subject | named by its mount point; `fs` and `removable` are not in the text |
| a label whose value is empty, `mount` included | named like any other: an empty value is part of what tells one series from another |
| `locale: ru` | text, sizes and times of delivered messages come from the Russian catalogue ([0008](../decisions/0008-english-repo-bilingual-ui.md)) |
| `channel: log` with `locale: ru` | the log line stays English: logs are diagnostic, and the locale governs delivered channels only |

### Digest

The digest carries what critical did not take instantly: the moves between `ok` and
`warning` in both directions, and every subject standing in `warning`. Critical is instant
and repeats on its own clock. The content is derived from the recorded transitions and the
current levels, so a restart cannot lose a queue.

A digest is due when the most recent occurrence of `digest.at` in `digest.timezone` at or
before the tick time is later than `last_digest_at`. The window a digest reports on ends at
the tick that sends it, so that is where `last_digest_at` is left: the occurrence decides
*whether* a digest is due, the tick time records *what has been reported*. Stamping the
occurrence instead would leave everything recorded between the hour and the tick inside
tomorrow's window as well, and say it twice. The message itself is still stamped with the
occurrence, because that is the day it speaks for. On a database that has never digested,
`last_digest_at` is the hub's first start time, so history is never replayed.

| Situation | Result |
|---|---|
| the tick crosses `digest.at` in `digest.timezone` | one message listing each subject's last transition of the window that was not delivered instantly, and every subject currently in `warning` |
| a subject that returned from `warning` to `ok` inside the window | listed as the recovery it was: it touched no critical, so nothing delivered it ([Notifications](#notifications)) |
| a subject whose move into `warning` was overtaken by `critical` inside the window | not listed: its last move was delivered instantly, and it is not standing in `warning` |
| a database that has never digested | no digest over history: `last_digest_at` starts at the hub's first start time |
| a subject both transitioned to `warning` and is still in `warning` | listed once |
| a warning transition written by the same tick that sends the digest | included: a transition recorded by a tick falls inside that tick's digest window |
| no warning transition since the last digest and no subject in `warning` | no message: silence while all is well, and the window still closes at that tick |
| a subject is in `critical` and nothing is in `warning` | no digest: the critical was reported instantly |
| several warnings on several nodes | one message, not one per subject, entries ordered by node name, then metric id, then labels |
| a frozen subject whose move into `warning` was recorded while its values were fresh | listed as the transition it was: freezing withholds judgement, not a record already written |
| a frozen subject standing in `warning` with no transition inside the window | left out of the standing list: its reading is stale |
| the digest hour on a hub where a node reports and no subject is configured at all | one message saying that nothing here is being judged and where to set a threshold, and how many series are waiting for one, every day until something is: a hub watching nothing must not look like a quiet one |
| the same on a hub no node has reported to yet | nothing: there is no series to watch, and an installation half done is not an incident |
| a digest that is sent on a hub where some series have no threshold | a closing line naming how many, so a volume nobody configured is visible |
| the digest notifier returns an error | `last_digest_at` is not advanced; the next tick sends the same window again |
| the hub restarts between a warning transition and `digest.at` | the transition is still in the digest: it was recorded when it happened |
| the digest already went out today and the hub restarts | no second digest: `last_digest_at` is the guard |
| the hub was down at `digest.at` and starts later the same day | the digest is sent on the first tick after startup |
| the hub was down for two days | one digest, not two: only the most recent occurrence counts |
| a transition recorded after `digest.at` by the tick that sent the digest | not listed again tomorrow: the window closed at the tick that reported it |
| `digest.at` names an hour a DST change removes | the instant is built in the configured zone and normalised forward, so the day is not skipped |

### Persistence and restart

| Operation | Result |
|---|---|
| a level changes | one event is appended to the log and the subject's level and `since` change with it; a reader never sees one without the other, and `since` is the instant of the change |
| the level does not change | no event, `since` untouched; only `last_notified_at` may move |
| a subject's first evaluation, level `ok` | the subject appears with `since` = that instant; no event: nothing changed |
| a subject's first evaluation, level `warning` or `critical` | the subject appears, plus one event whose previous level is `ok` |
| a level change that is delivered instantly | the event is recorded before the message goes out: a hub that dies in between delivers on a later tick, and no message is ever sent for an event that was not recorded |
| the hub restarts | every subject's level, its `since` and when it was last notified are as they were; nothing is re-notified |
| two ticks at the same instant over unchanged data | the second writes no event and sends no message |
| a tick fires while the previous one is still running | skipped and logged: there is only ever one evaluation pass |
| the hub is asked to stop mid-tick | a change already recorded stays recorded, an in-flight send is abandoned rather than waited on, and no further subject is evaluated |
| the same transition is written twice | one event: a retry of a change is not a second change |
| the stored data was written by a newer hub | the hub refuses to start rather than judge subjects against a shape it does not know |
| a stored level this build does not know | that subject is evaluated as if it were new, and the fact is logged: corrupt data must not stop the hub from watching the rest |

`since` and `last_notified_at` above are concepts, not columns. What must survive a
restart: each subject's configuration and level, when it reached it, and when it was last
notified about; an append-only log of transitions carrying the value that produced each
one, which is the event stream skins subscribe to
([0001](../decisions/0001-semantic-core-and-skins.md)); and how far the digest window has
closed, which is the hub's first start time until a tick crosses `digest.at` and that
tick's own time afterwards — silence closes the window as a delivered message does, a
refused delivery leaves it open ([Digest](#digest)), and a stored value is a boundary, never
proof that a message went out. How that is stored is the implementation's business
([0017](../decisions/0017-one-spec-and-decision-gates.md)).

### Configuration changes

A threshold is stored, not filed, so an edit applies on the next tick with no restart. The
rows about the file are about the first tick after a restart with a changed file.

| Change | Result |
|---|---|
| a threshold edited while a subject is in `warning` | the next tick evaluates with the new value; a resulting level change is an ordinary transition with an ordinary event |
| a subject's direction flipped while it holds a level | the level is dropped and recomputed from entry alone, so a value that is good under the new comparison reads as `ok` at once |
| `critical` removed while a subject stands in `critical` | the next tick recomputes from `warning` and the value alone; the fall is an ordinary transition, and leaving `critical` is announced as ever ([0016](../decisions/0016-leaving-critical-is-instant.md)) |
| every threshold of a subject removed while it stands in `warning` or `critical` | it stops being a subject: its level is forgotten, no event is written and no recovery is announced — the level did not recover, the question was withdrawn |
| a threshold configured for a subject that is currently frozen | stored, and judged on the first tick that finds the values fresh again |
| every threshold removed while the subject is frozen | it stops being a subject at once: freezing withholds judgement of stale values, and a removal is not a judgement |
| a threshold set again on a subject whose configuration was removed while it stood in `critical` | judged as a new subject, previous level `ok`, so a value still past the threshold transitions and alerts again |
| a stored threshold this build cannot read — an unknown direction, a value that is not finite | that subject is not judged and the fact is logged; the rest of the tick runs |
| a threshold configured for a series that is already past it | the first tick transitions it like any other: a subject that arrives critical alerts at once |
| `silence_after` widened while a node is silent-critical | the next tick finds `now − last_seen` inside the new window and recovers it |
| a sensor interval lowered while the agent still holds the old one | `stale_after` shrinks first, so healthy subjects may freeze for up to one configuration delivery ([ingest](ingest.md#configuration-delivery)) |
| a node removed from the file | no subjects for it: its stored levels are left untouched and never evaluated, and no recovery is notified |

### Startup validation

Rows the hub refuses to start on, in the manner of [hub-config.md](hub-config.md#startup).
Threshold values are not among them: they are checked by the form that writes them
([thresholds.md](thresholds.md#saving)), and the hub must survive whatever is already
stored.

| Configuration | Result |
|---|---|
| `digest.at` not `HH:MM`, or a timezone the system's zone database does not carry | startup error naming the key |
| `notify.locale` outside `en`, `ru`; `notify.channel` outside `log`, `telegram` | startup error naming the key |
| `channel: telegram` with either environment variable unset | startup error naming the variable, never its value |
| a stored threshold this build cannot read — an unknown direction, a value that is not finite | that subject is skipped and the fact is logged; the hub starts and keeps watching the rest |

## Invariants

- One state change produces exactly one event and at most one instant message; a subject
  that stays in `warning` is listed in each daily digest, which is not a repeat of that
  change but a statement of the current state.
- The tick is idempotent at a fixed instant: called twice with the same clock, the same
  stored data and the same thresholds, it changes nothing.
- A message is never delivered for a transition that was not recorded first, so the log is
  never behind what a reader was told.
- Nothing a threshold touches reaches an agent, so no threshold edit changes a
  `config_version` ([hub-config.md](hub-config.md#configuration-version)).
- A restart never re-notifies what was delivered and never drops what was not:
  `last_notified_at` is the record of a delivery, and `last_digest_at` the boundary of the
  window still to be reported.
- No level is ever computed from a frozen subject's values, so stale data cannot recover a
  state or repeat an alert.
- Recovery is the negated entry comparison with a margin on the threshold's magnitude, never
  a separate rule ([0013](../decisions/0013-relative-hysteresis.md)).
- Nothing alerts that was not configured to: an unconfigured series has no level, and a
  fresh installation is silent until someone sets a number
  ([0032](../decisions/0032-thresholds-are-set-in-the-interface.md)).
- Every user-facing string is delivered in `notify.locale`; logs stay English
  ([0008](../decisions/0008-english-repo-bilingual-ui.md)).

## Edge cases

- **Two subjects of one volume** — bytes and percent — are independent: they can hold
  different levels at the same time, and the node's level is the most severe among them,
  as it is among any other subjects ([state.md](state.md#model)).
- **A threshold of zero** has a margin of zero, so it cannot flap-protect: entry is strict
  below zero and exit clears at zero itself. It is legal, and the form says what it means.
  A threshold small enough that 20% of it is not a number the machine can tell from zero —
  below roughly 1e-323 — behaves the same way, and for the same reason: at that magnitude
  the margin does not exist to be computed.
- **Clock skew on the agent** cannot affect silence, which runs on hub receipt time, but a
  measurement stamped in the future is still the newest value of its series and is
  evaluated as such until a later one arrives.
- **The first tick after a node's first ever report** transitions `ok → warning` or
  `ok → critical` like any other, so a node that arrives already critical alerts at once
  and one that arrives in warning waits for the digest.

## Out of scope

- Entering and validating a threshold → [thresholds.md](thresholds.md).
- Showing levels on the web page → the page is a skin
  ([0001](../decisions/0001-semantic-core-and-skins.md)) reading them from
  [state.md](state.md). Showing the event log → a follow-up, not this spec.
- Inbound Telegram commands (`/status`) → after the POC; the bot only sends.
- A locale per recipient, which [0008](../decisions/0008-english-repo-bilingual-ui.md)
  anticipates → while there is one recipient, `notify.locale` is that locale.
- A per-subject hysteresis margin, which [0013](../decisions/0013-relative-hysteresis.md)
  allows → deferred until a metric proves noisy.
- Forecast alerting ("full in ~12 days") → a later addition on top of thresholds
  ([0033](../decisions/0033-a-subject-is-a-series.md)).
- Which sensors run and how often → [hub-config.md](hub-config.md), [agent.md](agent.md).
- Storing measurements and advancing last-seen → [ingest.md](ingest.md).

## Open questions

None.
