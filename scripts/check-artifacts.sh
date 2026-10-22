#!/bin/sh
# Fails when a change adds or modifies a built executable (Mach-O, ELF, PE) or
# a large binary file. It looks only at what the change touches, never at
# history: the files that differ from the base, plus anything staged, edited or
# untracked in the working tree, so it also catches a build artifact before it
# is committed.
#
# Usage: scripts/check-artifacts.sh [base]
#
# base is the commit the change starts from. When it is not given, it is
# $GUARD_BASE (CI sets this to the pull request's base, or to the commit
# before a push). A base that is empty, all zeros or unknown to this clone
# (a new branch, a shallow clone) falls back to HEAD~1, and a repository with
# one commit checks that commit; the working tree is always checked as well.
#
# A file is a large binary when it is over $GUARD_MAX_BYTES (default
# 1048576, 1 MiB) and has a NUL byte in its first 8 KiB. Large text, such as a
# minified bundle, is fine. A path listed in scripts/artifact-allowlist.txt
# is skipped; each line is a path or a glob, then " # why". The allowlist is
# part of the change, so adding to it is reviewed like any other line.
set -eu
cd "$(git rev-parse --show-toplevel)"

max=${GUARD_MAX_BYTES:-1048576}
allowlist=scripts/artifact-allowlist.txt
base=${1:-${GUARD_BASE:-}}

if [ -n "$base" ] && ! expr "$base" : '0*$' >/dev/null && git rev-parse -q --verify "$base^{commit}" >/dev/null; then
	range="$base...HEAD"
elif git rev-parse -q --verify HEAD~1 >/dev/null; then
	range="HEAD~1...HEAD"
else
	range=""
fi

list=$(mktemp)
trap 'rm -f "$list"' EXIT
{
	if [ -n "$range" ]; then
		git diff --name-only -z --diff-filter=AM "$range"
	else
		git ls-tree -r -z --name-only HEAD 2>/dev/null || true
	fi
	git diff --name-only -z --diff-filter=AM HEAD 2>/dev/null || git diff --name-only -z --diff-filter=AM --cached
	git ls-files -z --others --exclude-standard
} | tr '\0' '\n' | sort -u >"$list"

allowed() {
	[ -f "$allowlist" ] || return 1
	while IFS= read -r line; do
		pattern=${line%%#*}
		pattern=$(printf '%s' "$pattern" | sed 's/[[:space:]]*$//')
		[ -n "$pattern" ] || continue
		case "$1" in $pattern) return 0 ;; esac
	done <"$allowlist"
	return 1
}

# executable_kind prints the kind of executable a file starts as, or nothing.
executable_kind() {
	case $(head -c 4 "$1" | od -An -tx1 | tr -d ' \n') in
	feedface | feedfacf | cefaedfe | cffaedfe | cafebabe | bebafeca) echo "Mach-O" ;;
	7f454c46) echo "ELF" ;;
	4d5a*) echo "PE" ;;
	esac
}

has_nul() {
	[ "$(head -c 8192 "$1" | tr -d '\000' | wc -c)" -ne "$(head -c 8192 "$1" | wc -c)" ]
}

failed=0
while IFS= read -r path; do
	[ -n "$path" ] && [ -f "$path" ] && [ ! -L "$path" ] || continue
	allowed "$path" && continue
	kind=$(executable_kind "$path")
	if [ -n "$kind" ]; then
		echo "artifact guard: $path is a $kind executable; build outputs must not be committed (add it to .gitignore)" >&2
		failed=1
		continue
	fi
	size=$(wc -c <"$path" | tr -d ' ')
	if [ "$size" -gt "$max" ] && has_nul "$path"; then
		echo "artifact guard: $path is a $size-byte binary file (limit $max); commit source, not artifacts, or allowlist it in $allowlist with a reason" >&2
		failed=1
	fi
done <"$list"
exit $failed
