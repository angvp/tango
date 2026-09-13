# Guide: application auth

tanGO's `auth` package provides primitives for application login, logout, current-user lookup, CSRF tokens, and route protection. It does **not** provide a default `User` model, an interface to implement, signup views, or login/logout handlers.

Admin auth and application auth are always separate: admin owns `AdminUser`/`AdminSession`; each application owns its own user/session model pair, cookies, and login views.

## Models

Your user model can have any shape:

```go
type User struct {
	ID           int64  `tango:"pk"`
	Email        string `tango:"unique"`
	PasswordHash string
}
```

The session model follows a fixed convention:

```go
type UserSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=User,index"`
	ExpiresAt time.Time
}
```

After registering models, call `auth.ValidateSessionModel(sessionMeta)` if you want an eager check. The session functions also validate internally, so skipping the explicit call does not make reflection failures silent.

## Creating users

Account creation is application code. Hash passwords before storing them:

```go
hash, err := auth.HashPassword(password)
if err != nil {
	return err
}
user := User{Email: email, PasswordHash: hash}
return store.Create(ctx, userMeta, &user)
```

## JSON login

A JSON/API login view verifies credentials, creates a session, and sets a cookie:

```go
func Login(store *db.Store, userMeta, sessionMeta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var input struct {
			Email    string
			Password string
		}
		if err := ctx.Bind(&input); err != nil {
			return err
		}

		var users []User
		err := store.Query(ctx.Context(), &users, "SELECT id, email, password_hash FROM user WHERE email = ?", input.Email)
		if err != nil {
			return err
		}
		if len(users) != 1 || !auth.VerifyPassword(users[0].PasswordHash, input.Password) {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		}

		token, expiresAt, err := auth.CreateSession(ctx.Context(), store, sessionMeta, users[0].ID, 24*time.Hour)
		if err != nil {
			return err
		}
		http.SetCookie(ctx.ResponseWriter(), &http.Cookie{
			Name:     "app_session",
			Value:    token,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   ctx.Request().TLS != nil,
		})
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
}
```

## HTML login

For an HTML form, derive a CSRF token from a login secret you store in a cookie, render it into the form, and verify it on POST:

```go
csrfToken := auth.DeriveCSRFToken(loginCookie.Value)
if !auth.VerifyCSRFToken(ctx.Request().PostFormValue("csrf_token"), loginCookie.Value) {
	return ctx.JSON(http.StatusForbidden, map[string]string{"error": "invalid CSRF token"})
}
```

Redirects after login should validate `next` explicitly:

```go
target := auth.SafeRedirect(ctx.Request().PostForm.Get("next"), "/app/", "/app/")
return ctx.Redirect(target)
```

## Protecting routes

Wrap a view with `RequireLogin`. Pass both the login URL and the safe path prefix for `next`:

```go
protected := auth.RequireLogin(
	store,
	sessionMeta,
	"app_session",
	"/login/",
	"/app/",
	Dashboard,
)
```

`loginPath` is where unauthenticated users go. `allowedPrefix` is what the generated `next` value must start with before it is embedded in the redirect.

## Current user

Inside a view, read the current user ID explicitly and hydrate your own model:

```go
userID, ok, err := auth.CurrentUserID(ctx, store, sessionMeta, "app_session")
if err != nil {
	return err
}
if !ok {
	return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
}

var user User
if err := store.Get(ctx.Context(), userMeta, userID, &user); err != nil {
	return err
}
```

## Logout

Logout deletes the session row and clears the cookie:

```go
func Logout(store *db.Store, sessionMeta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		if cookie, err := ctx.Request().Cookie("app_session"); err == nil {
			if err := auth.DeleteSession(ctx.Context(), store, sessionMeta, cookie.Value); err != nil {
				return err
			}
		}
		http.SetCookie(ctx.ResponseWriter(), &http.Cookie{
			Name:     "app_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})
		return ctx.Redirect("/login/")
	}
}
```
