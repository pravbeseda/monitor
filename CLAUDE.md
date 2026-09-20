# Monitor

A personal control panel for life: metrics from infrastructure, finance and health flow
into one semantic core that computes their *meaning* (health 0–100, status, trend, anomaly
rank, freshness) and is rendered by interchangeable skins.

## Commands

```
go test -race -cover ./...   # tests
gofmt -l . && go vet ./...   # the fast pair, also run by the pre-commit hook
golangci-lint run            # the full linter set, also run by CI

MONITOR_TOKEN_LAPTOP_A=<token> go run ./cmd/hub --config config.yaml --db monitor.db
MONITOR_TOKEN=<token> go run ./cmd/agent --hub http://127.0.0.1:8080 --node laptop-a
```

**Where the project stands is not written here.** The work plan in
[docs/poc.md](docs/poc.md) owns that, together with the checklist in the open pull request;
a second copy of it in this file is a copy that goes stale.

## Start here

**Read `docs/index.md` first.** It maps every document. The architecture is recorded as
ADRs in `docs/decisions/` — each one states what was rejected and why, so read the ADR
before re-opening a settled question.

```
docs/index.md              map of all documentation — the entry point
docs/concept.md            product concept, principles, roadmap
docs/poc.md                POC spec, wire format, work plan, answered questions
docs/decisions/NNNN-*.md   ADRs, one decision per file (TEMPLATE.md is the skeleton)
docs/specs/<subsystem>.md  behaviour specs: the table each test derives from
docs/plans/                closed to new files (ADR 0017); the stage 1 record only
docs/log/YYYY-MM-DD-*.md   design notes: reasoning that did not fit the final documents
```

Documents are cross-linked with **relative Markdown links** (`[poc.md]` + `(../poc.md)`), never
Obsidian `[[wikilinks]]`: relative paths resolve identically in Obsidian, on the forge, and
for an agent reading files directly.

## This repository is public — hard rules

Per [ADR 0007](docs/decisions/0007-public-repository.md), the repository is public from the
first commit and contains **tooling only**. Configuration, secrets and measurements live on
the server. Publishing history is irreversible, so these rules bind every commit:

1. **Never commit** real node names, real domains or addresses, thresholds tied to a named
   host or volume, exported health or finance data, fixtures built from real exports,
   database dumps, screenshots of a live dashboard, or log excerpts containing mount points
   or node names. The same rule covers commit messages, issues and pull requests.
2. **Product defaults may ship; deployment settings may not.** Thresholds, intervals and
   sensor profiles describe how the tool behaves out of the box and belong in the repository.
   Hub address, tokens, node names and classes, chat ids and any host- or volume-specific
   override have no defaults: missing configuration is a clear startup error, never a
   "reasonable value". The test is whether the value says something about *an installation*.
3. **Examples and fixtures are synthetic** — invented names (`laptop-a`, `server-b`), round
   numbers, generated data. Never a real export, not even temporarily.
4. **Secrets never enter a file in the tree**, including example configs: they come from an
   environment file or the OS keychain.
5. **Security rests on secrets, not obscurity**: long random per-node tokens, rate limiting
   on ingest, authentication on the web page before the hub gets a public address.
6. If a sensor's own code would disclose a private fact (one specific broker, one specific
   account), it belongs in the private configuration repository as an external sensor —
   see [ADR 0003](docs/decisions/0003-sensors-are-modules.md).

When unsure whether something is personal, leave it out and say so in the summary.

**The scanner enforces this, not attention.** `.githooks/pre-commit` refuses a commit that
stages configuration, secrets or a database, and runs `gitleaks` over the staged diff. Hooks
are versioned, so a fresh clone must enable them once:

```
git config core.hooksPath .githooks   # required after cloning
brew install gitleaks                 # macOS; see the gitleaks README on Debian
brew install go golangci-lint         # the toolchain the hook and CI run
```

## Language

Per [ADR 0008](docs/decisions/0008-english-repo-bilingual-ui.md):

- **The repository is English** — code, identifiers, comments, documentation, config keys,
  metric ids, commit messages, CLI output, issue and PR text. No exceptions.
- **The interface is bilingual (en/ru)** from the first user-facing string: web, Telegram
  messages, alerts, digests, user-facing errors. No user-facing string is hard-coded at its
  usage site; strings come from a catalogue keyed by an English identifier, and locale
  governs number, date and byte-size formatting too.
- Log lines and CLI output stay English: they are diagnostic and end up in bug reports.

## Development process (mandatory)

Per [ADR 0009](docs/decisions/0009-development-process.md), amended by
[ADR 0017](docs/decisions/0017-one-spec-and-decision-gates.md). Every unit of work runs
through these stages in order. Only three things stop and wait for the user: a question of
the three kinds below, opening a pull request, and merging it. Code never starts before the
behaviour it implements exists as a table, but that is my constraint, not a stop. Everything
else is decided, done and reported.

1. **Scope.** Restate the task and name the assumptions. Ask only what qualifies as a
   question for the user; decide the rest and say what was decided.
2. **Spec.** Write or update `docs/specs/<subsystem>.md` when the work spans more than one
   session, touches a contract, or is an algorithm with state. Skip it, saying so, when
   none of those holds. The behaviour table describes only what an observer can see: a row
   naming a column, a package, a goroutine, a timeout or a database flag is a test, not a
   spec. Independent subagents review the table before any code is written; their findings
   are fixed, not forwarded.
3. **Checklist.** The steps live in the task and in the pull request body. No new documents
   in `docs/plans/`, and none in `.github/tasks`: in this repository that supersedes the
   global rule to save a requested plan as a file.
