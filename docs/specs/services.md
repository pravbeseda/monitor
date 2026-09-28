# Spec: Services

- **Status:** approved
- **Owns:** `internal/sensor/gdrive` and `internal/collect` (hub): which node the hub collects
  itself, what it needs to, when such a node counts as silent, and what the Google Drive
  sensor reports
- **Decisions:** [0003](../decisions/0003-sensors-are-modules.md),
  [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0039](../decisions/0039-the-hub-collects-a-service-node.md)

## Purpose

Some readings belong to no machine: how much space a cloud storage account has left is a
fact about the account, not about the computer that asks. Such a reading is a series of a
**service node** — a node of the compiled-in class `service`, listed in the hub's file like
any other, which no agent runs: the hub collects it itself, with the sensors it carries, and
its series stand under that node's name wherever a node's name is shown
([0039](../decisions/0039-the-hub-collects-a-service-node.md)).

The first such sensor is `gdrive`: the free space of one Google account, as Google counts it
against the account's quota — Drive, Gmail and Photos together. Like every sensor it decides
nothing: its series have no level until someone sets one ([thresholds.md](thresholds.md)).

What reaches a service node's collection, how often, and how its series age is the same
configuration and the same evaluation as for any node ([hub-config.md](hub-config.md),
[evaluation.md](evaluation.md)); the collection runs the agent's own loop
([agent.md](agent.md#ticking)). This spec owns only what differs.

## The file and the environment

```yaml
nodes:
  cloud:
    class: service
    sensors:
      gdrive: { enabled: true }
```

**The `service` class is compiled in**: an empty profile, `silence_after` 3h. A service node
has no `token_env`: no agent speaks for it, so there is nothing to authenticate. The file may
override the class key by key, as it may `server` and `laptop`; a class the file introduces
is never a service class. The name is reserved: a file written before it existed, whose
machines are of a class `service`, is refused with an error saying to rename that class.

**The hub carries one sensor, `gdrive`**, collected every 1h by default. A service class or
node runs only sensors the hub carries, and `gdrive` runs only there.

**A service node is silent when its sensors have stopped answering**, not when the hub stops
running it: it is seen each time it stores a measurement, and a report with none does not
count — except the very first, so that a node whose service refuses it from the start falls
silent too. A service that refuses the hub therefore raises the same instant critical message as a
node that stops reporting, and its recovery the same "reporting again"
([evaluation.md](evaluation.md)).

**Credentials live in the hub's environment file**, as every secret does
([0007](../decisions/0007-public-repository.md), the "Secrets" row), and are required only
while some service node enables `gdrive`:

| Variable | Holds |
|---|---|
| `MONITOR_GDRIVE_CLIENT_ID` | the OAuth client's id, from the deployment's own Google Cloud project |
| `MONITOR_GDRIVE_CLIENT_SECRET` | that client's secret |
| `MONITOR_GDRIVE_REFRESH_TOKEN` | a refresh token the account granted that client for the `drive.appdata` scope |

`drive.appdata` is a non-sensitive scope, so the client can be published without Google's
verification, and it opens only the application's own hidden folder: no file the account can
see. No read-only non-sensitive scope reads the quota, so a leaked token could write into
that folder — never read the account's files. How the three values are obtained once is
[install.md](../install.md#watching-google-drive).

## Measurements

No metric carries a label: one Google account per hub, one series per metric. Google's
figures are the account's `storageQuota.limit` and `storageQuota.usage`, the latter being
Drive, Gmail and Photos together — the usage the quota is enforced on.

| Metric | Value |
|---|---|
| `gdrive.free_bytes` | the limit less the usage, in whole bytes, and 0 when the usage exceeds the limit |
| `gdrive.free_pct` | 100 × `gdrive.free_bytes` ÷ the limit, rounded to two decimals half away from zero |

## Behaviour

One row = one test. Anchors: `spec: services.md#<heading>`.

### Startup

| Configuration | Result |
|---|---|
| a node of class `service` without `token_env`, enabling `gdrive` | the hub starts, and collects that node itself |
| a node of class `service` with `token_env` | startup error naming the node, and saying the class is reserved: the hub collects it, and a token would let an agent speak for it |
| a node of a class the file introduces, without `token_env` | startup error naming the node, as [hub-config.md](hub-config.md#startup) has it: only the compiled-in `service` class goes without a token |
| a node of class `service` enabling no sensor | startup error naming the node: it would never store a measurement, so it would fall silent for good; the compiled-in class alone enables none and is not refused |
| the `service` class or a node of it enabling a sensor the hub does not carry, such as `disk`, in a profile or by `enabled: true` | startup error naming the class or node and the sensor |
| any other class or node enabling `gdrive`, in a profile or by `enabled: true` | startup error naming the class or node: only the hub collects `gdrive` |
| a service node whose `silence_after` is shorter than twice the longest interval it runs plus three base ticks | startup error naming the node: one failed collection could make it fall silent, since each collection waits for a tick after its interval and a failing one may hold that tick for half a tick more |
| a top-level `sensors.<s>.enabled: true` | it reaches only the classes whose host can run `<s>`: an agent's sensor never reaches `service`, and `gdrive` reaches nothing else, so neither refuses a file that worked before |
| a service node enabling `gdrive` while one of the three `MONITOR_GDRIVE_*` variables is unset or empty | startup error naming the variable and no value |
| no node enabling `gdrive`, the three variables unset | the hub starts |

### Collection

| Situation | Seen |
|---|---|
| the hub starts with a service node enabling `gdrive` | `gdrive.free_bytes` and `gdrive.free_pct` appear under that node as soon as Google answers, without waiting a base tick |
| the hub first runs a service node whose service refuses it from the start | the node appears on `/debug` at once, and falls silent `silence_after` later |
| the hub restarted after being down longer than the service node's `silence_after`, Google answering within a minute | no silence message for the service node |
| the service node on `/debug` and in `/api/v1/state` | its agent version is the hub's own |
| `gdrive` fails once | nothing is asked of Google again until the sensor's interval has passed, as with an agent's sensor; neither the node nor its series go silent or stale |
| no collection has stored a measurement for longer than `silence_after` — the token revoked, Google unreachable | the service node falls silent: an instant critical message and a silent node under the timeline's "now" |
| `gdrive` answers again after that | the service node is reporting again, announced instantly |
| an ingest request naming the service node, with any node's token | refused with 403, as a node another token belongs to is ([ingest.md](ingest.md#authentication)) |
| a request to `/api/v1/ingest` or under `/api/v1/agent/` with `Authorization: Bearer ` and nothing after it | refused with 401: no token, and never the service node that has none |

### Google Drive

The sensor asks `GET https://www.googleapis.com/drive/v3/about?fields=storageQuota`. Google
writes each figure as a string of digits.

| Google answers | Collected |
|---|---|
| a limit of 107374182400 and a usage of 26843545600 | `gdrive.free_bytes` 80530636800, `gdrive.free_pct` 75 |
| a limit of 16106127360 and a usage of 5368709120 | `gdrive.free_bytes` 10737418240, `gdrive.free_pct` 66.67 |
| a usage equal to the limit | `gdrive.free_bytes` 0, `gdrive.free_pct` 0 |
| a usage above the limit — an account over its quota | `gdrive.free_bytes` 0, `gdrive.free_pct` 0 |
| no limit — an account with unlimited storage | no measurements and no error: nothing runs out, and the node falls silent, which is how an operator learns the sensor has nothing to watch there |
| a limit of 0; no usage; no `storageQuota`; a figure that is a JSON number, negative, beyond a 64-bit integer, or not digits | no measurements and an error the hub logs |
| a status other than 200 | no measurements and an error the hub logs, naming the status and Google's reason, such as `accessNotConfigured` for a project whose Drive API is off |
| Google unreachable, or not answering before the collection's deadline | no measurements and an error the hub logs naming the host, or the agent loop's own "no answer within" when its deadline is noticed first ([agent.md](agent.md#ticking)) |

### Authorization

A new access token is taken from the refresh token at every collection. Google does not
rotate refresh tokens, so nothing is ever written back. The log names the credential to
replace by its role; [install.md](../install.md#watching-google-drive) maps each to its
variable.

| Google's token endpoint answers | Logged |
|---|---|
| `invalid_grant` — the token expired, was revoked, or its app was left in Testing | the refresh token was refused and the account must be authorized again |
| `invalid_client` | the OAuth client's id or secret was refused |
| `unauthorized_client` | the refresh token was issued to another client than the one configured — the OAuth Playground's own, when its "use your own credentials" box was left clear |
| any other error | its OAuth error code |

In every case the collection stores nothing, and no credential — neither a value nor its
encoding in an `Authorization` header — appears in the log.

## Invariants

- No credential appears in a log line, an error or a response; errors name the credential's
  role or its variable, never its value.
- A service node's measurements reach storage through the checks an agent's do.
- A call to Google that has not answered by the collection's deadline is cancelled, not left
  running.
- The hub reaches nothing on a service node's behalf except the services its sensors read.

## Edge cases

- **A hub restarted**: the service node's first tick asks for its configuration in process
  and the second collects at once, so it reports without waiting a base tick.
- **A client left in Testing status**: Google expires its refresh tokens seven days after
  consent, which the node reports as silence and the log as `invalid_grant`; the install step
  publishes the client to production.
- **The refresh token unused for six months, or revoked**: the same. A new token in the
  environment takes effect when the hub restarts.
- **Two service nodes enabling `gdrive`**: both read the same account and hold the same
  numbers; nothing refuses it, and nothing needs it.

## Out of scope

- More than one Google account, and other services (Yandex Disk, Dropbox): each arrives when
  it is wanted, per [0039](../decisions/0039-the-hub-collects-a-service-node.md).
- Obtaining the refresh token: a one-time step in Google's own OAuth Playground
  ([install.md](../install.md#watching-google-drive)); the hub has no authorization flow.
- Rate limits: one request an hour is far below them, and a refusal is a status like any
  other.

## Open questions

None.
