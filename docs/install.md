# Installing Monitor

The operator's guide: which commands to type to get a hub host and a node running. What the
result is guaranteed to look like — every path, mode and refusal — is
[specs/deployment.md](specs/deployment.md), and why it looks that way is
[ADR 0019](decisions/0019-deployment-layout.md) and
[ADR 0020](decisions/0020-agent-reads-its-environment-file.md). This file does not repeat
either.

Every name below is synthetic ([ADR 0007](decisions/0007-public-repository.md)):
`hub.example.com` is the hub, `server-b` a Debian node, `laptop-a` a macOS one. Substitute
your own — and keep them out of this repository.

What you need: `ssh`/`sudo` access to each machine. Section 0 needs nothing else. The manual
path below also needs a checkout of this repository — it carries the verifier and the key a
release is checked with — and the Go toolchain `go.mod` names if you build the binaries
yourself. One Debian host runs the hub; every node, Debian or macOS, runs the agent.

## 0. The short way: one command

A tagged release carries its own installer, so a machine that can reach GitHub needs neither
this checkout nor a Go toolchain ([specs/installer.md](specs/installer.md)):

```sh
# the hub host
curl -fsSL https://raw.githubusercontent.com/pravbeseda/monitor/main/deploy/monitor-install.sh \
    | sudo sh -s -- hub

# a node, upgrading one that is already installed
curl -fsSL https://raw.githubusercontent.com/pravbeseda/monitor/main/deploy/monitor-install.sh \
    | sudo sh -s -- agent --hub https://hub.example.com --node server-b
```

The run downloads the newest release, checks its signature against the key it carries, and
hands over to the installer inside that release. Re-running it is how a machine is upgraded.

**A hub's first run stops before starting the service.** `hub.yaml` and `hub.env` describe
one installation and have no defaults, so the run installs everything else, writes
`/etc/monitor/hub.yaml.example` and `hub.env.example` beside where they belong, and prints
the two `install` commands that turn them into the real files. Fill those in — step 2 below
says what goes in them — and run the same command again.

**A first install of a node needs its token, and the one-line form cannot carry one**: piping
the script into `sh` uses up stdin, which is the only route a token may take. Download the
script instead, check the key it carries, and run it with the token piped in.

The key must be this project's:

```
c3f2428af516bf02bb789d78d139b0e135faae2ef715cb32f0865bdf3a11f122
```

That fingerprint changes only when the key is rotated, which is why it is worth checking and
a hash of the script itself is not. Everything below rests on it: whoever serves the script
chooses the key inside it, so a value that does not match means stopping, not retrying.

```sh
home=$(sudo -H sh -c 'printf %s "$HOME"')   # /root on Debian, /var/root on macOS
sudo install -d -m 0700 "$home/monitor"
sudo curl -fsSLo "$home/monitor/install.sh" \
    https://raw.githubusercontent.com/pravbeseda/monitor/main/deploy/monitor-install.sh
sudo sed -n '/BEGIN PUBLIC KEY/,/END PUBLIC KEY/p' "$home/monitor/install.sh" \
    | sed "s/^[[:space:]]*//; s/^release_key='//; s/'$//" \
    | openssl pkey -pubin -outform DER | openssl dgst -sha256

read -rs token                              # paste the node's token; it is not echoed
printf %s "$token" | sudo sh "$home/monitor/install.sh" agent \
    --hub https://hub.example.com --node server-b
unset token
sudo rm -r "$home/monitor"
```

A directory only root can write is deliberate: a script downloaded into a shared one can be
replaced between the check and the `sudo` that runs it.

The rest of this guide is the manual path: it needs no release, and it is what recovers a
machine the installer cannot ([ADR 0022](decisions/0022-updates-are-pulled.md)).

## 1. Get the binaries

A tag publishes them, signed; what a release contains and how it is checked is
[specs/release.md](specs/release.md). Download what a machine needs into a scratch directory
— not into this checkout, which is a git working tree — together with the manifest and its
signature:

```sh
version=1.2.3
base=https://github.com/pravbeseda/monitor/releases/download/v$version
mkdir -p ~/monitor-release && cd ~/monitor-release
curl -fLO "$base/monitor-agent-$version-linux-amd64"
curl -fLO "$base/monitor-hub-$version-linux-amd64"
curl -fLO "$base/SHA256SUMS"
curl -fLO "$base/SHA256SUMS.sig"
```

The agent comes as `linux-amd64`, `linux-arm64`, `darwin-amd64` and `darwin-arm64`, the hub
as `linux-amd64` and `linux-arm64`; download the ones your machines need.

Check every binary **before renaming it**, since the manifest names the asset it published.
The verifier and the key live in this checkout:

```sh
cd /path/to/monitor
./deploy/verify-release.sh ~/monitor-release/monitor-agent-$version-linux-amd64
./deploy/verify-release.sh ~/monitor-release/monitor-hub-$version-linux-amd64
```

