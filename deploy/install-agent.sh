#!/bin/sh
# Install the monitor agent as a system service. Every path, mode and refusal here is
# docs/specs/deployment.md; nothing is configurable and no binary is built.
#
#   printf %s "$token" | sudo ./install-agent.sh --binary ./monitor-agent \
#       --hub https://hub.example.com --node laptop-a
#
# The token comes from MONITOR_TOKEN or from stdin, never from an argument: arguments are
# visible in `ps` to every local account and land in shell history.
#
# A follow run (docs/specs/installer.md#answering-a-follow-run-for-the-agent) takes the hub,
# the node and the token from the installed environment file instead, and asks that hub which
# version this node follows.

set -eu

# Byte-wise ranges below, and the same answer from find and mktemp whatever the operator's
# locale is.
LC_ALL=C
export LC_ALL

# Every file mode below is set explicitly, but a directory's is not: without this, a caller
# whose umask is 0 would leave /etc/monitor writable by anyone.
umask 022

program=$(basename "$0")
source_dir=$(dirname "$0")

# What has already reached the disk. A run that fails after its first write stops there and
# names this list; it neither continues nor rolls back (deployment.md#invariants).
written=

# The temporary of the write in flight. A run that dies between creating it and the rename
# would otherwise leave it behind under a name nothing reclaims, and a write_env_file
# temporary holds the token.
temp=

# The directory the hub's answer lands in during a follow run.
reply=

on_exit() {
	status=$?
	[ -z "$temp" ] || rm -f "$temp"
	[ -z "$reply" ] || rm -rf "$reply"
	if [ "$status" -ne 0 ] && [ -n "$written" ]; then
		printf '%s: stopped after writing:\n%s' "$program" "$written" >&2
	fi
	exit "$status"
}
trap on_exit EXIT

usage() {
	cat >&2 <<EOF
usage: $program --binary <path> --hub <url> --node <name>
       $program --follow-target --release X.Y.Z --newest X.Y.Z --digest <sha256> --answer <file>
           [--binary <path>]

The token is read from MONITOR_TOKEN, or from stdin when that is unset — the route that
survives sudo resetting the environment:

  printf %s "\$token" | sudo ./$program --binary ./monitor-agent \\
      --hub https://hub.example.com --node laptop-a

A follow run reads MONITOR_HUB, MONITOR_NODE and MONITOR_TOKEN from the installed environment
file and asks that hub for this node's target. When it names another release it writes that
version into the answer file; when it names this one it does nothing for an agent already
running it, installs around a binary in place of that digest, installs the binary it was
given, or writes this release's version to ask for it.

DESTDIR stages the whole installation under a prefix and registers no service.
EOF
}

refuse() {
	printf '%s: %s\n' "$program" "$1" >&2
	exit 1
}

newline='
'
carriage_return=$(printf '\r')

# Blanks around a line, around a key and around a value are what systemd's EnvironmentFile
# and the agent's own parser both ignore, so this reader ignores them too. A carriage return
# counts as one: the agent ends a line at a lone \r (ADR 0020), so a file saved with CRLF
# line endings holds the same values to it, and must to this reader as well.
trim() {
	trimmed=$1
	while :; do
		case $trimmed in
		" "* | "	"* | "$carriage_return"*) trimmed=${trimmed#?} ;;
		*" " | *"	" | *"$carriage_return") trimmed=${trimmed%?} ;;
		*) return 0 ;;
		esac
	done
}

# Strip one matching pair of surrounding quotes, as the agent's parser does: the quotes say
# where the value ends, nothing more.
unquote() {
	unquoted=$1
	case $unquoted in
	"'"*"'" | '"'*'"')
		unquoted=${unquoted#?}
		unquoted=${unquoted%?}
		;;
	# A quote that opens and never closes is what the agent refuses the whole file over, so it
	# is not a value here either.
	"'"* | '"'*) return 1 ;;
	esac
}

