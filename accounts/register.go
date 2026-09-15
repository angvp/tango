package accounts

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// minPasswordLength is the baseline minimum for public self-service
// registration — a different trust boundary than admin's operator-run
// CLI, which enforces no minimum at all.
const minPasswordLength = 8

// maxPasswordLength matches bcrypt.GenerateFromPassword's hard limit:
// bcrypt.ErrPasswordTooLong beyond 72 bytes. Enforced here as an explicit,
// friendly validation error instead of letting a long password reach
// bcrypt and surface as an unhandled 500.
const maxPasswordLength = 72

// normalizeEmail lowercases and trims raw, so Alice@Example.com and
// alice@example.com are always treated as the same Account. Applied on
// every write and lookup — the database's unique constraint is not
// case-insensitive on its own.
func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// findAccountByEmail returns the Account row for the normalized email, or
// ok=false if none exists. Used only as a fast-path pre-check for a
// friendlier error on the common non-racing path — the database's unique
// constraint, not this lookup, is what actually prevents two concurrent
// registrations for the same email from both succeeding.
func findAccountByEmail(ctx context.Context, store *db.Store, email string) (Account, bool, error) {
	meta, _ := accountModelMetas()
	var rows []Account
	sqlQuery := "SELECT " +
		db.ColumnName("ID") + " AS ID, " +
		db.ColumnName("Email") + " AS Email, " +
		db.ColumnName("PasswordHash") + " AS PasswordHash, " +
		db.ColumnName("Active") + " AS Active, " +
		db.ColumnName("CreatedAt") + " AS CreatedAt FROM " +
		db.ColumnName(meta.Name) + " WHERE " + db.ColumnName("Email") + " = ?"
	if err := store.Query(ctx, &rows, sqlQuery, email); err != nil {
		return Account{}, false, err
	}
	if len(rows) == 0 {
		return Account{}, false, nil
	}
	return rows[0], true, nil
}

// registerView handles GET (render the registration form) and POST
// (validate, create the Account) for /accounts/register/. When signup is
// disabled, both methods render a clear closed-registration response
// instead — never a bare 404 — since a live project may have old links,
// bookmarks, or indexed pages pointing at this route.
func registerView(store *db.Store, cfg accountsConfig, limiter *security.RateLimiter) tango.View {
	return func(ctx *tango.Context) error {
		if cfg.signupDisabled {
			return render(ctx, http.StatusForbidden, registrationClosedTemplate, nil)
		}

		switch ctx.Request().Method {
		case http.MethodGet:
			token, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, registerTemplate, registerPageData{
				Next:      safeAccountsNext(ctx.Query("next"), ""),
				CSRFToken: token,
			})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyPreSessionCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}

			key := rateLimitKey(ctx.Request())
			if !limiter.Allow(key) {
				return tooManyRequests(ctx)
			}

			email := normalizeEmail(ctx.Request().PostForm.Get("email"))
			password := ctx.Request().PostForm.Get("password")
			next := ctx.Request().PostForm.Get("next")
			csrfCookie, _ := ctx.Request().Cookie(preSessionCSRFCookieName)

			rerender := func(status int, message string) error {
				limiter.RecordFailure(key)
				return render(ctx, status, registerTemplate, registerPageData{
					Next:      next,
					Email:     email,
					Error:     message,
					CSRFToken: csrfCookie.Value,
				})
			}

			if email == "" || password == "" {
				return rerender(http.StatusBadRequest, "Email and password are required.")
			}
			if len(password) < minPasswordLength {
				return rerender(http.StatusBadRequest, "Password must be at least 8 characters.")
			}
			if len(password) > maxPasswordLength {
				return rerender(http.StatusBadRequest, "Password must be at most 72 characters.")
			}

			if _, exists, err := findAccountByEmail(ctx.Context(), store, email); err != nil {
				return err
			} else if exists {
				return rerender(http.StatusConflict, "This email is already registered.")
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}

			meta, _ := accountModelMetas()
			account := Account{
				Email:        email,
				PasswordHash: string(hash),
				Active:       true,
				CreatedAt:    time.Now().UTC(),
			}
			if err := store.Create(ctx.Context(), meta, &account); err != nil {
				if db.IsUniqueConstraintViolation(err) {
					// The pre-check above missed a concurrent registration
					// for the same email — the database's own constraint is
					// the actual correctness boundary, not the pre-check.
					return rerender(http.StatusConflict, "This email is already registered.")
				}
				return err
			}

			if err := createAccountSession(ctx.Context(), store, cfg, ctx.ResponseWriter(), ctx.Request(), account.ID); err != nil {
				return err
			}
			return ctx.Redirect(safeAccountsNext(next, defaultPostLoginRedirect))

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// rateLimitKey derives the rate-limit key (source IP, port stripped) for
// a request, mirroring admin's own helper.
func rateLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyRequests(ctx *tango.Context) error {
	ctx.ResponseWriter().Header().Set("Retry-After", "60")
	return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
}
