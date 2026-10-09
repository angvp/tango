package accounts

import (
	"context"
	"net/http"
	"net/mail"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// passwordResetView handles GET (the form asking for a link) and POST
// (queue the link) for /accounts/password-reset/. Every syntactically valid
// address gets the same response, whether or not it has an account, and
// whether or not it is active, rate-limited or in its cooldown: the outbox,
// not the request, looks the account up, so neither the response nor its
// timing says. Only an empty or malformed address is a form error.
func passwordResetView(m *mailer, limiter *security.RateLimiter) tango.View {
	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			token, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, passwordResetRequestTemplate, tokenFormData{CSRFToken: token})
		case http.MethodPost:
			return m.askForReset(ctx, limiter)
		default:
			return methodNotAllowed(ctx)
		}
	}
}

// askForReset handles a submitted reset request.
func (m *mailer) askForReset(ctx *tango.Context, limiter *security.RateLimiter) error {
	if err := ctx.Request().ParseForm(); err != nil {
		return err
	}
	if !verifyPreSessionCSRF(ctx.Request()) {
		return forbiddenCSRF(ctx)
	}
	key := m.cfg.clientKey(ctx.Request())
	if !limiter.Allow(key) {
		return tooManyRequests(ctx)
	}
	limiter.RecordFailure(key)

	address, err := mail.ParseAddress(ctx.Request().PostForm.Get("email"))
	if err != nil {
		return render(ctx, http.StatusBadRequest, passwordResetRequestTemplate, tokenFormData{
			CSRFToken: submittedCSRFToken(ctx.Request()),
			Error:     "Enter a valid email address.",
		})
	}
	email := normalizeEmail(address.Address)
	m.queue(ctx.Context(), email, outboxJob{
		purpose: PurposePasswordReset,
		prepare: func(jobCtx context.Context) (delivery, bool, error) { return m.prepareReset(jobCtx, email) },
	})
	return render(ctx, http.StatusOK, passwordResetSentTemplate, nil)
}

// prepareReset issues a reset token for email's account and returns its
// email, or ok=false when there is no active account for email.
func (m *mailer) prepareReset(ctx context.Context, email string) (delivery, bool, error) {
	account, ok, err := findAccountByEmail(ctx, m.store, email)
	if err != nil || !ok || !account.Active {
		return delivery{}, false, err
	}
	token, err := issueToken(ctx, m.store, account, PurposePasswordReset, m.cfg.now(), resetTokenLifetime)
	if err != nil {
		return delivery{}, false, err
	}
	return m.emailWithLink(account, token, "/accounts/password-reset/confirm/", "Reset your password",
		"Someone asked to reset the password for this email address.\n\n"+
			"To choose a new password, open this link within 1 hour:\n\n%s\n\n"+
			"If you didn't ask, ignore this email: your password stays the same.\n"), true, nil
}

// resetLink is /accounts/password-reset/confirm/: its form asks for the new
// password, and a completed reset ends every session and token the
// account has, verifies its email, and sends the person to log in.
func (m *mailer) resetLink() tokenLink {
	return tokenLink{
		purpose: PurposePasswordReset,
		form:    passwordResetConfirmTemplate,
		act: func(ctx *tango.Context, row AccountToken, account Account) error {
			password := ctx.Request().PostForm.Get("password")
			if problem := passwordProblem(password); problem != "" {
				return render(ctx, http.StatusBadRequest, passwordResetConfirmTemplate, tokenFormData{
					CSRFToken: submittedCSRFToken(ctx.Request()),
					Error:     problem,
				})
			}
			if won, err := m.useToken(ctx.Context(), row); err != nil || !won {
				if err != nil {
					return err
				}
				return invalidLink(ctx)
			}
			if err := m.setPassword(ctx.Context(), account, password); err != nil {
				return err
			}
			return ctx.Redirect("/accounts/login/")
		},
	}
}

// setPassword sets account's password, verifies its email if it wasn't,
// and deletes every token and session the account has.
func (m *mailer) setPassword(ctx context.Context, account Account, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	account.PasswordHash = string(hash)
	if account.EmailVerifiedAt.IsZero() {
		account.EmailVerifiedAt = m.cfg.now().UTC()
	}
	if err := m.store.Update(ctx, accountMeta(), &account); err != nil {
		return err
	}
	if err := deleteWhere[AccountToken](ctx, m.store, tokenMeta(), db.Condition{Field: "AccountID", Op: db.OpEq, Value: account.ID}); err != nil {
		return err
	}
	return deleteWhere[AccountSession](ctx, m.store, sessionMeta(), db.Condition{Field: "UserID", Op: db.OpEq, Value: account.ID})
}