# The names the agent accepts as keys: a leading digit or a stray character makes it refuse
# the file, `export MONITOR_TOKEN=…` included.
is_variable_name() {
	case $1 in
	"" | [0-9]* | *[!A-Za-z0-9_]*) return 1 ;;
	esac
}

# Split one line of the environment file into $key and $value by the agent's own rules
# (ADR 0020), and fail for a line that assigns nothing. A line the agent reads as a key has
# to be a line this script recognises and rewrites in place: one it read more narrowly would
# leave a hand-written token invisible on a re-run, and a revoked one on disk under a second
# MONITOR_TOKEN after a rotation (deployment.md#edge-cases).
env_line() {
	trim "$1"
	case $trimmed in
	*=*) assignment=$trimmed ;;
	*) return 1 ;;
	esac
	trim "${assignment%%=*}"
	key=$trimmed
	is_variable_name "$key" || return 1
	trim "${assignment#*=}"
	unquote "$trimmed" || return 1
	value=$unquoted
}

# The agent refuses the whole file over one line it cannot read (ADR 0020), and this script
# preserves the lines it does not own — so a file carrying such a line is refused before
# anything is written. Without this, a re-run over a hand-edited file printed "installed",
# restarted the service, and left a node the agent will not start: the outcome the value
# checks exist to prevent, reached from the other side.
check_env_file() {
	[ -f "$1" ] || return 0
	number=0
	line=
	while IFS= read -r line || [ -n "$line" ]; do
		number=$((number + 1))
		trim "$line"
		# The agent ends a line at a lone carriage return, so one inside a line — a comment's
		# included — leaves a second line behind it that the agent reads on its own (ADR 0020).
		case $trimmed in
		*"$carriage_return"*) refuse "$1 line $number: the agent reads a carriage return inside it as a second line" ;;
		"" | "#"*) continue ;;
		esac
		env_line "$line" ||
			refuse "$1 line $number: the agent reads this file, and this line is not KEY=VALUE it accepts"
	done <"$1"
}

# The environment file's directory holds the token, and nothing but this script creates it,
# so requiring root to own it costs a legitimate installation nothing and closes both holes in
# a directory an unprivileged account owns: a MONITOR_TOKEN line planted there for a re-run to
# adopt, and a name planted there for root to write the token through. A symlink is such a
# name, and an absent directory is where one is planted, so this runs again once the directory
# exists — the check before the first write has nothing to look at while it does not.
# `find -user` is the ownership test both BSD and GNU have; `stat` is not.
check_env_dir() {
	[ ! -L "$1" ] || refuse "the environment file's directory is a symlink: $1"
	if [ -e "$1" ] && [ -z "$(find "$1" -maxdepth 0 -user root)" ]; then
		refuse "the environment file's directory is not owned by root: $1"
	fi
}

# Read one value out of the environment file without executing a line of it: the file holds
# a pasted secret, and POSIX `.` would run it (ADR 0020).
stored_value() {
	stored=
	line=
	if [ -f "$1" ]; then
		while IFS= read -r line || [ -n "$line" ]; do
			if env_line "$line" && [ "$key" = "$2" ]; then
				stored=$value
			fi
		done <"$1"
	fi
}

# Create an empty file at a path nothing else may have prepared. Unlinking first and then
# refusing to clobber (`set -C` opens with O_EXCL) is what stops a name another account
# planted from being written through: a symlink there would make root write into that
# account's file.
create_file() {
	rm -f "$1"
	(
		umask "$2"
		set -C
		: >"$1"
	)
}

# A temporary next to $1, under a name nothing can predict, in $temp. Every write after the
# creation re-opens the path by name and would follow a symlink dropped there in between, and
# the directory need not be root's — on a Homebrew Mac /usr/local/etc is not. mktemp creates
# it 0600, which is the environment file's own mode; a caller that needs another one chmods
# before the rename.
create_temp() {
	mkdir -p "$(dirname "$1")"
	temp=$(mktemp "$1.XXXXXXXX")
}