The exit status is the verdict. A binary that does not verify is not installed, whatever the
reason: the signature covers the manifest, and the manifest covers each asset's name and its
digest.

Only then give them the names the rest of this guide uses. `dist/` in the checkout is
ignored by git, so a verified binary can wait there without dirtying the tree:

```sh
mkdir -p /path/to/monitor/dist
mv ~/monitor-release/monitor-agent-$version-linux-amd64 /path/to/monitor/dist/monitor-agent
mv ~/monitor-release/monitor-hub-$version-linux-amd64 /path/to/monitor/dist/monitor-hub
chmod +x /path/to/monitor/dist/monitor-agent /path/to/monitor/dist/monitor-hub
```

A downloaded file arrives without its executable bit; the installs below set the mode they
need, but `--version` and `scp` want it set here.

`--version` answers which version a binary is, if it is one for this machine's platform:

```sh
./dist/monitor-hub --version    # monitor-hub 1.2.3
```

### Building by hand instead

Still supported, and it is what recovers a machine when a release cannot be reached
([ADR 0022](decisions/0022-updates-are-pulled.md)). From the checkout, cross-compile for a
Debian node:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/monitor-agent ./cmd/agent
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/monitor-hub ./cmd/hub
```

Use `GOARCH=arm64` for an arm server. The hub cross-compiles because its SQLite driver needs
no cgo ([ADR 0005](decisions/0005-poc-stack.md)).

**The macOS agent must be built on a Mac.** Its disk sensor is cgo
([ADR 0014](decisions/0014-macos-available-space.md)), so `GOOS=darwin` from Linux is not an
option:

```sh
go build -o dist/monitor-agent ./cmd/agent
```

A binary built this way reports the development version rather than a release version —
`./dist/monitor-agent --version` says so, and so does the hub for a node running it.

## 2. Set up the hub host

These are the steps `install-hub.sh` takes for you in section 0; done by hand they are the
recovery path. Copy what the host needs:

```sh
scp dist/monitor-hub config.example.yaml deploy/hub.env.example \
    deploy/systemd/monitor-hub.service hub.example.com:
```

Then, on the host, create the unprivileged account the hub runs as and its directories:

```sh
sudo adduser --system --group --no-create-home monitor
sudo mkdir -p /etc/monitor /var/lib/monitor
sudo chown monitor:monitor /var/lib/monitor
sudo chmod 0700 /var/lib/monitor
```

`/etc/monitor` stays owned by root — if this host is also a node, `install-agent.sh` refuses
to write into a directory that is not. The files inside carry the ownership instead:

```sh
sudo install -o root -g root -m 0755 monitor-hub /usr/local/bin/monitor-hub
sudo install -o monitor -g monitor -m 0640 config.example.yaml /etc/monitor/hub.yaml
sudo install -o monitor -g monitor -m 0600 hub.env.example /etc/monitor/hub.env
```

`agent.env` and `hub.env` look alike and are not read alike: systemd reads `hub.env` and
ignores a line it cannot parse, `;` comments included, while the agent reads `agent.env`
itself and refuses the whole file over one such line
([ADR 0020](decisions/0020-agent-reads-its-environment-file.md)). Edit `agent.env` as plain
`KEY=VALUE` lines and `#` comments only — an `export` prefix works in neither file.

Now edit both. `hub.yaml` is the product configuration — nodes, classes, thresholds,
digest, notifier ([specs/hub-config.md](specs/hub-config.md)). `hub.env` holds only secrets:
one token per node, named by that node's `token_env`, plus the Telegram credentials when the
channel is `telegram`. Generate a token per node, long and random:

```sh
openssl rand -base64 32
```

Install the unit and start the service:

```sh
sudo install -o root -g root -m 0644 monitor-hub.service \
    /etc/systemd/system/monitor-hub.service
sudo systemctl daemon-reload
sudo systemctl enable --now monitor-hub.service
systemctl status monitor-hub.service
```

