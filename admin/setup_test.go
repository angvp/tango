package admin_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// migratedAdminDB returns a fresh database for the run's Test dialect, with
// tables for every model registered on registry so far plus admin's own
// AdminUser and AdminSession, created by their migrations, and a Store over
// it.
func migratedAdminDB(t *testing.T, registry *tango.Registry) (*sql.DB, *db.Store) {
	t.Helper()
	adminModels := model.NewRegistry()
	for _, m := range []any{admin.AdminUser{}, admin.AdminSession{}} {
		if err := adminModels.Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, append(registry.Models().All(), adminModels.All()...))
	return sqlDB, db.NewStore(sqlDB, dialect)
}

// seedAdminAccountAndSession creates the "admin"/"secret" account and gives
// it the session testSessionToken names, so requests carrying that cookie
// are logged in.
func seedAdminAccountAndSession(t *testing.T, sqlDB *sql.DB, store *db.Store) {
	t.Helper()
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO admin_session (token, user_id, expires_at) SELECT $1, id, $2 FROM admin_user WHERE username = $3`,
		testSessionToken, time.Now().Add(time.Hour).UTC(), "admin",
	); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}
}

// insertReturningID runs an INSERT ending in RETURNING id and returns the
// new row's id. Both dialects support RETURNING; PostgreSQL's driver has
// no LastInsertId.
func insertReturningID(t *testing.T, sqlDB *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := sqlDB.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return id
}
