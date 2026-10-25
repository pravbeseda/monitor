# 0047. A pull request opens without a gate; only the merge waits

- **Status:** accepted
- **Date:** 2026-10-25
- **Source:** the disk sensor fix of 2026-10-25, held back after it was done because this
  repository's instructions and the user's global ones disagreed
- **Amends:** the consequence of [0017](0017-one-spec-and-decision-gates.md) that names
  opening a pull request among the things that stop and wait

## Context

[0017](0017-one-spec-and-decision-gates.md) kept three stops: a question for the user,
opening a pull request, and merging it. The user's global instructions, which apply to every
repository, say the opposite about the second: commit, push and open the pull request once
the checks pass and the self-review is through, then run the two-model review on it.

The two rules met on a finished fix: the work was committed and pushed, and then stopped to
ask permission for a step that adds nothing to what the pushed branch already made public
but a title and a body, which [0007](0007-public-repository.md) already binds. Opening a
pull request lands nothing; the merge is what does, and it keeps its gate.

## Decision

**Opening a pull request is a stage, not a gate.** Once the definition of done holds, the
pull request is opened without asking and reviewed straight away. Two things stop
and wait for the user: a question of the three kinds in
[0017](0017-one-spec-and-decision-gates.md), and the merge.

## Consequences

- A finished unit of work reaches the user as an open, reviewed pull request, not as a
  question about opening one.
- The merge is unchanged: only on the user's explicit instruction.
- A pull request opened on work that is not done is the defect, not the opening itself: the
  definition of done is what now guards it.

## Alternatives

- **Keep the gate** — rejected: it contradicts the global rule on every task, and it guards a
  step that adds only a title and a body to what the pushed branch already made public.
- **Open the pull request as a draft and ask before marking it ready** — rejected: it moves
  the same question one click later and still stops a finished task.