A healthy start writes one line to the journal —
`monitor-hub <version> listening on 127.0.0.1:8080 (nodes: 2, notify: log)` — and the page
answers on the host itself: `curl -s localhost:8080/ | head`. The hub binds to loopback only,
so nothing reaches it from outside until the nginx vhost exists (see
[What is not covered yet](#what-is-not-covered-yet)).

## 3. Install a node

`install-agent.sh` reads the service definition from beside itself, so copy the whole
`deploy/` directory along with the binary:

```sh
scp -r dist/monitor-agent deploy server-b:
```

The token goes in on **stdin**, because `sudo` resets the environment by default and
`MONITOR_TOKEN` would not survive the call; as an argument it would be visible in `ps` to
every local account and would land in shell history, so the script does not accept one there.
On the node:

```sh
read -rs token        # paste the node's token; bash and zsh keep it off the screen
printf %s "$token" | sudo ./deploy/install-agent.sh \
    --binary ./monitor-agent --hub https://hub.example.com --node server-b
unset token
```

Copy the binary and `deploy/` together: the service definitions pass `--env-file`, which an
agent built before them does not know, and the install would report success on a service that
exits every time it starts.

The macOS node is the same command with its own name (`--node laptop-a`) and its own
binary — a `darwin-arm64` or `darwin-amd64` asset, verified and renamed the same way. The
script picks
systemd or launchd from the system it is running on. It prints every path it wrote and the
command that shows the service's state.

To see what a run would write without touching the machine, stage it under a prefix — this
registers no service and needs no root:

```sh
DESTDIR=/tmp/staged ./deploy/install-agent.sh \
    --binary ./monitor-agent --hub https://hub.example.com --node server-b
```

## 4. Verify

On Debian:

```sh
systemctl status monitor-agent.service
journalctl -u monitor-agent.service -f
```

On macOS, where launchd has no journal, the log is a file readable by root only:

```sh
sudo launchctl print system/io.github.pravbeseda.monitor-agent
sudo tail -f /var/log/monitor-agent.log
```

A healthy first minute: the service is active, the log opens with
`monitor-agent <version>: node server-b reporting to https://hub.example.com`, the first tick
runs immediately rather than after an interval, and no `tick failed` line repeats. Within a
base tick the node and its volumes appear on the hub's page.

Two failures look different from a service problem and are worth knowing:

- **No token in the environment file** — the agent exits at startup naming `MONITOR_TOKEN`,
  and the supervisor keeps restarting it, so the message keeps coming — every five seconds on
  Debian, every ten or so on macOS, where launchd sets the throttle. Re-run the install with
  the token.
- **A wrong token** — the service stays happily up. The hub refuses the batches, so the node
  never appears with fresh values and eventually turns silent. Check `hub.env` on the hub
  host against what you installed on the node.

## 5. Upgrade a node, rotate its token

Both are a re-run of the same script, and `--hub` and `--node` are given every time.

An upgrade — download and verify a new binary as in step 1 (or build one), copy it over, and
run without a token:

```sh
sudo ./deploy/install-agent.sh \
    --binary ./monitor-agent --hub https://hub.example.com --node server-b < /dev/null
```

`< /dev/null` is what says "no new token, keep the stored one". Without it a run whose stdin
is a pipe rather than a terminal — `ssh node 'sudo ./install-agent.sh …'`, a CI job — waits
for a token that is never coming.

A rotation is the same run with the new token piped in, exactly as in step 3.

A re-run **replaces** the binary, the service definition, `MONITOR_HUB` and `MONITOR_NODE`
from the flags, and the token when one is supplied. It **keeps** the stored token when none
is, and every other line of a hand-edited environment file. It ends by restarting the
service, so the run is done when the service is back up.

Rotating a token is two-sided: the hub reads `hub.env` when it starts, so change the node's
variable there and restart it too.

```sh
sudo systemctl restart monitor-hub.service
```

### Upgrading the hub

Section 0 is one command for this. By hand it is a new binary in place and a restart, with no
configuration to change. Verify it first as in step 1, then, from the checkout:

```sh
scp dist/monitor-hub hub.example.com:
ssh hub.example.com
sudo install -o root -g root -m 0755 monitor-hub /usr/local/bin/monitor-hub
sudo systemctl restart monitor-hub.service
```

The hub accepts measurements from an agent older than itself, so it can be upgraded on its
own ([ADR 0022](decisions/0022-updates-are-pulled.md)).

## 6. Uninstall

Deliberately not a mode of the script — it is these commands
([specs/deployment.md](specs/deployment.md#out-of-scope)).

A Debian node:

```sh
sudo systemctl disable --now monitor-agent.service
sudo rm /etc/systemd/system/monitor-agent.service /usr/local/bin/monitor-agent \
    /etc/monitor/agent.env
sudo systemctl daemon-reload
```

A macOS node:

```sh
sudo launchctl bootout system/io.github.pravbeseda.monitor-agent
sudo rm /Library/LaunchDaemons/io.github.pravbeseda.monitor-agent.plist \
    /usr/local/bin/monitor-agent /usr/local/etc/monitor/agent.env /var/log/monitor-agent.log
```

The hub host, the same way:

```sh
sudo systemctl disable --now monitor-hub.service
sudo rm /etc/systemd/system/monitor-hub.service /usr/local/bin/monitor-hub
sudo systemctl daemon-reload
```

That leaves the configuration and the measurements — `/etc/monitor/hub.yaml`,
`/etc/monitor/hub.env` and `/var/lib/monitor` — standing. Removing them is a separate,
deliberate step: the database is the whole history.

## What is not covered yet

The other half of the stage-3 bullet in [poc.md](poc.md) does not exist yet, and nothing
above works around it: the nginx vhost and TLS in front of the hub, per-node token issuance,
and authentication on the web page. Until they land, the hub is reachable on its own host
only, over loopback.

Nothing updates a machine unattended: a new version is section 0's command or step 5's, both
run by hand. The timer that would do it by itself is
[ADR 0022](decisions/0022-updates-are-pulled.md), and it is not built.
