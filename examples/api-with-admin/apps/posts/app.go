package posts

import (
	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
)

// New constructs the "posts" app, exposing Post through both a small JSON
// API and (once wired into the admin app in main.go) the HTML admin.
func New(store *db.Store) tango.App {
	return tango.NewApp("posts", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Post{}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Post{}, admin.Options{
			ListDisplay: []string{"Title", "AuthorID", "CreatedAt"},
			Search:      []string{"Title"},
			Ordering:    []string{"CreatedAt"},
		}); err != nil {
			return err
		}

		meta, _ := registry.Models().Get("Post")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store, meta), tango.Name("list")),
			tango.Path("GET", "/{id}/", getPost(store, meta), tango.Name("detail")),
			tango.Path("POST", "/", createPost(store, meta), tango.Name("create")),
		})
	})
}
