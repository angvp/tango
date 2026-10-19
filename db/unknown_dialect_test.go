package db

import (
	"strings"
	"testing"
)

func TestNewStorePanicsOnAnUnknownDialect(t *testing.T) {
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("NewStore with an unknown dialect should panic")
		}
		if msg, _ := got.(string); !strings.Contains(msg, "unsupported dialect") {
			t.Fatalf("panic %v should name the unsupported dialect", got)
		}
	}()
	NewStore(nil, Dialect(99))
}
