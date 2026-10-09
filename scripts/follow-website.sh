#!/bin/sh
# Moves the website to a release and waits for it to finish:
#   scripts/follow-website.sh vX.Y.Z
# It starts "Follow a tanGO release" in the website's repository, finds the
# run it started, and fails, saying the release is published but the website
# is not updated, if that run fails, never appears or does not finish. The
# Release workflow runs it after creating the release, so a website that did
# not move is a red job and not a silent success. Nothing is undone.
# WAIT_INTERVAL (seconds), APPEAR_TRIES and FINISH_TRIES tune the polling;
# GH_TOKEN needs Actions read and write on the website's repository.
set -u
tag="${1:?usage: follow-website.sh vX.Y.Z}"
repo="${WEB_REPO:-angvp/tango-web}"
workflow="follow-tango-release.yml"
interval="${WAIT_INTERVAL:-10}"
appear_tries="${APPEAR_TRIES:-18}"
finish_tries="${FINISH_TRIES:-90}"

failed() {
	echo "release published, website not updated: $1" >&2
	echo "Run \"Follow a tanGO release\" by hand from $repo's Actions tab with version $tag (see RELEASING.md)." >&2
	exit 1
}

known_runs() {
	gh run list --repo "$repo" --workflow "$workflow" --event workflow_dispatch --limit 30 --json databaseId --jq '.[].databaseId'
}

before="$(known_runs)" || failed "could not list $repo's runs"
gh workflow run "$workflow" --repo "$repo" --ref main -f version="$tag" || failed "the follow workflow could not start"

# The run this dispatch started is the one that was not there before.
run=""
i=0
while [ -z "$run" ] && [ "$i" -lt "$appear_tries" ]; do
	sleep "$interval"
	for id in $(known_runs); do
		case " $(echo $before) " in *" $id "*) ;; *) run="$id"; break ;; esac
	done
	i=$((i + 1))
done
[ -n "$run" ] || failed "the follow run never appeared in $repo's Actions"

i=0
while [ "$i" -lt "$finish_tries" ]; do
	state="$(gh run view "$run" --repo "$repo" --json status,conclusion --jq '.status + " " + (.conclusion // "")')" || state=""
	case "$state" in
	"completed success") echo "the website follows $tag (run $run)"; exit 0 ;;
	completed*)
		url="$(gh run view "$run" --repo "$repo" --json url --jq .url 2>/dev/null || true)"
		failed "the follow run ended as ${state#completed }: ${url:-run $run}"
		;;
	esac
	sleep "$interval"
	i=$((i + 1))
done
failed "the follow run did not finish in time: run $run"
