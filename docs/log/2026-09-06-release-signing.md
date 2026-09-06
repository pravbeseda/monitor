# Release signing: what the first release pipeline rejected

Written while building [issue #16](https://github.com/pravbeseda/monitor/issues/16), the
prerequisite [ADR 0022](../decisions/0022-updates-are-pulled.md) names. The decision itself
is [specs/release.md](../specs/release.md); this note keeps the options that lost, so the
question is not re-opened from scratch.

## Signing tool

ADR 0022 left the tool to the spec and ruled out publishing checksums alone. Three
candidates:

- **`openssl dgst`, ECDSA P-256** — chosen. `openssl` is the one verifier already present on
  Debian and on macOS, so the frozen half of the future updater needs nothing installed.
  Signing and verifying round-trip between OpenSSL 3 and LibreSSL, which is what macOS ships
  as `/usr/bin/openssl`.
- **minisign** — cleaner tooling and shorter keys, rejected because every node would need
  the binary installed, which is a dependency in exactly the half ADR 0022 keeps frozen.
- **cosign, keyless through Sigstore** — no long-lived private key at all, rejected as too
  much machinery for two nodes: a `cosign` binary on each node and a network path to a trust
  root, to replace a key in a password manager.

## Signing each binary, rejected

The obvious shape — a `.sig` beside every asset — signs bytes and nothing else. A genuine
`monitor-agent-1.0.0-linux-amd64` renamed to `…-1.9.0-…` keeps a signature that verifies, and
the updater of ADR 0022 point 6 picks assets by name, so it would install a version nobody
asked for. Signing one `SHA256SUMS` manifest puts every name and digest inside the signed
bytes, and leaves one thing to check rather than six.

## Where the key lives

A repository secret was the first draft. Rejected: a tag push would then be authority to
sign, and a tag can name any commit in the repository, including a fork's pull-request head.
The key is an *environment* secret, the environment carries required reviewers and a tag
policy, and the run refuses a commit that is not on `main`. None of that is in a file — it is
repository settings — which is why the spec says so out loud.

## Smaller ones

- **Archives rather than raw binaries** — deferred, not rejected: what installs a binary
  today takes a path to one. The release that carries an installer (ADR 0022 point 6) can
  decide differently without breaking these.
- **An offline copy of the private key** — deliberately not kept. While only releases trust
  the key, losing it costs a rotation and nothing else; the copy earns its keeping when a
  node carries the public half and cannot be rotated remotely, which is the updater's
  problem to solve before it ships.
- **Trying every superseded public key when verifying** — rejected as speculative until a
  rotation actually happens; the failure message names the key it used, which is what makes
  a rotation diagnosable.
- **A `--version` flag** — added, not deferred: without it a downloaded binary cannot be
  asked which version it is, and the row saying a release reports its tag would be a claim
  no one can check.
