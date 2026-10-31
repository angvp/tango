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

## The operator dashboard

Logging in to `/admin/` lands on a dashboard: the database's reachability, dialect and migration state, and the last hour of request and job activity. Both come from sources `main.go` hands to `admin.WithDashboard`: a status function built from `tango.Status` (see `dashboard.go`), and an `observability.Window` that `tango.WithRecorder` feeds on every request. The activity is labelled "this instance only" and starts again from zero when the process restarts; use an external monitoring system for history or several instances. See the [admin dashboard guide](../../docs/guides/admin-dashboard.md).

## The admin in Spanish

The admin ships English only; this example supplies the rest. `catalog_es.go` is a complete `es` catalog for every key the admin uses and a small `es-MX` that overrides three of them (a request for `es-MX` gets the `es` text for the rest). A request picks its language from `?lang=`, then the `lang` cookie, then `Accept-Language` (`i18n.Chain(i18n.FromQuery("lang"), i18n.FromCookie("lang"))` in `apps/locale`), and `POST /language/` with `lang` and `next` remembers a choice in the cookie. Try `http://localhost:8000/admin/?lang=es`.

`i18n_test.go` walks the admin pages in `es` and `es-MX`, fails on any key the catalog lacks, and checks `i18n.Validate`. It is the pattern to copy for your own catalogs. See the [i18n guide](../../docs/guides/i18n.md).

## Content Security Policy

This example runs under a Content-Security-Policy, so its wiring can be copied as is. `main.go` installs `tango.SecurityHeaders` with `tango.StrictCSP()` for every response, which allows only the application's own origin and inline script or style that carries the request's nonce. The home page in `apps/home` is the one application template that uses the nonce: it passes `tango.CSPNonce` into its data and renders `{{NonceAttr .Nonce}}` on its `<style>` element. The admin composes `admin.ContentSecurityPolicy()` into its own tier, because its pages need a policy of their own and `/admin/` takes that policy in place of the global one. `security_test.go` checks the headers of the admin login, the home page and the JSON API, and scans the rendered markup of the admin login and the home page. The policy names no third-party origin; add one only when a page needs it. See the [content security policy guide](../../docs/guides/content-security-policy.md).

## Configuration

- `TANGO_DB_DSN` picks the database and defaults to `sqlite://app.db`.
- The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`.
- Ctrl-C or `SIGTERM` shuts the server down gracefully.

See the [routing guide](../../docs/guides/routing-and-reverse-lookup.md) and [admin registration](../../docs/guides/admin-registration.md).
