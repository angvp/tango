package cache_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/local"
)

func TestValidKeyAcceptsOnlyPortablePrintableASCII(t *testing.T) {
	for name, tt := range map[string]struct {
		key string
		ok  bool
	}{
		"a plain key":        {"profile:42", true},
		"every printable":    {"!\"#$%&'()*+,-./0123456789:;<=>?@AZ[\\]^_`az{|}~", true},
		"one byte":           {"k", true},
		"exactly 200 bytes":  {strings.Repeat("k", 200), true},
		"201 bytes":          {strings.Repeat("k", 201), false},
		"empty":              {"", false},
		"a space":            {"a b", false},
		"a tab":              {"a\tb", false},
		"a newline":          {"a\nb", false},
		"a NUL":              {"a\x00b", false},
		"DEL":                {"a\x7fb", false},
		"non-ASCII":          {"café", false},
		"a leading space":    {" a", false},
		"a trailing newline": {"a\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := cache.ValidKey(tt.key); got != tt.ok {
				t.Fatalf("ValidKey(%q) = %v, want %v", tt.key, got, tt.ok)
			}
		})
	}
}

func TestCheckTTLHasOnePortableRange(t *testing.T) {
	for name, tt := range map[string]struct {
		ttl time.Duration
		ok  bool
	}{
		"one nanosecond":           {1, true},
		"a minute":                 {time.Minute, true},
		"exactly 30 days":          {cache.MaxTTL, true},
		"30 days and a nanosecond": {cache.MaxTTL + 1, false},
		"zero":                     {0, false},
		"negative":                 {-time.Second, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := cache.CheckTTL(tt.ttl)
			if tt.ok && err != nil {
				t.Fatalf("CheckTTL(%v) = %v, want nil", tt.ttl, err)
			}
			if !tt.ok && !errors.Is(err, cache.ErrInvalidTTL) {
				t.Fatalf("CheckTTL(%v) = %v, want ErrInvalidTTL", tt.ttl, err)
			}
		})
	}
	if cache.MaxTTL != 30*24*time.Hour {
		t.Fatalf("MaxTTL = %v, want 30 days", cache.MaxTTL)
	}
}

func newLocal(t *testing.T) *local.Store {
	t.Helper()
	s, err := local.New(local.Config{MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrefixNamespacesKeysAndValidatesTheFinalKey(t *testing.T) {
	ctx := context.Background()
	base := newLocal(t)
	v2 := cache.Prefix(base, "profile:v2:")
	v3 := cache.Prefix(base, "profile:v3:")

	if err := v2.Set(ctx, "42", []byte("old"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := base.Get(ctx, "profile:v2:42"); err != nil || !ok || string(got) != "old" {
		t.Fatalf("underlying key = %q, %v, %v; want the prefixed key", got, ok, err)
	}
	if _, ok, err := v3.Get(ctx, "42"); err != nil || ok {
		t.Fatalf("a bumped prefix still sees the old value: ok=%v err=%v", ok, err)
	}
	if err := v2.Delete(ctx, "42"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := base.Get(ctx, "profile:v2:42"); ok {
		t.Fatal("Delete through a prefix left the value")
	}
}

func TestPrefixAcceptsAnUnusualPrefixWhileEveryResultingKeyIsValid(t *testing.T) {
	ctx := context.Background()
	s := cache.Prefix(newLocal(t), "")
	if err := s.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("an empty prefix is unusual but every key is valid: %v", err)
	}
	long := cache.Prefix(newLocal(t), strings.Repeat("p", 190))
	if err := long.Set(ctx, strings.Repeat("k", 10), []byte("v"), time.Minute); err != nil {
		t.Fatalf("a 190-byte prefix with a 10-byte key is exactly 200: %v", err)
	}
}

func TestPrefixRefusesAKeyThatWouldBeInvalidOnceCombined(t *testing.T) {
	ctx := context.Background()
	for name, tt := range map[string]struct{ prefix, key string }{
		"too long once combined":     {strings.Repeat("p", 190), strings.Repeat("k", 11)},
		"a prefix with a space":      {"my cache:", "k"},
		"a prefix with a newline":    {"a\n", "k"},
		"an invalid key":             {"p:", "bad key"},
		"an empty key and no prefix": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := cache.Prefix(newLocal(t), tt.prefix)
			if _, _, err := s.Get(ctx, tt.key); !errors.Is(err, cache.ErrInvalidKey) {
				t.Errorf("Get: %v, want ErrInvalidKey", err)
			}
			if err := s.Set(ctx, tt.key, []byte("v"), time.Minute); !errors.Is(err, cache.ErrInvalidKey) {
				t.Errorf("Set: %v, want ErrInvalidKey", err)
			}
			if err := s.Delete(ctx, tt.key); !errors.Is(err, cache.ErrInvalidKey) {
				t.Errorf("Delete: %v, want ErrInvalidKey", err)
			}
		})
	}
}
