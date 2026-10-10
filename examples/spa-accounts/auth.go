package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/angvp/tango/accounts"
	tangojwt "github.com/angvp/tango/auth/jwt"
)

// accessTokenTTL is how long an access token lives. Bearer tokens are
// stateless: nothing here can end one early, so keep this short.
const accessTokenTTL = 15 * time.Minute

// bearerAuth is the application's side of accounts' JSON mode: it issues
// and checks JWTs with auth/jwt, so accounts itself imports no token
// package. The subject of a token is the account's id.
type bearerAuth struct {
	service *tangojwt.Service
}

func (b bearerAuth) Issue(_ context.Context, account accounts.Account) (accounts.AuthGrant, error) {
	token, err := b.service.Issue(strconv.FormatInt(account.ID, 10), accessTokenTTL)
	if err != nil {
		return accounts.AuthGrant{}, err
	}
	return accounts.AuthGrant{AccessToken: token, ExpiresIn: accessTokenTTL}, nil
}

// Authenticate reports the account a request's bearer token names. A
// missing, malformed, expired or forged token is "not authenticated", not
// an error: err is for operational failures only, and verifying a token
// has none.
func (b bearerAuth) Authenticate(_ context.Context, r *http.Request) (int64, bool, error) {
	encoded, err := tangojwt.BearerToken(r)
	if err != nil {
		return 0, false, nil
	}
	claims, err := b.service.Verify(encoded)
	if err != nil {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, false, errors.Join(errors.New("token subject is not an account id"), err)
	}
	return id, true, nil
}
