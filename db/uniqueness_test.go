package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
	"github.com/jackc/pgx/v5/pgconn"
)

type uniquenessWidget struct {
	ID    int64  `tango:"pk"`
	Email string `tango:"unique"`
}

func TestIsUniqueConstraintViolation(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	createTable := map[db.Dialect]string{
		db.SQLite:   `CREATE TABLE uniqueness_widget (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE)`,
		db.Postgres: `CREATE TABLE uniqueness_widget (id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL UNIQUE)`,
	}[dialect]
	if _, err := sqlDB.Exec(createTable); err != nil {
		t.Fatalf("create table: %v", err)
	}

	registry := model.NewRegistry()
	if err := registry.Register(uniquenessWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	meta, _ := registry.Get("uniquenessWidget")

	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	if err := store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"}); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}

	err := store.Create(ctx, meta, &uniquenessWidget{Email: "a@example.com"})
	if err == nil {
		t.Fatal("second Create with duplicate email succeeded, want a unique-constraint violation")
	}
	if !db.IsUniqueConstraintViolation(err) {
		t.Fatalf("IsUniqueConstraintViolation(%v) = false, want true", err)
	}
}

func TestIsUniqueConstraintViolationRejectsUnrelatedErrors(t *testing.T) {
	if db.IsUniqueConstraintViolation(nil) {
		t.Fatal("IsUniqueConstraintViolation(nil) = true, want false")
	}
	if db.IsUniqueConstraintViolation(errors.New("some other database error")) {
		t.Fatal("IsUniqueConstraintViolation(unrelated error) = true, want false")
	}
}

func TestIsUniqueConstraintViolationPostgresErrorCode(t *testing.T) {
	// Constructed directly rather than requiring a live Postgres connection —
	// this only proves the error-code branch, independent of the Test
	// dialect TestIsUniqueConstraintViolation runs on.
	err := &pgconn.PgError{Code: "23505"}
	if !db.IsUniqueConstraintViolation(err) {
		t.Fatal("IsUniqueConstraintViolation(pgError 23505) = false, want true")
	}

	other := &pgconn.PgError{Code: "23503"} // foreign_key_violation
	if db.IsUniqueConstraintViolation(other) {
		t.Fatal("IsUniqueConstraintViolation(pgError 23503) = true, want false")
	}
}
