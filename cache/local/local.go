// Package local is a bounded in-process cache.Store, for development, tests
// and single-process deployments. It is per process: another instance has
// its own, and a restart empties it.
//
// It never grows past its limits. When it is full it first drops entries
// that have already expired, then the least recently used. There is no
// background goroutine and nothing to start or stop.
package local

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/angvp/tango/cache"
)

// Config bounds a Store. MaxEntries is required.
type Config struct {
	// MaxEntries is the most entries held. It must be positive.
	MaxEntries int
	// MaxBytes, when positive, is the most bytes held, counting each entry's
	// full key plus its value. Zero means no byte budget; negative is
	// invalid.
	MaxBytes int64
	// Now is the clock; nil means time.Now. Tests set it to control expiry.
	Now func() time.Time
}

type entry struct {
	key     string
	value   []byte
	expires time.Time
}

func (e *entry) footprint() int64 { return int64(len(e.key) + len(e.value)) }

// Store is a bounded in-memory cache.Store.
type Store struct {
	maxEntries int
	maxBytes   int64
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]*list.Element // key -> element holding *entry
	lru     *list.List               // front is the most recently used
	bytes   int64
}

var _ cache.Store = (*Store)(nil)

// New returns an empty Store bounded by cfg.
func New(cfg Config) (*Store, error) {
	if cfg.MaxEntries <= 0 {
		return nil, errors.New("cache/local: MaxEntries must be positive")
	}
	if cfg.MaxBytes < 0 {
		return nil, fmt.Errorf("cache/local: MaxBytes must not be negative, got %d", cfg.MaxBytes)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Store{
		maxEntries: cfg.MaxEntries,
		maxBytes:   cfg.MaxBytes,
		now:        now,
		entries:    map[string]*list.Element{},
		lru:        list.New(),
	}, nil
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := check(ctx, key); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	e := el.Value.(*entry)
	if s.expired(e) {
		s.remove(el)
		return nil, false, nil
	}
	s.lru.MoveToFront(el)
	return append([]byte{}, e.value...), true, nil
}

// Set implements cache.Store. It returns cache.ErrTooLarge, leaving the
// cache untouched, for an entry whose key and value alone exceed MaxBytes.
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if err := cache.CheckTTL(ttl); err != nil {
		return err
	}
	incoming := int64(len(key) + len(value))
	if s.maxBytes > 0 && incoming > s.maxBytes {
		return fmt.Errorf("%w: %d bytes with its key, budget is %d", cache.ErrTooLarge, incoming, s.maxBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expires := s.now().Add(ttl)
	copied := append([]byte{}, value...)
	if el, ok := s.entries[key]; ok { // replace in place: the old bytes leave the total first
		e := el.Value.(*entry)
		s.bytes += int64(len(copied)) - int64(len(e.value))
		e.value, e.expires = copied, expires
		s.lru.MoveToFront(el)
	} else {
		e := &entry{key: key, value: copied, expires: expires}
		s.entries[key] = s.lru.PushFront(e)
		s.bytes += e.footprint()
	}
	s.makeRoom()
	return nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.entries[key]; ok {
		s.remove(el)
	}
	return nil
}

// makeRoom restores the limits after a Set: expired entries go first, then
// the least recently used. The entry just written is at the front and fits
// the byte budget on its own, so it is never the one evicted.
func (s *Store) makeRoom() {
	if !s.overLimit() {
		return
	}
	for el := s.lru.Back(); el != nil; {
		prev := el.Prev()
		if s.expired(el.Value.(*entry)) {
			s.remove(el)
		}
		el = prev
	}
	for s.overLimit() && s.lru.Len() > 1 {
		s.remove(s.lru.Back())
	}
}

func (s *Store) overLimit() bool {
	return s.lru.Len() > s.maxEntries || (s.maxBytes > 0 && s.bytes > s.maxBytes)
}

func (s *Store) expired(e *entry) bool { return !s.now().Before(e.expires) }

func (s *Store) remove(el *list.Element) {
	e := el.Value.(*entry)
	s.lru.Remove(el)
	delete(s.entries, e.key)
	s.bytes -= e.footprint()
}

// check is the preamble every call shares: the context, then the key.
func check(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cache.ValidKey(key) {
		return cache.ErrInvalidKey
	}
	return nil
}
