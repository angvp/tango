#!/bin/sh
# Checks that the last release reached the website and that the website
# dispatch token still works. The weekly "Release health" workflow runs it,
# so a stuck deploy or a lapsed token shows up as a failed run weeks before a
# release needs either. It reads, and writes nothing.
#
#   - the live site shows the latest release ("Docs for vX.Y.Z"); a site that
#     is behind is fine for an hour after the release, which is how long the
#     website follow and its deploy can legitimately take;
#   - the site's home, docs and examples pages answer;
#   - WEB_TOKEN (the dispatch token; GH_TOKEN when unset) can read the website
#     repository's workflows. Nothing about the token, its scope or its expiry is printed.
#
# SITE_URL, WEB_REPO and GRACE_SECONDS override the defaults; NOW (epoch
# seconds) overrides the clock, for tests. --print-age prints the latest
# release's age and exits.
set -u
repo="${TANGO_REPO:-angvp/tango}"
web="${WEB_REPO:-angvp/tango-web}"
site="${SITE_URL:-https://tangoframework.com}"
grace="${GRACE_SECONDS:-3600}"
problems=""

problem() { problems="$problems$1
"; }

# epoch converts an ISO UTC time with GNU or BSD date.
epoch() {
	date -u -d "$1" +%s 2>/dev/null || date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$1" +%s
}

if ! latest="$(gh release view --repo "$repo" --json tagName,publishedAt --jq '.tagName + " " + .publishedAt')"; then
	echo "could not read $repo's latest release" >&2
	exit 1
fi
tag="${latest%% *}"
published="${latest#* }"
now="${NOW:-$(date -u +%s)}"
age=$((now - $(epoch "$published")))
if [ "${1:-}" = "--print-age" ]; then
	echo "tag=$tag age=$age"
	exit 0
fi

home="$(curl -fsS "$site/" 2>/dev/null)" || home=""
shown="$(printf '%s' "$home" | sed -n 's/.*Docs for \(v[0-9][0-9.]*\).*/\1/p' | head -n 1)"
if [ "$shown" != "$tag" ] && [ "$age" -gt "$grace" ]; then
	problem "the website shows ${shown:-no version} but the latest release is $tag, published $((age / 60)) minutes ago"
fi
for path in /docs/ /examples/; do
	curl -fsS -o /dev/null "$site$path" 2>/dev/null || problem "$site$path did not answer"
done
[ -n "$home" ] || problem "$site/ did not answer"

# The word "token" is all the output says about it: no value, no expiry.
GH_TOKEN="${WEB_TOKEN:-${GH_TOKEN:-}}" gh workflow list --repo "$web" >/dev/null 2>&1 || problem "the website dispatch token (TANGO_WEB_DISPATCH_TOKEN) could not read $web's workflows: renew it or check its access (see RELEASING.md)"

if [ -n "$problems" ]; then
	printf 'release health: problems\n%s' "$problems" >&2
	exit 1
fi
echo "release health: ok ($tag on the site, pages answer, token works)"
