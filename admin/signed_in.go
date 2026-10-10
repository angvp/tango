package admin

import "context"

// signedIn is the account the sidebar footer shows, and the CSRF token its
// logout form submits.
type signedIn struct {
	Username  string
	CSRFToken string
}

type signedInKey struct{}

func withSignedIn(ctx context.Context, who signedIn) context.Context {
	return context.WithValue(ctx, signedInKey{}, &who)
}

func signedInFrom(ctx context.Context) *signedIn {
	who, _ := ctx.Value(signedInKey{}).(*signedIn)
	return who
}
