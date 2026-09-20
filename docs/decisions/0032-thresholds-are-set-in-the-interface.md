# 0032. Thresholds are set in the interface and stored with the data

- **Status:** accepted
- **Date:** 2026-09-20
- **Source:** [0012](0012-threshold-model.md) (the layering half),
  [0033](0033-a-subject-is-a-series.md), [concept](../concept.md) principle 2

## Context

A threshold in the hub's file assumes one number can be right for a class of volumes. It is
not: two volumes of the same size on the same node differ by purpose — a scratch volume
lives full by design, a system volume at 5% is an incident.
[0012](0012-threshold-model.md) answered that with a model general enough to need no
exception per volume, plus a layering path down to a single volume for what was left over.

Two things undermine that answer. The model's generality is bought in code — a rule is a
shape the hub knows, so a metric that wants a level costs a code change rather than a line
of configuration, which is the opposite of [concept](../concept.md) principle 2. And the
numbers live in a file on the hub host: changing one means an edit over ssh and a restart,
which is not how a personal panel is tended.

## Decision

A threshold belongs to one subject, is entered in the hub's web interface, and is stored
beside the measurements:

- **Nothing has a level by default.** A series nobody has configured is collected, stored,
  charted and listed — and never alerts. There are no product defaults for thresholds.
- **The file keeps what identifies an installation and what agents need**: nodes, tokens,
  classes, sensor profiles, intervals, `silence_after`, the digest hour and the
  notification channel. It no longer carries `rules`, `volumes`, or any number a threshold
  is made of.
- **The form writes through the perimeter of [0023](0023-proxy-holds-the-web-perimeter.md)**
  — the proxy authenticates, the hub stays bound to localhost — and the hub refuses a save
  that does not come from its own pages, by the request's origin. That keeps 0023's "no
  session, no cookie": a stored CSRF token would need state the hub deliberately does not
  have. The write path takes the person's credential, not the credential issued to programs
  ([nginx-requirements.md](../nginx-requirements.md)), because a script's credential
  travels further and must not be able to silence every alert.
- **Node silence stays in the file.** It is a property of a node, not of a series, and it
  has to work on a fresh install before anyone opens a page.

A shipped threshold default would fail the test [CLAUDE.md](../../CLAUDE.md) sets for this
repository: a number that says what "nearly full" means for *this* volume says something
about an installation, not about the product.

## Consequences

- **Upgrading an existing hub silences its disk alerts** until each volume is given levels
  in the interface. The numbers in the old file are not migrated: the unit of configuration
  is no longer the same object ([0033](0033-a-subject-is-a-series.md)). The hub upgrades
  itself hourly and unattended ([0025](0025-the-hub-checks-hourly-and-downloads-a-binary-to-install-it.md)),
  so that moment is not one anybody chooses, and a hub that will not start is recovered by
  hand ([installer.md](../specs/installer.md)). For one release the file's `rules` and
  `volumes` keys are therefore accepted and ignored with a warning instead of refused
  ([hub-config.md](../specs/hub-config.md#startup)): a silenced hub can be configured, a
  crash-looping one cannot.
- **Silence must be visible.** A hub that watches nothing looks exactly like a hub where
  nothing is wrong, so the state says how many subjects are watched
  ([state.md](../specs/state.md#listing)), `/` says so above its tables, and the digest says
  it on the channel the operator actually reads ([evaluation.md](../specs/evaluation.md#digest)).
  Without that, this decision trades a false alarm for a silent failure.
- **What node silence keeps covering**: a node that dies is still noticed on a fresh
  installation, because `silence_after` stays in the file. The exposure this decision creates
  is narrower — a *filling* disk on a *live* node nobody has configured.
- **Configuration becomes data in the database.** A backup that loses the database now
  loses the thresholds too, so [issue #19](https://github.com/pravbeseda/monitor/issues/19)
  stops being optional.
- Startup can no longer refuse a bad threshold: the form rejects it instead, at the moment
  it is entered, and the hub must survive whatever is already stored.
- Any skin can offer the same editing surface later. The store is the source of truth; the
  page is one writer of it ([0001](0001-semantic-core-and-skins.md)).
- Editing a threshold still reaches no agent, so it still changes no `config_version`
  ([0010](0010-agent-configuration.md)).

## Alternatives

- **Keep thresholds in the file and have the interface rewrite it** — rejected: two writers
  of one file, a hand edit racing a form edit, and comments lost on every save.
- **Defaults in the file, overridden in the interface** — rejected: the number that alerts
  you becomes a merge of two sources you cannot see at once, and shipped defaults are
  exactly what makes a fresh install cry wolf on the volumes it knows nothing about.
- **Layering (class → node → volume) inside the store** — rejected: layering existed to make
  one default fit many volumes. With per-volume editing it has no work left. "Apply this to
  every volume of this node" is a convenience of the form, not a model underneath it.
- **A command-line editor instead of a form** — rejected: it keeps the ssh round trip that
  motivated the change, and a second interface would have to be kept in step with the page.
