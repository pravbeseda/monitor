# 2026-09-27 — Cloud storage: the first service node

The user wants to watch the limits of online services, cloud storage first, Google Drive
first of all. The outcome is [0039](../decisions/0039-the-hub-collects-a-service-node.md) and
[services.md](../specs/services.md); this note keeps what lost.

## rclone or our own code

`rclone about <remote>: --json` reads the quota of some forty storage services, and looked
like the off-the-shelf answer CLAUDE.md asks to weigh first. Three facts turned it:

- rclone's shared Google client is announced for retirement, so a deployment registers its
  own OAuth client with Google either way — the step rclone was meant to save.
- Refreshing a token is five lines of `golang.org/x/oauth2`; what rclone really saves is the
  first consent, and Google's OAuth Playground does that with no code at all.
- rclone writes the refreshed token back into its own config file, so a deployment that
  templates that file reports drift on every run, and the host gains a package to keep
  current. Our sensor keeps three static values in the hub's environment file, which the
  deployment already writes.

rclone still wins at five or more services; it can arrive then as another sensor with the
same metrics.

## Whose series

A cloud account is no machine, and every view is arranged by node. Three homes were weighed:
the node whose agent collects it (cheapest, but a full Drive turns a healthy server red, and
a later move splits the history), a second agent on some host under its own node name (the
install layout holds one agent per host), and a node the hub collects itself. The last won:
the reading stands under its own name and nothing changes on any machine.

## How a dead token shows

The spec review found that a failing sensor would have shown nowhere that alerts: the board
raises nothing while every series of a node is stale, and the hub's own heartbeat would keep
the node from falling silent. The user chose an alert over a board item or nothing, so a
service node is seen only when it stores a measurement, and a dead token becomes a silent
node — the one instant message the product already has. `silence_after` defaults to 3h, three
missed hourly collections, and must be at least two intervals and three base ticks: each
collection waits for a tick after its interval, and a failing one can hold its tick for half
a tick more. The self-review found the first bound, one tick, let a single failure through.

The self-review also found that the compiled-in class name `service` can collide with a
machine class an existing file already names so; such a file is refused with a message to
rename it, rather than turning those machines into service nodes.

## Smaller choices

- **Scope `drive.appdata`, not `drive.file`**: both are non-sensitive and both read the
  quota, but `drive.appdata` opens only the application's hidden folder.
- **Token errors are mapped, never wrapped**: `oauth2.RetrieveError` carries the HTTP
  response, whose request holds the client credentials; a log line that printed the value
  would leak them base64-encoded.
- **`AuthStyleInParams` is pinned**: left unset, `x/oauth2` retries a refused refresh a second
  way, doubling every failure.
- **The collector ticks twice at start**: the first tick asks for the configuration and makes
  the node known, the second collects at once, so a restart after a long outage does not
  announce a silence that ends a base tick later. Handing the loop its configuration up front
  did the same with two more exported functions, and lost in self-review.
