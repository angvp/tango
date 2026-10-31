#!/bin/sh
# Regenerates admin/assets/admin.css from admin/tailwind with the Tailwind CSS
# v3.4.19 standalone CLI. The script never downloads anything: pass the path
# to a CLI obtained from https://github.com/tailwindlabs/tailwindcss/releases.
#
# Usage: scripts/admin-css.sh /path/to/tailwindcss [--check]
#
# --check regenerates into a temporary file and fails when the result differs
# from the committed stylesheet, so a template whose classes changed without a
# rebuild is caught.
set -eu
cd "$(dirname "$0")/../admin"

cli=${1:?usage: scripts/admin-css.sh /path/to/tailwindcss [--check]}
mode=${2:-generate}
pinned=v3.4.19

if ! "$cli" --help 2>&1 | grep -F "$pinned" >/dev/null; then
	echo "admin-css: $cli is not Tailwind CSS $pinned; a v4 CLI ignores this config and writes different CSS" >&2
	exit 1
fi

out=assets/admin.css
case "$mode" in
generate)
	"$cli" -c tailwind/tailwind.config.js -i tailwind/admin.css -o "$out"
	;;
--check)
	tmp=$(mktemp)
	trap 'rm -f "$tmp"' EXIT
	"$cli" -c tailwind/tailwind.config.js -i tailwind/admin.css -o "$tmp"
	if ! cmp -s "$tmp" "$out"; then
		echo "admin-css: $out is stale; run scripts/admin-css.sh $cli and commit the result" >&2
		exit 1
	fi
	;;
*)
	echo "admin-css: unknown mode $mode" >&2
	exit 2
	;;
esac
