#!/bin/sh
# Install the monitor hub as a system service. Every path, mode and refusal here is
# docs/specs/deployment.md and docs/specs/installer.md; nothing is configurable, no binary is
# built, and no configuration is invented.
#
#   sudo ./install-hub.sh --binary ./monitor-hub
#
# The hub needs /etc/monitor/hub.yaml and /etc/monitor/hub.env, which describe one
# installation and therefore have no defaults (ADR 0007). A run that does not find them
# installs everything else, writes the examples beside them and stops short of starting the
# service, naming what is missing.

set -eu

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

# The temporary of the write in flight, removed however the run ends.
temp=

on_exit() {
	status=${1:-$?}
	[ -z "$temp" ] || rm -f "$temp"
	if [ "$status" -ne 0 ] && [ -n "$written" ]; then
		printf '%s: stopped after writing:\n%s' "$program" "$written" >&2
		written=
	fi
	exit "$status"
}
trap 'on_exit' EXIT
trap 'on_exit 130' INT
trap 'on_exit 143' TERM
trap 'on_exit 129' HUP

usage() {
	cat <<EOF
usage: $program --binary <path>

Installs the hub binary, its service definition and the example configuration. The real
configuration is the operator's: a run that does not find it leaves the service stopped.

DESTDIR stages the whole installation under a prefix and registers no service.
EOF
}

refuse() {
	printf '%s: %s\n' "$program" "$1" >&2
	exit 1
}

refuse_with_usage() {
	printf '%s: %s\n' "$program" "$1" >&2
	usage >&2
	exit 1
}

binary=
while [ $# -gt 0 ]; do
	case $1 in
	--binary)
		[ "$#" -ge 2 ] || refuse "$1 needs a value"
		binary=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		refuse_with_usage "unknown option: $1"
		;;
	esac
done

[ -n "$binary" ] || refuse "--binary is required"
[ -f "$binary" ] || refuse "--binary names no file: $binary"
[ -x "$binary" ] || refuse "--binary names a file that is not executable: $binary"

# DESTDIR stages the whole installation under a prefix: no root, no account, no service. A
# value that stages nothing is refused rather than quietly writing into the real system.
destdir=${DESTDIR:-}
if [ -n "$destdir" ]; then
	case $destdir in
	/*) ;;
	*) refuse "DESTDIR must be an absolute path: $destdir" ;;
	esac
	# `..` is refused outright rather than resolved: a failed cd falls back to the raw value,
	# and bash and dash disagree about a component that does not exist.
	case /$destdir/ in
	*/../*) refuse "DESTDIR must not contain ..: $destdir" ;;
	esac
	resolved=$(cd "$destdir" 2>/dev/null && pwd -P) || resolved=$destdir
	# A path of exactly two slashes is its own thing on some systems, so the trailing ones
	# are stripped rather than compared against a single "/".
	while [ "$resolved" != "${resolved%/}" ]; do
		resolved=${resolved%/}
	done
	[ -n "$resolved" ] || refuse "DESTDIR stages nothing, it resolves to /: $destdir"
else
	[ "$(id -u)" -eq 0 ] || refuse "must run as root: re-run under sudo"
	command -v systemctl >/dev/null 2>&1 ||
		refuse "this host has no systemd; the hub installs on Debian (ADR 0005)"
fi

binary_file=/usr/local/bin/monitor-hub
config_dir=/etc/monitor
config_file=$config_dir/hub.yaml
env_file=$config_dir/hub.env
data_dir=/var/lib/monitor
service_file=/etc/systemd/system/monitor-hub.service
service_source=$source_dir/systemd/monitor-hub.service
account=monitor

[ -f "$service_source" ] || refuse "the service definition is missing: $service_source"

# The examples travel beside this script inside a release; in a checkout the hub's own is the
# repository's config.example.yaml, which ADR 0007 names and which nothing else may rename.
yaml_example=$source_dir/hub.yaml.example
[ -f "$yaml_example" ] || yaml_example=$source_dir/../config.example.yaml
env_example=$source_dir/hub.env.example
for example in "$yaml_example" "$env_example"; do
	[ -f "$example" ] || refuse "the example configuration is missing: $example"
done

# A directory the layout owns must not be a symlink somebody else planted, and — once the
# account exists to compare against — must belong to whoever the layout names. An empty owner
# checks the symlink alone, which is all that can be checked before the account is made.
check_dir() {
	[ ! -L "$1" ] || refuse "a directory the layout owns is a symlink: $1"
	# `find -user` is the ownership test both BSD and GNU have; `stat` is not.
	if [ -n "$2" ] && [ -z "$destdir" ] && [ -d "$1" ] &&
		[ -z "$(find "$1" -maxdepth 0 -user "$2")" ]; then
		refuse "$1 is not owned by $2"
	fi
}

create_temp() {
	mkdir -p "$(dirname "$1")"
	temp=$(mktemp "$1.XXXXXXXX")
}

# Copy one file into place with the mode the layout table gives it. The temporary and the
# rename are what let a running hub's binary be replaced under it.
install_file() {
	create_temp "$3"
	cp "$1" "$temp"
	chmod "$2" "$temp"
	mv "$temp" "$3"
	temp=
	written="$written  $3
"
}

# Before the account exists there is nothing to compare a fresh directory against, so only a
# directory that is already there is judged by its owner.
account_or_nothing=
[ ! -d "$destdir$data_dir" ] || account_or_nothing=$account
check_dir "$destdir$config_dir" root
check_dir "$destdir$data_dir" "$account_or_nothing"

# The account owns the secrets and the database, so an account that can log in is not the
# system account this layout means (ADR 0019).
if [ -z "$destdir" ]; then
	if id "$account" >/dev/null 2>&1; then
		shell=$(getent passwd "$account" | cut -d: -f7)
		case $shell in
		*/nologin | */false) ;;
		*) refuse "the $account account can log in ($shell); the hub's secrets are not for a login account" ;;
		esac
	elif command -v adduser >/dev/null 2>&1; then
		adduser --system --group --no-create-home "$account" >/dev/null
	else
		useradd --system --no-create-home --shell /usr/sbin/nologin "$account"
	fi
