package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/observabilitysafe"
)

// chromeKey carries a page's chrome in the request context. htmlPage installs
// the brand alone, so a failure before the account is known to be staff shows
// no navigation. requireSession installs the navigation once the account is
// staff.
type chromeKey struct{}

func chromeFrom(ctx context.Context) chrome {
	page, _ := ctx.Value(chromeKey{}).(chrome)
	return page
}

func withChrome(ctx context.Context, page chrome) context.Context {
	return context.WithValue(ctx, chromeKey{}, page)
}

// htmlPage serves next as an HTML page. A failed view is answered with the
// server-failure page. A panic is not recovered here: the admin lets a panic
// reach the framework, as the host's Display functions rely on.
func htmlPage(brand chrome, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		replaceRequestContext(ctx, withChrome(ctx.Context(), brand))
		return serverFailure(ctx, next(ctx))
	}
}

// serverFailure answers a failed view with the server-failure page. The full
// error is logged with the request's reference, and the page shows only that
// reference. Nil and body-size errors are returned unchanged, so the framework
// answers them.
func serverFailure(ctx *tango.Context, err error) error {
	var tooLarge *http.MaxBytesError
	if err == nil || errors.As(err, &tooLarge) {
		return err
	}
	reference, hasReference := tango.RequestIDFromContext(ctx.Context())
	short := shortReference(reference)
	observabilitysafe.Call(func() {
		attrs := []slog.Attr{slog.Any("error", err)}
		if hasReference {
			attrs = append(attrs, slog.String("reference", short))
		}
		ctx.Logger().LogAttrs(ctx.Context(), slog.LevelError, tango.EventViewError, attrs...)
	})

	text := errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.server_title", "Something went wrong"),
		Sentence: i18n.T(ctx.Context(), "admin.error.server_load", "This page could not be loaded."),
		Next:     i18n.T(ctx.Context(), "admin.error.server_next", "Try again in a few minutes. If it keeps happening, give the reference below to the site administrator."),
	}
	text.LinkHref, text.LinkText = homeLink(ctx)
	if ctx.Request().Method != http.MethodGet {
		text.Sentence = i18n.T(ctx.Context(), "admin.error.server_save", "Your changes were not saved.")
	}
	if hasReference {
		text.ReferenceText = i18n.T(ctx.Context(), "admin.error.reference", "Reference: %s", short)
	}
	return renderErrorPage(ctx, http.StatusInternalServerError, text)
}

// shortReference is the part of a request ID a person can read back to support:
// the first eight characters of a hexadecimal ID, which is what RequestID
// generates, or the first sixteen characters of any other ID.
func shortReference(id string) string {
	limit := 16
	if isHexadecimal(id) {
		limit = 8
	}
	if len(id) > limit {
		return id[:limit]
	}
	return id
}

func isHexadecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// errorText is the copy of one error page: a title, one sentence, a next step
// and an optional link. ReferenceText is set only for a server failure whose
// request carries a reference.
type errorText struct {
	Title         string
	Sentence      string
	Next          string
	LinkHref      string
	LinkText      string
	ReferenceText string
}

// errorPageData is what the error page template renders.
type errorPageData struct {
	chrome
	errorText
}

var errorPageTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="inline-row">
    {{template "menuBtn" .}}
    <h1 class="topbar-title">{{.Title}}</h1>
  </div>
</header>
<div class="content-body">
  <div class="card max-w-lg">
    <div class="card-body">
      <p class="text-base text-ink">{{.Sentence}}</p>
      {{if .Next}}<p class="mt-3 text-sm text-ink-muted">{{.Next}}</p>{{end}}
      {{if .ReferenceText}}<p class="mt-3 text-sm text-ink-muted">{{.ReferenceText}}</p>{{end}}
      {{if .LinkHref}}<a href="{{.LinkHref}}" class="btn-secondary mt-3">{{.LinkText}}</a>{{end}}
    </div>
  </div>
