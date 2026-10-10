#!/bin/sh
# Runs what CI runs (.github/workflows/ci.yml): gofmt, vet, build, the tests
# and the offline Markdown link check. Tests use SQLite unless TANGO_TEST_DSN
# names a PostgreSQL database; the link check runs lychee, or the pinned lychee
# Docker image, and is skipped, loudly, when neither is available.
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

LYCHEE_ARGS="--offline --include-fragments --no-progress ./**/*.md"
if command -v lychee >/dev/null 2>&1; then
	lychee $LYCHEE_ARGS
elif docker info >/dev/null 2>&1; then
	docker run --rm -v "$PWD":/input:ro -w /input lycheeverse/lychee:0.24.2 $LYCHEE_ARGS
else
	echo "SKIPPED: Markdown links (install lychee: brew install lychee, or start Docker)" >&2
fi
