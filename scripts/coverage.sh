#!/usr/bin/env bash
# Statement coverage as CI measures it: the test profile plus the coverage of
# the programs the tests build and run (the scaffolded server, `tango shell`).
#
#   scripts/coverage.sh              # SQLite
#   TANGO_TEST_DSN=postgres://… scripts/coverage.sh
#
# Writes coverage.out and coverage-children.out (both git-ignored) and prints
# the total of the test profile alone and of the two merged.
set -euo pipefail
cd "$(dirname "$0")/.."

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT

TANGO_COVERDIR="$dir" go test ./... -count=1 -coverprofile=coverage.out -covermode=atomic
go tool covdata textfmt -i="$dir" -pkg='github.com/angvp/tango/...' -o coverage-children.out

total() { go tool cover -func="$1" | awk '/^total:/ {print $NF}'; }
echo "test profile:  $(total coverage.out)"
# Both profiles are atomic-mode text; concatenating the blocks of the second
# onto the first is what Codecov's merge amounts to for a total.
merged="$dir/merged.out"
{ cat coverage.out; tail -n +2 coverage-children.out; } >| "$merged"
echo "with children: $(total "$merged")"
