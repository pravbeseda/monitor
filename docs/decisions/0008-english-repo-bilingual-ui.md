# 0008. The repository is English; the interface is bilingual (en/ru)

- **Status:** accepted; an external sensor names its own metrics, per
  [0048](0048-a-sensor-names-its-metrics.md)
- **Date:** 2026-08-28

## Context

The repository is public ([0007](0007-public-repository.md)), so its contents address an
audience that does not read Russian. The primary user of the running system does.

## Decision

- **Everything in the repository is English**: code, identifiers, comments, documentation,
  configuration keys, metric ids, commit messages, issue and pull request text, CLI output.
- **Everything the user sees is localized**, with English and Russian supported from the
  first user-facing string: web pages, Telegram messages, alert and digest texts, error
  messages shown to the user.

## Consequences

- No user-facing string is hard-coded at its usage site; strings come from a message
  catalogue keyed by an English identifier.
- Locale governs number, date and byte-size formatting, not just wording. A digest in
  Russian formats sizes and timestamps in Russian conventions.
- The Telegram notifier carries a locale per recipient, the same as the web skins.
- Metric ids, labels and status values stay English in the API and are translated only at
  the presentation edge — including for LLM-generated advisor summaries, whose prompt
  language is a presentation concern.
- CLI output and log lines stay English: they are diagnostic, not user-facing, and end up in
  bug reports.

## Alternatives

- Russian documentation with an English codebase — rejected: it splits a public repository
  into a part contributors can read and a part they cannot.
- English-only interface — rejected: it makes the daily ritual worse for its only user, and
  the ritual is the product.
