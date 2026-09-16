# 0027. The hub's installer reuses a binary in place that is already its release

- **Status:** accepted
- **Amends:** [0025](0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md) point 3
  (when the installer asks for a binary) and the consequence that a hub never counting as
  running is downloaded again at every run
- **Date:** 2026-09-16
- **Source:** the operator's wish to check for a release more often than hourly without a
  stopped or unconfigured hub downloading its binary at every check

## Context

Under [0025](0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md) a follow run
is nothing to do only when the hub is *running* its release. Any other state of a hub whose
binary is already that release — its configuration missing, its service stopped by hand or
crash-looping, a unit that differs, a run stopped before its restart — is answered by asking
for the binary, so the kept script downloads some twenty megabytes at every run to install
bytes that are already in place. The shorter the timer, the more that costs.

The digest `--digest` carries already tells whether the binary in place is the release's, and
the layout's mode and root ownership tell that nobody but root can change its bytes.

## Decision

1. **For a hub not already running its release, the installer asks for a binary only when the
   one in place is not the release's**: a different digest, a symlink or anything but a regular
   file, a mode or, on a real run, an owner that is not the layout's, or no binary.
2. **Otherwise the binary in place is kept.** The installer installs everything but the binary,
   as an install run does, and starts a configured hub. The answer stays empty.
3. The hand-over, the answer's meaning and the kept script are unchanged: a release carrying
   this installer is enough, and no host replaces its kept script.

## Consequences

- A run with nothing to download costs kilobytes whatever state the hub is in. The timer stays
  hourly: a host that wants a shorter period overrides the timer locally, and a shorter product
  default is a decision of its own.
- A hub stopped by hand is still started again at the next run; stopping its timer first is
  still what keeps it stopped.
- A binary with the release's digest but a mode or an owner that is not the layout's is still
  downloaded again.
- A release whose installer predates this keeps 0025's behaviour while a target pins it.

## Alternatives

- **The kept script caches the binaries it downloaded** — rejected: it keeps state on the host
  that has to be pruned, changes a kept script every host would have to replace, and saves
  nothing the installer's own check does not once a binary is in place.
- **The kept script compares the installed version with the target** — rejected for the
  reasons 0024 and 0025 give: the target's form belongs in the release.
- **Keep the binary only in a directory no other account may write** — rejected: a downloaded
  binary is installed into that same directory and restarted with the same window between, so
  the condition would cost a download and protect nothing.
- **Copy a binary in place with a loosened mode and check the copy's digest** — rejected: it
  saves a download in a state only a hand-made change produces, at the cost of a second
  install path.
