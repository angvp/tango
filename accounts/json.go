package accounts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
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
}

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
