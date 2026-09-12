package posts

import "time"

// Post is the shared model exposed by both the JSON API and the admin.
type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	CreatedAt time.Time
}
