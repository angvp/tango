#!/bin/sh
# Prints the package patterns `go test` should run for the database named by
# TANGO_TEST_DSN: ./... on SQLite (empty or sqlite://), and only the
# `sensitive` packages of scripts/dialect-manifest.txt on PostgreSQL and
# MySQL. It fails when a package from `go list ./...` is missing from the
# manifest, or the manifest names a package that no longer exists, so a new
# package cannot slip out of the dialect legs unclassified.
#
#   go test -count=1 $(scripts/dialect-packages.sh)
set -eu
cd "$(dirname "$0")/.."

manifest=${DIALECT_MANIFEST:-scripts/dialect-manifest.txt}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Manifest entries: "<class> <path>", ignoring comments and blank lines.
sed -e 's/#.*//' "$manifest" | awk 'NF { print $2 }' | sort >"$tmp/classified"
go list ./... | sed -e 's|^github.com/angvp/tango$|.|' -e 's|^github.com/angvp/tango/|./|' | sort >"$tmp/actual"

missing=$(comm -13 "$tmp/classified" "$tmp/actual")
stale=$(comm -23 "$tmp/classified" "$tmp/actual")
if [ -n "$missing" ] || [ -n "$stale" ]; then
	[ -z "$missing" ] || printf 'not classified in %s (add each as sensitive or sqlite-only):\n%s\n' "$manifest" "$missing" >&2
	[ -z "$stale" ] || printf 'in %s but not a package any more:\n%s\n' "$manifest" "$stale" >&2
	exit 1
fi

case "${TANGO_TEST_DSN:-}" in
postgres://* | postgresql://* | mysql://*)
	sed -e 's/#.*//' "$manifest" | awk '$1 == "sensitive" { print $2 }'
	;;
*)
	echo ./...
	;;
esac
