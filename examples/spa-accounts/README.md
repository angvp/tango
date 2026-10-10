# Accounts for a single-page app

The backend of a separate client (a Next.js or other single-page app) that needs registration, login, password reset and email verification without cookies. It uses the `accounts` app's JSON mode, `accounts.WithJSON`, with bearer tokens from `auth/jwt`.

- **`auth.go`** is the application's side of the bearer contract: it issues and checks JWTs, so `accounts` imports no token package.
- **`apps/profiles`** is the host's own data: a `Profile` with a bounded `Username` (`varchar=30`, unique), `DisplayName` (`varchar=60`), an unbounded `Bio`, and `Preferences`, JSON text that the admin edits with `admin.JSON()`. `OnRegister` creates the profile in the same transaction as the account, so there is never an account without one; `ResolveUsername` lets login take the username instead of the email.
- **`cors.go`** is host-owned CORS middleware for the client's origin. tanGO ships none.
- **`main.go`** wires it together. Emailed links point at the client (`CLIENT_URL/verify#token=…`, `CLIENT_URL/reset#token=…`); a token in the fragment never reaches a server log or a `Referer` header.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/spa-accounts
export SPA_JWT_SECRET=$(openssl rand -hex 32)
go run . -migrate
go run .
```

Then, from any HTTP client:

```sh
curl -s localhost:8000/accounts/api/register/ -d '{"email":"ada@example.com","password":"correct-horse","profile":{"username":"ada"}}' -H 'Content-Type: application/json'
```

The response carries the account and a `grant` with the access token; the verification email, with its link, is printed in the server's terminal. Use the token on the application's own route: `curl -s localhost:8000/api/me/ -H "Authorization: Bearer <access_token>"`.

The six endpoints are `register/`, `login/`, `password-reset/`, `password-reset/confirm/`, `verify/` and `verify/resend/` under `/accounts/api/`. `login/` takes `{"identifier", "password"}`, where the identifier is an email or a username.

## The admin

`admin.New(store)` serves the staff admin at `/admin/`, so an operator can edit a profile. Create the first account with `go run . -tango-admin-create=admin` and log in at `http://localhost:8000/admin/`. The profile's `Preferences` field uses `admin.JSON()`: a value that is not one JSON document is refused with its error and the text as typed, the **Format** button pretty-prints valid JSON, and the stored value is exactly what was submitted. The `Bio` field uses `admin.Markdown()` with a **Preview** rendered by Goldmark's defaults, which omit raw HTML and drop unsafe links; the preview shows in a sandboxed frame, and the public site renders nothing from `Bio`, so a public page would need its own sanitization policy. Run `go test .` to see the same flow over HTTP.

## What it shows about bounded strings

A username longer than 30 characters, or a display name longer than 60, is refused by `Store.Create` before any SQL, counted in characters (`é` is one), on SQLite as well as PostgreSQL. The hook returns that error and `accounts` answers `422` with `"code": "invalid_field"`, keyed by the Go field name; the registration is rolled back, so no account, profile or email remains. When a client needs a public JSON field name, return an `accounts.FieldError` instead, as the `@` rule does. The generated migration gives the columns `VARCHAR(30)` and `VARCHAR(60)`. See [ADR 0049](../../docs/adr/0049-bounded-strings-are-declared-by-tag-and-validated-by-rune-count-in-go.md).

## Things to know

- Bearer tokens are stateless. A password reset ends the account's cookie sessions and tokens but cannot revoke an access token already issued; it stays valid until it expires (15 minutes here).
- `go test .` walks the whole flow with `mailtest.Sender`, on SQLite or, with `TANGO_TEST_DSN`, PostgreSQL.
- `TANGO_DB_DSN` picks the database (default `sqlite://app.db`); the address is `TANGO_ADDR`, else `PORT`, else `:8000`; `BASE_URL` is this API's origin.
