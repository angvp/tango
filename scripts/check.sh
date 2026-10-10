#!/bin/sh
# Runs what CI runs (.github/workflows/ci.yml): gofmt, vet, build, the tests
# and the offline Markdown link check. Tests use SQLite unless TANGO_TEST_DSN
# names a PostgreSQL database; lychee is skipped, loudly, when not installed.
set -eu
cd "$(dirname "$0")/.."

test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: format these files" >&2; exit 1; }
go vet ./...
go build ./...
go test -count=1 ./...

# These are their own Go modules (ADR 0054, ADR 0056), so ./... above skips
# them. The cache adapters' conformance tests need a server and skip without
# TANGO_TEST_REDIS_URL / TANGO_TEST_MEMCACHE_ADDR.
for module in storage/s3 cache/redis cache/memcache; do
	(cd "$module" && ! gofmt -l . | grep . && go vet ./... && go test -count=1 ./...)
done

if command -v lychee >/dev/null 2>&1; then
	lychee --offline --include-fragments --no-progress './**/*.md'
else
	echo "SKIPPED: Markdown links (install lychee: brew install lychee)" >&2
fi
