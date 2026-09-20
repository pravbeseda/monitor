# 0007. Public repository from the first commit; everything personal stays out

- **Status:** accepted; amended by [0032](0032-thresholds-are-set-in-the-interface.md)
- **Date:** 2026-08-28
- **Source:** [concept](../concept.md)

*Rule 1 below lists thresholds among the product defaults that may ship. Under
[0032](0032-thresholds-are-set-in-the-interface.md) a threshold belongs to one series and is
set by whoever runs the installation, so no threshold default ships; everything else here
stands.*

## Context

Monitor handles health, finance and infrastructure data. The project is nevertheless meant
to be open source, like the tools it sits next to (`node_exporter`, Home Assistant,
Grafana) — public tooling, private configuration.

Two risks are easy to confuse. Leaked **secrets** are loud and can be rotated in a minute.
Leaked **personal data** is quiet and cannot be recalled. The second is the greater risk
here, and it does not arrive through configuration files — it arrives through metric
schemas, test fixtures, screenshots, logs pasted into issues and commit messages.

Publishing a repository publishes its entire history, not its current state. Cleaning up
afterwards means rewriting history, rotating every secret it ever contained, and living
with forks and caches that keep the original.

## Decision

The repository is public from the first commit, so the discipline is never retrofitted.
Four levels, and only the first one is versioned here:

| Level | Contents | Where it lives |
|---|---|---|
| Tooling | agent, hub, evaluation engine, skins, general-purpose sensors | **this public repository** |
| Configuration | node names and classes, domains, chat ids, per-host and per-volume overrides, skin mappings | on the server; a private repository once it outgrows a few files |
| Secrets | node tokens, bot token, TLS material | environment file (mode 600) or OS keychain, never in configuration files |
| Measurements | the SQLite database | on the server only, never beside a checkout |

**Never committed:** real node names, real domains or addresses, thresholds tied to a named
host or volume, exported health or finance data, fixtures built from real exports, database
dumps, screenshots of a live dashboard, log excerpts containing mount points or node names.

**Rules that make the split hold:**

1. **Product defaults are public; deployment settings are mandatory.** Two kinds of value
   are easy to confuse, and only one of them may live in code:

   - **Product defaults** — thresholds, intervals, the filesystem-type allow-list, sensor
     profiles. They describe how the tool behaves out of the box, disclose nothing about any
     particular installation, and belong in the repository. `node_exporter` shipping a
     default metric set is the same thing.
   - **Deployment settings** — hub address, node tokens, node names and classes, chat ids,
     and any override aimed at a specific host or volume. These have **no defaults at all**:
     missing configuration is a clear startup error, never a "reasonable value", because a
     default here is how one person's setup leaks into the tool.

   The test is whether the value says something about *an installation*. "Warn below 15%"
   does not; "warn below 15% on `srv-backup`" does.

   This clarifies [0006](0006-alerting-rules.md), which says thresholds live in the hub's
   configuration and never in code. That still holds for what it was aimed at — a threshold
   must never be hard-coded in the evaluation logic, where changing it would need a rebuild.
   Product defaults are the other thing: values the tool falls back to when configuration
   says nothing, overridable at every layer without touching the binary.
2. **Examples are synthetic.** `config.example.yaml` and every sample in the documentation
   use invented names (`laptop-a`, `server-b`) and round numbers.
3. **Fixtures are synthetic.** Test data is generated or hand-written, never a real export.
4. **A secret scanner runs before every commit**: `.githooks/pre-commit` blocks staged
   configuration, secrets and databases, then runs `gitleaks` over the staged diff. The
   hooks directory is versioned and enabled with `git config core.hooksPath .githooks`.
   Forge-side secret scanning with push protection is enabled as a second line.
5. **Security rests on secrets, not on obscurity.** A public repository publishes the
   threat model too: long random per-node tokens, rate limiting on ingest, and
   authentication on the web page before the hub gets a public address.
6. **A sensor whose code discloses private facts** — a parser for one specific broker's
   statement, an integration with one specific account — ships as an external sensor in the
   private configuration repository, through the interface from
   [0003](0003-sensors-are-modules.md). The privacy boundary is an existing architectural
   boundary.

## Consequences

- The separate private configuration repository is planned, not built: while configuration
  is a handful of files, it lives on the server. It is split out when the first private
  sensor appears, or when configuration needs its own history and review.
- Issues, pull requests and commit messages are subject to the same rules as code.
- Documentation examples cannot be copy-pasted into production; that is the intended cost.

## Alternatives

- **Private now, public after the POC** — rejected: the boundary between tool and
  configuration would be drawn after the fact, and the whole history would need an audit.
- **Two repositories from day one** — deferred, not rejected: two checkouts for thirty
  lines of configuration is overhead the POC does not need.
