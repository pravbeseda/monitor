# Spec: Backup sensor

- **Status:** approved
- **Owns:** `internal/sensor/backup` (agent): the directory backup jobs write their status
  to, the file's format, and the metrics, labels and names the sensor reports from it
- **Decisions:** [0049](../decisions/0049-a-backup-job-reports-through-a-status-file.md),
  [0003](../decisions/0003-sensors-are-modules.md),
  [0033](../decisions/0033-a-subject-is-a-series.md),
  [0048](../decisions/0048-a-sensor-names-its-metrics.md)

## Purpose

A backup job knows what it did; the monitor does not. Each job writes one small file after
every run — whether it failed, when it last succeeded, the repository's figures — and this
sensor turns the files into series, so a threshold such as `above 2d` on an age catches a
job that stopped and `above 0` on the failure flag one that ran and failed. Like every
sensor it decides nothing: the series have no level until someone sets one on their page
([thresholds.md](thresholds.md)).

It takes no parameter: the directory is this spec's, and the files name their backups. It
runs in the compiled-in `server` and `laptop` profiles every 15m
([hub-config.md](hub-config.md#resolution)); a node with no directory reports nothing.

## The status file

**The directory** is `/var/lib/monitor-agent/backup` on Linux and
`/usr/local/var/lib/monitor-agent/backup` on macOS — the agent's, not the hub's
`/var/lib/monitor`, which belongs to the hub's account and is closed to everyone else
([deployment.md](deployment.md#where-things-live)). It may be a symbolic link to a directory.
The job's provisioning creates it and lets the job write to it; the agent only reads.

**The sensor reads** every regular file directly in the directory whose name ends in
`.json`, in that case, and does not start with a dot. Nothing else is read: a subdirectory,
a symbolic link, a named pipe, `X.JSON`, `.site-a.json`.

**A job writes its file atomically**: to a hidden name in the directory —
`mktemp "$dir/.status.XXXXXX"` works on Linux and macOS alike — then renamed over its
`.json` file, so the sensor never reads half of one. The file's name is the job's choice; the
sensor reads the backup and the job from its content.

**The content** is one JSON object, in UTF-8 with no byte order mark, of at most 65 536
bytes; whitespace may follow it, a second value may not.

```json
{
  "backup": "site-a",
  "job": "create",
  "failed": false,
  "last_success": "2026-08-28T04:30:00Z",
  "archives": 14,
  "latest_archive": "2026-08-28T04:30:00Z",
  "stored_bytes": 4000000000,
  "latest_archive_original_bytes": 2500000000
}
```

| Key | Type | Required | Meaning |
|---|---|---|---|
| `backup` | label | yes | the backup the job works on — a repository, as the job names it |
| `job` | label | yes | what the job does to it: `create`, `copy`, `verify`, … |
| `failed` | `true` or `false` | yes | the latest run failed |
| `last_success` | time | no | when a run last succeeded; absent until one has |
| `archives` | count | no | the archives the repository holds |
| `latest_archive` | time | no | when the newest of them was made |
| `stored_bytes` | count | no | what the repository occupies on its disk |
| `latest_archive_original_bytes` | count | no | the size of the data the newest archive holds, before compression and deduplication |

- **A label** is a string of 1 to 64 characters with no control character.
- **A time** is `YYYY-MM-DDThh:mm:ss`, optionally a fraction of a second, then `Z` or an
  offset `±hh:mm` — what `date -u +%Y-%m-%dT%H:%M:%SZ` prints on Linux and macOS alike.
- **A count** is a number written in digits only — no sign, fraction or exponent — from 0 to
  9 007 199 254 740 992 (2⁵³).
- **`null` on an optional key** means the key is absent, so a job may write `null` for a
  figure it does not have; on a required key it is an error.
- **Keys match exactly**: `Failed` is not `failed` but a key the table does not list, and a
  key the table does not list is ignored, so a job may carry more than the sensor reads. A
  listed key given twice is an error.

**The repository's figures are those of the repository the job writes to** — or checks, for
a job that only reads one — and a job writes those it has. Each is reported as given, with
no check against the others.

## Measurements

Every metric is labelled `backup` and `job`, carried verbatim from the file. One file is
one series of each metric it has a value for.

| Metric | Value | en | ru |
|---|---|---|---|
| `backup.failed` | `1` when `failed` is true, `0` when it is false | Failed {backup} {job} | Сбой {backup} {job} |
| `backup.success_age_seconds` | whole seconds from `last_success` to the agent's clock, truncated | Since success {backup} {job} | С успеха {backup} {job} |
| `backup.archives` | `archives` | Archives {backup} {job} | Архивов {backup} {job} |
| `backup.latest_archive_age_seconds` | whole seconds from `latest_archive` to the agent's clock, truncated | Newest archive {backup} {job} | Свежий архив {backup} {job} |
| `backup.stored_bytes` | `stored_bytes` | Stored {backup} {job} | Занято {backup} {job} |
| `backup.latest_archive_original_bytes` | `latest_archive_original_bytes` | Newest archive data {backup} {job} | Данные архива {backup} {job} |

The family `backup` is "Backups" / "Бэкапы". For a job that makes archives the two ages are
the same; they part for one that copies or checks a repository, whose run can succeed while
the newest archive it found is old.

## Behaviour

One row = one test. Anchors: `spec: backup-sensor.md#<heading>`.

### Files

| Directory | Collected |
|---|---|
| no directory | no measurements and no error |
| an empty directory | no measurements and no error |
| a symbolic link to a directory holding a valid file | that file's metrics |
| a regular file where the directory should be | no measurements and an error the agent logs |
| a directory that cannot be listed | no measurements and an error the agent logs |
| one file of `site-a` / `create`, every key present | the six metrics, each labelled `backup="site-a"`, `job="create"` |
| two files, `site-a` / `create` and `site-a` / `copy` | the metrics of each, labelled by its own job |
| beside a valid file: a subdirectory named `d.json`, a symbolic link to a valid file, a named pipe `p.json`, `.hidden.json`, `X.JSON` and `x.json.tmp` | the valid file's metrics only, no error, and the pipe does not hold the collection up |
| a file listed and then deleted before it is read, beside a valid one | the valid file's metrics, and no error: a backup being retired |
| a file that cannot be read, beside a valid one | the valid file's metrics, and an error the agent logs naming the other |
| two valid files naming the same `backup` and `job`, beside a third | the third's metrics only, and an error the agent logs naming both files and the pair: one series cannot hold two files |
| an invalid file naming the same `backup` and `job` as a valid one | the valid file's metrics, and an error naming the invalid one: only valid files make a pair |
| two invalid files beside a valid one | the valid file's metrics, and one error naming both files |

### Content

Each case below is a file beside a valid one: the valid file's metrics are collected, and
the case gives either the error the agent logs naming the file, or the file's own metrics.

| File | Result |
|---|---|
| exactly 65 536 bytes | read |
| 65 537 bytes | error |
| not JSON | error |
| a JSON array, string or number | error |
| an object followed by a newline and spaces | read |
| an object followed by a second object | error |
| invalid UTF-8 inside a string | error |
| starting with a UTF-8 byte order mark | error |
| `backup`, `job` or `failed` missing | error, one case each |
| `backup`, `job` or `failed` `null` | error, one case each |
| `failed` the string `"false"` | error |
| `backup` `""`, or 65 characters | error |
| `backup` 64 characters of Cyrillic | read |
| `job` holding a newline | error |
| `backup` `Сайт — A` | labelled `Сайт — A`, byte for byte |
| `archives` `14.0`, `14.5`, `1e3`, `-1`, `"14"` or 9 007 199 254 740 993 | error, one case each |
| `archives` 9 007 199 254 740 992 | read |
| `last_success` `2026-08-28 04:30:00Z`, `2026-08-28T04:30:00`, `2026-08-28T04:30:00+0300` or `yesterday` | error, one case each |
| `failed` given twice | error |
| `Failed: true` beside `failed: false` | read, `backup.failed` 0: a key of another case is not the key |
| a key the table does not list | read |
| `archives` and `stored_bytes` `null` | read, neither metric collected |

### Values

| File | Collected |
|---|---|
| `failed` true | `backup.failed` 1 |
| `failed` false | `backup.failed` 0 |
| `last_success` 1 day, 2 hours and 0.9 s before the agent's clock | `backup.success_age_seconds` 93 600 |
| `last_success` `2026-08-28T07:30:00+03:00`, the agent's clock at 05:30 UTC that day | `backup.success_age_seconds` 3 600 |
| `last_success` equal to the agent's clock | `backup.success_age_seconds` 0 |
| `last_success` 3 days before the agent's clock, in a file written a second ago | `backup.success_age_seconds` 259 200: the file's modification time is not the job's date |
| `failed` true and no `last_success` — a job that never succeeded | `backup.failed` 1 and no `backup.success_age_seconds` |
| only the required keys | `backup.failed` alone |
| `archives` 0 beside a `latest_archive` | `backup.archives` 0 and the age of `latest_archive`: an empty count is a reading, and the figures are not checked against each other |
| `latest_archive` 3 hours before the agent's clock | `backup.latest_archive_age_seconds` 10 800 |
| `last_success` 0.5 s, or `latest_archive` a day, later than the agent's clock — a clock set back — beside the other keys | no measurement of that age, the others collected, and no error: a negative age is not a reading |
| `stored_bytes` 4 000 000 000 and `latest_archive_original_bytes` 2 500 000 000 | those two values, unchanged |

### Applicability

| Machine | Manifest entry |
|---|---|
| Linux | `backup`, applicable |
| macOS | `backup`, applicable |

## Invariants

- Collection is read-only and cheap: one listing of the directory and one read of each file,
  nothing run, no repository opened, and a file is checked to be a regular one as it is
  opened, not only when listed.
- A value the file does not give is left out, never reported as zero: `0` archives or an age
  of `0` would read as a fact.
- An age is the job's date against the agent's clock, so a job that stops writing ages
  while the agent reports.
- Every measurement names the sensor that produced it, so its series ages by that sensor's
  interval ([0033](../decisions/0033-a-subject-is-a-series.md)).

## Edge cases

- **A job that stops running** leaves its file as it was: its ages grow and `backup.failed`
  keeps the last run's outcome.
- **A job that runs and fails** writes `failed: true` and keeps its earlier `last_success`,
  so the age since that success keeps growing beside the flag.
- **A backup that is retired** stops producing series once its file is deleted; the rows stay
  on `/debug`, marked as holding no fresh data, and whatever level they held is frozen
  ([evaluation.md](evaluation.md#freezing)). A series with a threshold stays in the list of
  what needs attention as holding no fresh data ([attention.md](attention.md#model)) until
  its threshold is cleared. **A renamed backup or job** leaves the same rows behind, and the
  new name starts new series.
- **A file written by hand** is read like any other: the sensor cannot tell a job from a
  person, and the directory's permissions decide who may write.
- **A machine that sleeps** reports nothing until it wakes, and its first reading carries the
  full ages.

## Out of scope

- Why a run failed: it is not a number, so it stays in the job's log.
- Whether an archive the job reports really landed: the repository's host can report its
  own side as another job, in this format.
- Writing the files, and choosing which backups and jobs report: the jobs' provisioning.
- Time Machine: its own sensor reads its own record
  ([timemachine-sensor.md](timemachine-sensor.md)).

## Open questions

None.
