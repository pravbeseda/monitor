# 0005. POC stack: Go, SQLite, server-side HTML

- **Status:** accepted; amended by [0035](0035-mission-control-is-rendered-by-the-hub.md) —
  skins stay server-rendered in the hub until one needs a client
- **Date:** 2026-08-28
- **Source:** [POC](../poc.md)

## Context

The agent must install on macOS and Debian (Windows later) with no runtime and no package
dependencies. The hub serves a handful of nodes at minute intervals.

## Decision

- **Go** for both binaries: a single static binary, cross-compilation with one command, and
  `gopsutil` covering disks, CPU and memory on every target OS.
- **SQLite** for storage (one `measurements` table, WAL) behind a `Storage` interface, with
  the pure-Go `modernc.org/sqlite` driver so cross-compilation needs no C toolchain. WAL means
  backups use SQLite's own online backup (`.backup` / `VACUUM INTO`), never a file copy: the
  latest transactions sit in the `-wal` file and copying the database alone can lose them.
- **Server-side HTML** (`html/template`), one page, no SPA and no frontend build.
- **HTTPS + JSON**, version in the path from the first commit (`/api/v1/ingest`).
- **Deployment**: systemd on Debian, launchd on macOS. TLS terminates at the nginx already
  running on the host, and the hub binds to localhost, so it never faces the internet
  directly.

## Consequences

- Moving to Postgres or VictoriaMetrics is a new `Storage` implementation, not a hub
  rewrite. The interface must exist from the first commit.
- SPA and skins come after the POC, once the State API exists; the POC page is deliberately
  disposable.

## Alternatives

- **Node + TypeScript** — rejected, though it is the stack the author reads fluently: the
  agent would need a runtime on every node or a ~95 MB single-file build, and its quality
  tooling is assembled by hand from four projects rather than shipped with the language.
  The trade is only sound because [0011](0011-quality-gates.md) makes the checks mandatory,
  and because skins remain TypeScript over the State API.
- **Rust** — more time for the same result at this scale.
- **Python** — painful to distribute an agent to several machines.
- **The MySQL already running on the hub's host** — rejected: a single writer at tens of rows
  a minute shows none of its strengths, tests would need a live service, and a system that
  watches a server must not go blind when that server's database does. Its real advantages —
  existing backups and familiar tooling — cost one cron line and the hub's own web page.
- **Caddy for TLS** — dropped: the host already runs nginx with a certificate.
