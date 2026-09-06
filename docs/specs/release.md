# Spec: Release

- **Status:** approved
- **Owns:** `.github/workflows/release.yml`, `deploy/verify-release.sh`,
  `deploy/tag-version.sh`, `deploy/release-signing-key.pub`, and the version string both
  binaries report — its use on
  the wire stays [ingest.md](ingest.md)
- **Decisions:** [0005](../decisions/0005-poc-stack.md),
  [0007](../decisions/0007-public-repository.md),
  [0011](../decisions/0011-quality-gates.md),
  [0014](../decisions/0014-macos-available-space.md),
  [0021](../decisions/0021-shell-is-linted-too.md),
  [0022](../decisions/0022-updates-are-pulled.md)

## Purpose

A tag turns a commit into binaries a machine can install without a Go toolchain, and into a
signature that says which binaries this repository released under that version. This spec
owns what a release contains, when one appears, and how anyone checks an artifact against
that signature.

What the signature defends is transport: a mirror, a proxy, a corrupted download, an asset
swapped for another. It says the release was assembled by whoever holds the private key; it
is not a defence against that key or the account holding it being taken
([0007](../decisions/0007-public-repository.md), rule 5).

A release carries the installer that installs it ([installer.md](installer.md) owns what is
inside that archive). The releases published before it did not, and they are the rollback
floor [0022](../decisions/0022-updates-are-pulled.md) names: a run pointed at one installs
nothing and says so — naming it is a stop, not a rollback.

Verification today is done from a checkout of this repository, which is where the verifier
and the public key live. The updater's resident half will carry its own copy of that key
([0022](../decisions/0022-updates-are-pulled.md), point 2) and verify without a checkout.

## What a release contains

The version is the tag without its leading `v`. Six binaries and the installer archive, one
manifest listing all seven, and one signature over that manifest — nine files.

| Asset | Why |
|---|---|
| `monitor-agent-<version>-linux-amd64` | Debian nodes |
| `monitor-agent-<version>-linux-arm64` | Debian nodes |
| `monitor-agent-<version>-darwin-arm64` | Apple Silicon nodes |
| `monitor-agent-<version>-darwin-amd64` | Intel nodes |
| `monitor-hub-<version>-linux-amd64` | the hub host |
| `monitor-hub-<version>-linux-arm64` | the hub host |
| `monitor-installer-<version>.tar.gz` | the installer that release installs itself with ([installer.md](installer.md)) |
| `SHA256SUMS` | one `<sha256>  <asset name>` line per asset above, in `sha256sum` format |
| `SHA256SUMS.sig` | the signature over that manifest |

