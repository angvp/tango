package admin

import (
	"context"

	"github.com/angvp/tango"
)

// signedIn is the account the sidebar footer shows, and the CSRF token its
// logout form submits.
type signedIn struct {
	Username  string
	CSRFToken string
	// Notice is the outcome the status region shows on this page, or nil.
	Notice *statusNotice
}

type signedInKey struct{}

// replaceRequestContext swaps the request's context in place. tango.Context has
// no setter, and a handler owns its request, so the replacement is visible to
// the rest of the handler chain.
func replaceRequestContext(ctx *tango.Context, c context.Context) {
	*ctx.Request() = *ctx.Request().WithContext(c)
}

func withSignedIn(ctx context.Context, who signedIn) context.Context {
	return context.WithValue(ctx, signedInKey{}, &who)
}

func signedInFrom(ctx context.Context) *signedIn {
	who, _ := ctx.Value(signedInKey{}).(*signedIn)
	return who
}
