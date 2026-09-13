package posts

import "time"

// Post is the shared model exposed by both the JSON API and the admin. It
// references Author via a foreign key (Milestone 14) — see
// docs/guides/relationships-and-admin-foreign-keys.md.
type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}
