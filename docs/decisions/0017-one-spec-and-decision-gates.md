# 0017. One document per unit of work; gates on decisions, not documents

- **Status:** accepted; opening a pull request is no longer a gate, per
  [0047](0047-a-pull-request-opens-without-a-gate.md)
- **Date:** 2026-08-29
- **Source:** stage 2 of the POC, where the process cost more attention than the work
- **Amends:** the stages and gates of [0009](0009-development-process.md); its purpose —
  behaviour written before code, decisions recorded as ADRs — stands

## Context

[0009](0009-development-process.md) put a gate on each artefact: the spec, then the plan.
Stage 2 showed what that produces. The spec and the plan described the same work at two
altitudes, so the plan became a table of contents for the spec, both had to be reviewed,
and a change to either could now contradict the other and the code — three places to keep
in sync where there had been one.

The spec also drifted into implementation. Columns, timeouts, goroutine ordering and DSN
flags are not what an observer sees; once written into a document they need maintaining
like code, without being executable like code.

The gates made it worse. Because approval attached to documents rather than to decisions,
questions with obvious answers — may an arithmetic slip in an accepted ADR be corrected? —
were escalated at the same weight as questions that shape the product.

## Decision

**One document per unit of work: the behaviour spec.** The plan is a checklist in the task
or in the pull request body. No further documents are added to `docs/plans/`; what already
merged there stays as a record of what was done, and the unmerged stage 2 plan goes with
this change.

**A behaviour table describes only what an observer can see.** The filter: if a row names a
column, a package, a goroutine, a timeout or a database flag, it is a test, not a spec.
Implementation constraints live in the checklist and in the tests.

**Gates are on decisions, not documents.** Three kinds of question reach the user:

1. the choice shapes what the product becomes;
2. two good options, and taste decides rather than argument;
3. the assistant found a contradiction in what the user already decided.

Everything else — the spec, the checklist, the implementation, the reviews — the assistant
decides, records and reports afterwards.

**Review is the assistant's work, not the user's.** Every spec and every implementation is
reviewed by independent subagents with distinct lenses, iterated until findings converge,
*before* the result is shown. What reaches the user is the residue: findings deliberately
not fixed and why, and anything that turned out to be a product decision.

**Progress is reported as working software**, not as steps completed.

## Consequences

- The stages of [0009](0009-development-process.md) still run in order, but only three
  things stop and wait: a question of the three kinds above, opening a pull request, and
  merging it.
- The definition of done is unchanged — tests pass, every behaviour row has a test, the
  spec matches the code, documentation updated, self-review run — but the self-review is
  now explicitly a multi-agent pass rather than a reading.
- `docs/plans/` is closed to new documents; `docs/index.md` keeps the one that merged.
- A spec that grows implementation detail is a defect to fix in the spec, the same way a
  stale document is.

## Alternatives

- **Keep 0009 unchanged** — rejected: it is what produced two review surfaces and a plan
  that duplicated a spec.
- **Drop behaviour specs and let the tests be the specification** — rejected: the pre-code
  review of stage 2 found six real defects in the rules, including a silence state that
  could never recover and a retry that could never fire. Those were found because the
  behaviour existed as a table before it existed as code.
- **Keep both documents but review only one** — rejected: the unreviewed one is the one
  that goes stale, and a stale plan is what the checklist-in-the-PR removes entirely.
