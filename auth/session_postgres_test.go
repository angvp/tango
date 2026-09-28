package auth_test

// Postgres coverage for the session helpers. These tests only run when
// TANGO_TEST_POSTGRES_DSN is set to a reachable PostgreSQL connection
// string, so `go test ./...` needs no Postgres server by default:
//
//	TANGO_TEST_POSTGRES_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./auth/...

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSessionCreateLookupAndDeletePostgres(t *testing.T) {
	store, sessionMeta, userID := buildAuthPostgresStore(t)
	ctx := context.Background()

	token, _, err := auth.CreateSession(ctx, store, sessionMeta, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	gotUserID, ok, err := auth.SessionUser(ctx, store, sessionMeta, token)
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if !ok || gotUserID != userID {
		t.Fatalf("SessionUser = (%v, %v), want (%d, true)", gotUserID, ok, userID)
	}

	if err := auth.DeleteSession(ctx, store, sessionMeta, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok, err := auth.SessionUser(ctx, store, sessionMeta, token); err != nil || ok {
		t.Fatalf("SessionUser after delete = (ok %v, err %v), want (false, nil)", ok, err)
	}
}

func buildAuthPostgresStore(t *testing.T) (*db.Store, model.ModelMeta, int64) {
	t.Helper()
	dsn := os.Getenv("TANGO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TANGO_TEST_POSTGRES_DSN not set")
	}

	models := model.NewRegistry()
	if err := models.Register(authTestUser{}); err != nil {
		t.Fatalf("register user: %v", err)
	}
	if err := models.Register(authTestSession{}); err != nil {
		t.Fatalf("register session: %v", err)
	}
	userMeta, _ := models.Get("authTestUser")
	sessionMeta, _ := models.Get("authTestSession")

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	drop := []string{`DROP TABLE IF EXISTS auth_test_session`, `DROP TABLE IF EXISTS auth_test_user`}
	for _, stmt := range append(drop,
		`CREATE TABLE auth_test_user (id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL)`,
		`CREATE TABLE auth_test_session (id BIGSERIAL PRIMARY KEY, token TEXT NOT NULL UNIQUE, user_id BIGINT NOT NULL, expires_at TIMESTAMPTZ NOT NULL)`,
	) {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		for _, stmt := range drop {
			_, _ = sqlDB.Exec(stmt)
		}
	})

	store := db.NewStore(sqlDB, db.Postgres)
	user := authTestUser{Email: "ada@example.test", PasswordHash: "hash"}
	if err := store.Create(context.Background(), userMeta, &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return store, sessionMeta, user.ID
}
