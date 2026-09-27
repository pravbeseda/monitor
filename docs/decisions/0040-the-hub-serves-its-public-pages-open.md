# 0040. The hub serves the public pages itself, under `/public/`, without a credential

- **Status:** accepted
- **Amends:** [0023](0023-proxy-holds-the-web-perimeter.md) (the proxy authenticates
  everything but the node's paths); [0026](0026-reader-time-zone-from-the-browser.md) (every
  HTML page answers `no-store`, varying by cookie and language) for the public pages, which
  read neither
- **Date:** 2026-09-27
- **Source:** [web spec](../specs/web.md#public), [proxy requirements](../nginx-requirements.md)

## Context

A Google OAuth app in production must name a homepage, a privacy policy and terms of
service, and Google fetches them without logging in
([install.md](../install.md#watching-google-drive)). The pages exist in the repository as
`public/`, but nothing served them: the hub had no such path, and the proxy puts every path
but ingest and the agents' prefix behind its credential
([0023](0023-proxy-holds-the-web-perimeter.md)), so they could not be reached on the hub's
name at all.

## Decision

- **The hub serves `public/` under `/public/`**, embedded in its binary by explicit patterns
  (`*.html`, `*.css`), as fixed files: no shell, no language, no cookie, no data.
- **The proxy lets `/public` and `/public/` through without a credential**, the third
  exception after ingest and `/api/v1/agent/`. Everything else stays as 0023 has it.

## Consequences

- The pages ship with every release, so an edit to them reaches the server the way code
  does, and there is no second copy of them on a host to drift.
- Anything under `/public/` is readable by anyone who knows the hub's name. Only files
  committed to `public/` and matched by the embed's patterns can appear there, and those are
  public already ([0007](0007-public-repository.md)); a page that ever needed an
  installation's data would not belong under this prefix.
- The pages are in English only, an exception to
  [0008](0008-english-repo-bilingual-ui.md): their readers are Google's reviewers, and the
  consent screen links one address per page.
- An embedded file has no modification time, so a browser cannot revalidate a page; the
  pages answer `no-cache` instead of `no-store`, and an edit shows on the next load.
- `/public/` is not a health path. It proves that the hub process answers and nothing about
  its storage or its evaluation, so an uptime check that probes it learns that much only.
- The proxy role needs one more exempt prefix ([requirement 4](../nginx-requirements.md)).

## Alternatives

- **nginx serves the files from a directory on the host** — rejected: nothing installs
  `public/` on a host, so the proxy role would carry its own copy, which drifts from the
  repository; and the proxy requirements ask for no static hosting.
- **GitHub Pages, or a static site the owner already publishes from another repository** —
  rejected: the pages leave this repository or are copied out of it, and a second place to
  publish drifts from the first. GitHub Pages also puts them on a domain the owner cannot
  verify, and the consent screen's authorized domains must be verified ones.
