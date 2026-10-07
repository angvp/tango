package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// User and UserSession are docs/guides/application-auth.md's models, as
// written there. "user" is a reserved word on PostgreSQL, so this checks
// the guide's example works on both Test dialects.
type User struct {
	ID           int64  `tango:"pk"`
	Email        string `tango:"unique"`
	PasswordHash string
}

type UserSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=User,index"`
	ExpiresAt time.Time
}

func TestApplicationAuthGuideModelsMigrateAndLogIn(t *testing.T) {
	ctx := context.Background()
	models := model.NewRegistry()
	for _, value := range []any{User{}, UserSession{}} {
		if err := models.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	userMeta, _ := models.Get("User")
	sessionMeta, _ := models.Get("UserSession")

	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	store.UseModels(models)

	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := User{Email: "ada@example.test", PasswordHash: hash}
	if err := store.Create(ctx, userMeta, &user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// The guide's login lookup.
	var users []User
	query := db.Query{Where: []db.Condition{{Field: "Email", Op: db.OpEq, Value: "ada@example.test"}}, Limit: 1}
	if err := store.List(ctx, userMeta, query, &users); err != nil {
		t.Fatalf("look up user by email: %v", err)
	}
	if len(users) != 1 || !auth.VerifyPassword(users[0].PasswordHash, "s3cret") {
		t.Fatalf("lookup = %+v, want Ada with a matching password", users)
	}

	token, _, err := auth.CreateSession(ctx, store, sessionMeta, users[0].ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	userID, ok, err := auth.SessionUser(ctx, store, sessionMeta, token)
	if err != nil || !ok || userID != user.ID {
		t.Fatalf("SessionUser = %v, %v, %v; want %d, true, nil", userID, ok, err, user.ID)
	}

	if err := store.Delete(ctx, userMeta, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, ok, err := auth.SessionUser(ctx, store, sessionMeta, token); err != nil || ok {
		t.Fatalf("SessionUser after deleting the user = %v, %v; want no session (cascade)", ok, err)
	}
}
