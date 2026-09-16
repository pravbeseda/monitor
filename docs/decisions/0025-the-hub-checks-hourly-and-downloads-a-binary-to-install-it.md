# 0025. The hub checks hourly and downloads a binary only to install it

- **Status:** accepted
- **Amended by:** [0027](0027-the-hub-installer-reuses-the-binary-in-place.md), for point 3's
  test of when a binary is asked for, and the consequence that a hub never counting as running
  is downloaded again
- **Amends:** [0022](0022-updates-are-pulled.md) point 6 (an installer installs itself when
  the target is its own version, for the hub's installer), point 7 (the stub runs daily) and
  the consequence that every run downloads the newest release;
  [0024](0024-the-hub-follows-a-target-with-a-kept-install-script.md) point 4 (the answer
  contract), point 5 (the test of a release already in place), and the consequences naming
  the frozen options and the rollback floor
- **Date:** 2026-09-14
- **Source:** the operator's wish to see a merge on the hub within the hour, before the first
  kept script was placed on a host

## Context

Under [0024](0024-the-hub-follows-a-target-with-a-kept-install-script.md) the timer runs once
a day, and every run downloads the newest release's hub binary before its installer has said
whether it wants it — and, with a pinned target, the named release's binary as well. Run
hourly, that is some tens of megabytes an hour for a hub that is already where its target
says, most hours of most days.

The kept script cannot skip the download by comparing versions itself: it does not read the
target, so it cannot tell an up-to-date hub from one whose target was just pinned back, and
reading the target in the stub is what 0024 rejected. The hub host's provisioning had not
placed a kept script yet, so the contract could still change for nothing.

## Decision

1. **The timer runs hourly**, spread over ten minutes, catching up after the host was down.
2. **A follow hand-over carries no binary.** It passes `--digest`, the SHA-256 the signed
   manifest gives this release's hub binary, beside the four options of 0024.
3. **The installer decides whether a binary is needed.** A target resolving to its own release
   on a hub already running it — the binary in place has that digest and the layout's mode and
   owner, the service definition is the release's, and the service runs that binary — is
   nothing to do. A target resolving to its
   own release otherwise is answered with that release's own version, which asks for the
   binary. A target resolving elsewhere is answered with that version, as before.
4. **The kept script downloads a binary only when asked**, checks it against the manifest it
   already verified for that release, and hands over once more with `--binary`; an answer to
   that hand-over stops the run.

## Consequences

- An hour with nothing to change costs the redirect of `releases/latest`, a manifest, its
  signature and an installer archive — kilobytes — and one more of each while a target pins an
  older release. A hub that never counts as running — unconfigured, crash-looping, or stopped
  by hand — is downloaded and installed again every hour instead, and a configured one comes
  back up within the hour.
- A merge reaches the hub within the hour unless the target pins a version or the pull request
  carries `release:none`.
- The rollback floor rises to the first release whose installer takes `--digest`: v0.1.2's
  installer refuses it as an unknown option.
- A run may hand over three times — the newest release, the release its installer named, and
  that release again with its binary.

## Alternatives

- **The kept script compares the newest version with the installed one** — rejected: it cannot
  see the target, so a hub pinned back to an older release would be skipped whenever it still
  matched the newest, and a pinned hub would download the newest binary every hour.
- **The kept script reads the target** — rejected for the reason 0024 gives: the target's form
  belongs in the release.
- **The kept script reuses the installed binary when its digest matches the manifest** —
  rejected: it keeps the contract, but a target pinned below the newest release still downloads
  the newest binary every hour.
- **Keep the daily timer** — rejected by the operator: a merge should be visible within the
  hour.
