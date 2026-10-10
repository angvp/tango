package storage_test

import (
	"strings"
	"testing"

	"github.com/angvp/tango/storage"
)

func TestNewKeyIsOpaqueUniqueAndValid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		key := storage.NewKey()
		if !storage.ValidKey(key) {
			t.Fatalf("NewKey() = %q, which ValidKey refuses", key)
		}
		if seen[key] {
			t.Fatalf("NewKey() repeated %q", key)
		}
		seen[key] = true
	}
	// 160 bits in lowercase base32: 32 characters.
	if got := len(storage.NewKey()); got != 32 {
		t.Fatalf("len(NewKey()) = %d, want 32", got)
	}
}

func TestValidKeyRefusesEverythingNewKeyCannotMake(t *testing.T) {
	good := storage.NewKey()
	for name, key := range map[string]string{
		"empty":         "",
		"short":         good[:31],
		"long":          good + "a",
		"uppercase":     strings.ToUpper(good),
		"a separator":   good[:16] + "/" + good[17:],
		"a traversal":   "../../etc/passwd",
		"a dot":         good[:31] + ".",
		"a backslash":   good[:31] + `\`,
		"a NUL":         good[:31] + "\x00",
		"a space":       good[:31] + " ",
		"outside alpha": good[:31] + "1",
		"unicode":       good[:30] + "é",
	} {
		t.Run(name, func(t *testing.T) {
			if storage.ValidKey(key) {
				t.Fatalf("ValidKey(%q) = true, want false", key)
			}
		})
	}
}
