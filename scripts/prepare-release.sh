#!/bin/sh
# Makes the mechanical edits of a release as a diff you review:
#
#   scripts/prepare-release.sh vX.Y.Z [--date YYYY-MM-DD]
#
#   - CHANGELOG.md: the Unreleased section becomes "## [X.Y.Z] - DATE" (UTC
#     today unless --date), a fresh empty Unreleased section goes above it, and
#     the comparison links follow;
#   - the version's migration-compat corpus is generated from this checkout
#     (internal/migrationcompat/generate.sh vX.Y.Z local) and listed in
#     Generators in internal/migrationcompat/versions.go.
#
# It never commits, tags or pushes. Running it again changes nothing: a
# version whose section is already dated is left alone, and an existing corpus
# is never regenerated, because a corpus stands for what that release's users
# have. It refuses a tree with changes outside CHANGELOG.md and
# internal/migrationcompat/, so the diff you review is only its own.
set -eu
usage() { echo "usage: scripts/prepare-release.sh vMAJOR.MINOR.PATCH [--date YYYY-MM-DD]" >&2; exit 2; }

version="${1:-}"
[ -n "$version" ] || usage
shift
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) usage ;; esac
case "${version#v}" in *[!0-9.]*) usage ;; esac
date=""
while [ $# -gt 0 ]; do
	case "$1" in
	--date) [ $# -ge 2 ] || usage; date="$2"; shift 2 ;;
	*) usage ;;
	esac
done
[ -n "$date" ] || date="$(date -u +%Y-%m-%d)"
case "$date" in [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;; *) usage ;; esac

cd "$(dirname "$0")/.."

# Only this script's own files may be changed.
unrelated="$(git status --porcelain | sed 's/^...//' | grep -v -e '^CHANGELOG.md$' -e '^internal/migrationcompat/' || true)"
if [ -n "$unrelated" ]; then
	echo "prepare-release: changes outside CHANGELOG.md and internal/migrationcompat/; commit or stash them first:" >&2
	echo "$unrelated" >&2
	exit 1
fi

num="${version#v}"
dir="v$(echo "${num}" | tr . _)"

# --- CHANGELOG.md
if grep -q "^## \[$num\] - [0-9]" CHANGELOG.md; then
	echo "CHANGELOG.md: $num is already dated; left unchanged"
else
	if grep -q "^## \[$num\]" CHANGELOG.md; then
		echo "prepare-release: CHANGELOG.md has a section for $num without a date; date it by hand" >&2
		exit 1
	fi
	if ! awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} /^\[[^]]*\]: /{f=0} f&&NF{n++} END{exit n==0}' CHANGELOG.md; then
		echo "prepare-release: the Unreleased section of CHANGELOG.md is empty: nothing to release" >&2
		exit 1
	fi
	tmp="$(mktemp)"
	awk -v num="$num" -v date="$date" '
		/^## \[Unreleased\]$/ { print; print ""; print "## [" num "] - " date; next }
		/^\[Unreleased\]: / {
			prev = $0; sub(/.*compare\//, "", prev); sub(/\.\.\..*/, "", prev)
			base = $0; sub(/compare\/.*/, "compare/", base); sub(/^\[Unreleased\]: /, "", base)
			print "[Unreleased]: " base "v" num "...HEAD"
			print "[" num "]: " base prev "...v" num
			next
		}
		{ print }
	' CHANGELOG.md > "$tmp"
	cat "$tmp" > CHANGELOG.md
	rm -f "$tmp"
	echo "CHANGELOG.md: $num dated $date"
fi

# --- the migration-compat corpus
mc=internal/migrationcompat
if [ -d "$mc/$dir" ]; then
	echo "$mc/$dir exists; not regenerated"
else
	"$mc/generate.sh" "$version" local
fi
if grep -q "Dir: \"$dir\"" "$mc/versions.go"; then
	echo "$mc/versions.go: $dir already listed"
else
	alias="v$(echo "$num" | tr -d .)"
	tmp="$(mktemp)"
	awk -v dir="$dir" -v alias="$alias" '
		/^\t(v[0-9]+ )?"github.com\/angvp\/tango\/internal\/migrationcompat\/v[0-9_]+\/migrations"$/ { last = NR }
		{ lines[NR] = $0 }
		END {
			for (i = 1; i <= NR; i++) {
				print lines[i]
				if (i == last) print "\t" alias " \"github.com/angvp/tango/internal/migrationcompat/" dir "/migrations\""
			}
		}
	' "$mc/versions.go" | awk -v dir="$dir" -v alias="$alias" '
		/^\t\{Dir: "unreleased"/ { print "\t{Dir: \"" dir "\", Migrations: " alias ".Migrations}," }
		{ print }
	' > "$tmp"
	cat "$tmp" > "$mc/versions.go"
	rm -f "$tmp"
	gofmt -w "$mc/versions.go"
	echo "$mc/versions.go: $dir listed"
fi
