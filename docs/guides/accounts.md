# Guide: the `accounts` app

`github.com/angvp/tango/accounts` is an optional, installable, first-party reusable app: a ready-made HTML email/password register/login/logout flow, built entirely by composing `auth`'s primitives (see [application auth](application-auth.md)) rather than adding to them.

**Install `accounts` when** you want a conventional account system — self-service registration, login, logout — with minimal code, and the default `Account`/`AccountSession` shape (below) is close enough to what you need.

**Skip `accounts` and use `auth` primitives directly when** you need a different identity shape (username instead of email, additional required fields at signup, a different session-duration/cookie story per route, a token story other than bearer), or your own login/registration flow entirely. `auth` has no opinion about any of this and composes the same way `accounts` itself does — installing `accounts` is not a prerequisite for using `auth`.

`accounts` never shares or converts data with admin's own `AdminUser`/`AdminSession` — they are permanently separate identity spaces, the same way `auth`'s own Application user pattern is separate from admin.

## Models

```go
type Account struct {
	ID              int64  `tango:"pk"`
	Email           string `tango:"unique"`
	PasswordHash    string
	Active          bool
	CreatedAt       time.Time
	EmailVerifiedAt time.Time // the zero time means unverified
}

type AccountSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=Account,index"`
	ExpiresAt time.Time
}
```

`accounts` also registers `AccountToken`, which stores the hashes of emailed password-reset and verification links (see below).

`Account` is deliberately bare: identity, credential, activity state, timestamps. tanGO deliberately ships no `IsStaff`, `Role`, `Group`, or `Permission`-shaped field. An app that needs roles or permissions today queries `Account` itself (if installed) or its own domain data, from its own [View wrapper](routing-and-reverse-lookup.md#middleware-vs-view-wrappers).

`Email` is always stored lowercased and trimmed, and every lookup normalizes the same way — `Alice@Example.com` and `alice@example.com` are always the same `Account`.

## Installing

```go
config := tango.Config{
	InstalledApps: []tango.App{
		accounts.New(store),
		// ... your other apps
	},
}
```

`accounts.New(store)` with no options is the baseline: signup enabled, a 30-day session in a `tango_account_session` cookie. Run `tango makemigrations`/`tango migrate` as usual once it's installed — `Account`/`AccountSession` are ordinary models like any reusable app's.

## Options

| Option | Effect | Default |
|---|---|---|
| `accounts.WithTrustedProxies(networks...)` | For an app behind a reverse proxy: counts failed attempts per forwarded client address, using the rightmost `X-Forwarded-For` address that is not one of these networks, and only for a connection inside them. Without it the limiters count the connection address and read no header. See [running more than one instance](running-more-than-one-instance.md#behind-a-reverse-proxy). |
| `accounts.WithSignupDisabled()` | Closes self-service registration. `/accounts/register/` stays mounted and returns a clear "Registration is currently closed" response (never a bare 404); `/accounts/login/` is unaffected. | Signup enabled |
| `accounts.WithSessionDuration(d time.Duration)` | How long a created session stays valid. | 30 days — deliberately longer than admin's fixed 24 hours, since public-user and operator expectations differ |
| `accounts.WithSessionCookieName(name string)` | The session cookie's name. | `accounts.DefaultSessionCookieName` (`"tango_account_session"`) |

## Routes

`accounts` mounts a fixed prefix, like every other reusable app in tanGO (`admin` owns `/admin/`, the `greetings` example owns `/greetings/`) — there is no host-configurable mount prefix:

- `GET`/`POST /accounts/register/` — registration. On success, creates the `Account` (`Active=true` immediately), creates a session, and redirects straight to the post-login destination. No separate login step is needed after signing up. With mail enabled, it also emails a verification link; the account is usable before it's verified.
- `GET`/`POST /accounts/login/` — login. Any failure (unknown email, wrong password, or an inactive account) produces the exact same generic error — none of those cases are distinguishable from the response, so a failed attempt never reveals whether a given email is even registered. This is deliberately asymmetric with registration's specific "this email is already registered" error: login is a credential check, where a generic error closes off an oracle for identifying registered emails; registration is a self-service form where a genuine user needs to know why their signup didn't go through, and an email address isn't a secret the way a password is.
- `POST /accounts/logout/` — deletes only the current session; any other session belonging to the same account is untouched. No `GET` route.

Both `register` and `login` accept a `next` query parameter/form value, validated against any same-origin path (not restricted to `/accounts/`, since accounts's login can be reached from protecting any page across your site) — an off-site or malformed `next` falls back to `/`.

Both endpoints are CSRF-protected and rate-limited (a small per-IP, in-memory limiter — not distributed, no CAPTCHA, no configurable policy).

## Password reset and email verification

Both need outbound mail, so they're off until you pass `accounts.WithMail`:

```go
sender, err := mail.SMTPSenderFromEnv()
if err != nil {
	return err
}
accounts.New(store, accounts.WithMail(accounts.MailConfig{
	Sender:  sender,
	From:    "Shop <noreply@example.com>",
	BaseURL: "https://example.com",
}))
```

- **`BaseURL`:** the origin links are built on. It must be an absolute `https://` origin, or `http://` on localhost during development. Links never come from the request's `Host` header, which an attacker controls.
- **Checked at registration:** a missing `Sender`, a malformed `From` or a bad `BaseURL` makes `-check` fail.
- **`Logger`:** an optional `*slog.Logger` for the two events below; nil uses `slog.Default()`.
- **Without `WithMail`:** none of the routes below is mounted, and `accounts` sends nothing.

