#!/bin/sh
# Install or upgrade the monitor hub or agent from a signed release.
# Every rule here is docs/specs/installer.md.
#
#   curl -fsSL <this script> | sudo sh -s -- agent --hub https://hub.example.com --node laptop-a
#   sudo ./monitor-install.sh hub
#
# This script downloads a release, checks it against the key below and hands over to the
# installer that release carries. It installs nothing itself, and the release installs
# nothing this script has not checked.
#
# The whole body is one function, invoked on the last line, so a transfer cut short runs
# nothing rather than half of this file.

set -eu

main() {
	LC_ALL=C
	export LC_ALL
	umask 022

	program=${0##*/}

	# The public half of the release signing key. Generated from
	# deploy/release-signing-key.pub, and asserted equal to it by a test: two copies that
	# could drift are worse than one.
	release_key='-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEabB6p+jY9j7naasjBxF13XHafcaP
85hw7hBNsW6rKlL9WWTo77AkvSUsHk3FZ3/BOixI7M1fFKrjsY6Hxj/rUA==
-----END PUBLIC KEY-----'
	default_origin=https://github.com/pravbeseda/monitor

	work=
	trap 'cleanup' EXIT
	trap 'cleanup 130' INT
	trap 'cleanup 143' TERM
	trap 'cleanup 129' HUP

	role=
	version=
	allow_downgrade=0
	hub=
	node=
	parse_arguments "$@"

	check_tools
	check_staging

	origin=${MONITOR_RELEASE_ORIGIN:-$default_origin}
	[ -n "$version" ] || version=$(newest_version)
	check_version "$version"

	make_workdir
	fetch_and_check
	check_not_a_downgrade
	unpack

	printf '%s: installing %s %s\n' "$program" "$role" "$version"
	set -- "$role" --binary "$work/binary"
	[ -z "$hub" ] || set -- "$@" --hub "$hub"
	[ -z "$node" ] || set -- "$@" --node "$node"
	# The agent's token is the only thing stdin may carry, so the hub is given none: a run
	# in a pipeline would otherwise hand whatever is on that pipe to a reader expecting one.
	if [ "$role" = hub ]; then
		sh "$work/release/install.sh" "$@" </dev/null
	else
		sh "$work/release/install.sh" "$@"
	fi
}

refuse() {
	printf '%s: %s\n' "$program" "$1" >&2
	exit 1
}

usage() {
	cat <<EOF
usage: $program <hub|agent> [--version X.Y.Z] [--allow-downgrade] [--hub <url>] [--node <name>]

Downloads the newest release, checks its signature and installs from it. The agent's token is
read from MONITOR_TOKEN or from stdin, which the one-line form cannot offer.
EOF
}

refuse_with_usage() {
	printf '%s: %s\n' "$program" "$1" >&2
	usage >&2
	exit 1
}

cleanup() {
	status=${1:-$?}
	[ -z "$work" ] || rm -rf "$work"
	exit "$status"
}

parse_arguments() {
	[ $# -ge 1 ] || refuse_with_usage "no role: this installs a hub or an agent"
	case $1 in
	hub | agent)
		role=$1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) refuse_with_usage "unknown role: $1" ;;
	esac

	while [ $# -gt 0 ]; do
		case $1 in
		--version)
			if [ $# -lt 2 ] || [ -z "$2" ]; then
				refuse_with_usage "$1 needs a value"
			fi
			version=$2
			shift 2
			;;
		--hub | --node)
			if [ $# -lt 2 ] || [ -z "$2" ]; then
				refuse_with_usage "$1 needs a value"
			fi
			[ "$role" = agent ] || refuse_with_usage "$1 is the agent's; the hub takes none"
			if [ "$1" = --hub ]; then hub=$2; else node=$2; fi
			shift 2
			;;
		--allow-downgrade)
			allow_downgrade=1
			shift
			;;
		*) refuse_with_usage "unknown option: $1" ;;
		esac
	done
}

