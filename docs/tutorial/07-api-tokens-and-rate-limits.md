# Tutorial, part 7: API tokens and rate limits

Continuing from [part 6](06-accounts-and-forms.md), this part gives the JSON API authentication that works outside a browser. A client trades an email and password for a short-lived **bearer token** (a JWT) and sends it with each request. You'll also close a hole that has been open since part 4 — anyone could delete any post — and put rate limits in front of the endpoints most worth abusing.

Tokens are the right tool when the client isn't a browser: a script, a CLI, a phone app. They're checked without a database lookup, which is also their weakness — a token stays valid until it expires, so you'll see below how the board keeps a deactivated account from using one.

## A signing secret

Tokens are signed with a secret only the server knows. `main.go` already loads `.env` at startup (it's in `.gitignore`), so put a random secret there:

```sh
echo "BOARD_JWT_SECRET=$(openssl rand -hex 32)" >> .env
```

Then build the token service in `project/project.go`. It needs an issuer and an audience — names that get stamped into every token and checked on the way back in, so a token minted for one service can't be replayed against another:

```go
// project/project.go
// newTokenService builds the API's token issuer from BOARD_JWT_SECRET.
func newTokenService() (*jwt.Service, error) {
	secret := os.Getenv("BOARD_JWT_SECRET")
	if len(secret) < jwt.MinimumSecretBytes {
		return nil, fmt.Errorf("BOARD_JWT_SECRET must be set to a random value of at least %d bytes", jwt.MinimumSecretBytes)
	}
	return jwt.NewService(
		jwt.Key{ID: "board-1", Secret: []byte(secret)},
		nil,
		"board",     // issuer
		"board-api", // audience
		jwt.WithMaxTTL(time.Hour),
	)
}
```

Call it at the top of `Config`. `Config` has no error to return, so a missing secret stops the process right there, with the message, rather than at the first login:

```go
// project/project.go (as of part 7)
func Config(store *db.Store) tango.Config {
	tokens, err := newTokenService()
	if err != nil {
		log.Fatal(err)
	}
	// ...
```

(`project/project.go` now imports `"fmt"`, `"log"`, `"os"`, `"time"` and `"github.com/angvp/tango/auth/jwt"`.) Everything that boots the project reads `.env`, which includes `go run . -check`, `tango shell` and the `tango` CLI commands that build your app, so they'll all need the secret too. The JWT package brings in one new dependency; fetch it with:

```sh
go mod tidy
```

## Issuing tokens

A new app owns the token endpoint:

```sh
tango newapp api
```

```go
// apps/api/app.go
// Package api issues API tokens: a client trades an account's email and
// password for a short-lived bearer token it sends with each request.
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/ratelimit"
)

const tokenTTL = time.Hour

func New(store *db.Store, tokens *jwt.Service) tango.App {
	return tango.NewApp("api", func(registry *tango.Registry) error {
		accountMeta, _ := registry.Models().Get("Account")

		// Compared against when the email is unknown, so a wrong email takes
		// as long as a wrong password and response times don't reveal
		// which emails have accounts.
		dummyHash, err := auth.HashPassword("not a real password")
		if err != nil {
			return err
		}

		// 5 attempts per client IP, then one more every 12 seconds.
		limiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 5, Refill: 12 * time.Second})
		if err != nil {
			return err
		}

		return registry.Routes().Include("/api/", tango.URLs{
			tango.Path("POST", "/token/", issueToken(store, accountMeta, tokens, dummyHash),
				tango.Name("token"),
				tango.Use(ratelimit.Middleware(limiter, ratelimit.RemoteIPKey()))),
		})
	})
}

func issueToken(store *db.Store, accountMeta model.ModelMeta, tokens *jwt.Service, dummyHash string) tango.View {
	return func(ctx *tango.Context) error {
		var input struct {
			Email    string
			Password string
		}
		if err := ctx.Bind(&input); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		}

		// accounts stores emails lowercased and trimmed; look them up the same way.
		email := strings.ToLower(strings.TrimSpace(input.Email))
		var found []accounts.Account
		err := store.List(ctx.Context(), accountMeta, db.Query{
			Where: []db.Condition{{Field: "Email", Op: db.OpEq, Value: email}},
			Limit: 1,
		}, &found)
		if err != nil {
			return err
		}

		hash := dummyHash
		if len(found) == 1 {
			hash = found[0].PasswordHash
		}
		passwordOK := auth.VerifyPassword(hash, input.Password)
		if len(found) != 1 || !passwordOK || !found[0].Active {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		}

		token, err := tokens.Issue(strconv.FormatInt(found[0].ID, 10), tokenTTL)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, map[string]any{
			"token":      token,
			"token_type": "Bearer",
			"expires_in": int(tokenTTL.Seconds()),
		})
	}
}
```

Three details worth noticing:

- **The lookup matches how `accounts` stores emails** — lowercased and trimmed — so `Ana@Example.com ` logs in as `ana@example.com`.
- **Every failure looks the same.** Unknown email, wrong password, and a deactivated account all get the same `401 invalid credentials`. When the email is unknown, the view still checks the password against a dummy hash, so the response takes as long as a wrong password would — otherwise the timing alone would tell an attacker which emails have accounts.
- **The token's subject is the account ID.** tanGO doesn't interpret it; your views turn it back into an account.

`tango.Use(...)` attaches middleware to this one route. `ratelimit.NewLimiter` is a token bucket: each client IP starts with 5 attempts and gets one back every 12 seconds, so a script guessing passwords is slowed to a crawl while a person who mistypes twice never notices. Over the limit, the client gets a `429` with a `Retry-After` header.

`ratelimit.RemoteIPKey()` keys on the connection's IP address. If you deploy behind a proxy or load balancer, every request arrives from the proxy's address — pass the proxy's network (`ratelimit.RemoteIPKey(proxyNet)`). The key is then the address your proxy saw: `X-Forwarded-For` is read from the right, skipping your proxies, and only when the request really came from one of them. Anything a client writes to the left of that is ignored, so it can't choose its own key. This needs a proxy that adds the address it sees to `X-Forwarded-For`; `X-Real-IP` is not read.

## Requiring a token

In `posts`, two pieces work together. `tokens.Middleware(jwt.BearerToken)` runs on every `/posts/` request: it reads `Authorization: Bearer …`, verifies the token, and puts its claims on the request. A missing token is fine — reading posts stays public — but an invalid one is rejected with a `401`. Then `jwt.Require` wraps the views that need a token.

```go
// apps/posts/app.go (as of part 7)
package posts

import (
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/ratelimit"
)

func New(store *db.Store, tokens *jwt.Service) tango.App {
	return tango.NewApp("posts", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Post{}); err != nil {
			return err
		}
		if err := registry.Models().Register(Comment{}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Post{}, admin.Options{
			ListDisplay: []string{"Title", "CreatedAt"},
			Search:      []string{"Title"},
			Ordering:    []string{"CreatedAt"},
			Label:       "Title",
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Comment{}, admin.Options{
			ListDisplay: []string{"PostID", "Author", "CreatedAt"},
			Search:      []string{"Author", "Body"},
		}); err != nil {
			return err
		}

		postLimiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 10, Refill: 6 * time.Minute})
		if err != nil {
			return err
		}
		commentLimiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 10, Refill: time.Minute})
		if err != nil {
			return err
		}

		postMeta, _ := registry.Models().Get("Post")
		commentMeta, _ := registry.Models().Get("Comment")
		accountMeta, _ := registry.Models().Get("Account")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store), tango.Name("list")),
			tango.Path("POST", "/", jwt.Require(createPost(store, postMeta, accountMeta)),
				tango.Name("create"),
				tango.Use(ratelimit.Middleware(postLimiter, accountKey))),
			tango.Path("GET", "/{id}/", getPost(store, postMeta), tango.Name("detail")),
			tango.Path("DELETE", "/{id}/", jwt.Require(deletePost(store, postMeta, accountMeta)), tango.Name("delete")),
			tango.Path("GET", "/{id}/comments/", listComments(store, commentMeta), tango.Name("comments")),
			tango.Path("POST", "/{id}/comments/", createComment(store, commentMeta),
				tango.Name("comment"),
				tango.Use(ratelimit.Middleware(commentLimiter, ratelimit.RemoteIPKey()))),
		}, tango.WithMiddleware(tokens.Middleware(jwt.BearerToken)))
	})
}
```

`tango.WithMiddleware` attaches middleware to everything in an `Include`. Middleware nests from the outside in — `Config.Middleware`, then `Include`'s `WithMiddleware`, then a route's `Use` — so by the time the rate limiter on `POST /posts/` runs, the token has already been verified. That's what lets the limiter key on the account, not the IP.

`posts` now looks up the `Account` model while registering, so `accounts` has to come first in `InstalledApps`. Update `project/project.go` (and import `"board/apps/api"`):

```go
// project/project.go (as of part 7)
InstalledApps: []tango.App{
	accounts.New(store),
	posts.New(store, tokens),
	api.New(store, tokens),
	web.New(store),
	admin.New(store),
},
```

## Turning a token into an account

Put the token helpers in `apps/posts/auth.go`:

```go
// apps/posts/auth.go
package posts

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/ratelimit"
)

// tokenAccount returns the account behind the request's API token. A token
// stays valid until it expires, so the account is re-checked here: a token
// for a deleted or deactivated account stops working immediately.
func tokenAccount(ctx *tango.Context, store *db.Store, accountMeta model.ModelMeta) (accounts.Account, bool, error) {
	claims, ok := jwt.FromContext(ctx.Context())
	if !ok {
		return accounts.Account{}, false, nil
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return accounts.Account{}, false, nil
	}
	var account accounts.Account
	err = store.Get(ctx.Context(), accountMeta, id, &account)
	if errors.Is(err, db.ErrNotFound) {
		return accounts.Account{}, false, nil
	}
	if err != nil {
		return accounts.Account{}, false, err
	}
	return account, account.Active, nil
}

// accountKey rate-limits per account when the request carries a verified
// token, and per client IP when it doesn't.
func accountKey(r *http.Request) (string, error) {
	if claims, ok := jwt.FromContext(r.Context()); ok {
		return "account:" + claims.Subject, nil
	}
	return ratelimit.RemoteIPKey()(r)
}

func unauthorized(ctx *tango.Context) error {
	return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}
```

`tokenAccount` looks the account up on every request, even though the token is valid by itself. That one query is what makes deactivation work: untick **Active** on an account in the admin, and its tokens stop working immediately instead of an hour later.

`accountKey` is a `ratelimit.KeyFunc` — any function from a request to a string. Keying on the account means one busy user can't use up everyone's budget, and switching networks doesn't reset theirs.

## Owners only

`createPost` takes the owner from the token, and `deletePost` refuses to delete someone else's post:

```go
// apps/posts/views.go (as of part 7)
func createPost(store *db.Store, meta, accountMeta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		account, ok, err := tokenAccount(ctx, store, accountMeta)
		if err != nil {
			return err
		}
		if !ok {
			return unauthorized(ctx)
		}

		var post Post
		if err := ctx.Bind(&post); err != nil {
			return badRequest(ctx, "invalid JSON body")
		}
		if post.Title == "" {
			return badRequest(ctx, "title is required")
		}
		post.ID = 0                 // the database assigns IDs, never the client
		post.AccountID = account.ID // the poster is whoever the token belongs to
		post.CreatedAt = time.Now().UTC()
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		return ctx.JSON(http.StatusCreated, post)
	}
}

func deletePost(store *db.Store, meta, accountMeta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		account, ok, err := tokenAccount(ctx, store, accountMeta)
		if err != nil {
			return err
		}
		if !ok {
			return unauthorized(ctx)
		}

		var post Post
		err = store.Get(ctx.Context(), meta, ctx.Param("id"), &post)
		if errors.Is(err, db.ErrNotFound) {
			return notFound(ctx)
		}
		if err != nil {
			return err
		}
		if post.AccountID != account.ID {
			return ctx.JSON(http.StatusForbidden, map[string]string{"error": "you can only delete your own posts"})
		}
		// Deleting a post cascades: its comments are deleted first, in the same transaction.
		if err := store.Delete(ctx.Context(), meta, post.ID); err != nil {
			return err
		}
		ctx.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	}
}
```

`401` means "I don't know who you are"; `403` means "I know who you are, and you can't do this". The difference tells a client whether logging in again would help. (`createPost` no longer uses the session cookie — the API is tokens only now, and the browser keeps using the forms from part 6.)

## Try it

```sh
go run .

curl -X POST http://localhost:8000/api/token/ \
  -d '{"email":"ana@example.com","password":"correct horse battery"}'
# {"expires_in":3600,"token":"eyJhbGciOi...","token_type":"Bearer"}

TOKEN=eyJhbGciOi...   # paste the token

curl -X POST http://localhost:8000/posts/ -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Posted from curl"}'
# 201 {"ID":3,"Title":"Posted from curl","Body":"","AccountID":1,...}

curl -X POST http://localhost:8000/posts/ -d '{"title":"no token"}'
# 401 {"error":"unauthorized"}

curl -X DELETE http://localhost:8000/posts/1/ -H "Authorization: Bearer $OTHER_TOKEN"
# 403 {"error":"you can only delete your own posts"}  (with a token for another account)
```

Ask for a token with a wrong password six times in a row and the sixth answer is `429 {"error":"rate limit exceeded"}` with a `Retry-After` header saying how many seconds to wait.

The limits live in memory, per process: they reset when the app restarts, and two copies of the app behind a load balancer each keep their own counts. That's the right trade-off for one server; see [rate limiting](../guides/rate-limiting.md) for the details and [JWT authentication](../guides/jwt-auth.md) for key rotation.

**Part 8** makes the board live: new posts appear on every open front page without a reload, over WebSockets.

Continue: [Tutorial, part 8: a live feed with WebSockets](08-live-feed.md)
