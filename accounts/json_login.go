package accounts

import (
	"net/http"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// jsonAPI is JSON mode's handlers and what they share with the HTML flow:
// the store, the configuration, the failed-attempt limiters and the mailer.
type jsonAPI struct {
	store           *db.Store
	cfg             accountsConfig
	auth            JSONAuth
	mail            *mailer
	loginLimiter    *security.RateLimiter
	registerLimiter *security.RateLimiter
}

// routes mounts each endpoint for POST, and answers the other methods 405
// in the JSON error shape.
func (j *jsonAPI) routes() tango.URLs {
	endpoints := map[string]func(*tango.Context) error{
		"login/":    j.login,
		"register/": j.register,
	}
	var routes tango.URLs
	for path, endpoint := range endpoints {
		routes = append(routes, tango.Path(http.MethodPost, jsonAPIPrefix+path, j.view(endpoint)))
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			routes = append(routes, tango.Path(method, jsonAPIPrefix+path, j.view(methodNotAllowedJSON)))
		}
	}
	return routes
}

// login handles POST /accounts/api/login/: {"identifier", "password"}. An
// identifier containing @ is an email. Every refusal is the same 401.
func (j *jsonAPI) login(ctx *tango.Context) error {
	members, err := readObject(ctx, "identifier", "password")
	if err != nil {
		return err
	}
	identifier, err := stringMember(members, "identifier")
	if err != nil {
		return err
	}
	password, err := stringMember(members, "password")
	if err != nil {
		return err
	}
	key, err := j.admit(ctx, j.loginLimiter)
	if err != nil {
		return err
	}
	if emailTooLong(identifier) {
		j.loginLimiter.RecordFailure(key)
		return fieldError("identifier", "must be at most 254 characters")
	}

	account, ok, err := j.authenticateIdentifier(ctx, identifier, password)
	if err != nil {
		return err
	}
	if !ok {
		j.loginLimiter.RecordFailure(key)
		return &apiError{status: http.StatusUnauthorized, code: "invalid_credentials", message: genericLoginError}
	}
	grant, err := j.grantFor(ctx.Context(), account)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]any{"account": accountSummary(account), "grant": grant})
}

// authenticateIdentifier resolves identifier to an account and checks its
// password. An identifier that is not an email names no account, and takes as
// long as a wrong password.
func (j *jsonAPI) authenticateIdentifier(ctx *tango.Context, identifier, password string) (Account, bool, error) {
	if !strings.Contains(identifier, "@") {
		rejectUnknown(password)
		return Account{}, false, nil
	}
	return authenticate(ctx.Context(), j.store, normalizeEmail(identifier), password)
}
