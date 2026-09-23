# Spec: Anomaly

- **Status:** approved
- **Owns:** `internal/anomaly` (hub) — the norm of every series, how far its newest value
  lies from that norm, and which series are unusual enough to rank — and the `anomaly`
  field every subject of [`/api/v1/state`](state.md#wire-format) carries. Reading stored
  points stays with `internal/storage`; which subjects exist and whether they are stale
  stays with [state](state.md); excluding a series is set on
  [its own page](thresholds.md).
- **Decisions:** [0001](../decisions/0001-semantic-core-and-skins.md),
  [0030](../decisions/0030-the-state-api-reports-the-stored-verdict.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)

## Purpose

A threshold answers "is this value bad?", and only where somebody set a number. An anomaly
answers a different question — "is this value unlike what this series usually does?" — for
every series, with nothing set. It is what lets mission control surface a load five times
its usual on a machine nobody wrote a threshold for
([mission-control.md](mission-control.md)).

It judges no level, writes no event and sends no message: an anomaly is shown, never
notified ([0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)). It
knows nothing about any metric: a disk, a load average and an uptime are judged by the same
rule, against their own history. Unusual is either way: a failure fixed today is as unusual
as one that started today.

## Model

**The value judged** is the newest value of the series — the one the State API reports
beside it.

**The norm period** is the seven days ending 24 hours before the current hour began, on the
hub's clock: every stored point stamped from `H − 192h` to `H − 24h`, both bounds
inclusive, where `H` is the start of the UTC hour the answer is given in. Days here are
24 hours long whatever a time zone's clocks do. Every answer inside one hour is
measured against the same period. The last day is left out on purpose: a change is compared
with the week before it, so it stays visible for about a day instead of becoming its own
norm within hours.

**A series has a norm** when its norm period holds at least 24 points spanning at least
24 hours, oldest to newest. A series without one has no anomaly: a series first seen
yesterday cannot be unusual yet.

**The band** is drawn from the norm period's values by nearest rank — the value at
position `⌈p/100 · n⌉` of the `n` values sorted ascending:

```
low   = the 1st percentile
norm  = the 50th percentile
high  = the 99th percentile
```

Every one of the three is a value the series really reported. The band holds 98% of the
period's points — all of them when there are fewer than 100 — and it counts points, not
time: a laptop's band is drawn from the hours it was awake. Its two sides are measured
separately, because a metric's spikes usually go one way.

**The width of a side** is its distance from the norm — `high − norm` above, `norm − low`
below. When that is zero, the side reaches to the period's own extreme instead: the highest
value above, the lowest below. And it is never less than a **floor**: the larger of 1% of
the norm's magnitude and the smallest step between two distinct values the period holds on
that side, the norm included — none when that side holds the norm alone. The floor keeps a
series from turning unusual over a step it has already taken all week; a step it never took
in that direction still counts.

**The score** says where the value lies in widths of the side it is on:

```
value above norm   score =  (value − norm) / width above
value below norm   score = −(norm − value) / width below
value equal        score = 0
```

A score of 1 is the edge of the band; 2 is twice as far from the norm as the edge. The
score is rounded to two decimals, half away from zero, with −0 reported as 0, and
everything below reads the rounded score. A side can still be zero wide in one case: the
norm is zero and the period never left it on that side, as a count of failed services is
at zero on a good week. A value off that norm on that side has no score, reported as
`null`.

**A value is anomalous** when its score is 2 or more in either direction, or `null`.

**The rank** orders the anomalous subjects of one answer, 1 the most unusual: `null`
scores first, then by the magnitude of the score, largest first; ties keep the order of
[state](state.md#ordering). A subject that is not anomalous has no rank.

**Which subjects carry one.** Every series subject that is not stale and has a norm. A stale
value judges nothing here, as it judges nothing in evaluation
([evaluation](evaluation.md#freezing)); a node's silence subject has no value to judge.
Whether the series is watched, and at which level, makes no difference: a series can be
both critical and anomalous. A series the reader excluded on its page
([thresholds](thresholds.md#saving)) carries none: some series are unusual by nature.

## Wire format

Each subject of `/api/v1/state` carries `anomaly`, an object or `null`:

```json
{ "node": "server-b", "metric": "load.avg_5m", "labels": {},
  "watched": false, "level": null, "since": null, "stale": false,
  "unit": "number", "value": 3.1, "ts": "2026-09-23T09:55:00.000Z",
  "anomaly": { "norm": 0.4, "score": 7.5, "rank": 1 } }
```

`norm` is in the unit of the series, as `value` is. `score` is a number with at most two
decimals, or `null`; `rank` is a positive integer or `null`. `anomaly` is `null` exactly when
the subject carries none — a stale series, a series with no norm, an excluded series, a
node's silence, or any series when its stored points could not be read
([reading](#reading)).

## Behaviour

One row = one test. Anchors: `spec: anomaly.md#<heading>`.

### The norm period {#norm-period}

Unless a row says otherwise, the answer is given at 10:20 on the 23rd, so the period runs
from 10:00 on the 15th to 10:00 on the 22nd, and the series reports every 5 minutes and has
done so for weeks.

| Series | Anomaly |
|---|---|
| a point stamped 10:00:00.000 on the 22nd | part of the norm |
| a point stamped 10:00:00.001 on the 22nd | not part of the norm: the last day is what is being compared |
| a point stamped 10:00:00.000 on the 15th | part of the norm |
| a point stamped 09:59:59.999 on the 15th | not part of the norm |
| the same series asked at 10:59 | the same norm as at 10:20 |
| the same series asked at 11:00 | a norm from 11:00 on the 15th to 11:00 on the 22nd |
| first reported at 04:00 on the 22nd | `null`: its norm period holds six hours |
| 23 points in the norm period, over three days | `null`: fewer than 24 points |
| 40 points in the norm period, all inside 20 hours | `null`: they span less than a day |
| 24 points in the norm period, the first and last exactly 24 hours apart | measured: both bounds are inclusive |
| a laptop that reported 9 hourly points on each of three days of the period | measured |
| a silence of three days inside the norm period | measured against the points that are there |

### The score {#score}

The norm period holds the values 1, 2, …, 100, once each, so `low` is 1, `norm` 50 and
`high` 99, and each side is 49 wide.

| Newest value | Score | Rank |
|---|---|---|
| 50 | 0 | `null` |
| 99 | 1 | `null`: the edge of the band |
| 147 | 1.98 | `null` |
| 148 | 2 | a rank: the bound is inclusive |
| 1 | −1 | `null` |
| −48 | −2 | a rank: the same bound below |
| 1000 | 19.39 | a rank |

The norm period holds 95 values of 0.1 and 5 of 2.0 — an idle machine with a nightly job —
so `low` and `norm` are 0.1 and `high` 2.0. The side below has no width of its own,
reaches no lower extreme and holds no step, so it is 1% of 0.1.

| Newest value | Score | Rank |
|---|---|---|
| 2.0 | 1 | `null`: the nightly job is part of the norm |
| 3.9 | 2 | a rank |
| 0.1 | 0 | `null` |
| 0.09 | −10 | a rank: the machine never went below its idle all week |

The norm period holds 2014 values of 0.00 and two of 0.5 and 0.6 — a machine idle all week
but for two samples of a job — so `low`, `norm` and `high` are all 0, and the side above
reaches the period's highest value, 0.6:

| Newest value | Score | Rank |
|---|---|---|
| 0.01 | 0.02 | `null` |
| 0.5 | 0.83 | `null` |
| 1.3 | 2.17 | a rank |

The norm period holds 99 values of 1.99 and one of 3.98, and the side above is 1.99 wide:

| Newest value | Score | Rank |
|---|---|---|
| 5.97 | 2 | a rank: the score is compared as reported, whatever the arithmetic's last bit |

The norm period holds only zeros, as a count of failed services does:

| Newest value | Score | Rank |
|---|---|---|
| 0 | 0 | `null` |
| 1 | `null` | a rank, ahead of every numbered score |

The norm period holds only ones — a service failed all week:

| Newest value | Score | Rank |
|---|---|---|
| 0 | −100 | a rank: fixed today is unusual too; the floor is 1% of 1 |

The norm period holds ones and a single zero — a service failed all week but for a moment:

| Newest value | Score | Rank |
|---|---|---|
| 0 | −1 | `null`: the week already went there once |
| 2 | 100 | a rank: the step below does not widen the side above |

The norm period holds only 62.13, a volume nothing wrote to all week:

| Newest value | Score | Rank |
|---|---|---|
| 62.12 | −0.02 | `null`: 0.01 against a floor of 0.62 |
| 60.8 | −2.14 | a rank |
| 65 | 4.62 | a rank: space freed is unusual too |

The norm period holds only −5:

| Newest value | Score | Rank |
|---|---|---|
| −5.04 | −0.8 | `null`: the floor is 1% of the norm's magnitude, 0.05 |
| −5.1 | −2 | a rank |

### Which subjects carry one {#subjects}

| Subject | `anomaly` |
|---|---|
| a watched series at `critical` whose value is also outside its band | an object with a rank: a level and an anomaly are independent |
| an unwatched series with a norm | an object: nothing needs to be set for a series to be judged here |
| a stale series | `null`, whatever its newest value |
| the same series once it reports fresh values again | an object again |
| a node's silence subject | `null` |
| a series excluded on its page, outside its band | `null`, and no other series' rank counts it |
| the same series once the exclusion is removed | an object again, ranked if it is unusual |
| a series of a node the configuration no longer names | `null`: its series are all stale ([state](state.md#listing)) |

### The rank {#rank}

| Answer | Ranks |
|---|---|
| three anomalous series scoring 2.5, −9 and 4 | −9 is 1, 4 is 2, 2.5 is 3 |
| an anomalous series scoring `null` beside one scoring 40 | the `null` is 1 |
| two anomalous series scoring 3 and −3 | ranked in the order [state](state.md#ordering) lists them |
| two anomalous series scoring `null` | ranked in the order state lists them |
| no anomalous series | every `rank` is `null` |
| ranks of one answer | 1, 2, 3 … with no gap and no repeat |

### Reading {#reading}

| Situation | Anomaly |
|---|---|
| two answers in one hour, nothing reported between them | the same anomalies |
| a new value arrives | scored in the next answer, against the norm of that answer's hour |
| the stored points cannot be read for a norm | the rest of the state answered as ever, every `anomaly` `null`; the next answer tries again |

## Invariants

- `norm` is a value the series really reported inside its norm period; nothing is
  interpolated.
- `rank` is present exactly when the reported score is 2 or more in magnitude, or `null`.
- The ranks of one answer are 1 to the number of anomalous subjects, each once.
- A stale subject, an excluded one and a silence subject never carry an anomaly.
- Computing an anomaly writes nothing, sends nothing and changes no level.

## Edge cases

- **A reboot** drops `uptime.boot_seconds` far below its week, but a series that only climbs
  has a wide band: the reboot ranks only when the machine had been up for about 11 days or
  more, and then for about a day. A machine patched weekly does not surface its reboot here;
  a threshold `below 1800` does ([host-sensors](host-sensors.md#edge-cases)).
- **A steadily filling disk** sits a day's fill below the lowest value of its norm period,
  and at a constant pace scores about −1.3: it does not rank. Filling about 3.4
  times faster than the week before, for a whole day, does.
- **A change that lasts** — a load that stays five times higher — ranks for about a day
  and then becomes its own norm. A lasting state is a threshold's business.
- **A laptop's uptime** keeps counting through sleep while the laptop reports only when
  awake, so after a weekend asleep its first value is far above a band drawn from working
  hours: it scores about 2.2 to 2.7 and ranks for about a day each week, until the reader
  excludes it on its page.
- **A series whose spikes are rare** — rarer than 1% of its points — ranks at every such
  spike beyond twice the band. That is what the band is for.
- **A flat series at a small value** — two-decimal data sitting at 0.4 all week — has a floor
  of 0.004, so a single step of 0.01 ranks. Real load and memory do not sit still; a series
  that does is unusual when it moves.

## Out of scope

- **Notifying an anomaly** — shown only
  ([0036](../decisions/0036-an-anomaly-is-a-value-outside-its-weeks-band.md)); a digest line
  waits until the board shows how often anomalies occur.
- **Daily or weekly seasonality** — the band is the whole week; comparing an hour with the
  same hour of other days is a later refinement.
- **A per-series tolerance** — a wider or narrower cut-off than 2 — deferred until a series
  proves noisy, as the per-subject hysteresis margin was
  ([0013](../decisions/0013-relative-hysteresis.md)); a series that should not rank at all
  is excluded instead.
- **Trend, forecast and health 0–100** — the semantic engine's other fields, not built yet.
- **Anomalies in the past** — `at=` time travel is refused by the State API until it is
  designed ([state](state.md#endpoint)).

## Open questions

None.
