# Board

The bulletin board built in the [tutorial](../../docs/tutorial/01-bootstrap-routing-json.md), at its final (part 10) state: JSON API with bearer tokens and rate limits, server-rendered pages with accounts and CSRF-safe forms, the HTML admin, a live front page over WebSockets, a session-pruning job, structured logs, and graceful shutdown.

The code follows the tutorial text; when one changes, change the other.

## Admin changelist filters

The HTML admin shows the boolean and foreign key filters from the admin guide's [changelist filters](../../docs/guides/admin-registration.md#changelist-filters) section: Accounts filter by `Active`, Posts by their author, and Comments by their post. `TestAdminChangelistFilters` in `admin_filters_test.go` exercises them over HTTP on whichever database `TANGO_TEST_DSN` selects.

## Admin bulk delete

Posts and Comments set `BulkDelete: true`, so their changelists offer checkboxes and a **Delete selected** button that deletes the checked rows of the page after a confirmation. Deleting a Post also deletes its Comments, exactly as the single-row delete does. `TestAdminBulkDeleteRemovesSelectedPostsAndTheirComments` in `admin_bulk_delete_test.go` covers it over HTTP.

## Bylines and the recent read

The front page and `GET /posts/` show each post's byline, the part of its account's email before the `@`: an account registered as `ada@example.com` shows as `ada`. `Store.Related` loads the authors of the whole list in one statement. `GET /posts/recent/` returns the newest twenty posts, each with its account (public fields only) and its comments, through `Query.Preload`: three statements for the page. `byline_preload_test.go` asserts those statement counts with the counting wrapper in `statement_count_test.go`.

## Run locally

```sh
echo "BOARD_JWT_SECRET=$(openssl rand -hex 32)" >> .env
go run . -migrate
go run . -tango-admin-create=admin
go run .
go run ./shell -c 'Models()'
go test ./...
```

`TANGO_DB_DSN` picks the database (`sqlite://app.db` by default, or `postgres://…`) and `TANGO_ADDR` the listen address (`:8000`). See part 10 for the full configuration table.

The `Dockerfile` is the one from part 10. In this repository the module resolves `github.com/angvp/tango` through a local `replace => ../..`, so build the image from a copy of the project that depends on a published tanGO version.
