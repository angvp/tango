package db

import (
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestParseDSNAcceptsEachDocumentedForm(t *testing.T) {
	tests := []struct {
		dsn  string
		want DSN
	}{
		{"sqlite://app.db", DSN{SQLite, "sqlite", "app.db?_pragma=busy_timeout(5000)&_foreign_keys=on"}},
		{"sqlite://data/app.db", DSN{SQLite, "sqlite", "data/app.db?_pragma=busy_timeout(5000)&_foreign_keys=on"}},
		{"sqlite:///var/data/app.db", DSN{SQLite, "sqlite", "/var/data/app.db?_pragma=busy_timeout(5000)&_foreign_keys=on"}},
		{"sqlite://:memory:", DSN{SQLite, "sqlite", ":memory:?_pragma=busy_timeout(5000)&_foreign_keys=on"}},
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
		{"unknown scheme", "oracle://u:secret@localhost/app"},
		{"file URI", "file:app.db?mode=memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDSN(tt.dsn)
			if err == nil {
				t.Fatalf("ParseDSN(%q) error = nil, want an error", tt.dsn)
			}
			for _, form := range []string{"sqlite://<path>", "sqlite:///<absolute path>", "sqlite://:memory:", "postgres://", "mysql://"} {
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

func TestParseDSNMySQLAppliesDefaultsAndLetsTheDSNOverrideThem(t *testing.T) {
	got, err := ParseDSN("mysql://app:p%40ss%2Fword@db.internal:3307/shop")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if got.Dialect != MySQL || got.Driver != "mysql" {
		t.Fatalf("got dialect %v driver %q, want MySQL and mysql", got.Dialect, got.Driver)
	}
	cfg, err := mysql.ParseDSN(got.Source)
	if err != nil {
		t.Fatalf("driver cannot parse %q: %v", got.Source, err)
	}
	if cfg.User != "app" || cfg.Passwd != "p@ss/word" || cfg.Addr != "db.internal:3307" || cfg.DBName != "shop" {
		t.Fatalf("connection parts wrong: user %q addr %q db %q", cfg.User, cfg.Addr, cfg.DBName)
	}
	if !cfg.ParseTime || cfg.Loc != time.UTC || !cfg.ClientFoundRows || cfg.Collation != "utf8mb4_bin" {
		t.Fatalf("defaults missing: parseTime %v loc %v clientFoundRows %v collation %q", cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows, cfg.Collation)
	}
	if cfg.Params["transaction_isolation"] != "'READ-COMMITTED'" {
		t.Fatalf("isolation = %q, want READ COMMITTED", cfg.Params["transaction_isolation"])
	}
	if !strings.Contains(cfg.Params["sql_mode"], "STRICT_TRANS_TABLES") {
		t.Fatalf("sql_mode = %q, want strict mode", cfg.Params["sql_mode"])
	}

	overridden, err := ParseDSN("mysql://u@h/d?transaction_isolation=%27REPEATABLE-READ%27&timeout=3s")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	cfg, err = mysql.ParseDSN(overridden.Source)
	if err != nil {
		t.Fatalf("driver cannot parse %q: %v", overridden.Source, err)
	}
	if cfg.Params["transaction_isolation"] != "'REPEATABLE-READ'" || cfg.Timeout != 3*time.Second || cfg.Addr != "h:3306" {
		t.Fatalf("override lost: isolation %q timeout %v addr %q", cfg.Params["transaction_isolation"], cfg.Timeout, cfg.Addr)
	}
}

func TestParseDSNMySQLRejectsBadFormsWithoutEchoingThem(t *testing.T) {
	for name, dsn := range map[string]string{
		"no host":     "mysql:///shop",
		"no database": "mysql://u:secret@h",
		"nested path": "mysql://u:secret@h/a/b",
		"bad port":    "mysql://u:secret@h:notaport/shop",
		"bad option":  "mysql://u:secret@h/shop?timeout=forever",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDSN(dsn)
			if err == nil {
				t.Fatalf("ParseDSN(%q) error = nil, want an error", dsn)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "forever") {
				t.Fatalf("error %q echoes the DSN", err)
			}
		})
	}
}
