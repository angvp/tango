package accounts

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
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
	members, err := readObject(ctx, "email", "password", "profile")
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
	profile, err := j.profileMember(members)
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
	outcome, err := j.create(ctx, email, password, profile)
	if err != nil {
		if fields, ok := fieldErrorsIn(err); ok {
			return refuse(&apiError{status: http.StatusUnprocessableEntity, code: "invalid_field", message: "invalid field", fields: fields})
		}
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

// maxProfileBytes caps the raw JSON of the "profile" member. It guards
// request size; it is not a limit on any host field.
const maxProfileBytes = 4 * 1024

// profileMember returns the request's profile object untouched, or nil when
// there is none. A profile needs OnRegister, must be an object, and is capped.
func (j *jsonAPI) profileMember(members map[string]json.RawMessage) (json.RawMessage, error) {
	raw, present := members["profile"]
	if !present {
		return nil, nil
	}
	if j.cfg.json.OnRegister == nil {
		return nil, invalidBody("profile is not accepted: the application has no registration hook")
	}
	if len(raw) > maxProfileBytes {
		return nil, &apiError{status: http.StatusRequestEntityTooLarge, code: "body_too_large", message: "profile is too large"}
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, invalidBody("profile must be a JSON object")
	}
	return raw, nil
}

// create registers the account. With an OnRegister hook the account and the
// hook's writes share one transaction, so neither exists without the other;
// the caller queues mail and issues the grant only after it commits.
func (j *jsonAPI) create(ctx *tango.Context, email, password string, profile json.RawMessage) (registrationOutcome, error) {
	hook := j.cfg.json.OnRegister
	if hook == nil {
		return registerAccount(ctx.Context(), j.store, email, password)
	}
	var outcome registrationOutcome
	err := j.store.InTx(ctx.Context(), func(tx *db.Store) error {
		var err error
		outcome, err = registerAccount(ctx.Context(), tx, email, password)
		if err != nil {
			return err
		}
		if !outcome.created() {
			// Roll back: nothing was written, and a unique violation leaves
			// a PostgreSQL transaction unusable.
			return errNotCreated
		}
		return hook(ctx.Context(), tx, Registration{AccountID: outcome.Account.ID, Email: outcome.Account.Email, Profile: profile})
	})
	if errors.Is(err, errNotCreated) {
		return outcome, nil
	}
	return outcome, err
}

// errNotCreated ends the registration transaction when registerAccount
// refused the input or found the email taken.
var errNotCreated = errors.New("accounts: registration refused")