### Routes

- `GET`/`POST /accounts/password-reset/` asks for a reset link. Every syntactically valid address gets the same answer: an existing, unknown, inactive or recently emailed one. Only an empty or malformed address gets a form error, which says nothing about accounts. The request only queues a job; the background outbox looks the account up and emails a link only to an active account, so neither the response nor its timing reveals whether an account exists.
- `GET`/`POST /accounts/password-reset/confirm/?token=…` sets a new password under registration's rules. The link lasts one hour. A completed reset ends every session and every other link the account has, marks the email verified, and sends the user to log in.
- `GET`/`POST /accounts/verify/?token=…` confirms the email address from the link sent at registration. It lasts 24 hours.
- `POST /accounts/verify/resend/` sends a logged-in, unverified account a new verification link.

GET never uses a link: it shows a form or a button, and only POST acts. Email scanners and link previews follow links, and they shouldn't reset a password or verify an address. These pages send `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.

An expired, used, replaced or unknown link gets the same "This link is not valid" page. So does a link sent before the account's email changed.

### Limits

- **Cooldown:** each address gets at most one email of each kind every five minutes, and each client IP five reset or resend requests a minute. An email the full outbox drops gives its cooldown back.
- **More than one instance:** the cooldown and the outbox are per process; see [running more than one instance](running-more-than-one-instance.md).
- **Outbox:** emails wait in an in-memory outbox of 100, sent one at a time by a worker that `accounts` registers as a [Lifecycle](application-lifecycle.md) component.
- **Full outbox:** the email is dropped and `tango.accounts.mail_dropped` is logged.
- **Failed send:** `tango.accounts.mail_failed` is logged, with the address and link redacted; a failure preparing the email is logged by its error class only.
- **Shutdown:** the worker sends what it can before its stop deadline, then the send in progress is cancelled; nothing is sent after shutdown finishes.
- **What's lost:** a crash loses whatever is queued, and nothing is retried.
- **Per process:** the cooldown and the outbox are per process, like the login rate limiter.

### Requiring a verified email

Verification never stops anyone logging in. To require it for a page, use `accounts.RequireVerified`, shaped like `RequireLogin`:

```go
billing := accounts.RequireVerified(store, accounts.DefaultSessionCookieName, "/accounts/login/", Billing)
```

A logged-out visitor is redirected to log in. A logged-in but unverified account gets a `403` page with a button to send a new link; that button needs `WithMail`.

### Changing an account's email

`accounts` has no email-change flow. Whoever changes `Account.Email`, your own code or an operator in admin, must also clear `EmailVerifiedAt`, because the new address hasn't been proven. They should also delete the account's `AccountToken` rows. A link sent to the old address already stops working, because each link is tied to the address it was sent to.

See [ADR 0045](../adr/0045-accounts-reset-and-verification-never-reveal-an-account.md) and the [mail guide](mail.md).

## JSON mode for a single-page app

`accounts.WithJSON` adds six JSON endpoints for a separate client (a Next.js or other single-page app) that cannot use the cookie-and-HTML flow. It requires `WithMail` and is checked when the app registers, so `-check` fails on a bad configuration. Without it nothing changes.

