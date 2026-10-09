#!/bin/sh
# Rehearses a release: runs the checks the Release workflow would run for
# vX.Y.Z against the current commit, and changes nothing.
#
#   scripts/release-dryrun.sh vX.Y.Z
#
# It runs scripts/check.sh, compares the API with the previous tag the way the
# Release workflow does, and runs releasecheck with -dry-run: the changelog
# section is dated, the version's migration-compat corpus is registered, CI
# passed on this commit, and a patch release has no incompatible change. A dry
# run does NOT check that the commit is on main or ask the Go module proxy
# about the tag (asking would make the proxy cache "unknown revision" and delay
# the real release), and it says so. It creates no tag, release or file, edits
# nothing and writes nothing to the network. It refuses a dirty working tree,
# because it should judge what would be committed, not what is on disk.
set -eu
version="${1:-}"
case "$version" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "usage: scripts/release-dryrun.sh vMAJOR.MINOR.PATCH" >&2; exit 2 ;;
esac
case "${version#v}" in *[!0-9.]*) echo "usage: scripts/release-dryrun.sh vMAJOR.MINOR.PATCH" >&2; exit 2 ;; esac

cd "$(dirname "$0")/.."
if [ -n "$(git status --porcelain)" ]; then
	echo "release dry run: uncommitted changes. Commit or stash them: a dry run judges a commit." >&2
	exit 1
fi
commit="$(git rev-parse HEAD)"

scripts/check.sh

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
report="$tmp/gorelease.txt"
: > "$report"
previous="$(git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD 2>/dev/null || true)"
if [ -n "$previous" ]; then
	# The same gorelease the Release workflow runs.
	gorelease="$(sed -n 's/.*\(golang.org\/x\/exp\/cmd\/gorelease@[^ ]*\).*/\1/p' .github/workflows/release.yml | head -n 1)"
	if [ -n "$gorelease" ] && go run "$gorelease" -base="$previous" > "$report" 2> "$tmp/gorelease.err"; then
		echo "API changes since $previous:"
		cat "$report"
	else
		echo "warning: gorelease failed against $previous; a patch release would be refused" >&2
		: > "$report"
	fi
fi

GITHUB_TOKEN="${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}" \
	go run ./internal/releasecheck/cmd/releasecheck -dry-run -tag "$version" -commit "$commit" -gorelease "$report"
