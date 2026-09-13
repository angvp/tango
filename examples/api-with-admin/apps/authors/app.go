package authors

import (
	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// New constructs the "authors" app, registering Author with the admin only
// (no JSON routes) — a model doesn't need its own API to appear in admin.
func New() tango.App {
	return tango.NewApp("authors", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Author{}); err != nil {
			return err
		}
		return registry.Admin().Register(Author{}, admin.Options{
			ListDisplay: []string{"Name", "Email"},
			Search:      []string{"Name", "Email"},
			Ordering:    []string{"Name"},
			// Label lets admin show "Jane Doe" wherever an Author is
			// referenced elsewhere (e.g. posts.Post.AuthorID) instead of a
			// raw row ID — see Milestone 14.
			Label: "Name",
		})
	})
}