# Copy one file into place with the mode the layout table gives it. The temporary and the
# rename are what let a running agent's binary be replaced under it.
install_file() {
	create_temp "$3"
	cp "$1" "$temp"
	chmod "$2" "$temp"
	mv "$temp" "$3"
	written="$written  $3
"
}

# Rewrite MONITOR_HUB, MONITOR_NODE and MONITOR_TOKEN, and leave every other line of an
# existing file alone — a file edited by hand is supported (deployment.md#edge-cases).
write_env_file() {
	create_temp "$1"

	has_hub=0
	has_node=0
	has_token=0
	line=
	if [ -f "$1" ]; then
		while IFS= read -r line || [ -n "$line" ]; do
			if env_line "$line"; then
				case $key in
				MONITOR_HUB)
					line="MONITOR_HUB=$hub"
					has_hub=1
					;;
				MONITOR_NODE)
					line="MONITOR_NODE=$node"
					has_node=1
					;;
				MONITOR_TOKEN)
					line="MONITOR_TOKEN=$token"
					has_token=1
					;;
				esac
			fi
			printf '%s\n' "$line"
		done <"$1" >>"$temp"
	fi
	[ "$has_hub" -eq 1 ] || printf 'MONITOR_HUB=%s\n' "$hub" >>"$temp"
	[ "$has_node" -eq 1 ] || printf 'MONITOR_NODE=%s\n' "$node" >>"$temp"
	[ "$has_token" -eq 1 ] || printf 'MONITOR_TOKEN=%s\n' "$token" >>"$temp"

	mv "$temp" "$1"
	written="$written  $1
"
}

binary=
hub=
node=
token=
follow=
release=
newest=
digest=
answer=

while [ "$#" -gt 0 ]; do
	case $1 in
	--binary)
		[ "$#" -ge 2 ] || refuse "$1 needs a value"
		binary=$2
		shift 2
		;;
	--release | --newest | --digest | --answer)
		[ "$#" -ge 2 ] || refuse "$1 needs a value"
		case $1 in
		--release) release=$2 ;;
		--newest) newest=$2 ;;
		--digest) digest=$2 ;;
		--answer) answer=$2 ;;
		esac
		shift 2
		;;
	--follow-target)
		follow=1
		shift
		;;
	--hub)
		[ "$#" -ge 2 ] || refuse "$1 needs a value"
		hub=$2
		shift 2
		;;
	--node)
		[ "$#" -ge 2 ] || refuse "$1 needs a value"
		node=$2
		shift 2
		;;
	*)
		printf '%s: unknown option: %s\n' "$program" "$1" >&2
		usage
		exit 1
		;;
	esac
done

# Everything below refuses before the first write, so a rejected run leaves the node exactly
# as it was (deployment.md#refusing).
if [ -n "$follow$release$newest$digest$answer" ]; then
	# A follow run comes out of a release, which carries this file beside the script.
	[ -f "$source_dir/install-follow.sh" ] ||
		refuse "a follow run needs install-follow.sh beside this script: $source_dir/install-follow.sh"
	# shellcheck source=deploy/install-follow.sh
	. "$source_dir/install-follow.sh"
	check_follow_options
	[ -z "$hub" ] || refuse "--hub is read from the environment file in a follow run"
	[ -z "$node" ] || refuse "--node is read from the environment file in a follow run"
else
	[ -n "$binary" ] || refuse "--binary is required"
	[ -n "$hub" ] || refuse "--hub is required"
	[ -n "$node" ] || refuse "--node is required"
fi
if [ -n "$binary" ]; then
	[ -f "$binary" ] || refuse "--binary names no file: $binary"
	[ -x "$binary" ] || refuse "--binary names a file that is not executable: $binary"
fi

binary_file=/usr/local/bin/monitor-agent
case $(uname -s) in
Linux)
	env_file=/etc/monitor/agent.env
	service_source=$source_dir/systemd/monitor-agent.service
	service_file=/etc/systemd/system/monitor-agent.service
	log_file=
	init_tool=systemctl
	service_label=monitor-agent.service
	status_command='systemctl status monitor-agent.service'
	;;
