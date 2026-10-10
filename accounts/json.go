package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/angvp/tango/db"
)

// JSONAuth is the bearer-token side of JSON mode, supplied by the host so
// accounts never imports a token package.
type JSONAuth interface {
	// Issue returns the bearer grant for an account that has just
	// authenticated or registered. The Account it receives has its
	// PasswordHash withheld. An error is an operational failure (a signing
	// key that is unavailable, say) and is answered with a generic 500.
	Issue(ctx context.Context, account Account) (AuthGrant, error)
	// Authenticate returns the id of the account a request's bearer token
	// names. ok is false for a missing or invalid token; err is an
	// operational failure, kept apart from "not authenticated".
	Authenticate(ctx context.Context, r *http.Request) (accountID int64, ok bool, err error)
}

// AuthGrant is the bearer credential JSON mode returns after register and
// login.
type AuthGrant struct {
	// AccessToken is the bearer token. It must not be empty.
	AccessToken string
	// TokenType defaults to "Bearer".
	TokenType string
	// ExpiresIn is how long the token stays valid, rendered as whole seconds.
	ExpiresIn time.Duration
}

// JSONConfig turns on the JSON endpoints, which a separate client such as a
// single-page app calls with a bearer token. See WithJSON.
type JSONConfig struct {
	// Auth issues and checks bearer tokens. It is required.
	Auth JSONAuth
	// VerifyURL and ResetURL are the links the JSON endpoints' emails carry:
	// absolute http(s) URLs with no userinfo and exactly one {token}
	// placeholder, such as "https://app.example.com/verify#token={token}".
	// Prefer a fragment: a token in a query string can leak through server
	// logs, browser history and Referer headers. They apply only to mail the
	// JSON endpoints trigger; the HTML flow's mail is unchanged.
	VerifyURL string
	ResetURL  string
	// OnRegister, when set, runs inside the transaction that creates the
	// account, after the Account row exists and before it commits, so the
	// host's own rows (a profile, say) and the account succeed or fail
	// together. tx is a Store bound to that transaction. An error rolls
	// everything back and sends no email. FieldError (several can be joined
	// with errors.Join) and *db.ValueTooLongError from tx.Create answer 422
	// invalid_field; any other error is logged and answered with a generic
	// 500, and a panic rolls back and propagates.
	OnRegister func(ctx context.Context, tx *db.Store, reg Registration) error
	// ResolveIdentifier, when set, lets login take something other than an
	// email, such as a username the host stores. It receives an identifier
	// that does not contain "@" (one that does is an email and never reaches
	// it) and returns the id of the account it names. The host guarantees its
	// identifiers never contain "@". An error is an operational failure and
	// answers a generic 500. accounts still loads the account, checks Active
	// and compares the password, so found=false and a wrong password look the
	// same to the client.
	ResolveIdentifier func(ctx context.Context, store *db.Store, identifier string) (accountID int64, found bool, err error)
}

// Registration is what OnRegister is told about a new account. It
// deliberately is not the Account, which carries the password hash.
type Registration struct {
	AccountID int64
	// Email is the normalized address.
	Email string
	// Profile is the request's "profile" object, untouched, or empty when
	// the request had none. Its 4 KiB cap guards request size only; limits
	// on a host model's fields are the model's own (varchar=n, counted in
	// runes by Store.Create).
	Profile json.RawMessage
}

// FieldError is an OnRegister failure the client can fix: Field is the
// public JSON field name, Message what is wrong with it. Return one, or
// several joined, to answer 422 invalid_field with those messages.
type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// WithJSON mounts the JSON endpoints under /accounts/api/. JSON mode is
// bearer-only: it never reads or sets a cookie and needs no CSRF token. It
// requires WithMail and a valid config, checked when the app registers.
// Without it accounts mounts no JSON route and behaves exactly as before.
func WithJSON(config JSONConfig) Option {
	return func(c *accountsConfig) { c.json = &config }
}

// jsonAPIPrefix is the fixed prefix of every JSON endpoint.
const jsonAPIPrefix = "/accounts/api/"

// validate returns why config cannot be used.
func (config JSONConfig) validate(mailEnabled bool) error {
	if config.Auth == nil {
		return errors.New("accounts: JSONConfig.Auth is nil")
	}
	if !mailEnabled {
		return errors.New("accounts: WithJSON requires WithMail")
	}
	if err := validateLinkTemplate("VerifyURL", config.VerifyURL); err != nil {
		return err
	}
	return validateLinkTemplate("ResetURL", config.ResetURL)
}

// validateLinkTemplate checks one emailed-link template.
func validateLinkTemplate(name, template string) error {
	if strings.Count(template, "{token}") != 1 {
		return fmt.Errorf("accounts: JSONConfig.%s must contain {token} exactly once, got %q", name, template)
	}
	u, err := url.Parse(strings.Replace(template, "{token}", "TOKEN", 1))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("accounts: JSONConfig.%s must be an absolute http or https URL, got %q", name, template)
	}
	if u.User != nil {
		return fmt.Errorf("accounts: JSONConfig.%s must not carry credentials, got %q", name, template)
	}
	return nil
}

// grantFor asks the host for account's grant, withholding the password
// hash, and checks what comes back.
func (j *jsonAPI) grantFor(ctx context.Context, account Account) (map[string]any, error) {
	account.PasswordHash = ""
	grant, err := j.auth.Issue(ctx, account)
	if err != nil {
		return nil, err
	}
	if grant.AccessToken == "" {
		return nil, errors.New("accounts: JSONAuth.Issue returned an empty access token")
	}
	if grant.TokenType == "" {
		grant.TokenType = "Bearer"
	}
	return map[string]any{
		"access_token": grant.AccessToken,
		"token_type":   grant.TokenType,
		"expires_in":   int64(grant.ExpiresIn / time.Second),
	}, nil
}

// accountSummary is the account object every success body carries.
func accountSummary(account Account) map[string]any {
	return map[string]any{
		"id":             account.ID,
		"email":          account.Email,
		"email_verified": !account.EmailVerifiedAt.IsZero(),
	}
}

// fieldErrorsIn collects the client-fixable errors in err (a FieldError, a
// *db.ValueTooLongError, or several joined) as field to message, and reports
// whether err held nothing else.
func fieldErrorsIn(err error) (map[string]string, bool) {
	fields := map[string]string{}
	if !collectFieldErrors(err, fields) || len(fields) == 0 {
		return nil, false
	}
	return fields, true
}

func collectFieldErrors(err error, fields map[string]string) bool {
	if err == nil {
		return true
	}
	var fieldErr FieldError
	var tooLong *db.ValueTooLongError
	switch {
	case errors.As(err, &fieldErr) && !isJoined(err):
		fields[fieldErr.Field] = fieldErr.Message
		return true
	case errors.As(err, &tooLong) && !isJoined(err):
		fields[tooLong.Field] = fmt.Sprintf("must be at most %d characters (got %d)", tooLong.Max, tooLong.Got)
		return true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	all := true
	for _, inner := range joined.Unwrap() {
		all = collectFieldErrors(inner, fields) && all
	}
	return all
}

func isJoined(err error) bool {
	_, ok := err.(interface{ Unwrap() []error })
	return ok
}
