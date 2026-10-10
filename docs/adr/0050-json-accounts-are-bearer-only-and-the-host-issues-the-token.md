# JSON accounts are bearer-only, and the host issues the token

A single-page app on another origin needs registration, login, password reset and email verification without HTML pages, redirects or cookies. `accounts.WithJSON` adds six endpoints under `/accounts/api/` for that, inside the `accounts` app so they share its use cases, limiters, outbox and enumeration-safe answers with the HTML flow instead of copying them.

JSON mode is bearer-only. It never reads or sets a cookie, needs no CSRF token and creates no `AccountSession`. `accounts` does not mint tokens either: the host passes a `JSONAuth` whose `Issue(ctx, Account)` returns an `AuthGrant` (token, type, lifetime) and whose `Authenticate(ctx, r)` names the account behind a request, which only `verify/resend/` needs. `Issue` receives the account with its password hash withheld, and an error from either is an operational failure (a generic `500`), kept apart from "not authenticated". So `accounts` imports no token package, the token format and lifetime stay the host's decision, and `auth/jwt` is one implementation among several.

There is no logout, refresh or revocation endpoint. A bearer token is stateless, so a password reset ends the account's cookie sessions and stored tokens but cannot revoke an access token already issued; hosts keep lifetimes short. Emailed links in this mode come from host-configured templates (`VerifyURL`, `ResetURL`, `{token}` exactly once, absolute `http(s)`, no credentials), and the guide advises a URL fragment so the token stays out of logs and `Referer` headers. The contract is strict on the way in (`415`, `413`, `400 invalid_body` for unknown or duplicate members) and additive-only on the way out (`{"error", "code", "fields"?}` and success bodies may gain members), and every response is `Cache-Control: no-store`. Login always spends one password comparison, in both flows, so its timing does not say whether an account exists.

Rejected:

- **Cookie sessions with CSRF for JSON.** A separate origin needs CORS with credentials and `SameSite=None`, which brings back the CSRF surface the bearer header avoids, and it ties a mobile or non-browser client to cookies.
- **`accounts` issuing JWTs itself.** It would pull a token package and a signing-key policy into `accounts` and decide lifetimes for every host.
- **A server-side token table with logout and refresh.** That is a session system; hosts that want one have `auth`'s cookie sessions or can build it behind `JSONAuth`.
- **A separate `accountsapi` app.** It would duplicate the limiters, the outbox and the use cases, and the two flows would drift.

A profile the host writes at registration lives in its own model, with its text fields bounded by `tango:"varchar=n"` ([ADR 0049](0049-bounded-strings-are-declared-by-tag-and-validated-by-rune-count-in-go.md)); the hook's `*db.ValueTooLongError` becomes a `422` without the host mapping it.

Consequences: the host writes about thirty lines (`Issue`, `Authenticate`) and owns CORS; revoking an issued access token before it expires is not possible without the host adding state of its own; the six endpoints, their status and `code` values, and the Go surface (`WithJSON`, `JSONConfig`, `JSONAuth`, `AuthGrant`, `Registration`, `FieldError`) join the Covered API.
