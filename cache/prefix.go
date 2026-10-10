package cache

import (
	"context"
	"time"
)

// Prefix returns a Store that puts prefix in front of every key before
// delegating to store. It validates the final prefixed key, so an unusual
// prefix (even an empty one) is accepted as long as every resulting key is
// valid, and a combination that is too long or has a space is refused with
// ErrInvalidKey.
//
// Versioning is yours: change "profile:v2:" to "profile:v3:" and every older
// entry becomes unreachable and expires by its TTL. That is the way to
// invalidate in bulk; there is no Clear.
func Prefix(store Store, prefix string) Store {
	return &prefixed{store: store, prefix: prefix}
}

type prefixed struct {
	store  Store
	prefix string
}

func (p *prefixed) key(key string) (string, error) {
	full := p.prefix + key
	if !ValidKey(full) {
		return "", ErrInvalidKey
	}
	return full, nil
}

func (p *prefixed) Get(ctx context.Context, key string) ([]byte, bool, error) {
	full, err := p.key(key)
	if err != nil {
		return nil, false, err
	}
	return p.store.Get(ctx, full)
}

func (p *prefixed) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	full, err := p.key(key)
	if err != nil {
		return err
	}
	return p.store.Set(ctx, full, value, ttl)
}

func (p *prefixed) Delete(ctx context.Context, key string) error {
	full, err := p.key(key)
	if err != nil {
		return err
	}
	return p.store.Delete(ctx, full)
}
