package testdb

import (
	"regexp"
	"testing"
)

func TestNewSchemaNameHasThePrefixAndSixteenHexDigits(t *testing.T) {
	shape := regexp.MustCompile(`^tango_test_[0-9a-f]{16}$`)
	name := newSchemaName(t)
	if !shape.MatchString(name) {
		t.Fatalf("newSchemaName() = %q, want tango_test_ followed by 16 lowercase hex digits", name)
	}
}

func TestNewSchemaNameIsDistinctAcrossCalls(t *testing.T) {
	const calls = 256
	seen := make(map[string]struct{}, calls)
	for i := 0; i < calls; i++ {
		name := newSchemaName(t)
		if _, dup := seen[name]; dup {
			t.Fatalf("newSchemaName() returned %q twice in %d calls", name, calls)
		}
		seen[name] = struct{}{}
	}
}
