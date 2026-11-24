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
			Filters:     []string{"AuthorID"},
			BulkDelete:  true,
			// Search the authors instead of listing every one of them in a
			// select: the form and the filter read one author, and the
			// search matches the Name and Email that the authors app sets.
			Widgets: map[string]admin.Widget{"AuthorID": admin.Lookup(admin.LookupOptions{})},
		}); err != nil {
			return err
		}

		meta, _ := registry.Models().Get("Post")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store, meta), tango.Name("list"), tango.Doc(tango.RouteDoc{
				Summary:   "List posts, newest first",
				Query:     listFilter{},
				Responses: map[int]any{200: []Post{}},
			})),
			tango.Path("GET", "/{id}/", getPost(store, meta), tango.Name("detail"), tango.Doc(tango.RouteDoc{
				Summary:   "Get one post",
				Responses: map[int]any{200: Post{}},
			})),
			tango.Path("POST", "/", createPost(store, meta), tango.Name("create"), tango.Doc(tango.RouteDoc{
				Summary:   "Create a post for an author",
				Body:      Post{},
				Responses: map[int]any{201: Post{}},
			})),
		})
	})
}

// listFilter names the list route's query parameters for the OpenAPI
// document. Only its type is read; the view parses the request itself.
type listFilter struct {
	AuthorID int64  `query:"author_id"`
	Search   string `query:"q"`
}
