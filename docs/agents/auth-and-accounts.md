# Agent Recipe: Auth Primitives and Accounts

Use this when adding login/session behavior to a tanGO app.

Canonical files: `accounts/accounts.go`, `accounts/current_account.go`, `accounts/require_login.go`, and, for password reset and verification, `accounts/mail.go`, `accounts/password_reset.go` and `accounts/verification.go`. Runnable proof: `examples/accounts-mail` (password reset, verification, `RequireVerified`) and `examples/board` (register, login, sessions). Runnable proof for JSON mode: `examples/spa-accounts` (JSON mode: `accounts/json.go`, `accounts/json_http.go`). Human guide: `docs/guides/accounts.md`.

## Choose the Layer

- Use `accounts` for a conventional email/password HTML flow with register, login, logout, and current-account helpers.
- Use `auth` primitives directly when the project owns a custom identity model or custom login/register UI.

## Do With `accounts`

- Install `accounts.New(store, opts...)` through `InstalledApps`.
- Use its fixed `/accounts/register/`, `/accounts/login/`, and `/accounts/logout/` routes.
- Protect host views with `accounts.RequireLogin(store, cookieName, loginPath, next)`.
- Read the current account from host views with `accounts.CurrentAccountID` or `accounts.CurrentAccount`.
- Pass the same cookie name used when configuring `accounts.New`; use `accounts.DefaultSessionCookieName` for the default.
- For password reset and email verification, pass `accounts.WithMail(accounts.MailConfig{Sender, From, BaseURL})`. `BaseURL` is the configured public origin (`https://…`, or `http://localhost…` in development); never build links from `Request.Host`. Without `WithMail` the reset/verify routes don't exist.
- Require a verified email for a host view with `accounts.RequireVerified(store, cookieName, loginPath, next)`. Verification never blocks login.
- For a separate single-page client, add `accounts.WithJSON(accounts.JSONConfig{Auth, VerifyURL, ResetURL, OnRegister?, ResolveIdentifier?})` (needs `WithMail`). It is bearer-only: the host implements `accounts.JSONAuth` (usually over `auth/jwt`) because `accounts` issues no tokens; there is no logout. Put `{token}` in a URL fragment, not a query string. CORS is the host's. Runnable proof: `examples/spa-accounts`.
- Create the host's own rows at registration in `OnRegister`, using the `tx` it is given (never the outer store); return `accounts.FieldError` for a client-fixable problem. Bound its text fields with `tango:"varchar=n"`.
- Every app installing `accounts` runs `tango makemigrations` after upgrading, for `Account.EmailVerifiedAt` and `AccountToken`.

Tiny shape:

```go
protected := accounts.RequireLogin(store, accounts.DefaultSessionCookieName, "/accounts/login/", Dashboard)
```

## Do With `auth`

- Use `auth.HashPassword`, `auth.VerifyPassword`, `auth.CreateSession`, `auth.SessionUser`, `auth.DeleteSession`, `auth.RequireLogin`, and `auth.CurrentUserID` for custom identity flows.
- Keep the user model owned by the app.

## Don't

- Do not mix admin users with application accounts; they are separate identity spaces.
- Do not assume `accounts.Account` has roles, groups, or permissions. It deliberately has no permission-shaped field.
- If an app needs roles or permissions, write a View wrapper against app-owned data.
- Do not change `Account.Email` without clearing `EmailVerifiedAt`; `accounts` has no email-change flow.
- Do not read or set a cookie, or ask for a CSRF token, in JSON mode; do not put a bearer token in a query string.
- Do not call the outer store inside `OnRegister` or `Store.InTx`; use the transaction store.
- Do not reveal whether an email has an account in any reset-like flow you write; follow `accounts`' single response.

## Check

- Compare install shape with `accounts/accounts.go`.
- Compare current-account helper behavior with `accounts/current_account.go`.
- Compare mail-flow wiring with `accounts/mail.go`, and test it with `mail/mailtest.Sender`.
- Run the shared checklist: `docs/agents/checklist.md`.
