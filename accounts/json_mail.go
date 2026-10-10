package accounts

import (
	"errors"
	"net/http"
	netmail "net/mail"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

// The JSON endpoints that send or consume emailed tokens. Tokens travel in
// the request body, never a URL, and every unusable one is the same
// invalid_token answer.

func invalidToken() *apiError {
	return &apiError{status: http.StatusBadRequest, code: "invalid_token", message: "this link is invalid or has expired"}
}

func noContent(ctx *tango.Context) error {
	ctx.ResponseWriter().WriteHeader(http.StatusNoContent)
	return nil
}

func accepted(ctx *tango.Context) error {
	return ctx.JSON(http.StatusAccepted, map[string]string{"status": "accepted"})
}

// requestReset handles POST /accounts/api/password-reset/: {"email"}. Every
// well-formed address gets the same 202 whether or not it has an account, is
// active, or is in its cooldown: the outbox, not the request, looks the
// account up. Every request counts against the client's budget.
func (j *jsonAPI) requestReset(ctx *tango.Context) error {
	members, err := readObject(ctx, "email")
	if err != nil {
		return err
	}
	raw, err := stringMember(members, "email")
	if err != nil {
		return err
	}
	key, err := j.admit(ctx, j.mail.resetLimiter)
	if err != nil {
		return err
	}
	j.mail.resetLimiter.RecordFailure(key)

	address, err := netmail.ParseAddress(raw)
	if err != nil {
		return fieldError("email", "must be a valid email address")
	}
	email := normalizeEmail(address.Address)
	if emailTooLong(email) {
		return emailTooLongError("email")
	}
	j.mail.requestPasswordReset(ctx.Context(), email, templateLinker(j.cfg.json.ResetURL))
	return accepted(ctx)
}

// confirmReset handles POST /accounts/api/password-reset/confirm/:
// {"token", "password"}. It issues no grant, and previously issued bearer
// tokens stay valid until they expire: accounts never claims to revoke them.
func (j *jsonAPI) confirmReset(ctx *tango.Context) error {
	members, err := readObject(ctx, "token", "password")
	if err != nil {
		return err
	}
	token, err := stringMember(members, "token")
	if err != nil {
		return err
	}
	password, err := stringMember(members, "password")
	if err != nil {
		return err
	}
	row, account, ok, err := j.mail.usableToken(ctx.Context(), token, PurposePasswordReset)
	if err != nil {
		return err
	}
	if !ok {
		return invalidToken()
	}
	outcome, err := j.mail.completePasswordReset(ctx.Context(), row, account, password)
	if err != nil {
		return err
	}
	if outcome.Problem != "" {
		return fieldError("password", strings.TrimSuffix(outcome.Problem, "."))
	}
	if outcome.Invalid {
		return invalidToken()
	}
	return noContent(ctx)
}

// verify handles POST /accounts/api/verify/: {"token"}.
func (j *jsonAPI) verify(ctx *tango.Context) error {
	members, err := readObject(ctx, "token")
	if err != nil {
		return err
	}
	token, err := stringMember(members, "token")
	if err != nil {
		return err
	}
	row, account, ok, err := j.mail.usableToken(ctx.Context(), token, PurposeEmailVerification)
	if err != nil {
		return err
	}
	if !ok {
		return invalidToken()
	}
	won, err := j.mail.completeVerification(ctx.Context(), row, account)
	if err != nil {
		return err
	}
	if !won {
		return invalidToken()
	}
	return noContent(ctx)
}

// resend handles POST /accounts/api/verify/resend/ for the account the
// bearer token names. It answers the same 202 whether or not an email was
// queued (an inactive or already verified account, or an address in its
// cooldown, gets none); 401 only when nobody is authenticated.
func (j *jsonAPI) resend(ctx *tango.Context) error {
	if _, err := readObject(ctx); err != nil {
		return err
	}
	id, ok, err := j.auth.Authenticate(ctx.Context(), ctx.Request())
	if err != nil {
		return err
	}
	var account Account
	if ok {
		if err := j.store.Get(ctx.Context(), accountMeta(), id, &account); err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				return err
			}
			ok = false
		}
	}
	if !ok {
		return &apiError{status: http.StatusUnauthorized, code: "unauthenticated", message: "authentication required"}
	}
	key, err := j.admit(ctx, j.mail.resendLimiter)
	if err != nil {
		return err
	}
	j.mail.resendLimiter.RecordFailure(key)
	if account.Active && account.EmailVerifiedAt.IsZero() {
		j.mail.queueVerification(ctx.Context(), account, templateLinker(j.cfg.json.VerifyURL))
	}
	return accepted(ctx)
}
