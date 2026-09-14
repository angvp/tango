# Agent Recipe: Auth Primitives and Accounts

Use this when adding login/session behavior to a tanGO app.

Canonical files: `accounts/accounts.go`, `accounts/current_account.go`, and `accounts/require_login.go`.

## Choose the Layer

- Use `accounts` for a conventional email/password HTML flow with register, login, logout, and current-account helpers.
- Use `auth` primitives directly when the project owns a custom identity model or custom login/register UI.

## Do With `accounts`

- Install `accounts.New(store, opts...)` through `InstalledApps`.
- Use its fixed `/accounts/register/`, `/accounts/login/`, and `/accounts/logout/` routes.
- Protect host views with `accounts.RequireLogin(store, cookieName, loginPath, next)`.
- Read the current account from host views with `accounts.CurrentAccountID` or `accounts.CurrentAccount`.
- Pass the same cookie name used when configuring `accounts.New`; use `accounts.DefaultSessionCookieName` for the default.

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

## Check

- Compare install shape with `accounts/accounts.go`.
- Compare current-account helper behavior with `accounts/current_account.go`.
- Run the shared checklist: `docs/agents/checklist.md`.
