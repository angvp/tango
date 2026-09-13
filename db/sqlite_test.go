package db

import "testing"

func TestSQLiteForeignKeysDSNAppendsParam(t *testing.T) {
	got := SQLiteForeignKeysDSN("app.db")
	want := "app.db?_foreign_keys=on"
	if got != want {
		t.Fatalf("SQLiteForeignKeysDSN(%q) = %q, want %q", "app.db", got, want)
	}
}

func TestSQLiteForeignKeysDSNAppendsWithAmpersandWhenQueryAlreadyPresent(t *testing.T) {
	got := SQLiteForeignKeysDSN("app.db?_pragma=busy_timeout(1000)")
	want := "app.db?_pragma=busy_timeout(1000)&_foreign_keys=on"
	if got != want {
		t.Fatalf("SQLiteForeignKeysDSN(...) = %q, want %q", got, want)
	}
}

func TestSQLiteForeignKeysDSNWorksWithMemoryDSN(t *testing.T) {
	got := SQLiteForeignKeysDSN(":memory:")
	want := ":memory:?_foreign_keys=on"
	if got != want {
		t.Fatalf("SQLiteForeignKeysDSN(%q) = %q, want %q", ":memory:", got, want)
	}
}
