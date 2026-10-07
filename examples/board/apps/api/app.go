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
