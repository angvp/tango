package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// pageChromeKey carries a page's navigation and branding into the request
// context, so the helpers that answer a failed request can render the shared
// layout without the closure of the view that built it.
type pageChromeKey struct{}

// pageChrome is the navigation and branding an HTML page shows.
type pageChrome struct {
	nav   []navItem
	brand Branding
}

func pageChromeFrom(ctx context.Context) pageChrome {
	page, _ := ctx.Value(pageChromeKey{}).(pageChrome)
	return page
}

// htmlPage serves next as an HTML page. A view that fails is answered with the
// server-failure page, and the detail is logged with the request's reference.
// Body-size rejections pass through unchanged so the framework still answers
// them with 413.
func htmlPage(page pageChrome, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		*ctx.Request() = *ctx.Request().WithContext(context.WithValue(ctx.Context(), pageChromeKey{}, page))
		return serverFailure(ctx, next(ctx))
	}
}

// serverFailure turns a view's error into the server-failure page. Nil and
// body-size errors are returned unchanged.
func serverFailure(ctx *tango.Context, err error) error {
	var tooLarge *http.MaxBytesError
	if err == nil || errors.As(err, &tooLarge) {
		return err
	}
	ctx.Logger().LogAttrs(ctx.Context(), slog.LevelError, tango.EventViewError, slog.Any("error", err))

	text := errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.server_title", "Something went wrong"),
		Sentence: i18n.T(ctx.Context(), "admin.error.server_load", "This page could not be loaded."),
		Next:     i18n.T(ctx.Context(), "admin.error.server_next", "Try again in a few minutes. If it keeps happening, give the reference below to the site administrator."),
		LinkHref: "/admin/",
		LinkText: i18n.T(ctx.Context(), "admin.error.home", "Back to the admin home"),
	}
	if ctx.Request().Method != http.MethodGet {
		text.Sentence = i18n.T(ctx.Context(), "admin.error.server_save", "Your changes were not saved.")
	}
	if reference, ok := tango.RequestIDFromContext(ctx.Context()); ok {
		text.ReferenceText = i18n.T(ctx.Context(), "admin.error.reference", "Reference: %s", reference)
	}
	return renderErrorPage(ctx, http.StatusInternalServerError, text)
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
    <div>{{template "breadcrumbs" .}}<h1 class="topbar-title">{{.Title}}</h1></div>
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
	page := pageChromeFrom(ctx.Context())
	return render(ctx, status, errorPageTemplate, errorPageData{
		chrome:    chrome{Nav: page.nav, Brand: page.brand}.withContext(ctx.Context()),
		errorText: text,
	})
}

// homeLink is the next step on pages a person reaches by following a link.
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
// acted on.
func forbiddenCSRF(ctx *tango.Context) error {
	href, linkText := homeLink(ctx)
	return renderErrorPage(ctx, http.StatusForbidden, errorText{
		Title:    i18n.T(ctx.Context(), "admin.error.csrf_title", "Your form expired"),
		Sentence: i18n.T(ctx.Context(), "admin.error.csrf", "This form was open too long, or you signed in again in another window."),
		Next:     i18n.T(ctx.Context(), "admin.error.csrf_next", "Reload the page and submit the form again."),
		LinkHref: href,
		LinkText: linkText,
	})
}

// forbidden answers a signed-in account that lacks staff access.
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
