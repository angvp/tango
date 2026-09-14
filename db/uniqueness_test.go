package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/angvp/tango/model"
	"github.com/jackc/pgx/v5/pgconn"
	_ "modernc.org/sqlite"
)

type uniquenessWidget struct {
	ID    int64  `tango:"pk"`
	Email string `tango:"unique"`
}

func TestIsUniqueConstraintViolationSQLite(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(`CREATE TABLE uniqueness_widget (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	registry := model.NewRegistry()
	if err := registry.Register(uniquenessWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	meta, _ := registry.Get("uniquenessWidget")

	store := NewStore(sqlDB, SQLite)
	ctx := context.Background()

	if err := store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"}); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}

	err = store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"})
	if err == nil {
		t.Fatal("second Create with duplicate email succeeded, want a unique-constraint violation")
	}
	if !IsUniqueConstraintViolation(err) {
		t.Fatalf("IsUniqueConstraintViolation(%v) = false, want true", err)
	}
}

func TestIsUniqueConstraintViolationRejectsUnrelatedErrors(t *testing.T) {
	if IsUniqueConstraintViolation(nil) {
		t.Fatal("IsUniqueConstraintViolation(nil) = true, want false")
	}
	if IsUniqueConstraintViolation(errors.New("some other database error")) {
		t.Fatal("IsUniqueConstraintViolation(unrelated error) = true, want false")
	}
}

func TestIsUniqueConstraintViolationPostgresErrorCode(t *testing.T) {
	// Constructed directly rather than requiring a live Postgres connection —
	// this only proves the error-code branch, independent of
	// TestIsUniqueConstraintViolationPostgres below (which is skipped
	// without TANGO_TEST_POSTGRES_DSN).
	err := &pgconn.PgError{Code: "23505"}
	if !IsUniqueConstraintViolation(err) {
		t.Fatal("IsUniqueConstraintViolation(pgError 23505) = false, want true")
	}

	other := &pgconn.PgError{Code: "23503"} // foreign_key_violation
	if IsUniqueConstraintViolation(other) {
		t.Fatal("IsUniqueConstraintViolation(pgError 23503) = true, want false")
	}
}

func TestIsUniqueConstraintViolationPostgres(t *testing.T) {
	sqlDB, err := sql.Open("pgx", postgresTestDSN(t))
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `DROP TABLE IF EXISTS uniqueness_widget`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE uniqueness_widget (id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(), `DROP TABLE IF EXISTS uniqueness_widget`)
	})

	registry := model.NewRegistry()
	if err := registry.Register(uniquenessWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	meta, _ := registry.Get("uniquenessWidget")

	store := NewStore(sqlDB, Postgres)

	if err := store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"}); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}

	err = store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"})
	if err == nil {
		t.Fatal("second Create with duplicate email succeeded, want a unique-constraint violation")
	}
	if !IsUniqueConstraintViolation(err) {
		t.Fatalf("IsUniqueConstraintViolation(%v) = false, want true", err)
	}
}
