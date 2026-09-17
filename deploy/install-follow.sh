#!/bin/sh
# What install-hub.sh and install-agent.sh share when they answer a follow run
# (docs/specs/installer.md#the-handover). Sourced, never run: it defines functions over the
# caller's $program, $destdir and follow options, and refuses through the caller's refuse.
# shellcheck disable=SC2154 # those variables are the sourcing installer's

# The grammar deploy/tag-version.sh enforces. A copy of the function in monitor-install.sh,
# which cannot share a file with a release; a test asserts the two are the same.
is_version() {
	# Splitting on dots drops a trailing empty field, so the dots are judged before it.
	case $1 in
	.* | *. | *..*) return 1 ;;
	esac
	oldifs=$IFS
	IFS=.
	set -f
	# shellcheck disable=SC2086 # deliberate: the IFS above is what splits on dots
	set -- $1
	set +f
	IFS=$oldifs
	[ $# -eq 3 ] || return 1
	for part in "$@"; do
		case $part in
		# A component wider than the shell's integers is not a version anyone releases, and
		# comparing one would error rather than answer.
		'' | *[!0-9]* | 0?* | ??????????*) return 1 ;;
		esac
	done
	return 0
}

# The five options of a follow run come together or not at all, and each is what it says.
check_follow_options() {
	[ -n "$follow" ] || refuse "a follow run needs --follow-target as well"
	[ -n "$release" ] || refuse "a follow run needs --release as well"
	[ -n "$newest" ] || refuse "a follow run needs --newest as well"
	[ -n "$digest" ] || refuse "a follow run needs --digest as well"
	[ -n "$answer" ] || refuse "a follow run needs --answer as well"
	is_version "$release" || refuse "--release is not a version: $release"
	is_version "$newest" || refuse "--newest is not a version: $newest"
	case $digest in
	*[!0-9a-f]*) refuse "--digest is not a SHA-256 in lowercase hexadecimal: $digest" ;;
	esac
	[ ${#digest} -eq 64 ] || refuse "--digest is not a SHA-256 in lowercase hexadecimal: $digest"
	if [ ! -f "$answer" ] || [ -s "$answer" ]; then
		refuse "--answer must name an empty file: $answer"
	fi
}

# A file that chooses what root installs — or a directory holding one — must be root's alone:
# no symlink, nothing group or other may write, and on a real run root as its owner. $1 is the
# staged path, $2 the path named in the refusal, $3 why it matters.
check_root_only() {
	[ -e "$1" ] || [ -L "$1" ] || return 0
	[ ! -L "$1" ] || refuse "$2 is a symlink, and $3"
	[ -z "$(find "$1" -maxdepth 0 \( -perm -g+w -o -perm -o+w \))" ] ||
		refuse "$2 is writable by group or other, and $3"
	if [ -z "$destdir" ] && [ -z "$(find "$1" -maxdepth 0 -user root)" ]; then
		refuse "$2 is not owned by root, and $3"
	fi
}

# The version a target names — latest resolved to --newest — in $wanted, or a failure for
# anything but latest or one version.
# shellcheck disable=SC2034 # $wanted is the sourcing installer's to read
resolve_target() {
	case $1 in
	latest) wanted=$newest ;;
	*)
		is_version "$1" || return 1
		wanted=$1
		;;
	esac
}

# The SHA-256 of a file, or nothing when it cannot be read.
digest_of() {
	line=$(openssl dgst -sha256 "$1" 2>/dev/null) || return 0
	printf '%s' "${line##* }"
}

# Whether the systemd unit $1 runs a process whose executable has the release's digest: a binary
# replaced under a running service leaves the old bytes behind /proc/<pid>/exe.
systemd_runs_release() {
	pid=$(systemctl show -p MainPID --value "$1" 2>/dev/null) || return 1
	[ "${pid:-0}" != 0 ] || return 1
	[ "$(digest_of "/proc/$pid/exe")" = "$digest" ]
}

# Whether the binary in place at $1 is the release handed over, as the layout installs it: a
# regular file of that digest, mode 0755 and, on a real run, root's.
binary_is_release() {
	[ ! -L "$destdir$1" ] || return 1
	[ -f "$destdir$1" ] || return 1
	[ "$(digest_of "$destdir$1")" = "$digest" ] || return 1
	[ -z "$(find "$destdir$1" -maxdepth 0 ! -perm 0755)" ] || return 1
	[ -n "$destdir" ] || [ -n "$(find "$1" -maxdepth 0 -user root)" ]
}

# The answer of a follow run once $wanted is known, the same for both roles
# (installer.md#the-handover): another release is named; this release already running ends the
# run with nothing to do; a binary in place that is the release's is kept; without one the
# release's own version asks for its binary. It returns only when the run goes on to install.
# $1 is the binary's path in the layout, $2 what named the target, and already_running is the
# caller's.
answer_follow() {
	if [ "$wanted" != "$release" ]; then
		printf '%s\n' "$wanted" >"$answer"
		printf '%s: %s names %s, not this release %s; nothing is installed from it\n' \
			"$program" "$2" "$wanted" "$release"
		exit 0
	fi
	if binary_is_release "$1"; then
		if already_running; then
			printf '%s: %s %s is already installed; nothing to do\n' "$program" "${1##*/}" "$release"
			exit 0
		fi
		[ -n "$binary" ] ||
			printf '%s: %s %s is in place; keeping the binary in place and installing the rest\n' \
				"$program" "${1##*/}" "$release"
		return 0
	fi
	[ -z "$binary" ] || return 0
	printf '%s\n' "$release" >"$answer"
	printf '%s: %s names this release %s; asking for its binary\n' "$program" "$2" "$release"
	exit 0
}
