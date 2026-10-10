#!/bin/sh
# Release-quality validation: the root module and the three examples under the
# race detector, on SQLite and then on PostgreSQL. It takes well over ten
# minutes, so run it in the background and read the log. The PostgreSQL pass
# needs the tango-test-pg container (postgres:17-alpine on 127.0.0.1:55432,
# user tango, database tango_test); it is skipped, loudly, when the container
# is not running. Prints VALIDATION-DONE last, whatever the outcome above.
cd "$(dirname "$0")/.." || exit 1

EXAMPLES="examples/spa-accounts examples/board examples/uploads"
quiet() { grep -v "^ok\|no test files"; }

echo "== sqlite root"; go test -race ./... 2>&1 | quiet
for d in $EXAMPLES; do
	echo "== sqlite $d"; (cd "$d" && go vet ./... && go test -race ./... 2>&1 | grep -v "no test files")
done

pw=$(docker inspect tango-test-pg --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null | sed -n 's/^POSTGRES_PASSWORD=//p')
if [ -z "$pw" ]; then
	echo "SKIPPED: PostgreSQL (container tango-test-pg is not running)" >&2
else
	export TANGO_TEST_DSN="postgres://tango:${pw}@127.0.0.1:55432/tango_test?sslmode=disable"
	echo "== pg root"; go test -race -p 1 ./... 2>&1 | quiet
	for d in $EXAMPLES; do
		echo "== pg $d"; (cd "$d" && go test -race -p 1 ./... 2>&1 | grep -v "no test files")
	done
fi
echo VALIDATION-DONE