```go
accounts.New(store,
	accounts.WithMail(accounts.MailConfig{Sender: sender, From: "app@example.com", BaseURL: "https://api.example.com"}),
	accounts.WithJSON(accounts.JSONConfig{
		Auth:              bearerAuth, // your accounts.JSONAuth: Issue and Authenticate
		VerifyURL:         "https://app.example.com/verify#token={token}",
		ResetURL:          "https://app.example.com/reset#token={token}",
		OnRegister:        createProfile,   // optional
		ResolveIdentifier: findByUsername,  // optional
	}),
)
```

JSON mode is **bearer-only**. It never reads or sets a cookie and needs no CSRF token, and it creates no `AccountSession`. `accounts` does not mint tokens: your `JSONAuth.Issue` returns an `AuthGrant` (access token, token type, lifetime) for an account that has just logged in or registered, and `Authenticate` names the account behind a request's bearer token for the one endpoint that needs it. That keeps `accounts` independent of any token package; [`auth/jwt`](jwt-auth.md) is the usual choice. Bearer tokens are stateless, so there is no logout endpoint, and a password reset cannot revoke an access token that was already issued: keep lifetimes short. [ADR 0050](../adr/0050-json-accounts-are-bearer-only-and-the-host-issues-the-token.md) records why.

CORS is the host's: tanGO ships none. Allow your client's origin on `/accounts/api/` and the `Authorization` and `Content-Type` headers in a middleware of your own, as [`examples/spa-accounts`](../../examples/spa-accounts) does.

### Endpoints

All take and return `application/json` and live under `/accounts/api/`.

| Endpoint | Request | Success |
|---|---|---|
| `POST register/` | `{"email", "password", "profile"?}` | `201` `{"account", "grant"}` |
| `POST login/` | `{"identifier", "password"}` | `200` `{"account", "grant"}` |
| `POST password-reset/` | `{"email"}` | `202` `{"status": "accepted"}`, the same for every valid address |
| `POST password-reset/confirm/` | `{"token", "password"}` | `204` |
| `POST verify/` | `{"token"}` | `204` |
| `POST verify/resend/` | none, with `Authorization: Bearer …` | `202` |

`account` is `{id, email, email_verified}`, never the password hash. Responses may gain members in a later release; clients ignore members they do not know. Requests are strict: a body that is not a JSON object, an unknown member, a duplicate member or a member of the wrong type is `400 invalid_body`; another content type is `415`; a body over 16 KiB is `413`. Every response, success or error, carries `Cache-Control: no-store`. Any other method on an endpoint is a JSON `405`.

Errors are `{"error": "message", "code": "…", "fields"?: {…}}`. Match on `code`: `invalid_body`, `invalid_field` (`422`, with `fields` keyed by the field), `invalid_credentials` (`401`, the same for an unknown identifier, a wrong password and an inactive account), `invalid_token` (`400`, the same for an unknown, expired, used or wrong-purpose token), `rate_limited` (`429`, with `Retry-After`), `unauthenticated` (`401`, a missing or invalid bearer token), `already_registered` (`409`), `registration_closed` (`403`), `unsupported_media_type`, `body_too_large`, `method_not_allowed` and `internal`. The message text is not covered.

JSON mode shares its rules with the HTML flow: the same password and email checks, the same per-address cooldowns and per-IP limits, the same in-memory outbox, and the same enumeration-safe answers. Login always spends one password comparison, even for an unknown identifier, so its timing does not say whether an account exists; this is also true of the HTML login. A completed reset ends every `AccountSession` and token row of the account.

### Emailed links

`VerifyURL` and `ResetURL` are templates for the links in the emails that JSON endpoints trigger: an absolute `http` or `https` URL, with no credentials, containing `{token}` exactly once. The HTML flow keeps its own links. **Put the token in a fragment** (`https://app.example.com/reset#token={token}`): a fragment is never sent to a server, so it stays out of access logs and `Referer` headers. A token in a query string can leak through both, and through browser history. The client reads the token from `location.hash`, clears it, and posts it to `password-reset/confirm/` or `verify/`.

### Registration hook and your own data

