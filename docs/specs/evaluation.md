# Spec: Evaluation and alerts

- **Status:** approved
- **Owns:** `internal/evaluate` (hub): the tick, thresholds, hysteresis, silence detection
  and the notification boundary. The channels behind that boundary — the log line and the
  Telegram bot — are `internal/notify`, which formats and delivers but never decides.
  Persistence of levels, events and the digest mark stays with `internal/storage`; the
  `rules`, `digest`, `notify` and `volumes` keys are parsed and validated by
  `internal/config`, which keeps owning the file, using the rule names `internal/evaluate`
  exports.
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0006](../decisions/0006-alerting-rules.md),
  [0007](../decisions/0007-public-repository.md),
  [0012](../decisions/0012-threshold-model.md),
  [0013](../decisions/0013-relative-hysteresis.md),
  [0015](../decisions/0015-evaluation-on-a-tick.md),
  [0016](../decisions/0016-leaving-critical-is-instant.md)

## Purpose

Evaluation turns stored measurements into meaning: every subject gets a level (`ok`,
`warning`, `critical`), a change of level is written to an event log, and events become
notifications under the rules of [0006](../decisions/0006-alerting-rules.md) and
[0016](../decisions/0016-leaving-critical-is-instant.md). It runs on its own tick, never
inside a request ([0015](../decisions/0015-evaluation-on-a-tick.md)), so
[ingest](ingest.md) keeps checking shape and not meaning.

It does not collect, does not render, and does not decide what an agent runs: a threshold
never reaches an agent ([0010](../decisions/0010-agent-configuration.md)).

## Model

**A subject is what has a level**: the triple `(node, rule, labels)`. For the `disk` rule a
subject is one volume — `server-b`, `disk`, `{mount: /, fs: ext4, removable: false}` — and
its two metrics are read together, because the threshold model of
[0012](../decisions/0012-threshold-model.md) needs both. A rule whose metric list holds one
entry (a future finance or health metric) is the same shape with one series.

**A rule declares the metrics it reads and the sensor they come from.** The `disk` rule
reads `disk.free_bytes` (`free`) and `disk.free_pct` (`pct`) from sensor `disk`, joined on
byte-identical labels. Naming the sensor is what makes staleness computable, below; the
metric ids alone do not identify one.

**Levels are ordered** `ok < warning < critical`. A level is entered and left by the
floor-plus-band of [0012](../decisions/0012-threshold-model.md), with the 20% margin of
[0013](../decisions/0013-relative-hysteresis.md) on every comparison of the exit:

```
enter L   when  free < floor(L)   or  (pct < ratio(L)  and  free < ceiling(L))
leave L   when  free >= 1.2·floor(L)  and  (pct >= 1.2·ratio(L)  or  free >= 1.2·ceiling(L))
```

The exit rule is the negation of the entry rule, never a per-condition clearance. Only
`free` and `pct` are inputs, compared over decimal bytes and percentage points at full
precision, with each margin computed as `threshold × 1.2`; a value exactly at a margin
counts as cleared. The size of the volume appears in the tables below to make the pairs
plausible and is never read.

**A rule with no band** — a volume whose `role` is `backup` — keeps only the floor
comparison on both sides. The absent band contributes `false` to the entry disjunction and
`true` to the exit conjunction, so neither side degenerates: `true` at entry would alert
every volume, `false` at exit would latch every one of them.

**The level of a subject** is chosen from its previous level, most severe first; a subject
with no stored state has previous level `ok`:

```
for L in [critical, warning]:
    if enter(L)                     -> L
    if previous >= L and !leave(L)  -> L      # hysteresis holds the level
-> ok
```