# A version names a release, and it reaches a URL and a file name, so its grammar is checked
# before it is used and is the one deploy/tag-version.sh enforces on the way in.
check_version() {
	oldifs=$IFS
	IFS=.
	set -f
	# shellcheck disable=SC2086 # deliberate: the IFS above is what splits on dots
	set -- $1
	set +f
	IFS=$oldifs
	[ $# -eq 3 ] || refuse "not a version: $version"
	for part in "$@"; do
		case $part in
		'' | *[!0-9]* | 0?*) refuse "not a version: $version" ;;
		esac
	done
}

# DESTDIR stages everything under a prefix, and the seams below exist for those runs alone: a
# real run uses the key in this file and the origin above, or it is not the release this
# project publishes.
check_staging() {
	destdir=${DESTDIR:-}
	if [ -n "$destdir" ]; then
		case $destdir in
		/*) ;;
		*) refuse "DESTDIR must be an absolute path: $destdir" ;;
		esac
		# A staged run that writes into the real system is not a staged run. `..` is refused
		# outright rather than resolved: a failed cd falls back to the raw value, and bash
		# and dash disagree about a component that does not exist.
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
		staged=1
		return 0
	fi
	staged=0
	for seam in MONITOR_RELEASE_ORIGIN MONITOR_RELEASE_KEY; do
		eval "value=\${$seam:-}"
		[ -z "$value" ] ||
			refuse "$seam is for a staged run: a real run uses the key and the origin this script carries"
	done
	[ "$(id -u)" -eq 0 ] || refuse "must run as root: re-run under sudo"
}

check_tools() {
	for tool in curl openssl tar; do
		command -v "$tool" >/dev/null 2>&1 || refuse "$tool is not installed, and this needs it"
	done
}

# curl, with the transport pinned on a real run. A staged run reaches a test server on
# loopback, which is not https and is not the internet.
get() {
	if [ "$staged" -eq 1 ]; then
		curl -fsSL --max-time 120 "$@"
	else
		curl -fsSL --max-time 120 --proto '=https' --proto-redir '=https' "$@"
	fi
}

# The newest release is whichever tag the origin's own "latest" answers with.
newest_version() {
	latest=$(get -o /dev/null -w '%{url_effective}' "$origin/releases/latest") ||
		refuse "the origin cannot be reached: $origin"
	latest=${latest##*/}
	printf '%s' "${latest#v}"
}

make_workdir() {
	# An inherited TMPDIR can name a directory somebody else owns, and a directory somebody
	# else owns can be swapped between the check and the use. A real run ignores it.
	[ "$staged" -eq 1 ] || TMPDIR=/tmp
	TMPDIR=${TMPDIR:-/tmp}
	export TMPDIR
	work=$(umask 077 && mktemp -d "${TMPDIR%/}/monitor-install.XXXXXXXX") ||
		refuse "cannot create a working directory"
}

download() {
	get -o "$work/$2" "$origin/releases/download/v$version/$1" ||
		refuse "$1 is not in release $version, or the origin cannot be reached"
}

digest_of() {
	line=$(openssl dgst -sha256 "$1" 2>/dev/null) || refuse "openssl could not read $1"
	printf '%s' "${line##* }"
}

# The digest the manifest gives one name, and nothing if it does not name it at all.
manifest_digest() {
	found=
	while read -r listed_digest listed_name || [ -n "$listed_digest" ]; do
		[ "$listed_name" = "$1" ] || continue
		found=$listed_digest
	done <"$work/SHA256SUMS"
	printf '%s' "$found"
}

check_asset() {
	[ "$(manifest_digest "$1")" = "$(digest_of "$work/$2")" ] ||
		refuse "$1 does not match the digest release $version gives it"
}

