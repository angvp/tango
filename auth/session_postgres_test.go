package auth_test

// Postgres coverage for the session helpers. These tests run when the
// Test dialect is PostgreSQL (see package testdb):
//
//	TANGO_TEST_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./auth/...

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
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
	testdb.PostgresOnly(t, "Postgres twin of TestSessionCreateLookupAndDelete in session_test.go")

	models := model.NewRegistry()
	if err := models.Register(authTestUser{}); err != nil {
		t.Fatalf("register user: %v", err)
	}
	if err := models.Register(authTestSession{}); err != nil {
		t.Fatalf("register session: %v", err)
	}
	userMeta, _ := models.Get("authTestUser")
	sessionMeta, _ := models.Get("authTestSession")

	sqlDB, _ := testdb.Open(t)
	for _, stmt := range []string{
		`CREATE TABLE auth_test_user (id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL)`,
		`CREATE TABLE auth_test_session (id BIGSERIAL PRIMARY KEY, token TEXT NOT NULL UNIQUE, user_id BIGINT NOT NULL, expires_at TIMESTAMPTZ NOT NULL)`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	store := db.NewStore(sqlDB, db.Postgres)
	user := authTestUser{Email: "ada@example.test", PasswordHash: "hash"}
	if err := store.Create(context.Background(), userMeta, &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return store, sessionMeta, user.ID
}
