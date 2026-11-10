package posts

import (
	"time"

	"api-with-admin/apps/authors"
)

// Post is the shared model exposed by both the JSON API and the admin. It
// references Author via a foreign key — see
// docs/guides/relationships-and-admin-foreign-keys.md.
type Post struct {
	ID       int64 `tango:"pk"`
	Title    string
	Body     string
	AuthorID int64 `tango:"fk=Author"`
	// Author is loaded only when a query asks for it with Query.Preload. It is
	// not a column, so it needs no migration.
	Author    *authors.Author `tango:"belongs_to=AuthorID" json:"author,omitempty"`
	CreatedAt time.Time
}
