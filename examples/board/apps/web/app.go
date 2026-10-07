// Package web serves the board's HTML pages. It owns no models: it reads
// the posts app's data and renders it with html/template.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sync"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"

	"board/apps/live"
)

//go:embed templates/*.gohtml
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

type pages struct {
	store       *db.Store
	feed        *live.Feed
	postMeta    model.ModelMeta
	commentMeta model.ModelMeta
	accountMeta model.ModelMeta
	tmpl        *template.Template

	routes       *tango.RouteRegistry
	reverserOnce sync.Once
	reverser     tango.Reverser
	reverserErr  error
}

func New(store *db.Store, feed *live.Feed) tango.App {
	return tango.NewApp("web", func(registry *tango.Registry) error {
		p := &pages{store: store, feed: feed, routes: registry.Routes()}
		p.postMeta, _ = registry.Models().Get("Post")
		p.commentMeta, _ = registry.Models().Get("Comment")
		p.accountMeta, _ = registry.Models().Get("Account")

		tmpl, err := template.New("").Funcs(template.FuncMap{"url": p.url}).
			ParseFS(templatesFS, "templates/*.gohtml")
		if err != nil {
			return err
		}
		p.tmpl = tmpl

		// Accounts are managed from the admin like any other model.
		if err := registry.Admin().Register(accounts.Account{}, admin.Options{
			ListDisplay: []string{"Email", "Active", "CreatedAt"},
			Search:      []string{"Email"},
			ReadOnly:    []string{"PasswordHash", "CreatedAt"},
			Label:       "Email",
		}); err != nil {
			return err
		}

		return registry.Routes().Include("/", tango.URLs{
			tango.Path("GET", "/", p.home, tango.Name("home")),
			tango.Path("GET", "/p/{id}/", p.post, tango.Name("post")),
			tango.Path("GET", "/new/", p.requireLogin(p.newPost), tango.Name("new_post")),
			tango.Path("POST", "/new/", p.requireLogin(p.createPost)),
			tango.Path("POST", "/p/{id}/comments/", p.requireLogin(p.createComment), tango.Name("add_comment")),
			tango.Path("GET", "/static/*", serveStatic(), tango.Name("static")),
		})
	})
}

func serveStatic() tango.View {
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/static/", http.FileServer(http.FS(assets)))
	return func(ctx *tango.Context) error {
		files.ServeHTTP(ctx.ResponseWriter(), ctx.Request())
		return nil
	}
}
