# API with admin

A JSON API and the HTML admin over one set of models. `authors` is managed only in the admin; `posts` has the admin and a JSON API, and every post belongs to an author through a foreign key. It's the compact proof of tanGO's app composition: two apps, one registry, generated migrations.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/api-with-admin
go run . -migrate
go run . -tango-admin-create=admin
go run .
```

`-tango-admin-create` prompts for a password. Open `http://localhost:8000/admin/`, log in, and add an author. Then use the API with that author's ID:

```sh
curl -X POST http://localhost:8000/posts/ \
  -H 'Content-Type: application/json' \
  -d '{"Title":"Hello","Body":"First post","AuthorID":1}'

curl 'http://localhost:8000/posts/?author_id=1'
```

`go test .` runs the same flow against a fresh database.

## Configuration

- `TANGO_DB_DSN` picks the database and defaults to `sqlite://app.db`.
- The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`.
- Ctrl-C or `SIGTERM` shuts the server down gracefully.

See the [routing guide](../../docs/guides/routing-and-reverse-lookup.md) and [admin registration](../../docs/guides/admin-registration.md).
