package db

// Postgres coverage for Store. These tests only run when
// TANGO_TEST_POSTGRES_DSN is set to a reachable PostgreSQL connection
// string, so `go test ./...` needs no Postgres server by default:
//
//	TANGO_TEST_POSTGRES_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./db/...

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/angvp/tango/model"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgresWidget struct {
	ID        int64 `tango:"pk"`
	Name      string
	Active    bool
	Count     int
	Score     float64
	CreatedAt time.Time
}

func TestStoreCreateBackfillsGeneratedPrimaryKeyPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	createdAt := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	widget := postgresWidget{Name: "alpha", Active: true, Count: 3, Score: 1.5, CreatedAt: createdAt}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID == 0 {
		t.Fatalf("Create did not backfill generated primary key: %+v", widget)
	}
}

func TestStoreCreatePreservesCallerSuppliedPrimaryKeyPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	widget := postgresWidget{ID: 42, Name: "beta", Active: false, Count: 1, Score: 2.5, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID != 42 {
		t.Fatalf("Create overwrote caller-supplied primary key: got %d, want 42", widget.ID)
	}
}

func TestStoreGetScansMatchingRowIntoDestPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	widget := postgresWidget{Name: "gamma", Active: true, Count: 5, Score: 9.5, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var got postgresWidget
	if err := store.Get(context.Background(), meta, widget.ID, &got); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != widget.Name {
		t.Fatalf("Get scanned Name = %q, want %q", got.Name, widget.Name)
	}
}

func TestStoreGetUnknownPrimaryKeyReturnsErrNotFoundPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	var got postgresWidget
	err := store.Get(context.Background(), meta, int64(999999), &got)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreUpdatePersistsFieldChangesPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	widget := postgresWidget{Name: "delta", Active: true, Count: 1, Score: 1, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	widget.Name = "delta-updated"
	widget.Count = 2
	if err := store.Update(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	var got postgresWidget
	if err := store.Get(context.Background(), meta, widget.ID, &got); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "delta-updated" || got.Count != 2 {
		t.Fatalf("Get after Update = %+v, want Name=delta-updated Count=2", got)
	}
}

func TestStoreUpdateUnknownPrimaryKeyReturnsErrNotFoundPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	widget := postgresWidget{ID: 999999, Name: "missing"}
	err := store.Update(context.Background(), meta, &widget)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreDeleteRemovesRowPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	widget := postgresWidget{Name: "epsilon", CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if err := store.Delete(context.Background(), meta, widget.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	var got postgresWidget
	err := store.Get(context.Background(), meta, widget.ID, &got)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestStoreDeleteUnknownPrimaryKeyReturnsErrNotFoundPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	err := store.Delete(context.Background(), meta, int64(999999))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreListOrdersAndPaginatesRowsPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	for i, name := range []string{"zeta", "eta", "theta"} {
		widget := postgresWidget{Name: name, Count: i, CreatedAt: time.Now().UTC()}
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	}

	var got []postgresWidget
	err := store.List(context.Background(), meta, Query{OrderBy: []string{"Name"}, Limit: 2}, &got)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d rows, want 2", len(got))
	}
	if got[0].Name != "eta" || got[1].Name != "theta" {
		t.Fatalf("List order = %+v, want eta then theta", got)
	}
}

func TestStoreListRejectsUnknownOrderByFieldPostgres(t *testing.T) {
	sqlDB := openPostgresTestDB(t)
	meta := registerPostgresModel(t, postgresWidget{})
	store := NewStore(sqlDB, Postgres)

	var got []postgresWidget
	err := store.List(context.Background(), meta, Query{OrderBy: []string{"Nonexistent"}}, &got)
	if err == nil {
		t.Fatalf("List with unknown OrderBy field returned nil error")
	}
}

func postgresTestDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TANGO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TANGO_TEST_POSTGRES_DSN not set; skipping Postgres-backed Store tests")
	}
	return dsn
}

func openPostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("pgx", postgresTestDSN(t))
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	_, err = sqlDB.ExecContext(context.Background(), `DROP TABLE IF EXISTS postgres_widget`)
	if err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, err = sqlDB.ExecContext(context.Background(), `
		CREATE TABLE postgres_widget (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL,
			count INTEGER NOT NULL,
			score DOUBLE PRECISION NOT NULL,
			created_at TIMESTAMPTZ
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(), `DROP TABLE IF EXISTS postgres_widget`)
	})

	return sqlDB
}

func registerPostgresModel(t *testing.T, value any) model.ModelMeta {
	t.Helper()

	registry := model.NewRegistry()
	if err := registry.Register(value); err != nil {
		t.Fatalf("register model: %v", err)
	}

	meta, ok := registry.Get("PostgresWidget")
	if !ok {
		t.Fatalf("model PostgresWidget not registered")
	}
	return meta
}