`OnRegister` runs inside the transaction that creates the account, after the `Account` row exists and before it commits, with a `*db.Store` bound to that transaction (see [`Store.InTx`](persistence-crud-and-raw-sql.md#transactions-with-intx)). It receives a `Registration` with the account id, the normalized email and the request's `profile` object untouched. Create your own rows with `tx`; if the hook fails, the account, your rows and the verification email are all gone.

```go
func createProfile(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
	var in struct{ Username string `json:"username"` }
	if err := json.Unmarshal(reg.Profile, &in); err != nil {
		return accounts.FieldError{Field: "username", Message: "is required"}
	}
	return tx.Create(ctx, profileMeta, &Profile{AccountID: reg.AccountID, Username: in.Username})
}
```

Return a `FieldError` (several can be joined with `errors.Join`) to answer `422 invalid_field` with your public field names, or let `tx.Create` return a `*db.ValueTooLongError` from a [`varchar=n`](models-and-tags.md#bounded-strings) field; that is also a `422`, keyed by the Go field name. Any other error is logged and answered with a generic `500`. A panic rolls back and propagates. `accounts` caps the `profile` object at 4 KiB of JSON, a guard on request size only; limits on a field's content are the model's own, counted in characters (runes), not bytes.

`ResolveIdentifier` lets login accept something other than an email, such as a username. An identifier containing `@` is always an email and never reaches it; the host guarantees its own identifiers never contain `@`. It returns the account id; `accounts` still loads the account, checks `Active` and compares the password, so an unknown username and a wrong password look the same.

## Protecting your own routes

`accounts.RequireLogin` is a [View wrapper](routing-and-reverse-lookup.md#middleware-vs-view-wrappers) for your own routes, mirroring `auth.RequireLogin`'s shape:

```go
protected := accounts.RequireLogin(store, accounts.DefaultSessionCookieName, "/accounts/login/", Dashboard)
```

Pass whatever cookie name you configured `accounts.New` with (`accounts.DefaultSessionCookieName` if you used the default — `RequireLogin` has no way to see accounts's own configuration, since it's a private detail of the specific `accounts.New` call that installed the app). An unauthenticated, invalid, expired, or now-inactive-account request redirects to the given login path with `next` carrying the page the visitor was trying to reach.

`Active` is checked on **every** request through an already-valid session, not just at login — deactivating an `Account` invalidates its standing sessions immediately. This is stricter than `auth.SessionUser` (which has no notion of `Active` at all) and matches `AdminUser`'s own precedent.

## Current account

```go
account, ok, err := accounts.CurrentAccount(ctx, store, accounts.DefaultSessionCookieName)
if err != nil {
	return err
}
if !ok {
	// no current account: missing/invalid/expired session, or Active=false
}
```

`accounts.CurrentAccountID` returns just the `int64` ID if you don't need the full row. Both are thin, `Active`-aware sugar over `auth.CurrentUserID` — a plain function call your own Views use, not a route. There is no `/accounts/me/` page; a real account-settings/profile page is its own feature surface, left to a later milestone.

## Admin integration

There is no dedicated option to register `Account` with `admin` — it's fully manual, the same way any reusable app's models are, once you've also installed `admin`:

```go
registry.Admin().Register(accounts.Account{}, admin.Options{
	ListDisplay: []string{"Email", "Active", "CreatedAt"},
	Search:      []string{"Email"},
	ReadOnly:    []string{"PasswordHash", "CreatedAt"},
})
```

Keep `PasswordHash` read-only (or omit it from `ListDisplay`/leave it out of any custom field ordering) — it should never be directly editable through the generic admin form. There is no `accounts`-specific CLI for managing accounts (no `tango accounts deactivate <email>`); once `Account` is registered with admin, the generic admin CRUD — including flipping `Active` — is the whole operational story for managing accounts outside the web flow.

## Non-goals

- The default flow is HTML. JSON endpoints exist only with `WithJSON` (above), are bearer-only and do not issue tokens themselves. Without `WithJSON`, the only JSON responses are errors a client may need to tell apart from a page: a rejected or missing CSRF token answers `403` and rate limiting answers `429` (with `Retry-After`), each with a JSON `{"error": …}` object. Closed registration is a `403` HTML page. See [versioning and compatibility](../compatibility.md#the-covered-api) for what is promised.
- No cookie-based JSON mode, no logout or token-refresh endpoint, no account-update or email-change endpoint, and no CORS handling.
- No custom user model abstraction or interface beyond `auth`'s own Application user pattern.
- No roles, groups, or permissions of any kind on `Account`.
- No dedicated CLI.
- No template-override hook — write your own login/register views composing `auth` primitives directly if you need different HTML.
- No `/accounts/me/` page.
- No host-configurable mount prefix.
- No OAuth/OIDC/social login, no email-change flow, and no customizable email wording: reset and verification emails are fixed English plain text.
