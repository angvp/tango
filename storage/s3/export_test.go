package s3

// SetKeyFunc makes the Store take its generated keys from next, so a test can
// force a collision.
func (s *Store) SetKeyFunc(next func() string) { s.nextKey = next }