The margin is 20% and no key changes it: a file naming one is refused as an unknown key
([hub-config.md](hub-config.md#startup)). [0013](../decisions/0013-relative-hysteresis.md)
allows a per-metric override for an inherently noisy metric; no metric needs one yet, so
the key is deferred rather than shipped unused.

## The tick

Evaluation runs on its own schedule ([0015](../decisions/0015-evaluation-on-a-tick.md)),
against one consistent view of the data taken at the instant it evaluates for: a
measurement that arrives while a tick is running is evaluated by the next tick, never by
half of this one. Within a tick a node's silence is decided before its other subjects, so a
node that has just fallen silent has its subjects frozen in that same tick rather than the
next. A level change is recorded before any message about it is sent. Messages and digest
entries come out in a stable order: by node name, then by the subject's `mount` label.

Two ticks never run at once: a tick that would start while the previous one is still
running is skipped, and the skip is logged. A notifier that does not return cannot hold
evaluation open — the send is abandoned, counted as a failure, and the event is retried on
a later tick.

## Configuration

These keys extend [hub-config.md](hub-config.md). None of them reaches an agent, so none
of them changes a `config_version`.

```yaml
digest: { at: "09:00", timezone: UTC }   # product default
notify: { channel: log, locale: en }     # channel: log | telegram

rules:
  disk:
    warning:  { floor: 10GB, ratio: 15, ceiling: 100GB }
    critical: { floor: 4GB,  ratio: 7,  ceiling: 40GB }
    backup:
      warning:  { floor: 50GB }
      critical: { floor: 10GB }

classes:
  server:
    rules: { disk: { critical: { floor: 8GB } } }

nodes:
  server-b:
    volumes:
      "/data/backup": { role: backup }
```

- **Product defaults** are the numbers above, from [0012](../decisions/0012-threshold-model.md);
  the evaluation tick is 1m ([0015](../decisions/0015-evaluation-on-a-tick.md)) and no key
  changes it.
- **Layering** follows [0010](../decisions/0010-agent-configuration.md) and merges field by
  field: product default → top-level `rules` → class `rules` → node `rules` → volume
  `rules`. The `backup` branch is a rule of its own, not an overlay on the default one: it
  merges only with the `backup` branch of the layers below it, so an absent `ratio` or
  `ceiling` stays absent instead of being inherited.
- **A `volumes` key selects a subject by a byte-identical `mount` label**, the mount point
  exactly as the OS reports it ([disk-sensor.md](disk-sensor.md#labels)). Nothing is
  normalised: a trailing slash is a different volume.
- **Sizes are decimal** (`10GB` = 10 000 000 000), matching how the interface renders them
  and how disks are sold. `ratio` is a percentage number, not a fraction, and a band is
  removed by setting both of its halves to zero — half of one is refused, because half a
  band would be ignored in silence.
- **Secrets are never in the file**: with `channel: telegram` the bot token and the chat id
  come from `MONITOR_TELEGRAM_TOKEN` and `MONITOR_TELEGRAM_CHAT_ID`
  ([0007](../decisions/0007-public-repository.md) rule 4).

## Behaviour

One row = one test. Anchors: `spec: evaluation.md#<heading>`. A row that asserts a message
asserts what is delivered on the configured channel.

### Levels

A 128 GB volume unless the row says otherwise; defaults from the table above. `pct` is
carried to two decimals, as the sensor reports it ([disk-sensor.md](disk-sensor.md)).

| Previous | free / pct | Level | Why |
|---|---|---|---|
| ok | 40 GB, 31.25% | ok | neither arm holds |
| ok | exactly 10 GB on a 40 GB volume, 25.00% | ok | the floor comparison is strict, and 25% is above the ratio |
| ok | 19 GB, 14.84% | warning | band: under 15% and under the 100 GB ceiling |
| ok | 19.2 GB, 15.00% | ok | the ratio comparison is strict |
| ok | 9 GB on a 20 GB volume, 45.00% | warning | floor: under 10 GB, whatever the percentage |
| ok | 99 GB on an 8 TB volume, 1.24% | warning | the warning band holds, and 99 GB is above the 40 GB critical ceiling, so it is not critical |
| ok | 1.1 TB on an 8 TB volume, 13.75% | ok | under the ratio, so only the ceiling can decide, and 1.1 TB is above it |
| ok | 5 GB, 3.91% | critical | the critical band alone: under 7% and under the 40 GB ceiling |
| ok | exactly 4 GB on a 40 GB volume, 10.00% | warning | the critical floor is strict, and 10% is above the critical ratio |
| ok | 45 GB on a 900 GB volume, 5.00% | warning | under the critical ratio but above the 40 GB critical ceiling |
| ok | 40 GB on a 1 TB volume, 4.00% | warning | the ceiling comparison is strict, so the critical band does not hold |
| ok | 3 GB, 2.34% | critical | both critical arms hold |
| warning | 3 GB, 2.34% | critical | the more severe level wins immediately |
| ok | 0 GB, 0.00% | critical | a full volume is critical, not an error |

### Hysteresis

| Previous | free / pct | Level | Why |
|---|---|---|---|
| warning | 11 GB on a 40 GB volume, 27.50% | warning | past entry, below the 12 GB margin |
| warning | 12 GB on a 40 GB volume, 30.00% | ok | clears the floor with its margin |
| warning | 20 GB, 15.63% | warning | past entry, below the 18% margin |
| warning | 23 GB, 17.97% | warning | one step below the margin: 18% of 128 GB is 23.04 GB |
| warning | 23.04 GB, 18.00% | ok | exactly the ratio margin |
| warning | 24 GB, 18.75% | ok | clears 12 GB and the 18% margin |
| warning | 12 GB on a 128 GB volume, 9.38% | warning | the rule re-enters at that size, so no exit is considered: clearing the floor is not clearing the rule |
| warning | 120 GB on an 8 TB volume, 1.50% | ok | the ceiling comparison clears first |
| critical | 4.5 GB on a 45 GB volume, 10.00% | critical | only the 4.8 GB floor margin holds it: the band cleared at 8.4% |
| warning | 4.5 GB on a 45 GB volume, 10.00% | warning | hysteresis holds a level, it never raises one |
| critical | 6 GB on a 20 GB volume, 30.00% | warning | clears critical, still under the warning floor |
| critical | 24 GB, 18.75% | ok | clears both levels in one tick |

### Backup volumes

A 2 TB volume the node's `volumes` map declares as `role: backup`.

| Previous | free / pct | Level | Why |
|---|---|---|---|
| ok | 40 GB, 2.00% | warning | under the 50 GB floor |
| warning | 55 GB, 2.75% | warning | below the 60 GB margin |
| warning | 60 GB, 3.00% | ok | clears the floor with its margin |
| ok | 70 GB, 3.50% | ok | the default rule would warn at 3.5%; a backup rule has no band |
| ok | 9 GB, 0.45% | critical | under the 10 GB floor |
| critical | 11 GB, 0.55% | critical | below the 12 GB margin |
| critical | 12 GB, 0.60% | warning | clears the critical margin, still under the 50 GB warning floor |

### Freezing

Stale values are never re-evaluated: a frozen subject keeps its level and its `since`,
writes no event, sends no repeat, and is left out of the digest. What freezing withholds is
judgement of stale values, not a record already written: a message recorded from fresh
values and never delivered is still owed.

`stale_after` is 3× the interval the node resolves for the rule's sensor
([hub-config.md](hub-config.md#resolution)). Age is the tick time minus the measurement's
own `ts`, measured against the **older** of the joined series.

| Situation | Result |
|---|---|
| the node is silent (see below) | its subjects are frozen in that same tick, except the `silence` subject itself |
| a subject's older series is older than `stale_after` | that subject is frozen; the others are evaluated |
| one of the two series is stale and the other fresh | frozen: the join is only as fresh as its older half |
| a subject whose second series has never arrived | no subject at all: an incomplete join is not a level |
| a removable volume is unplugged | frozen by the same rule; it neither recovers nor repeats |
| the node resolves the rule's sensor as `enabled: false` | no subjects for that rule on that node; stored states are left untouched |
| the node resolves no interval for the rule's sensor | no subjects for that rule on that node |
| a volume that reappears under different labels | a new subject starting at `ok`; the old one freezes |
| the node reports again | evaluation resumes on the next tick, and a changed level writes one event |
| a subject freezes with an instant message still undelivered | the message is delivered anyway: it was recorded from values that were fresh at the time |

The `silence` subject is never frozen: its input is hub receipt time, which is always
fresh. A skewed agent clock can freeze that agent's own subjects; freezing only holds
state, so nothing is lost when the clock is corrected.

### Node silence

The node is a subject too: rule `silence`, empty labels, level `ok` or `critical`. The
window is the `silence_after` its class resolves to ([0006](../decisions/0006-alerting-rules.md)).

| State | Event | New state | Side effect |
|---|---|---|---|
| ok | now − last_seen > `silence_after` | critical | notify immediately |
| critical | now − last_seen > `silence_after`, notified under 24h ago | critical | nothing |
| critical | now − last_seen > `silence_after`, notified 24h ago or more | critical | notify again |
| critical | now − last_seen within `silence_after` | ok | notify recovery |
| ok | now − last_seen within `silence_after` | ok | nothing |
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
the rule, the subject's labels, the level it left and the level it reached, the joined
values that produced it, and how long the subject has been in the level it left (`since`).
A digest carries a list of those, one entry per subject.

| Configuration | Result |
|---|---|
| `channel: log` | one English log line per message; nothing is sent, so the default channel needs no secret |
| `channel: telegram` | one message per notification to the configured chat |
| `locale: ru` | text, byte sizes and times of delivered messages come from the Russian catalogue ([0008](../decisions/0008-english-repo-bilingual-ui.md)) |
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
| several warnings on several nodes | one message, not one per subject, entries ordered by node name then by mount |
| a frozen subject in `warning` | left out: its data is stale, so it is neither a transition nor a current reading |
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
restart: each subject's level, when it reached it, and when it was last notified about; an
append-only log of transitions carrying the values that produced each one, which is the
event stream skins subscribe to ([0001](../decisions/0001-semantic-core-and-skins.md)); and
how far the digest window has closed, which is the hub's first start time until a tick
crosses `digest.at` and that tick's own time afterwards — silence closes the window as a
delivered message does, a refused delivery leaves it open ([Digest](#digest)), and a
stored value is a boundary, never proof that a message went out. How that is stored is
the implementation's business
([0017](../decisions/0017-one-spec-and-decision-gates.md)).

### Configuration changes

The file is read once at startup ([hub-config.md](hub-config.md)), so every row here is
about the first tick after a restart with a changed file.

| Change | Result |
|---|---|
| a threshold edited while a subject is in `warning` | the next tick evaluates with the new numbers; a resulting level change is an ordinary transition with an ordinary event |
| `silence_after` widened while a node is silent-critical | the next tick finds `now − last_seen` inside the new window and recovers it |
| a sensor interval lowered while the agent still holds the old one | `stale_after` shrinks first, so healthy subjects may freeze for up to one configuration delivery ([ingest](ingest.md#configuration-delivery)) |
| a `volumes` entry for a mount no measurement has ever carried | the hub starts; the override applies if that volume appears |
| a `volumes` key that differs from a reported `mount` only by a trailing slash | a different subject: mounts are matched byte-identically |
| a node removed from the file | no subjects for it: its stored states are left untouched and never evaluated, and no recovery is notified |
| a rule removed from the file | the same: its subjects' states are left untouched and unevaluated |

### Startup validation

Rows the hub refuses to start on, in the manner of [hub-config.md](hub-config.md#startup).
What a layer says on its own terms — a size, a ratio, a rule name, the shape of a `backup`
branch — is checked at every layer, including a class no node uses yet. What only a
finished rule can be judged on — critical against warning — is checked on the resolved rule
of every node and every declared volume, because a layer above may raise the value that
makes it consistent ([hub-config.md](hub-config.md#invariants)).

| Configuration | Result |
|---|---|
| a size that is not a number with a known unit (`10GB`, `500MB`) | startup error naming the key |
| `ratio` outside 0–100 | startup error naming the key |
| a rule name no rule reads | startup error naming the rule: an unknown map key is not caught by the unknown-field check |
| a resolved rule whose critical `floor`, `ratio` or `ceiling` is above the warning value for the same field | startup error naming node, volume and field |
| `ratio` or `ceiling` under a `backup` branch | startup error: a backup rule is a floor |
| a resolved rule left with a `ratio` and no `ceiling`, or the reverse | startup error naming the rule: a band needs both, and half of one would be ignored in silence. A layer may still write one half and inherit the other, the way every other field layers |
| `role` other than `backup` | startup error naming node and mount |
| `digest.at` not `HH:MM`, or a timezone the system's zone database does not carry | startup error naming the key |
| `notify.locale` outside `en`, `ru`; `notify.channel` outside `log`, `telegram` | startup error naming the key |
| `channel: telegram` with either environment variable unset | startup error naming the variable, never its value |

## Invariants

- One state change produces exactly one event and at most one instant message; a subject
  that stays in `warning` is listed in each daily digest, which is not a repeat of that
  change but a statement of the current state.
- The tick is idempotent at a fixed instant: called twice with the same clock and the same
  stored data, it changes nothing.
- A message is never delivered for a transition that was not recorded first, so the log is
  never behind what a reader was told.
- Nothing a threshold touches reaches an agent, so no threshold edit changes a
  `config_version` ([hub-config.md](hub-config.md#configuration-version)).
- A restart never re-notifies what was delivered and never drops what was not:
  `last_notified_at` is the record of a delivery, and `last_digest_at` the boundary of the
  window still to be reported.
- No level is ever computed from a frozen subject's values, so stale data cannot recover a
  state or repeat an alert.
- Recovery is the negated entry rule with a margin on every comparison, never a
  per-condition clearance ([0013](../decisions/0013-relative-hysteresis.md)).
- Every user-facing string is delivered in `notify.locale`; logs stay English
  ([0008](../decisions/0008-english-repo-bilingual-ui.md)).

## Edge cases

- **A metric the configuration does not declare** is stored by ingest and ignored here: no
  rule reads it, so it has no subject and no level.
- **Clock skew on the agent** cannot affect silence, which runs on hub receipt time, but a
  measurement stamped in the future is still the newest value of its series and is
  evaluated as such until a later one arrives.
- **The first tick after a node's first ever report** transitions `ok → warning` or
  `ok → critical` like any other, so a node that arrives already critical alerts at once
  and one that arrives in warning waits for the digest.

## Out of scope

- Showing levels on the web page → the page is a skin
  ([0001](../decisions/0001-semantic-core-and-skins.md)) reading them from
  [state.md](state.md). Showing the event log → a follow-up, not this spec.
- Inbound Telegram commands (`/status`) → after the POC; stage 2 only sends.
- A locale per recipient, which [0008](../decisions/0008-english-repo-bilingual-ui.md)
  anticipates → while there is one recipient, `notify.locale` is that locale.
- A per-metric hysteresis margin, which [0013](../decisions/0013-relative-hysteresis.md)
  allows → deferred until a metric proves noisy.
- Forecast alerting ("full in ~12 days") → [0012](../decisions/0012-threshold-model.md)
  defers it until history is long enough.
- Which sensors run and how often → [hub-config.md](hub-config.md), [agent.md](agent.md).
- Storing measurements and advancing last-seen → [ingest.md](ingest.md).

## Open questions

None.
