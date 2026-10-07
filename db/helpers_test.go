package db_test

import (
	"database/sql"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// openTables returns a fresh database for the run's Test dialect holding a
// table for each model, built by the framework's own migrations (what
// `tango migrate` applies), plus the registry the models were registered
// in.
func openTables(t *testing.T, models ...any) (*sql.DB, db.Dialect, *model.Registry) {
	t.Helper()
	registry := registerModels(t, models...)
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.All())
	return sqlDB, dialect, registry
}

// registerModels registers each model in a new registry.
func registerModels(t *testing.T, models ...any) *model.Registry {
	t.Helper()
	registry := model.NewRegistry()
	for _, value := range models {
		if err := registry.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	return registry
}

// metaFor returns the registered metadata for value's struct type.
func metaFor(t *testing.T, registry *model.Registry, value any) model.ModelMeta {
	t.Helper()
	name := reflect.TypeOf(value).Name()
	meta, ok := registry.Get(name)
	if !ok {
		t.Fatalf("model %s not registered", name)
	}
	return meta
}

// registerModel registers value alone and returns its metadata.
func registerModel(t *testing.T, value any) model.ModelMeta {
	t.Helper()
	return metaFor(t, registerModels(t, value), value)
}

// bind rewrites a test's raw query, written with "?" placeholders, into
// dialect's placeholder syntax. Store.Query and Store.QueryRow pass SQL
// through untranslated, so tests calling them (or querying a table
// directly) must do this themselves.
func bind(dialect db.Dialect, query string) string {
	if dialect != db.Postgres {
		return query
	}
	var builder strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			builder.WriteString("$" + strconv.Itoa(n))
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
