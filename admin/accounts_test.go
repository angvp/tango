package admin_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

// seedSession registers admin.AdminSession into a standalone model registry
// (mirroring how the admin package itself derives AdminSession's metadata)
// and inserts one row belonging to userID, for tests that need an existing
// session to observe being invalidated.
func seedSession(t *testing.T, store *db.Store, userID int64) {
	t.Helper()
	registry := model.NewRegistry()
	if err := registry.Register(admin.AdminSession{}); err != nil {
		t.Fatalf("register AdminSession: %v", err)
	}
	meta, _ := registry.Get("AdminSession")
	session := admin.AdminSession{
		Token:     "tok1",
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(context.Background(), meta, &session); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func setupAccountStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if _, err := sqlDB.Exec(`CREATE TABLE admin_user (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		active BOOLEAN NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_user: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE admin_session (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL,
		expires_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_session: %v", err)
	}

	return db.NewStore(sqlDB, db.SQLite)
}

func TestCreateAccountHashesPasswordAndRejectsDuplicateUsername(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()

	if err := admin.CreateAccount(ctx, store, "alice", "s3cret"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	var stored struct{ PasswordHash string }
	if err := store.QueryRow(ctx, &stored, "SELECT password_hash AS PasswordHash FROM admin_user WHERE username = ?", "alice"); err != nil {
		t.Fatalf("query stored hash: %v", err)
	}
	if stored.PasswordHash == "s3cret" {
		t.Fatal("password stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("s3cret")); err != nil {
		t.Fatalf("stored hash does not verify against original password: %v", err)
	}

	err := admin.CreateAccount(ctx, store, "alice", "different")
	if !errors.Is(err, admin.ErrAccountExists) {
		t.Fatalf("CreateAccount duplicate = %v, want ErrAccountExists", err)
	}
}

func TestResetPasswordUpdatesHashAndInvalidatesSessions(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()

	if err := admin.CreateAccount(ctx, store, "alice", "old-password"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	seedSession(t, store, 1)

	if err := admin.ResetPassword(ctx, store, "alice", "new-password"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	var stored struct{ PasswordHash string }
	if err := store.QueryRow(ctx, &stored, "SELECT password_hash AS PasswordHash FROM admin_user WHERE username = ?", "alice"); err != nil {
		t.Fatalf("query stored hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("new-password")); err != nil {
		t.Fatalf("stored hash does not verify against new password: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("old-password")); err == nil {
		t.Fatal("old password still verifies after reset")
	}

	var after struct{ Count int }
	if err := store.QueryRow(ctx, &after, "SELECT COUNT(*) AS Count FROM admin_session"); err != nil {
		t.Fatalf("count sessions after reset: %v", err)
	}
	if after.Count != 0 {
		t.Fatalf("sessions remaining after ResetPassword = %d, want 0", after.Count)
	}

	if err := admin.ResetPassword(ctx, store, "no-such-user", "x"); !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("ResetPassword unknown user = %v, want ErrAccountNotFound", err)
	}
}

func TestDeactivateDisablesAccountWithoutDeletingItAndInvalidatesSessions(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()

	if err := admin.CreateAccount(ctx, store, "alice", "password"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	seedSession(t, store, 1)

	if err := admin.Deactivate(ctx, store, "alice"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	var row struct {
		Active bool
	}
	if err := store.QueryRow(ctx, &row, "SELECT active AS Active FROM admin_user WHERE username = ?", "alice"); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if row.Active {
		t.Fatal("account still active after Deactivate")
	}

	var count struct{ Count int }
	if err := store.QueryRow(ctx, &count, "SELECT COUNT(*) AS Count FROM admin_session"); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count.Count != 0 {
		t.Fatalf("sessions remaining after Deactivate = %d, want 0", count.Count)
	}

	if err := admin.Deactivate(ctx, store, "no-such-user"); !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("Deactivate unknown user = %v, want ErrAccountNotFound", err)
	}
}

func TestHandleCLICreateResetDeactivate(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	var stdout strings.Builder

	handled, err := admin.HandleCLI(ctx, store, []string{"-tango-admin-create=bob"}, strings.NewReader("hunter2\n"), &stdout, &stdout)
	if !handled || err != nil {
		t.Fatalf("HandleCLI create: handled=%v err=%v", handled, err)
	}
	if !strings.Contains(stdout.String(), "created admin account") {
		t.Fatalf("stdout = %q, want created-account message", stdout.String())
	}

	stdout.Reset()
	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-resetpassword=bob"}, strings.NewReader("newpass\n"), &stdout, &stdout)
	if !handled || err != nil {
		t.Fatalf("HandleCLI resetpassword: handled=%v err=%v", handled, err)
	}

	stdout.Reset()
	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-deactivate=bob"}, strings.NewReader(""), &stdout, &stdout)
	if !handled || err != nil {
		t.Fatalf("HandleCLI deactivate: handled=%v err=%v", handled, err)
	}

	handled, err = admin.HandleCLI(ctx, store, []string{"-check"}, strings.NewReader(""), &stdout, &stdout)
	if handled || err != nil {
		t.Fatalf("HandleCLI with unrelated flag: handled=%v err=%v, want false/nil", handled, err)
	}
}
