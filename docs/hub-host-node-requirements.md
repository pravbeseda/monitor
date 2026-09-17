# What the hub host needs to report on itself

The hub host is provisioned by Ansible, in the repository that owns that host, and the hub is
installed from there. This file is the requirement handed to it for the next step: **the same
play also installs the agent on that host**, so the host's own disks are watched like any
other node's. It says what the result must be, not how the role is written. Per
[ADR 0007](decisions/0007-public-repository.md) no host name, node name, domain or token may
enter this repository; `hub.example.com` and `server-a` below are placeholders.

Related: [install.md](install.md) is the operator's guide the role automates,
[specs/installer.md](specs/installer.md) and [specs/deployment.md](specs/deployment.md) own
what an install does and leaves on disk, and [nginx-requirements.md](nginx-requirements.md)
is the proxy half of the same host. Why every play restarts both services rather than
detecting a change is [log/2026-09-13-hub-host-node.md](log/2026-09-13-hub-host-node.md).

The durations below are the hub's compiled-in defaults; a `hub.yaml` that overrides
`base_tick`, the class's `silence_after` or the disk interval moves them accordingly.

## What is added

One more service beside the hub, installed by the same installer in its other role
([installer.md](specs/installer.md#edge-cases)):

| What | Value |
|---|---|
| unit | `monitor-agent.service` |
| binary | `/usr/local/bin/monitor-agent` |
| its settings and token | `/etc/monitor/agent.env`, root, `0600` |
| account | root — the agent stats every mounted volume |

Nothing about the hub, the proxy or the firewall changes.

## Requirements

1. **The host is a node in `hub.yaml`.** One entry under `nodes` with a name of the role's
   choosing, `class: server` — compiled into the hub, silent after 10 minutes without a
   push — and a `token_env` naming a variable no other node uses. The name is a role
   variable; it lives in the Ansible repository, never here.

2. **The node's token is generated once and kept in the vault.** At least 32 characters or
   the hub refuses to start ([hub-config.md](specs/hub-config.md#startup));
   `openssl rand -base64 32` gives 44. The same value goes to two places: the `token_env`
   variable in `/etc/monitor/hub.env`, and the agent's install on stdin (requirement 5). It
   is this node's alone: not another node's token, not a proxy credential.

3. **Every play enables and restarts the hub before the agent's tasks, and waits for it.**
   The hub reads `hub.yaml` and `hub.env` only at startup, so a node it has not read is
   refused with `401`. A handler is not enough: its "changed" flag is lost when a play stops
   before handlers run, and the next play would leave the hub on the old configuration for
   good. So, unconditionally:
   - `systemctl enable` and restart `monitor-hub.service` — a first installer run without
     configuration leaves the service disabled;
   - then wait until the service's own process listens on the port of the listen address
     (requirement 4) — `ss -Hltnp` lists a socket on that port whose PID equals
     `systemctl show -p MainPID --value monitor-hub.service` — and fail the play if it never
     does. Match by port and PID, not by the address as written: `MONITOR_LISTEN` may be
     `localhost:<port>`, and `ss` prints the numeric address the hub bound. The unit
     restarts on every exit, so `systemctl restart` succeeds even for a hub that
     dies at once on a bad configuration or on an address already taken; a connection alone
     proves nothing then, because whatever holds the address accepts it.

   The restart costs a moment of `502` behind the proxy, which the proxy recovers from by
   itself ([nginx-requirements.md](nginx-requirements.md), requirement 10), and a missed tick
   at most on the other nodes, which retry on their next one.

4. **The agent talks to the hub over loopback, not through the proxy.** Its hub URL is
   `http://` followed by the whole address the hub listens on, taken from **the same
   variable** that sets `MONITOR_LISTEN` in `hub.env` and the proxy's upstream —
   `127.0.0.1:8080` when the hub is left on its default. Build it from the address, not from
   a port glued to `127.0.0.1`: `MONITOR_LISTEN` may be any loopback address. The token then
   never leaves the host, the node does not spend the proxy's rate limit, and a proxy outage
   never makes this node silent. Plain HTTP is right here and only here: every other node
   uses `https://hub.example.com`.

5. **Every play installs the agent with the release installer, verified, with the token on
   stdin.** Follow [install.md, section 0](install.md#0-the-short-way-one-command): fetch
   `monitor-install.sh` into a directory only root can write, check the fingerprint of the
   key it carries, run it, and delete it — a copy left behind carries an old key and an old
   origin. The expected fingerprint is a role variable copied from install.md; a mismatch
   fails the play, and is resolved by re-reading install.md after a key rotation, never by
   skipping the check. The run is

   ```sh
   sh monitor-install.sh agent --version 1.2.3 --hub http://127.0.0.1:8080 --node server-a
   ```

   - The version is pinned in a role variable of the agent's own, so the hub and the agent
     can be moved separately. A pinned version older than the installed one is refused
     unless the run adds `--allow-downgrade`.
   - The vault token is the whole of stdin, on every run: the installer reads stdin whenever
     it is not a terminal and waits for it to close, and what a task with no `stdin` of its
     own inherits is not something to rely on. Trailing newlines are dropped; anything else
     in stdin becomes part of the token or is refused.
   - `MONITOR_TOKEN` is not set in the task's environment: when it is, stdin is not read.
   - The run ends by restarting the agent, so the play reports a change for it every time.
     That is expected; a run that stopped half-way is finished by the next play.

6. **The token is never an argument, never in output, and never kept on disk but in
   `hub.env` and `agent.env`.** The short-lived, owner-only temporaries a template task and
   the installer write on their way to those two files are fine.
   - The install task runs with `no_log`, and so does the task that writes `hub.env`, with
     `diff: false` besides: a module's arguments show up in its logged invocation, and a
     template's diff prints the file.
   - Pipelining is on for this host and the install task is not `async`, so module arguments
     are not written to a temporary file on it.
   - The fetch, the fingerprint check and the run sit in one `block`. Its `rescue` prints
     `ansible_failed_result.stderr | default('')` — a failed fetch has none — and nothing
     else of that result: the installer never prints the token, but the result carries the
     module's arguments, `stdin` among them. The `rescue` then fails the play, and the
     `always` deletes the script. Without the `block`, a failed run removes the host from the
     play before either happens.

7. **`/etc/monitor` stays owned by root, mode `0755`.** `hub.yaml` and `hub.env` inside it
   belong to `monitor`; the directory itself does not. The agent install refuses a directory
   owned by anyone but root, or a symlink
   ([deployment.md](specs/deployment.md#refusing)); a hub role that chowns the whole
   directory to `monitor` breaks this step.

8. **The role does not write `agent.env` or the agent's unit.** The installer owns both and
   rewrites them on every run ([ADR 0020](decisions/0020-agent-reads-its-environment-file.md)).

9. **Installs are sequential, and the hub goes first.** Two installer runs at once on one
   host are not supported, and the hub's update timer counts as one: starting its service
   waits for a run already in progress instead of overlapping it. The hub is installed and
   restarted before the agent: the hub accepts measurements from an agent older than itself,
   and nothing promises the reverse ([ADR 0022](decisions/0022-updates-are-pulled.md)).

10. **The hub is installed by its own update service, never with a pinned version.** The play
    writes `/etc/monitor/hub.target` from a role variable — `latest` or a version — places the
    kept script and the two update units, and enables the timer, all as
    [install.md](install.md#keeping-the-hub-upgraded-unattended) describes. It then runs
    `systemctl start monitor-hub-update.service`, which returns when the run has finished,
    after `hub.yaml` and `hub.env` are written and before requirement 3's restart and wait.
    The play never runs `monitor-install.sh hub --version …`: once the timer has moved the
    hub past that version, the downgrade guard refuses it and the play fails. A failed update
    run fails the play. When a timer run is already in progress, the start waits for that run
    instead of starting another, and that run may have read the target before the play
    rewrote it; a play that changed the target therefore starts the service a second time.

## What we are not asking for

- No change detection: every play restarts both services on purpose (requirements 3 and 5).
- No nginx or firewall change: the agent never leaves loopback.
- No separate account for the agent: it runs as root by design
  ([deployment.md](specs/deployment.md#where-things-live)).
- No update timer for the agent: upgrading it is re-running the play with a new version. A
  node can follow the hub's target from a timer of its own
  ([ADR 0028](decisions/0028-agents-follow-a-target-the-hub-serves.md),
  [install.md](install.md#keeping-a-node-upgraded-unattended)); on this host the play keeps
  that choice, and placing the timer is a change to these requirements.

## How we check it is done

On the hub host, with the real node name in place of `server-a` and the real listen address
in place of `127.0.0.1:8080`. The journal needs `sudo`: without it an account outside the
journal's groups sees nothing, and a count of zero proves nothing. `grep -c` exits 1 when it
counts zero, so a task wrapping these lines judges by the output, not the status.

```sh
systemctl is-enabled monitor-hub.service monitor-agent.service   # enabled, enabled
systemctl is-active monitor-agent.service                        # active
sudo journalctl -u monitor-agent.service -n 20                   # "node server-a reporting to http://127.0.0.1:8080"
sudo journalctl -u monitor-agent.service --since -15min | grep -c 'tick failed'   # 0
sudo stat -c '%U %a' /etc/monitor /etc/monitor/agent.env         # root 755, root 600
```

Then in the browser, `https://hub.example.com/`: the node's last-seen time is fresh within
one base tick (5 minutes) of the play. Its volumes appear a bootstrap tick after the agent
starts — the first push carries no measurements. Every play restarts the agent, and a
restarted agent collects again only on its second tick, so right after a play their collected
time may be up to 20 minutes old: the disk interval for a server plus one bootstrap tick.

Requirement 6 — the token stays out of the play's output at the verbosity that prints module
arguments, with diffs on. Two runs, and each must reach the tasks that carry the token:

- a green one that also changes the content of `hub.env` — adding a line to it will do — so
  that its template task would have a diff to print;
- a failing one that overrides the agent's version variable with a version that was never
  released. The hub follows its target and still succeeds, the agent's fetch fails inside
  the `block`, and the `rescue` prints the installer's `stderr`.

The token is read from a file descriptor, so it is not an argument of `grep` either (bash):

```sh
ansible-playbook … -vvv --diff 2>&1 | grep -cFf /dev/fd/3 3<<<"$token"                               # 0
ansible-playbook … -vvv --diff -e <agent version variable>=0.0.1 2>&1 | grep -cFf /dev/fd/3 3<<<"$token"   # 0, the play failed in the agent's rescue
```
