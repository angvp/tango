#!/bin/sh
# Pushes the current branch, then waits for CI on that commit and fails if it
# fails. Pushing is the start of the check, not the end.
set -eu
cd "$(dirname "$0")/.."

git push origin HEAD
sha="$(git rev-parse HEAD)"

run=""
for _ in 1 2 3 4 5 6 7 8 9 10; do
	run="$(gh run list --commit "$sha" --limit 1 --json databaseId --jq '.[0].databaseId // empty')"
	[ -n "$run" ] && break
	sleep 3
done
[ -n "$run" ] || { echo "no CI run appeared for $sha" >&2; exit 1; }
gh run watch "$run" --exit-status
