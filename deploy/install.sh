#!/bin/sh
# The entry point of the installer a release carries. It chooses the per-binary installer for
# a role and forwards what it was given; it downloads nothing and verifies nothing, because
# the half that fetched this release is the half that holds the key
# (docs/specs/installer.md#the-handover).
#
#   sh install.sh agent --binary ./monitor-agent --hub https://hub.example.com --node laptop-a
#   sh install.sh hub --binary ./monitor-hub
#
# The token, when there is one, arrives on stdin and is passed through untouched.

set -eu

LC_ALL=C
export LC_ALL

program=$(basename "$0")
source_dir=$(dirname "$0")

usage() {
	cat >&2 <<EOF
usage: $program <hub|agent> --binary <path> [--hub <url>] [--node <name>]

Runs the installer for that role out of this release. DESTDIR is inherited.
EOF
}

refuse() {
	printf '%s: %s\n' "$program" "$1" >&2
	exit 1
}

[ $# -ge 1 ] || {
	usage
	exit 1
}

role=$1
shift

case $role in
hub | agent) ;;
-h | --help)
	usage
	exit 0
	;;
*)
	printf '%s: unknown role: %s\n' "$program" "$role" >&2
	usage
	exit 1
	;;
esac

installer=$source_dir/install-$role.sh
[ -f "$installer" ] || refuse "this release carries no installer for $role: $installer"

# exec, so the run's exit status and its output are the installer's, and stdin — the token,
# or /dev/null — reaches it unchanged.
exec sh "$installer" "$@"