4. **Branch.** Work on a branch off `main`. Never commit to `main`.
5. **Implement, step by step, test-first.** Red → green → refactor per step. Every row of a
   behaviour table the step touches gets a test citing it with a `spec:` anchor; work with
   no spec has no rows and no anchors.
6. **Self-review.** Independent subagents with distinct lenses review the diff, iterated
   until the findings converge. Fix what they find. The user sees the residue only: what was
   deliberately not fixed and why, and anything that turned out to be a question for them.
7. **Documentation.** Update the spec, ADRs and `docs/index.md`; report which documents
   were touched.
8. **Pull request.** → **gate:** never opened without explicit permission for that PR.
9. **Merge.** → **gate:** only on the user's explicit instruction.

**A question reaches the user only when** one of these holds:

1. the choice shapes what the product becomes;
2. two good options, and taste decides rather than argument;
3. a contradiction was found in something the user already decided.

This narrows the global "stop and ask when something is unclear": what I can settle by
reading the code, the specs and the ADRs, I settle and report.

An arithmetic slip, a naming choice, a test's shape, a self-review finding with an obvious
fix, the order of the work — these are decided and reported, never asked. A review comment a
person left on a pull request is not a self-review finding: those are still discussed before
they are implemented.

**Progress is reported as working software**, not as steps completed: what now works and
how to see it, not "step 4 of 14 is done". The per-step report the global rules ask for is
one line naming what now works.

**Definition of done** for any unit of work: tests pass, every behaviour-table row that the
change touches has a test, the spec matches the code, the documentation was updated, and
the multi-agent self-review was run and its findings resolved.

### Holding the line

The user has asked to be kept on this process. Three things are worth saying out loud when
they are about to be skipped, in one or two sentences each: code written before the
behaviour it implements exists as a table, a commit on `main`, and a pull request or a
merge without permission. Say it once per occurrence; a repeated objection the user has
overruled is noise, not diligence. If the user confirms anyway, do it and record the skip
in the pull request, so it is visible rather than forgotten.

## Documentation duties (mandatory)

Documentation is part of the work, not a follow-up task:

1. **Architectural decision taken → write an ADR** from `TEMPLATE.md` with the next free
   number and add its row to `docs/index.md`. A decision is architectural if reversing it
   later would mean rewriting code rather than editing it.
2. **Open question answered → update the document that poses it** in the same session, and
   promote the answer to an ADR when it is architectural.
3. **New document created → add it to `docs/index.md`.** An unlisted document does not exist.
4. **Behaviour changed → update the document that describes it.** Never leave code and
   documentation contradicting each other; if both cannot be fixed now, say so explicitly.
5. **Substantial discussion → leave a note in `docs/log/`** capturing what was rejected and
   why. That reasoning is lost otherwise when the session ends.

Keep documents lean: no duplication between them, no filler. An ADR is the single source of
truth for its decision — other documents link to it instead of restating the reasoning. An
accepted ADR keeps its number and its reasoning; the only edits it takes are its status
line, a pointer at the ADR that supersedes or amends it, and the correction of a factual
slip in an illustration. A reversal is a new ADR, never a rewrite.

Report at the end of a task which documents were touched.

## Architecture in one screen

Each line is an index into the ADR that owns it.

- **Two binaries, one monorepo**: `cmd/agent` per node, `cmd/hub` as a monolith (ingest +
  storage + evaluation + web + notifier), shared code in `internal/...`. → 0004
- **Push, not pull**: agents POST to `/api/v1/ingest` over HTTPS with a per-node token; node
  silence is an expected state configured per node class (`silence_after`). → 0002
- **Sensors are in-process modules** (`Sensor: collect() -> []Measurement`); the interface
  must not reveal whether an implementation is built in or external. → 0003
- **A metric costs no code**: a measurement declares itself, its id carries its unit, and
  what a series is judged by — a direction and up to two values — is set on its page and
  stored with the data. What an agent collects and how often stays configuration. → 0032, 0033
- **The semantic core is the single source of truth**; the State API carries semantics only,
  never skin-specific fields. Skins are equal consumers of it, and drill-down, `at=` time
  travel and the event stream live outside them. → 0001
- **Alerts fire on transitions** with hysteresis; entering or leaving critical is instant,
  everything else batches into a daily digest, unresolved critical repeats at most once a
  day. → 0006, 0016
- **Evaluation runs on the hub's own tick**, never inside an ingest request: one writer of
  the event log, and silence, digest and repeat are the same pass. → 0015
- **Hysteresis is relative**: recovery is the entry comparison negated and shifted by 20%
  of the threshold's magnitude, whatever the unit. → 0013, 0033
- **Stack**: Go for both binaries, SQLite (`modernc.org/sqlite`, no CGO) behind a `Storage`
  interface, server-side `html/template`, systemd/launchd, TLS terminated by the host's
  nginx with the hub bound to localhost. → 0005
- **The agent carries no configuration** beyond the hub address and its token: which sensors
  run and how often comes from the hub in the ingest response, layered sensor default →
  node class → node → sensor. Thresholds are not in it at all. → 0010, 0032
- **Quality is machine-enforced**: CI runs `gofmt`, `go vet`, `golangci-lint`,
  `go test -race -cover` and `shellcheck` over the shell the project ships, and a red check
  blocks the merge; the pre-commit hook runs the fast pair (`gofmt`, `go vet`). → 0011, 0021
- **A subject is a series and a threshold is one comparison per level**: a direction
  (`below`/`above`) and up to two values, set per series in the interface and stored with
  the data; nothing has a level until someone sets one. → 0032, 0033
- The versioned API prefix (`/api/v1/...`) is deliberate — keep it on every new endpoint.
- The project's value is the normalization and prioritization layer, not storage or
  charting; weigh new low-level work against what off-the-shelf tools already do.
