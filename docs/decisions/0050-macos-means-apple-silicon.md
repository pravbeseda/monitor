# 0050. A macOS node is an Apple Silicon Mac; Intel Macs are not supported

- **Status:** accepted
- **Amends:** [0028](0028-agents-follow-a-target-the-hub-serves.md), whose consequence about
  an Intel Homebrew Mac no longer applies
- **Date:** 2026-10-29
- **Source:** [issue #17](https://github.com/pravbeseda/monitor/issues/17), closed as not
  planned

## Context

Every release built an agent for both Macs, `darwin-arm64` and `darwin-amd64`. On an Intel Mac
Homebrew gives `/usr/local` to an admin account, which can then replace the binary and the
kept install script that launchd runs as root
([issue #17](https://github.com/pravbeseda/monitor/issues/17)). Each fix the issue weighed has
a cost: refusing such a directory blocks every Intel Homebrew Mac, a private install path
breaks the one layout per system of [0019](0019-deployment-layout.md), and documenting the gap
leaves it open. The operator chose to drop Intel Macs rather than pay any of them.

## Decision

**macOS means Apple Silicon.**

- A release carries no `darwin-amd64` agent; the darwin agent is `darwin-arm64` alone.
- `monitor-install.sh` refuses on an Intel Mac before it asks for a binary, and says Intel Macs
  are not supported. A shell under Rosetta on an Apple Silicon Mac still gets `darwin-arm64`.
- Linux on `amd64` is not affected: the Debian layout keeps `/usr/local` root's.

## Consequences

- Issue #17 is closed rather than fixed, and the layout of [0019](0019-deployment-layout.md)
  stays one per system.
- A release has five binaries, not six ([release.md](../specs/release.md#what-a-release-contains)).
- An Intel Mac that already runs an agent stays on the last release that built one: its update
  timer finds no binary for its platform in a newer release, installs nothing and reports a
  failed run until the node is uninstalled.
- Supporting an Intel Mac again means a new ADR that answers issue #17 first.

## Alternatives

- **Install the binary somewhere the project owns on macOS** — what issue #17 leaned towards.
  Rejected: a second macOS layout and its tests, for a platform no node uses.
- **Refuse when the binary's directory is not root's** — rejected: it turns the same Intel Macs
  away with a worse message, and keeps building a binary nobody can install.
- **Keep the `darwin-amd64` asset and mark it unsupported** — rejected by the operator: the
  project would keep publishing a binary it does not stand behind, with the exposure intact.