fi

mkdir -p "$destdir$config_dir" "$destdir$data_dir"
# Again, because the account that owns the parent could have planted a symlink between the
# check above and this line. The owner is not re-checked: root has just made the directory,
# and the chown below is what gives it away.
check_dir "$destdir$config_dir" root
check_dir "$destdir$data_dir" ""
if [ -z "$destdir" ]; then
	chown "$account:$account" "$destdir$data_dir"
	chmod 0700 "$destdir$data_dir"
fi

install_file "$binary" 0755 "$destdir$binary_file"
install_file "$service_source" 0644 "$destdir$service_file"
install_file "$yaml_example" 0644 "$destdir$config_dir/hub.yaml.example"
install_file "$env_example" 0600 "$destdir$config_dir/hub.env.example"

# A configuration file the operator copied as root keeps root's ownership and the umask's
# mode — a world-readable file of node tokens. The layout's values are restored rather than
# reported, because an install that leaves them wrong is the install that leaks them.
corrected=
fix_mode() {
	[ -f "$1" ] || return 0
	# -perm with a leading minus is "at least these bits"; the test that matters is "exactly
	# these", which both finds spell as the plain mode.
	if [ -z "$(find "$1" -maxdepth 0 -perm "$2")" ]; then
		chmod "$2" "$1"
		corrected="$corrected  $1 (mode)
"
	fi
	if [ -z "$destdir" ] && [ -z "$(find "$1" -maxdepth 0 -user "$account")" ]; then
		chown "$account:$account" "$1"
		corrected="$corrected  $1 (owner)
"
	fi
}
fix_mode "$destdir$config_file" 0640
fix_mode "$destdir$env_file" 0600

missing=
for file in "$destdir$config_file" "$destdir$env_file"; do
	[ -f "$file" ] || missing="$missing  ${file#"$destdir"}
"
done

printf '%s: installed\n%s' "$program" "$written"
[ -z "$corrected" ] || printf 'Corrected:\n%s' "$corrected"

# The unit file was replaced, so systemd is told either way; enabling it waits until there is
# a configuration, or a reboot would start a service that cannot run and restart it for ever.
[ -n "$destdir" ] || systemctl daemon-reload

if [ -n "$missing" ]; then
	printf 'The hub will not start until these exist:\n%s' "$missing"
	printf 'Copy the examples beside them and fill them in:\n'
	printf '  sudo install -o %s -g %s -m 0640 %s.example %s\n' \
		"$account" "$account" "$config_file" "$config_file"
	printf '  sudo install -o %s -g %s -m 0600 %s.example %s\n' \
		"$account" "$account" "$env_file" "$env_file"
	printf 'Then run this script again.\n'
elif [ -z "$destdir" ]; then
	systemctl enable monitor-hub.service
	systemctl restart monitor-hub.service
fi

if [ -n "$destdir" ]; then
	printf 'Staged under %s: no account was created and no service was registered.\n' "$destdir"
fi
printf 'The service state: systemctl status monitor-hub.service\n'
