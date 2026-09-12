package authors

// Author is a second, independent model registered with the admin
// alongside posts.Post, to demonstrate multiple models in the sidebar nav.
type Author struct {
	ID    int64 `tango:"pk"`
	Name  string
	Email string `tango:"unique"`
}
