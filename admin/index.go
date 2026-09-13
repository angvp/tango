package admin

import (
	"net/http"

	"github.com/angvp/tango"
)

// indexView backs the admin's root ("/admin/"), so visiting the admin
// without a specific model in the URL lands somewhere useful rather than
// a 404: it redirects to the first registered model's list, or renders a
// minimal empty state if no model is registered with the admin yet.
func indexView(nav []navItem, brand Branding) tango.View {
	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodGet {
			return methodNotAllowed(ctx)
		}

		if len(nav) > 0 {
			return ctx.Redirect(nav[0].Path)
		}

		return render(ctx, http.StatusOK, indexTemplate, indexPageData{
			chrome: chrome{Nav: nav, Brand: brand},
		})
	}
}

type indexPageData struct {
	chrome
}

var indexTemplate = cloneWithContent(`{{define "content"}}
<header class="topbar">
  <div class="flex items-center gap-2">
    {{template "menuBtn"}}
    <h1 class="topbar-title">tanGO Admin</h1>
  </div>
</header>
<div class="content-body">
  <div class="card">
    <div class="empty-state">
      <p class="empty-state-title">No models registered yet</p>
      <p class="empty-state-body">Register a model with the admin (registry.Admin().Register) to see it here.</p>
    </div>
  </div>
</div>
{{end}}`)
