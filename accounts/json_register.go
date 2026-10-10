package accounts

import (
	"net/http"
	"strings"

	"github.com/angvp/tango"
)

// register handles POST /accounts/api/register/: {"email", "password"}. It
// creates the account through the same use case as the HTML form, queues the
// verification email (with the JSON VerifyURL) once the account exists, and
// answers 201 with the account and a grant. A duplicate email is the
// deliberate 409 of ADR 0021, behind the failed-attempt limiter.
func (j *jsonAPI) register(ctx *tango.Context) error {
	if j.cfg.signupDisabled {
		return &apiError{status: http.StatusForbidden, code: "registration_closed", message: "registration is closed"}
	}
	members, err := readObject(ctx, "email", "password")
	if err != nil {
		return err
	}
	rawEmail, err := stringMember(members, "email")
	if err != nil {
		return err
	}
	password, err := stringMember(members, "password")
	if err != nil {
		return err
	}
	key, err := j.admit(ctx, j.registerLimiter)
	if err != nil {
		return err
	}
	refuse := func(e *apiError) error {
		j.registerLimiter.RecordFailure(key)
		return e
	}

	email := normalizeEmail(rawEmail)
	switch {
	case email == "":
		return refuse(fieldError("email", "is required"))
	case emailTooLong(email):
		return refuse(fieldError("email", "must be at most 254 characters"))
	case password == "":
		return refuse(fieldError("password", "is required"))
	}
	outcome, err := registerAccount(ctx.Context(), j.store, email, password)
	if err != nil {
		return err
	}
	if outcome.Problem != "" {
		return refuse(fieldError("password", strings.TrimSuffix(outcome.Problem, ".")))
	}
	if outcome.Duplicate {
		return refuse(&apiError{status: http.StatusConflict, code: "already_registered", message: "this email is already registered"})
	}

	j.mail.queueVerification(ctx.Context(), outcome.Account, templateLinker(j.cfg.json.VerifyURL))
	grant, err := j.grantFor(ctx.Context(), outcome.Account)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, map[string]any{"account": accountSummary(outcome.Account), "grant": grant})
}
