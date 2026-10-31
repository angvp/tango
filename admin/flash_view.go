package admin

import "github.com/angvp/tango"

// statusNotice is the outcome the status region shows, with the form that
// dismisses it.
type statusNotice struct {
	Text      string
	Dismiss   string
	Next      string
	CSRFToken string
}

// dismissView clears the status region for the page the form was sent from.
// The outcome was already expired when it was shown, so the response only
// makes the dismissal explicit and returns the person to the same page.
func dismissView() tango.View {
	return func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		if !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}
		clearFlashCookie(ctx.ResponseWriter(), ctx.Request())
		return ctx.Redirect(safeAdminNext(ctx.Request().PostFormValue("next"), adminRootPath))
	}
}