</div>
{{end}}`)

// renderErrorPage writes text as an HTML page in the shared layout with status.
func renderErrorPage(ctx *tango.Context, status int, text errorText) error {
	return render(ctx, status, errorPageTemplate, errorPageData{
		chrome:    chromeFrom(ctx.Context()).withContext(ctx.Context()),
		errorText: text,
	})
}

// homeLink is the next step on pages that lead back to the admin home.
func homeLink(ctx *tango.Context) (string, string) {
	return "/admin/", i18n.T(ctx.Context(), "admin.error.home", "Back to the admin home")
}

// notFound answers a request for a row or page that does not exist.
func notFound(ctx *tango.Context) error {
	href, linkText := homeLink(ctx)
	return renderErrorPage(ctx, http.StatusNotFound, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.not_found_title", "We could not find that page"),
		Sentence: i18n.T(ctx.Context(), "admin.error.not_found", "It may have been deleted, or the link may be wrong."),
		Next:     i18n.T(ctx.Context(), "admin.error.not_found_next", "Go back to the admin home and find it there."),
		LinkHref: href,
		LinkText: linkText,
	})
}

// methodNotAllowed answers a request whose method the page does not accept.
// The caller sets the Allow header first.
func methodNotAllowed(ctx *tango.Context) error {
	href, linkText := homeLink(ctx)
	return renderErrorPage(ctx, http.StatusMethodNotAllowed, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.method_not_allowed_title", "That action is not available"),
		Sentence: i18n.T(ctx.Context(), "admin.error.method_not_allowed", "This page does not accept that kind of request."),
		Next:     i18n.T(ctx.Context(), "admin.error.method_not_allowed_next", "Go back and use the button on the page instead."),
		LinkHref: href,
		LinkText: linkText,
	})
}

// forbiddenCSRF answers a form whose CSRF check failed. The request is never
// acted on. The next step returns to the page the form was posted to, which
// shows the form again, when that path is an admin path.
func forbiddenCSRF(ctx *tango.Context) error {
	href, linkText := homeLink(ctx)
	if back := safeAdminNext(ctx.Request().URL.Path, ""); back != "" {
		href, linkText = back, i18n.T(ctx.Context(), "admin.error.back_to_page", "Back to the page")
	}
	return renderErrorPage(ctx, http.StatusForbidden, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.csrf_title", "Your form expired"),
		Sentence: i18n.T(ctx.Context(), "admin.error.csrf", "This form was open too long, or you signed in again in another window."),
		Next:     i18n.T(ctx.Context(), "admin.error.csrf_next", "Reload the page and submit the form again."),
		LinkHref: href,
		LinkText: linkText,
	})
}

// forbidden answers a signed-in account that lacks staff access. It is served
// before the navigation is installed, so the page shows no model names.
func forbidden(ctx *tango.Context) error {
	return renderErrorPage(ctx, http.StatusForbidden, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.not_staff_title", "You do not have access"),
		Sentence: i18n.T(ctx.Context(), "admin.error.not_staff", "This admin account does not have staff access."),
		Next:     i18n.T(ctx.Context(), "admin.error.not_staff_next", "Ask an administrator to give you staff access."),
	})
}

// tooManyLoginAttempts answers a sign-in attempt throttled for its source.
func tooManyLoginAttempts(ctx *tango.Context) error {
	ctx.ResponseWriter().Header().Set("Retry-After", "60")
	return renderErrorPage(ctx, http.StatusTooManyRequests, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.too_many_login_attempts_title", "Too many sign-in attempts"),
		Sentence: i18n.T(ctx.Context(), "admin.error.too_many_login_attempts", "Too many failed sign-in attempts came from this network."),
		Next:     i18n.T(ctx.Context(), "admin.error.too_many_login_attempts_next", "Wait a minute, then sign in again."),
		LinkHref: "/admin/login/",
		LinkText: i18n.T(ctx.Context(), "admin.error.sign_in", "Sign in"),
	})
}

// answeredMethods are the methods an admin page can receive. A page answers
// each one it does not handle with the HTML 405 page.
var answeredMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// answeredMethodSummary marks a route that exists only to answer a method its
// page does not handle. The framework page audit matches it to expect a 405.
const answeredMethodSummary = "Answers a method its page does not handle with the HTML 405 page."

var answeredMethodDoc = tango.Doc(tango.RouteDoc{Hidden: true, Summary: answeredMethodSummary})

// unhandledMethodRoutes routes the methods of answeredMethods that path does not
// handle to the HTML 405 page. Its Allow header lists the handled methods, and
// the brand alone is shown, because the request is not yet known to be staff.
func unhandledMethodRoutes(path string, brand Branding, handled ...string) tango.URLs {
	allow := strings.Join(handled, ", ")
	answer := htmlPage(chrome{Brand: brand}, func(ctx *tango.Context) error {
		ctx.ResponseWriter().Header().Set("Allow", allow)
		return methodNotAllowed(ctx)
	})
	var urls tango.URLs
	for _, method := range answeredMethods {
		if !slices.Contains(handled, method) {
			urls = append(urls, tango.Path(method, path, answer, answeredMethodDoc))
		}
	}
	return urls
}