The darwin agents are built on macOS because the disk sensor is cgo there
([0014](../decisions/0014-macos-available-space.md)); every other asset is static. The hub is
a Debian service only ([0005](../decisions/0005-poc-stack.md)), so no darwin hub is built.
The hub is not in [issue #16](https://github.com/pravbeseda/monitor/issues/16)'s list and is
here because [0022](../decisions/0022-updates-are-pulled.md) point 5 upgrades it the same way.

The binaries are published raw rather than archived, because what installs one takes a path
to a file; the installer is a `tar.gz` because it is a directory of scripts and service
definitions.

### Why the manifest is what is signed

A signature over a binary covers its bytes and nothing else — not its name, not its version,
not the release it hangs from. A genuine `monitor-agent-1.0.0-linux-amd64` renamed to
`monitor-agent-1.9.0-linux-amd64` would carry a signature that still verifies, and an
updater picking assets by name ([0022](../decisions/0022-updates-are-pulled.md), point 6)
would install a version it was not pointed at. Signing the manifest puts every name and
every digest inside the signed bytes, so an asset is genuine only under the name this
release gave it. One signature also means one thing to check, on a machine whose verifier is
frozen.

### The key

**A signature is ECDSA P-256 over SHA-256**, made and verified by `openssl dgst`, which is
the one verifier present out of the box on both Debian and macOS — what a frozen updater
needs ([0022](../decisions/0022-updates-are-pulled.md), point 2). The public half lives in
the repository as `deploy/release-signing-key.pub`; the suffix is deliberate, since the
privacy hook refuses a staged `.pem` or `.key`
([0007](../decisions/0007-public-repository.md)).

The secret is the PEM itself, named `RELEASE_SIGNING_KEY`, in a GitHub environment called
`release` — not a repository secret. That environment carries a deployment policy limiting
it to `v*` tags, and without one the guarantee is not there: an environment secret with no
policy is reachable from any ref that names the environment, so anyone who can push a branch
could read the key. Half of this decision therefore lives in repository settings rather than
in a file, which is why the commands that make it are written here.

The environment and its policy are made once:

```sh
gh api --method PUT repos/pravbeseda/monitor/environments/release \
    -F "deployment_branch_policy[protected_branches]=false" \
    -F "deployment_branch_policy[custom_branch_policies]=true"
gh api --method POST repos/pravbeseda/monitor/environments/release/deployment-branch-policies \
    -f name='v*' -f type=tag
```

Required reviewers on top of that policy are available and deliberately not set: they would
make every release wait for a click. So the guarantee is exactly this and no more — the key
is unreachable from a branch, and what signs is a `v*` tag on a commit the run finds on
`main`. A credential that can push such a tag can therefore sign; closing that is what
required reviewers would be for, and turning them on is a settings change, not a code
change.

### Generating and rotating the key

These three commands make the first key and every replacement — a rotation is a new pair, not
a repair. Since [installer.md](installer.md) landed, two more copies follow the public half:
the one inside `deploy/monitor-install.sh` and the fingerprint `install.md` publishes, both
held in step by tests.

```sh
openssl ecparam -name prime256v1 -genkey -noout -out ~/release-signing-key.priv
openssl ec -in ~/release-signing-key.priv -pubout -out deploy/release-signing-key.pub
gh secret set RELEASE_SIGNING_KEY --env release < ~/release-signing-key.priv
```

Then commit the new `deploy/release-signing-key.pub` and delete `~/release-signing-key.priv`:
a run signs with the secret and verifies with the committed public half, so a mismatch
between the two fails the run rather than publishing something no one can verify. Rotating
is what answers a lost or leaked key, and a release published under the old key stays
verifiable only with the `.pub` committed beside it at that tag.

**There is no second copy of the private half.** The secret in that environment is the only
one, and GitHub cannot read a secret back. That is a deliberate choice for as long as
releases are the only thing trusting the key: losing it costs a rotation — a new pair, a new
`.pub` committed, a new secret — and only already-published releases become unverifiable.
It stops being cheap when a machine starts carrying the public half in a part no release can
replace, because a rotation is then hands on every machine. The installer of
[installer.md](installer.md) deliberately leaves nothing on a machine, so it is not that
moment; the timer that makes it resident is, and where an offline copy lives has to be
answered before that unit of work, not after.

The shell this adds — `verify-release.sh` and `tag-version.sh` — is POSIX `sh` and stands
under the same lint gate as the rest of the shell this project ships
([0021](../decisions/0021-shell-is-linted-too.md)).

## Behaviour

One row = one test. Anchors: `spec: release.md#<heading>`. The publishing rows are the
exception the project's own rule allows for infrastructure: they describe what a run does on
GitHub and are proved by a run. The one piece of that with logic of its own — the tag
grammar — is `deploy/tag-version.sh`, and it is tested here like anything else.

### Publishing

| Event | Outcome |
|---|---|
| tag `v1.2.3` pushed | a release named `v1.2.3` appears, carrying the six binaries, the installer archive, `SHA256SUMS` and `SHA256SUMS.sig` |
| tag `v1.2.3` pushed | each binary is named `monitor-<command>-1.2.3-<os>-<arch>`, the archive `monitor-installer-1.2.3.tar.gz`, and the manifest lists exactly those seven names |
| tag `v1.2.3` pushed, older releases present | a client asking the repository for its latest release gets the highest version published, which is `v1.2.3` |
| a run for `v1.2.3` still in progress | no release for that tag is visible to such a client until all nine files are attached |
| two tags pushed together, their runs finishing in either order | the higher version is the latest release, whichever run published last |
| tag `v1.2` or `v1.2.3-rc1` pushed | no release; the run fails, naming the tag it refused |
| tag `1.2.3` pushed, without the leading `v` | no run and no release |
| a tag on a commit that is not on `main` | no release; the run fails, naming the commit |
| a tag on a commit that does not compile for one of the six binaries | no release, and no asset from the targets that did build |
| a tag on a commit whose tests fail on either operating system | no release |
| a tag deleted, re-created on another commit and pushed, its release already published | the published release keeps every asset it had; the run fails |
| a draft left by an earlier run that died before publishing | it is deleted, and this run publishes its own |
| the committed public key stops matching the signing secret | no release: the run verifies its own manifest with the committed key before publishing anything |

### The version a binary reports

| Given | Outcome |
|---|---|
| a binary from release `v1.2.3`, asked with `--version` | it answers `monitor-<command> 1.2.3`, before it needs any configuration |
| a binary built from a checkout | it reports the development default, and releasing edits no source file |

### Verifying an artifact

`deploy/verify-release.sh [--key <path>] [--sums <path>] <artifact>` — the manifest defaults
to `SHA256SUMS` beside the artifact, its signature to that name plus `.sig`, and the key to
`release-signing-key.pub` beside the script. **The exit status is the verdict**; what is
printed is diagnostic and not part of the contract, and a failure that reached the signature
or the digest names the key and the manifest it used.

| Given | Outcome |
|---|---|
| an asset, manifest and signature as published, no options | exits 0, saying the artifact matches the release |
| the same, with `--key` and `--sums` naming those same files | exits 0 |
| an asset changed by one byte | exits non-zero, saying its digest is not the one the release named |
| an asset renamed to another asset's name | exits non-zero |
| an artifact the manifest does not list | exits non-zero, naming it |
| a manifest changed after signing | exits non-zero, saying the signature does not verify |
| a manifest signed by another key | exits non-zero |
| no manifest, or no signature beside it | exits non-zero, naming the file it wanted |
| a signature file that is empty or not a signature | exits non-zero |
| `--key` naming a path that does not exist | exits non-zero, naming the path |
| `--key` naming a file that is not a key at all | exits non-zero |
| more than one artifact named, or none, or a directory | exits non-zero, printing the usage |
| no `openssl` on `PATH` | exits non-zero, naming `openssl` |
| `-h` | prints the usage on stdout and exits 0 |

The verdict comes from `openssl`'s exit status and never from its output: LibreSSL, which is
what `/usr/bin/openssl` is on macOS, prints `Verified OK` on runs that fail.

## Invariants

- A release is published whole or not at all: no asset reaches a visible release whose
  siblings failed to build, test, sign or verify.
- Every binary in a release is listed in the manifest that release's signature covers.
- The private key never leaves the release environment's secrets: no artifact carries it, no
  job that does not sign ever sees it, it is written outside the workspace and removed
  whatever the run's outcome, and the only actions running beside it are `actions/checkout`
  and `actions/download-artifact`, pinned by commit.
- The version a published binary reports equals the tag without its `v`, and no source edit
  is part of releasing.
- An existing *published* release is never rewritten by a run: a fix is a new tag. A
  draft, which owns no tag and which no client can see, is a run's to discard.
- A release carries no configuration, node name, host or secret
  ([0007](../decisions/0007-public-repository.md)).
- A release carries the six binaries, the installer archive, a manifest and a signature, and
  nothing else.
- The archive is packed reproducibly: the same commit gives the same bytes, so a digest that
  changed means the contents did.

## Edge cases

- **A tag on a commit that is not on `main`** is refused. A tag is what makes the project
  sign, so what it may name is narrower than what a branch may hold; releasing a hotfix means
  merging it first. The run still tests what it builds:
  [0011](../decisions/0011-quality-gates.md) guards the boundary of `main`, and a green
  boundary is not a promise that today's `main` is green.
- **The signing key is missing from the environment.** The run fails and publishes nothing,
  which to an observer is a failed run like any other: both leave no release.
- **Key rotation** is the three commands under [Generating and rotating the
  key](#generating-and-rotating-the-key); nothing else recovers a lost or leaked key.
  Rotating the copy a stub carries, once one exists, is hands on every machine over the
  manual path of [install.md](../install.md) — the updater's spec owns that.
- **Verify before renaming.** An installation renames the binary to `monitor-agent`, and the
  manifest names the asset. Verification belongs to the downloaded file, under the name it
  was downloaded with.
- **A prerelease.** There is no channel: the grammar is `MAJOR.MINOR.PATCH` and nothing else.
- **Mutable tags and assets.** Nothing in a workflow can stop a write-scoped credential from
  moving a tag or replacing an asset; the invariants above bind runs, not people. A ruleset
  on `refs/tags/v*` that blocks deletion and force-pushes is what binds people, and it is a
  repository setting rather than a file in the tree.

## Out of scope

- The timer, the resident half it needs and the hub's target version — the rest of
  [0022](../decisions/0022-updates-are-pulled.md), each its own unit of work. What is inside
  the installer archive, and what installing does, is [installer.md](installer.md)'s.
- Installing an artifact once it is downloaded: [deployment.md](deployment.md) owns what an
  installation looks like, and [install.md](../install.md) the operator's steps for
  downloading, verifying and installing one.
- Distribution through apt or Homebrew, rejected for now by
  [0022](../decisions/0022-updates-are-pulled.md).

## Open questions

None.
