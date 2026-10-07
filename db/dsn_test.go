package db

import (
	"strings"
	"testing"
)

func TestParseDSNAcceptsEachDocumentedForm(t *testing.T) {
	tests := []struct {
		dsn  string
		want DSN
	}{
		{"sqlite://app.db", DSN{SQLite, "sqlite", "app.db?_foreign_keys=on"}},
		{"sqlite://data/app.db", DSN{SQLite, "sqlite", "data/app.db?_foreign_keys=on"}},
		{"sqlite:///var/data/app.db", DSN{SQLite, "sqlite", "/var/data/app.db?_foreign_keys=on"}},
		{"sqlite://:memory:", DSN{SQLite, "sqlite", ":memory:?_foreign_keys=on"}},
		{"sqlite://app.db?_pragma=busy_timeout(1000)", DSN{SQLite, "sqlite", "app.db?_pragma=busy_timeout(1000)&_foreign_keys=on"}},
		{"postgres://u:p@localhost:5432/app?sslmode=disable", DSN{Postgres, "pgx", "postgres://u:p@localhost:5432/app?sslmode=disable"}},
		{"postgresql://u:p@db.internal/app", DSN{Postgres, "pgx", "postgresql://u:p@db.internal/app"}},
	}
	for _, tt := range tests {
		t.Run(tt.dsn, func(t *testing.T) {
			got, err := ParseDSN(tt.dsn)
			if err != nil {
				t.Fatalf("ParseDSN(%q) error = %v", tt.dsn, err)
			}
			if got != tt.want {
				t.Fatalf("ParseDSN(%q) = %+v; want %+v", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestParseDSNRejectsUnrecognizedForms(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{"empty", ""},
		{"bare path", "app.db"},
		{"bare absolute path", "/var/data/app.db"},
		{"sqlite without path", "sqlite://"},
		{"sqlite single slash", "sqlite:app.db"},
		{"unknown scheme", "mysql://u:secret@localhost/app"},
		{"file URI", "file:app.db?mode=memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDSN(tt.dsn)
			if err == nil {
				t.Fatalf("ParseDSN(%q) error = nil, want an error", tt.dsn)
			}
			for _, form := range []string{"sqlite://<path>", "sqlite:///<absolute path>", "sqlite://:memory:", "postgres://"} {
				if !strings.Contains(err.Error(), form) {
					t.Fatalf("ParseDSN(%q) error = %q, want it to name %q", tt.dsn, err, form)
				}
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("ParseDSN(%q) error = %q leaks the DSN's credentials", tt.dsn, err)
			}
		})
	}
}
