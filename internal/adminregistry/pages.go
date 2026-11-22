package adminregistry

import (
	"errors"
	"fmt"
)

// ErrDuplicatePage is returned when a page path is registered twice.
var ErrDuplicatePage = errors.New("adminregistry: page path already registered")

// PageEntry is a staff-only admin page as the registry stores it. View is the
// host's tango.View; it is held as an any because this package cannot import
// the root package, and the admin package, which can, checks its type.
type PageEntry struct {
	Path  string
	Title string
	Nav   bool
	View  any
}

// AddPage records a page. It refuses a path that is already registered.
func (r *Registry) AddPage(entry PageEntry) error {
	for _, existing := range r.pages {
		if existing.Path == entry.Path {
			return fmt.Errorf("%w: %q", ErrDuplicatePage, entry.Path)
		}
	}
	r.pages = append(r.pages, entry)
	return nil
}

// Pages returns the registered pages in registration order.
func (r *Registry) Pages() []PageEntry {
	return append([]PageEntry(nil), r.pages...)
}