Darwin)
	env_file=/usr/local/etc/monitor/agent.env
	service_source=$source_dir/launchd/io.github.pravbeseda.monitor-agent.plist
	service_file=/Library/LaunchDaemons/io.github.pravbeseda.monitor-agent.plist
	# launchd creates a missing StandardOutPath world-readable on first start, so the log is
	# a file this install owns (deployment.md#where-things-live).
	log_file=/var/log/monitor-agent.log
	init_tool=launchctl
	service_label=io.github.pravbeseda.monitor-agent
	status_command='launchctl print system/io.github.pravbeseda.monitor-agent'
	;;
*)
	refuse "unsupported system: the agent installs on Debian (systemd) and macOS (launchd)"
	;;
esac
[ -f "$service_source" ] || refuse "the service definition is missing: $service_source"

# DESTDIR stages the whole installation under a prefix: no root, no init system, no service
# command (deployment.md#staged-installs).
destdir=${DESTDIR:-}
if [ -z "$destdir" ]; then
	[ "$(id -u)" -eq 0 ] || refuse "must run as root: re-run under sudo"
	env_dir=$(dirname "$env_file")
	check_env_dir "$env_dir"
	command -v "$init_tool" >/dev/null 2>&1 ||
		refuse "this host has neither systemd nor launchd; the agent installs on Debian and macOS"
fi

# The environment file of a follow run names the hub that chooses what root installs, so it and
# its directory are root's alone before a line of it is read.
if [ -n "$follow" ]; then
	why="it names the hub that chooses what root installs"
	check_root_only "$destdir$(dirname "$env_file")" "$(dirname "$env_file")" "$why"
	check_root_only "$destdir$env_file" "$env_file" "$why"
	[ -f "$destdir$env_file" ] ||
		refuse "there is no $env_file: a follow run upgrades an agent installed by hand first"
fi

check_env_file "$destdir$env_file"

# A value the agent's parser cannot read back as itself is refused before anything is written,
# and in a follow run before the hub is asked. The messages name no value: one of them is the token.
check_values() {
	for checked in "$hub" "$node" "$token"; do
		# The environment file is one KEY=VALUE per line, and a lone carriage return ends a line
		# for the agent too (ADR 0020), so either one would write a second line the agent reads as
		# configuration — a hub URL the operator never passed, on a node that reports there
		# silently.
		case $checked in
		*"$newline"* | *"$carriage_return"*)
			refuse "a value contains a line break, and the environment file is one KEY=VALUE per line"
			;;
		esac
		# Go's TrimSpace, which the agent trims with, knows whitespace this script's trim does not
		# — a non-breaking space, a vertical tab, a line separator. Rather than grow a second
		# parser to match it (ADR 0020), refuse anything outside printable ASCII: a token, a URL
		# and a node name are ASCII, and a value pasted with an invisible character would install
		# as one string and be read back as another, on a node that authenticates nowhere.
		case $checked in
		*[!\ -~]*)
			refuse "a value holds a character outside printable ASCII, and only ASCII is carried the same way by both readers"
			;;
		esac
		# A quote that never closes is not read back differently — it makes the agent refuse the
		# whole file, so the install would report success on a node that never starts again. The
		# blanks come off first: the agent trims before it looks for the quote, and a quote hiding
		# behind a space walked through the check that did not.
		trim "$checked"
		case $trimmed in
		"'"*"'" | '"'*'"') ;;
		"'"* | '"'*)
			refuse "a value opens with a quote it does not close, which makes the agent refuse the whole file"
			;;
		esac
		# Everything else the agent does to a value on the way back — trimming the blanks around
		# it, stripping one pair of quotes — this file writes verbatim. Where that round trip is
		# not the identity the node would run with a value nobody typed, and a quote that never
		# closes makes the agent refuse the whole file, leaving an install that reports success on
		# a node that never starts again.
		if ! env_line "MONITOR_VALUE=$checked" || [ "$value" != "$checked" ]; then
			refuse "a value the agent would read back as something else: it is padded with blanks, or quoted"
		fi
	done
}

