package accounts

import (
	"net/http"

	"github.com/angvp/tango"
)

// The JSON endpoints' shapes, as Go types, for tango.Doc: the openapi package
// reads them to describe the endpoints. They mirror the bodies the handlers
// read and write; the handlers do not use them.

type docAccount struct {
	ID            int64  `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

type docGrant struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type docSession struct {
	Account docAccount `json:"account"`
	Grant   docGrant   `json:"grant"`
}

type docError struct {
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields,omitempty"`
}

type docLoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type docRegisterRequest struct {
	Email    string         `json:"email"`
	Password string         `json:"password"`
	Profile  map[string]any `json:"profile,omitempty"`
}

type docEmailRequest struct {
	Email string `json:"email"`
}

type docResetConfirmRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type docTokenRequest struct {
	Token string `json:"token"`
}

type docAccepted struct {
	Status string `json:"status"`
}

func (docAccount) SchemaName() string             { return "AccountSummary" }
func (docGrant) SchemaName() string               { return "AuthGrant" }
func (docSession) SchemaName() string             { return "Session" }
func (docError) SchemaName() string               { return "Error" }
func (docLoginRequest) SchemaName() string        { return "LoginRequest" }
func (docRegisterRequest) SchemaName() string     { return "RegisterRequest" }
func (docEmailRequest) SchemaName() string        { return "PasswordResetRequest" }
func (docResetConfirmRequest) SchemaName() string { return "PasswordResetConfirmRequest" }
func (docTokenRequest) SchemaName() string        { return "VerifyRequest" }
func (docAccepted) SchemaName() string            { return "Accepted" }

// hiddenPage keeps the HTML pages of the cookie flow out of generated API
// documentation: they are forms for browsers, not part of the JSON API.
var hiddenPage = tango.Doc(tango.RouteDoc{Hidden: true})

// jsonDocs maps each endpoint's path to its documentation.
func jsonDocs() map[string]tango.RouteDoc {
	failure := func(statuses ...int) map[int]any {
		responses := map[int]any{}
		for _, status := range statuses {
			responses[status] = docError{}
		}
		return responses
	}
	with := func(responses map[int]any, more map[int]any) map[int]any {
		for status, body := range more {
			responses[status] = body
		}
		return responses
	}
	docs := map[string]tango.RouteDoc{
		"login/": {
			Summary:     "Sign in",
			Description: "Exchanges an email or other identifier and a password for a bearer token. Every refusal is the same 401.",
			Body:        docLoginRequest{},
			Responses: with(failure(http.StatusBadRequest, http.StatusUnauthorized, http.StatusTooManyRequests),
				map[int]any{http.StatusOK: docSession{}}),
		},
		"register/": {
			Summary: "Create an account",
			Body:    docRegisterRequest{},
			Responses: with(failure(http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusTooManyRequests),
				map[int]any{http.StatusCreated: docSession{}}),
		},
		"password-reset/": {
			Summary:     "Request a password reset email",
			Description: "Answers 202 for every well-formed address, whether or not it has an account.",
			Body:        docEmailRequest{},
			Responses: with(failure(http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusTooManyRequests),
				map[int]any{http.StatusAccepted: docAccepted{}}),
		},
		"password-reset/confirm/": {
			Summary: "Set a new password with an emailed token",
			Body:    docResetConfirmRequest{},
			Responses: with(failure(http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusTooManyRequests),
				map[int]any{http.StatusNoContent: nil}),
		},
		"verify/": {
			Summary: "Verify an email address with an emailed token",
			Body:    docTokenRequest{},
			Responses: with(failure(http.StatusBadRequest, http.StatusTooManyRequests),
				map[int]any{http.StatusNoContent: nil}),
		},
		"verify/resend/": {
			Summary:     "Send the verification email again",
			Description: "Requires the bearer token of the account to verify.",
			Bearer:      true,
			Responses: with(failure(http.StatusUnauthorized, http.StatusTooManyRequests),
				map[int]any{http.StatusAccepted: docAccepted{}}),
		},
	}
	for path, doc := range docs {
		doc.Tags = []string{"accounts"}
		docs[path] = doc
	}
	return docs
}
