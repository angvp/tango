# Guide: the `accounts` app

`github.com/angvp/tango/accounts` is an optional, installable, first-party reusable app: a ready-made HTML email/password register/login/logout flow, built entirely by composing `auth`'s primitives (see [application auth](application-auth.md)) rather than adding to them.

**Install `accounts` when** you want a conventional account system — self-service registration, login, logout — with minimal code, and the default `Account`/`AccountSession` shape (below) is close enough to what you need.

**Skip `accounts` and use `auth` primitives directly when** you need a different identity shape (username instead of email, additional required fields at signup, a different session-duration/cookie story per route, JSON auth), or your own login/registration flow entirely. `auth` has no opinion about any of this and composes the same way `accounts` itself does — installing `accounts` is not a prerequisite for using `auth`.

`accounts` never shares or converts data with admin's own `AdminUser`/`AdminSession` — they are permanently separate identity spaces, the same way `auth`'s own Application user pattern is separate from admin.

## Models

```go
type Account struct {
	ID           int64  `tango:"pk"`
	Email        string `tango:"unique"`
	PasswordHash string
	Active       bool
	CreatedAt    time.Time
}

type AccountSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=Account,index"`
	ExpiresAt time.Time
}
```

`Account` is deliberately bare: identity, credential, activity state, a timestamp. v0.1 deliberately ships no `IsStaff`, `Role`, `Group`, or `Permission`-shaped field. An app that needs roles or permissions today queries `Account` itself (if installed) or its own domain data, from its own [View wrapper](routing-and-reverse-lookup.md#middleware-vs-view-wrappers).

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
| `accounts.WithSignupDisabled()` | Closes self-service registration. `/accounts/register/` stays mounted and returns a clear "Registration is currently closed" response (never a bare 404); `/accounts/login/` is unaffected. | Signup enabled |
| `accounts.WithSessionDuration(d time.Duration)` | How long a created session stays valid. | 30 days — deliberately longer than admin's fixed 24 hours, since public-user and operator expectations differ |
| `accounts.WithSessionCookieName(name string)` | The session cookie's name. | `accounts.DefaultSessionCookieName` (`"tango_account_session"`) |

## Routes

`accounts` mounts a fixed prefix, like every other reusable app in tanGO (`admin` owns `/admin/`, the `greetings` example owns `/greetings/`) — there is no host-configurable mount prefix in v0.1:

- `GET`/`POST /accounts/register/` — registration. On success, creates the `Account` (`Active=true` immediately — there's no email verification step yet), creates a session, and redirects straight to the post-login destination. No separate login step is needed after signing up.
- `GET`/`POST /accounts/login/` — login. Any failure (unknown email, wrong password, or an inactive account) produces the exact same generic error — none of those cases are distinguishable from the response, so a failed attempt never reveals whether a given email is even registered. This is deliberately asymmetric with registration's specific "this email is already registered" error: login is a credential check, where a generic error closes off an oracle for identifying registered emails; registration is a self-service form where a genuine user needs to know why their signup didn't go through, and an email address isn't a secret the way a password is.
- `POST /accounts/logout/` — deletes only the current session; any other session belonging to the same account is untouched. No `GET` route.

Both `register` and `login` accept a `next` query parameter/form value, validated against any same-origin path (not restricted to `/accounts/`, since accounts's login can be reached from protecting any page across your site) — an off-site or malformed `next` falls back to `/`.

Both endpoints are CSRF-protected and rate-limited (a small per-IP, in-memory limiter — not distributed, no CAPTCHA, no configurable policy in v0.1).

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

`accounts.CurrentAccountID` returns just the `int64` ID if you don't need the full row. Both are thin, `Active`-aware sugar over `auth.CurrentUserID` — a plain function call your own Views use, not a route. There is no `/accounts/me/` page in v0.1; a real account-settings/profile page is its own feature surface, left to a later milestone.

## Admin integration

There is no dedicated option to register `Account` with `admin` — it's fully manual, the same way any reusable app's models are, once you've also installed `admin`:

```go
registry.Admin().Register(accounts.Account{}, admin.Options{
	ListDisplay: []string{"Email", "Active", "CreatedAt"},
	Search:      []string{"Email"},
	ReadOnly:    []string{"PasswordHash", "CreatedAt"},
})
```

Keep `PasswordHash` read-only (or omit it from `ListDisplay`/leave it out of any custom field ordering) — it should never be directly editable through the generic admin form. There is no `accounts`-specific CLI for managing accounts (no `tango accounts deactivate <email>`); once `Account` is registered with admin, the generic admin CRUD — including flipping `Active` — is the whole v0.1 operational story for managing accounts outside the web flow.

## Non-goals

- No JSON auth endpoints — HTML only. A JSON variant is a different contract (status codes, response bodies, no redirect-with-`next`) for its own milestone.
- No custom user model abstraction or interface beyond `auth`'s own Application user pattern.
- No roles, groups, or permissions of any kind on `Account`.
- No dedicated CLI.
- No template-override hook — write your own login/register views composing `auth` primitives directly if you need different HTML.
- No `/accounts/me/` page.
- No host-configurable mount prefix.
- No password-reset email flow or email-verification/confirmation step at signup, no OAuth/OIDC/social login.
