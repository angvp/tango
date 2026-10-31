package admin

import (
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// densityBasePath is the route the changelist density control posts to.
const densityBasePath = "/admin/density/"

// densityView handles POST for /admin/density/. It runs behind requireSession
// and checks the session's CSRF token before it touches the cookie. Only
// comfortable and compact are stored; anything else is refused.
func densityView() tango.View {
	return func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		if !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}

		switch density := ctx.Request().PostForm.Get("density"); density {
		case densityComfortable, densityCompact:
			setDensityCookie(ctx.ResponseWriter(), ctx.Request(), density)
		default:
			return invalidDensity(ctx)
		}
		return ctx.Redirect(safeAdminReturnPath(ctx.Request().PostForm.Get("next")))
	}
}

func invalidDensity(ctx *tango.Context) error {
	return ctx.JSON(http.StatusBadRequest, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.invalid_density", "invalid density")})
}
