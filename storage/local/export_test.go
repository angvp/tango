package local

import "github.com/angvp/tango/storage"

// NewWithKeys opens a Store whose generated keys come from next, so a test
// can force a collision.
func NewWithKeys(root string, next func() string) (*Store, error) {
	s, err := New(root)
	if err != nil {
		return nil, err
	}
	s.nextKey = next
	return s, nil
}

var _ storage.Store = (*Store)(nil)
