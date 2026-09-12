# Spec: Deployment

- **Status:** approved
- **Owns:** in `deploy/`: the systemd units, the launchd daemon, the environment files and
  `install-agent.sh` — the release verifier beside them is [release.md](release.md)'s
- **Decisions:** [0005](../decisions/0005-poc-stack.md),
  [0007](../decisions/0007-public-repository.md),
  [0010](../decisions/0010-agent-configuration.md),
  [0019](../decisions/0019-deployment-layout.md),
  [0020](../decisions/0020-agent-reads-its-environment-file.md)

## Purpose

A node runs the agent as a system service that survives a reboot and a crash, and the host
runs the hub the same way. This spec owns what an installation looks like on disk, what the
services guarantee, and what `install-agent.sh` does to a node — nothing about what the
binaries then do once they are running ([agent.md](agent.md) owns that).

It deliberately covers **one** half of the operational work: getting the processes running
and supervised. TLS, the nginx vhost, per-node token issuance and authentication on the web
page are the other half, and nothing here makes a statement about them.

## Where things live

Every path is fixed by [0019](../decisions/0019-deployment-layout.md). Nothing here varies
per installation, which is what lets the unit files be constants.

| What | Debian | macOS | Owner, mode |
|---|---|---|---|
| agent binary | `/usr/local/bin/monitor-agent` | `/usr/local/bin/monitor-agent` | root, 0755 |
| agent settings and token | `/etc/monitor/agent.env` | `/usr/local/etc/monitor/agent.env` | root, 0600 |
| agent service | `/etc/systemd/system/monitor-agent.service` | `/Library/LaunchDaemons/io.github.pravbeseda.monitor-agent.plist` | root, 0644 |
| agent log, macOS only | — | `/var/log/monitor-agent.log` | root, 0600 |
| hub binary | `/usr/local/bin/monitor-hub` | — | root, 0755 |
| hub settings and secrets | `/etc/monitor/hub.env` | — | `monitor`, 0600 |
| hub configuration | `/etc/monitor/hub.yaml` | — | `monitor`, 0640 |
| hub configuration example | `/etc/monitor/hub.yaml.example` | — | root, 0644 |
| hub settings example | `/etc/monitor/hub.env.example` | — | root, 0600 |
| hub database | `/var/lib/monitor/monitor.db` | — | `monitor`, 0600 |
| hub data directory | `/var/lib/monitor` | — | `monitor`, 0700 |
| configuration directory | `/etc/monitor` | `/usr/local/etc/monitor` | root, 0755 |
| hub service | `/etc/systemd/system/monitor-hub.service` | — | root, 0644 |

The hub is a Debian service only ([0005](../decisions/0005-poc-stack.md)); the agent runs on
both.

**The agent runs as root** — it stats every mounted volume, and a launchd daemon is a root
process by definition. **The hub runs as the unprivileged `monitor` account**: it listens on
a socket and needs nothing root can give it. The account is created by `install-hub.sh`
([installer.md](installer.md)), which refuses one that can log in; on the manual path it is
created by hand, and the install guide has the command.

## The environment files

Everything that differs between installations is a secret or a deployment setting, and none
of it may live in this repository ([0007](../decisions/0007-public-repository.md)). Each
binary therefore reads one environment file, and the unit that starts it is a constant.

**The agent reads its own file** and never shells out to read it
([0020](../decisions/0020-agent-reads-its-environment-file.md)): the service passes the path
with `--env-file` and nothing from inside it. The hub keeps systemd's own
`EnvironmentFile=`.

For the agent that file is the whole of its local configuration: its three keys are the
three values [agent.md](agent.md) allows a node to hold. The hub keeps its *secrets* and the
address it listens on there, and its product configuration in `hub.yaml`
([hub-config.md](hub-config.md)). The listen address belongs with them because a free port is
a fact about one host, not about the product.

| Key | In | Meaning |
|---|---|---|
| `MONITOR_HUB` | `agent.env` | base URL of the hub |
| `MONITOR_NODE` | `agent.env` | this node's name, as the hub knows it |
| `MONITOR_TOKEN` | `agent.env` | this node's token |
| `MONITOR_LISTEN` | `hub.env` | the loopback address the hub serves on, when the host's free port is not the default ([hub-config.md](hub-config.md#startup)) |
| `MONITOR_TELEGRAM_TOKEN`, `MONITOR_TELEGRAM_CHAT_ID` | `hub.env` | notifier credentials, when the channel is Telegram |
| the variables `hub.yaml` names in each node's `token_env` | `hub.env` | the token each node presents |

## Behaviour

One row = one test. Anchors: `spec: deployment.md#<heading>`.

### Installing on a fresh node

The script installs a binary that was built elsewhere; it never builds one
([0019](../decisions/0019-deployment-layout.md)).

| State | Event | Behaviour |
|---|---|---|
| nothing installed | `install-agent.sh --binary B --hub U --node N` with the token on stdin | the binary lands executable, the environment file holds the three values and is readable by its owner only, the service is registered and running |
| nothing installed | the same with `MONITOR_TOKEN` set in the environment and stdin empty | identical |
| nothing installed | any successful run | it prints every path it wrote and the command that shows the service's state |
| any | any run | the token appears in no argument, no message and no shell history |

Under `sudo` the environment is reset by default, so **stdin is the route that always
works**: `printf %s "$token" | sudo ./install-agent.sh …`. `MONITOR_TOKEN` is read when it
survives — a root shell, or a `sudo` configured to pass it — and passing the token as a
command-line argument is not offered at all, because arguments are visible in `ps`.

### Refusing

Every refusal below is decided before the first file is written, so the node is untouched.