# Whether the token may be sent to a hub address: over https, or in clear to this host's own
# loopback, which is how an agent on the hub's host reaches it.
is_trusted_hub() {
	case $1 in
	https://*) return 0 ;;
	http://*) authority=${1#http://} ;;
	*) return 1 ;;
	esac
	authority=${authority%%/*}
	for host in 127.0.0.1 localhost '[::1]'; do
		case $authority in
		"$host") return 0 ;;
		"$host":*)
			case ${authority#"$host":} in
			'' | *[!0-9]*) return 1 ;;
			*) return 0 ;;
			esac
			;;
		esac
	done
	return 1
}

# Ask the hub which version this node follows, into $wanted, or end the run when it names none.
# The token reaches curl on a pipe from printf, a builtin, so no argument or environment holds it.
# -q comes first so that no .curlrc of whoever ran sudo can trace, proxy or redirect the token;
# --max-filesize refuses a body announced as larger than any target, and the size check below
# refuses one that was not announced.
ask_hub() {
	reply=$(umask 077 && mktemp -d "${TMPDIR:-/tmp}/monitor-agent-target.XXXXXXXX")
	fetched=0
	code=$(printf 'Authorization: Bearer %s\n' "$token" |
		curl -q -sS --max-time 30 --max-filesize 64 -H @- -D "$reply/headers" -o "$reply/body" \
			-w '%{http_code}' "${hub%/}/api/v1/agent/target") || fetched=$?
	# 63 is curl cutting off a body over --max-filesize, whatever the status: a proxy's error
	# page is judged by its status below, and only a 200 that long is no target.
	if [ "$fetched" -eq 63 ] && [ "$code" = 200 ]; then
		refuse "the hub's answer for this node is not a target"
	fi
	[ "$fetched" -eq 0 ] || [ "$fetched" -eq 63 ] ||
		refuse "the hub cannot be reached for this node's target: $hub"
	case $code in
	200) ;;
	204)
		printf '%s: the hub names no target for %s; nothing is installed\n' "$program" "$node"
		exit 0
		;;
	401)
		if grep -qi '^www-authenticate:' "$reply/headers"; then
			refuse "a proxy in front of the hub asked for its own credential: /api/v1/agent/ does not pass through it"
		fi
		refuse "the hub refused this node's token"
		;;
	*) refuse "the hub answered $code for this node's target; nothing is installed" ;;
	esac
	# The longest version is 29 characters and a newline, so the size is judged before anything
	# is read; the line count refuses a second line the substitution would drop.
	if [ "$(wc -c <"$reply/body")" -gt 30 ] || [ "$(wc -l <"$reply/body")" -gt 1 ] ||
		! resolve_target "$(cat "$reply/body")"; then
		refuse "the hub's answer for this node is not a target"
	fi
}

# Whether, with binary_is_release already true, the service definition is the release's and on
# a real run the service runs the binary in place rather than one it replaced.
already_running() {
	cmp -s "$service_source" "$destdir$service_file" || return 1
	[ -z "$destdir" ] || return 0
	case $init_tool in
	systemctl) systemd_runs_release "$service_label" ;;
	launchctl)
		# macOS has no /proc: the process runs the binary in place when lsof reports the inode
		# that path has now, not the one a rename replaced.
		pid=$(launchctl print "system/$service_label" 2>/dev/null | awk '$1 == "pid" { print $3 }')
		[ -n "$pid" ] || return 1
		running=$(lsof -a -p "$pid" -d txt -F in 2>/dev/null |
			awk -v path="$binary_file" '/^i/ { inode = substr($0, 2) } /^n/ && substr($0, 2) == path { print inode }')
		[ -n "$running" ] && [ -n "$(find "$binary_file" -maxdepth 0 -inum "$running")" ]
		;;
	esac
}

# A follow run decides from the hub's answer before anything is written: it answers with another
# version, finds nothing to change, or goes on to install with the values it read.
if [ -n "$follow" ]; then
	# stored_value parses through env_line, which sets $key, so the loop names its own.
	for setting in MONITOR_HUB MONITOR_NODE MONITOR_TOKEN; do
		stored_value "$destdir$env_file" "$setting"
		[ -n "$stored" ] || refuse "$env_file holds no $setting, which a follow run installs with"
		case $setting in
		MONITOR_HUB) hub=$stored ;;
		MONITOR_NODE) node=$stored ;;
		MONITOR_TOKEN) token=$stored ;;
		esac
	done
	# The file's token is the one the hub knows for this node; nothing else is used.
	unset MONITOR_TOKEN
	check_values
	is_trusted_hub "$hub" ||
		refuse "MONITOR_HUB in $env_file is neither https nor loopback, and whoever answers in clear chooses what root installs"

	ask_hub
	answer_follow "$binary_file" "the hub at $hub"
fi

token=${token:-${MONITOR_TOKEN:-}}
unset MONITOR_TOKEN # no child process needs it in its environment
if [ -z "$token" ] && [ ! -t 0 ]; then
	# All of what was piped in, not its first line: `read` would store the first line of a
	# multi-line paste and leave the rest unseen, so a truncated token installed and the run
	# reported success. The substitution drops the trailing newline a pipe usually ends with;
	# a newline inside the value survives it, for the check below to refuse.
	token=$(cat)
fi
if [ -z "$token" ]; then
	# A token already installed and not supplied again is kept: writing an empty one over a
	# working one would break a node during a routine upgrade (deployment.md#re-running).
	stored_value "$destdir$env_file" MONITOR_TOKEN
	token=$stored
fi
[ -n "$token" ] || refuse "no token: set MONITOR_TOKEN or pipe the token in on stdin"

check_values

[ -z "$binary" ] || install_file "$binary" 0755 "$destdir$binary_file"
if [ -z "$destdir" ]; then
	# Created here and checked again: between the refusal above and this line an account that
	# owns the parent could have planted the symlink that check exists to catch.
	mkdir -p "$env_dir"
	check_env_dir "$env_dir"
fi
write_env_file "$destdir$env_file"
install_file "$service_source" 0644 "$destdir$service_file"
if [ -n "$log_file" ]; then
	mkdir -p "$(dirname "$destdir$log_file")"
	# An existing log is kept; a symlink standing where it should be is not a log.
	if [ ! -e "$destdir$log_file" ] || [ -L "$destdir$log_file" ]; then
		create_file "$destdir$log_file" 077
	fi
	chmod 0600 "$destdir$log_file"
	written="$written  $destdir$log_file
"
fi

if [ -z "$destdir" ]; then
	case $init_tool in
	systemctl)
		systemctl daemon-reload
		systemctl enable monitor-agent.service
		systemctl restart monitor-agent.service
		;;
	launchctl)
		launchctl bootout "system/$service_label" 2>/dev/null || :
		# `launchctl disable` writes an override that outlives bootout and a reboot, so a
		# label disabled once would bootstrap and never run. Clearing it is best effort:
		# there is nothing to clear on a label that was never disabled.
		launchctl enable "system/$service_label" 2>/dev/null || :
		# bootout returns before the daemon has finished going away, and bootstrap fails
		# while it is still there. Retrying is what makes an upgrade reliable.
		attempt=1
		while ! launchctl bootstrap system "$service_file" 2>/dev/null; do
			[ "$attempt" -lt 20 ] ||
				refuse "launchctl bootstrap failed and the agent is not running"
			attempt=$((attempt + 1))
			sleep 0.5
		done
		;;
	esac
fi

printf '%s: installed\n%s' "$program" "$written"
if [ -n "$destdir" ]; then
	printf 'Staged under %s: no service was registered.\n' "$destdir"
fi
printf 'The service state: %s\n' "$status_command"
