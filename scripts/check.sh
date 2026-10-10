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

# storage/s3 is its own Go module (ADR 0054), so ./... above skips it.
(cd storage/s3 && gofmt -l . | grep . && exit 1; go vet ./... && go test -count=1 ./...)

if command -v lychee >/dev/null 2>&1; then
	lychee --offline --include-fragments --no-progress './**/*.md'
else
	echo "SKIPPED: Markdown links (install lychee: brew install lychee)" >&2
fi