| State | Event | Behaviour |
|---|---|---|
| any | `--binary` names a file that is missing or not executable | refuses, naming the path |
| any | `--hub` or `--node` missing | refuses, naming the flag |
| any | a flag that takes a value is given without one | refuses, naming the flag |
| nothing installed | no token in the environment and none on stdin | refuses, naming `MONITOR_TOKEN` |
| any | a value carrying a line break | refuses, naming no value: one of the three is the token |
| any | a value opening with a quote it does not close, which would make the agent refuse the whole file | refuses, naming no value |
| any | a value the agent would read back as something else — padded with blanks, or wrapped in quotes the format strips | refuses, naming no value |
| any | a value holding a character outside printable ASCII, which the agent trims as whitespace or reads differently | refuses, naming no value |
| already installed | a line of the existing file is neither blank, a comment, nor an assignment the agent accepts | refuses, naming the file and the line number |
| any | an unknown flag | refuses, printing the usage |
| any | the service definition it installs is not beside the script | refuses, naming the path |
| a real install | the host has neither systemd nor launchd | refuses, naming what is supported |
| a real install | the invoking user is not root | refuses, saying to re-run under `sudo` |
| a real install | the environment file's directory exists and is not owned by root | refuses, naming the directory |
| a real install | that directory is a symlink, whenever the run looks — including after it created it | refuses, naming the directory |

### Re-running

Re-running is how a node is upgraded and how a token is rotated, so it is the ordinary case
rather than an error. `--hub` and `--node` are given every time; only the token may be
omitted.

| State | Event | Behaviour |
|---|---|---|
| already installed | a run with a newer binary | the binary is replaced and the service restarted |
| already installed | a run with the same arguments and the same binary | every installed file ends byte-identical, and the service is running |
| already installed | a run with a different `--hub` or `--node` | the environment file carries the new value and the service is restarted |
| already installed | a run whose token differs | the environment file carries the new token, still readable by its owner only |
| already installed | a run with no token available | the stored token is kept and the run succeeds |

### The services

| Service | Event | Behaviour |
|---|---|---|
| agent | the node reboots | it starts without anyone logging in |
| agent | it exits, whatever the status | it is restarted after a short delay, and goes on being restarted however fast it keeps failing |
| agent | it writes to stdout or stderr | the lines reach the journal on Debian, and `/var/log/monitor-agent.log` on macOS, where launchd has no journal to reach — and on neither system can another account read them |
| agent | its environment file has no `MONITOR_TOKEN` | it exits at startup naming the variable, and the restart loop keeps that message coming to that log |
| hub | the host reboots | it starts once the network is up |
| hub | it exits, whatever the status | it is restarted after a short delay |
| hub | it starts | it reads `/etc/monitor/hub.yaml` and `/var/lib/monitor/monitor.db`, and serves on the loopback address only |
| hub | it writes to stdout or stderr | the lines reach the system log |

A *wrong* token is not a service concern: the agent keeps ticking and the hub refuses its
batches ([agent.md](agent.md#edge-cases)), which surfaces as a silent node rather than as a
failed service.

### Staged installs

`DESTDIR` puts a whole installation under a prefix, as a package build would. It is what
makes the behaviour above testable without touching the machine running the test.

| State | Event | Behaviour |
|---|---|---|
| any | `DESTDIR` set | every path is written beneath it, with the same names and the same modes, in the layout of the host's own operating system |
| any | `DESTDIR` set | no service is registered, started or restarted, and the refusals a real install makes — root, its ownership of the environment file's directory, an init system — do not apply |

## Invariants

- No file this repository ships names a host, a node, a URL or a token: the unit files are
  the same on every installation, and everything that differs is in an environment file
  ([0007](../decisions/0007-public-repository.md)).
- The token is never an argument to a command, never printed, and never lands in a file
  another user can read.
- No deployment setting reaches a service's arguments either
  ([0020](../decisions/0020-agent-reads-its-environment-file.md)).
- Every refusal in the table above happens before the first write, so a rejected run leaves
  the node exactly as it was. The one exception is named there: the environment file's
  directory is checked again after it has been created, because an account that owns its
  parent can plant a symlink at any moment, and by then the binary is installed.
- A run that fails after it has begun writing stops at that failure and names what it
  already wrote; it does not continue, and it does not roll back. Re-running it is the fix.
- A successful run leaves a service that starts on boot.

## Edge cases

- **A second install while the service is running** — the binary is replaced under a running
  process, which is why the run ends with a restart rather than a reload.
- **A token that is already installed and not supplied again** — kept. The alternative, an
  empty token written over a working one, breaks a node during a routine upgrade.
- **The environment file edited by hand** — supported for the lines the agent itself accepts:
  the run rewrites `MONITOR_HUB` and `MONITOR_NODE` from its flags and leaves any other line
  alone, the stored token included. A line the agent would refuse is refused here instead,
  because the agent refuses the whole file over one of them and the install would otherwise
  report success on a node that never starts.
- **A binary for the wrong operating system** — not detected. It installs, the service fails
  to start, and the system log says so.
- **`sudo` stripping `MONITOR_TOKEN`** — the default on both systems, and the reason stdin
  exists. A run that finds neither is a refusal, not a silent install with an empty token.

## Out of scope

- The nginx vhost, TLS, web authentication and per-node token issuance — the other half of
  the stage-3 bullet in [poc.md](../poc.md).
- Building, publishing or verifying the binary: the script takes one that exists, and
  [release.md](release.md) owns where it comes from.
- Uninstalling: two documented commands in the install guide, not a mode of the script.
- Installing as an act — what a run downloads, checks and calls:
  [installer.md](installer.md). This spec owns the result on disk, for the hub as for the
  agent.

## Open questions

None.