fetch_and_check() {
	key=$work/release-key.pub
	if [ "$staged" -eq 1 ] && [ -n "${MONITOR_RELEASE_KEY:-}" ]; then
		cp "$MONITOR_RELEASE_KEY" "$key"
	else
		printf '%s\n' "$release_key" >"$key"
	fi
	openssl pkey -pubin -in "$key" -noout >/dev/null 2>&1 ||
		refuse "the key this run verifies with is not a public key openssl can read"

	download SHA256SUMS SHA256SUMS
	download SHA256SUMS.sig SHA256SUMS.sig
	# The verdict is openssl's exit status and never its output: LibreSSL prints "Verified
	# OK" on runs that fail.
	openssl dgst -sha256 -verify "$key" -signature "$work/SHA256SUMS.sig" "$work/SHA256SUMS" \
		>/dev/null 2>&1 ||
		refuse "the signature of release $version does not verify with this run's key"

	platform=$(platform_name)
	binary_asset=monitor-$role-$version-$platform
	archive_asset=monitor-installer-$version.tar.gz

	# The manifest is asked first, so an asset missing from it is told apart from one the
	# origin would not serve, and neither is fetched on a guess.
	[ -n "$(manifest_digest "$archive_asset")" ] ||
		refuse "release $version carries no installer archive: it predates the installer, and the manual path of install.md is what reaches it"
	[ -n "$(manifest_digest "$binary_asset")" ] ||
		refuse "release $version lists no $binary_asset for this platform"

	download "$archive_asset" installer.tar.gz
	check_asset "$archive_asset" installer.tar.gz
	download "$binary_asset" binary
	check_asset "$binary_asset" binary
	chmod 0755 "$work/binary"
}

# uname reports the kernel's idea of the machine, and under Rosetta that is the emulated one.
platform_name() {
	case $(uname -s) in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) refuse "unsupported system: $(uname -s)" ;;
	esac
	arch=$(uname -m)
	if [ "$os" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
		arch=arm64
	fi
	case $arch in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) refuse "unsupported architecture: $arch" ;;
	esac
	printf '%s-%s' "$os" "$arch"
}

# Protection against an operator's slip, not against an attack: the version compared against
# is reported by the very binary being replaced, so a run that cannot read one installs.
check_not_a_downgrade() {
	[ "$allow_downgrade" -eq 0 ] || return 0
	installed_binary=$destdir/usr/local/bin/monitor-$role
	if [ ! -x "$installed_binary" ]; then
		return 0
	fi
	unreadable="could not tell which version is installed; installing $version over it"
	installed=$("$installed_binary" --version 2>/dev/null) || {
		printf '%s: %s\n' "$program" "$unreadable"
		return 0
	}
	installed=${installed##* }
	case $installed in
	'' | *[!0-9.]* | ??????????*)
		printf '%s: %s\n' "$program" "$unreadable"
		return 0
		;;
	esac
	newer_or_same "$version" "$installed" ||
		refuse "release $version is older than the installed $installed; --allow-downgrade installs it anyway"
}

# Component by component and numerically: 1.10.0 is newer than 1.9.0, which no text
# comparison agrees with.
newer_or_same() {
	oldifs=$IFS
	IFS=.
	set -f
	# shellcheck disable=SC2086 # deliberate: the IFS above is what splits on dots
	set -- $1 $2
	set +f
	IFS=$oldifs
	left=0
	right=0
	for index in 1 2 3; do
		eval "left=\${$index:-0}"
		eval "right=\${$((index + 3)):-0}"
		[ "$left" -lt "$right" ] && return 1
		[ "$left" -gt "$right" ] && return 0
	done
	return 0
}

# What a signature says about an archive is nothing about what unpacking it does, so the
# entries are read before any of them is written.
unpack() {
	tar -tzf "$work/installer.tar.gz" >"$work/entries" 2>/dev/null ||
		refuse "the installer archive is not readable"
	while IFS= read -r entry || [ -n "$entry" ]; do
		case $entry in
		/* | *..*) refuse "the installer archive reaches outside itself: $entry" ;;
		esac
	done <"$work/entries"
	if tar -tvzf "$work/installer.tar.gz" 2>/dev/null | grep -qv '^[-d]'; then
		refuse "the installer archive holds something that is not a file or a directory"
	fi

	mkdir "$work/unpacked"
	tar -xzf "$work/installer.tar.gz" -C "$work/unpacked" --no-same-owner --no-same-permissions ||
		refuse "the installer archive could not be unpacked"

	# One top-level directory, whatever it is called.
	entry=$(ls "$work/unpacked")
	[ -d "$work/unpacked/$entry" ] ||
		refuse "the installer archive is not one directory of files"
	ln -s "$work/unpacked/$entry" "$work/release"
	[ -f "$work/release/install.sh" ] ||
		refuse "the installer archive carries no install.sh"
}

main "$@"
