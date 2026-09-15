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
		is_staff BOOLEAN NOT NULL,
		is_superuser BOOLEAN NOT NULL,
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

func TestCreateAccountDefaultsToStaffAndSuperuser(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()

	if err := admin.CreateAccount(ctx, store, "alice", "s3cret"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	var row struct {
		IsStaff     bool
		IsSuperuser bool
	}
	if err := store.QueryRow(ctx, &row, "SELECT is_staff AS IsStaff, is_superuser AS IsSuperuser FROM admin_user WHERE username = ?", "alice"); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if !row.IsStaff || !row.IsSuperuser {
		t.Fatalf("IsStaff=%v IsSuperuser=%v, want both true by default", row.IsStaff, row.IsSuperuser)
	}
}

func TestCreateAccountWithoutStaffAndWithoutSuperuserOptOut(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()

	if err := admin.CreateAccount(ctx, store, "bob", "s3cret", admin.WithoutStaff(), admin.WithoutSuperuser()); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	var row struct {
		IsStaff     bool
		IsSuperuser bool
	}
	if err := store.QueryRow(ctx, &row, "SELECT is_staff AS IsStaff, is_superuser AS IsSuperuser FROM admin_user WHERE username = ?", "bob"); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if row.IsStaff || row.IsSuperuser {
		t.Fatalf("IsStaff=%v IsSuperuser=%v, want both false after opting out", row.IsStaff, row.IsSuperuser)
	}
}

func TestGrantAndRevokeStaffAndSuperuserChangeOnlyTheNamedFlag(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	if err := admin.CreateAccount(ctx, store, "alice", "s3cret"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	readFlags := func() (active, isStaff, isSuperuser bool, hash string) {
		t.Helper()
		var row struct {
			Active       bool
			IsStaff      bool
			IsSuperuser  bool
			PasswordHash string
		}
		if err := store.QueryRow(ctx, &row, "SELECT active AS Active, is_staff AS IsStaff, is_superuser AS IsSuperuser, password_hash AS PasswordHash FROM admin_user WHERE username = ?", "alice"); err != nil {
			t.Fatalf("query row: %v", err)
		}
		return row.Active, row.IsStaff, row.IsSuperuser, row.PasswordHash
	}

	_, _, _, originalHash := readFlags()

	if err := admin.RevokeStaff(ctx, store, "alice"); err != nil {
		t.Fatalf("RevokeStaff: %v", err)
	}
	active, isStaff, isSuperuser, hash := readFlags()
	if isStaff || !isSuperuser || !active || hash != originalHash {
		t.Fatalf("after RevokeStaff: active=%v isStaff=%v isSuperuser=%v hashChanged=%v, want only isStaff to flip",
			active, isStaff, isSuperuser, hash != originalHash)
	}

	if err := admin.GrantStaff(ctx, store, "alice"); err != nil {
		t.Fatalf("GrantStaff: %v", err)
	}
	if _, isStaff, _, _ := readFlags(); !isStaff {
		t.Fatal("after GrantStaff: isStaff = false, want true")
	}

	if err := admin.RevokeSuperuser(ctx, store, "alice"); err != nil {
		t.Fatalf("RevokeSuperuser: %v", err)
	}
	active, isStaff, isSuperuser, hash = readFlags()
	if isSuperuser || !isStaff || !active || hash != originalHash {
		t.Fatalf("after RevokeSuperuser: active=%v isStaff=%v isSuperuser=%v hashChanged=%v, want only isSuperuser to flip",
			active, isStaff, isSuperuser, hash != originalHash)
	}

	if err := admin.GrantSuperuser(ctx, store, "alice"); err != nil {
		t.Fatalf("GrantSuperuser: %v", err)
	}
	if _, _, isSuperuser, _ := readFlags(); !isSuperuser {
		t.Fatal("after GrantSuperuser: isSuperuser = false, want true")
	}

	if err := admin.GrantStaff(ctx, store, "no-such-user"); !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("GrantStaff unknown user = %v, want ErrAccountNotFound", err)
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

func TestHandleCLICreateWithNoStaffAndNoSuperuserFlags(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	var stdout strings.Builder

	handled, err := admin.HandleCLI(ctx, store,
		[]string{"-tango-admin-create=carol", "-tango-admin-no-staff", "-tango-admin-no-superuser"},
		strings.NewReader("hunter2\n"), &stdout, &stdout)
	if !handled || err != nil {
		t.Fatalf("HandleCLI create with opt-outs: handled=%v err=%v", handled, err)
	}

	var row struct {
		IsStaff     bool
		IsSuperuser bool
	}
	if err := store.QueryRow(ctx, &row, "SELECT is_staff AS IsStaff, is_superuser AS IsSuperuser FROM admin_user WHERE username = ?", "carol"); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if row.IsStaff || row.IsSuperuser {
		t.Fatalf("IsStaff=%v IsSuperuser=%v, want both false with opt-out flags", row.IsStaff, row.IsSuperuser)
	}
}

// TestReadPasswordReturnsErrorOnEmptyInput covers readPassword's
// no-input branch (via HandleCLI's create and resetpassword flags): a
// stdin that closes without a line — e.g. a script piping nothing into
// "tango admin create" by mistake — must return a clear error instead of
// silently accepting an empty password.
func TestReadPasswordReturnsErrorOnEmptyInput(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	var stdout strings.Builder

	handled, err := admin.HandleCLI(ctx, store, []string{"-tango-admin-create=alice"}, strings.NewReader(""), &stdout, &stdout)
	if !handled || err == nil {
		t.Fatalf("HandleCLI create with no stdin input: handled=%v err=%v, want handled=true, err != nil", handled, err)
	}

	if err := admin.CreateAccount(ctx, store, "alice", "s3cret"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	stdout.Reset()
	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-resetpassword=alice"}, strings.NewReader(""), &stdout, &stdout)
	if !handled || err == nil {
		t.Fatalf("HandleCLI resetpassword with no stdin input: handled=%v err=%v, want handled=true, err != nil", handled, err)
	}
}

// TestHandleCLIPropagatesAccountOperationErrors covers HandleCLI's error
// branches for each flag's underlying account operation failing: creating
// a duplicate username, resetting or deactivating an unknown account, and
// the shared grant/revoke verb loop's error path — all must surface the
// error rather than reporting success.
func TestHandleCLIPropagatesAccountOperationErrors(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	var stdout strings.Builder

	if err := admin.CreateAccount(ctx, store, "bob", "s3cret"); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	handled, err := admin.HandleCLI(ctx, store, []string{"-tango-admin-create=bob"}, strings.NewReader("hunter2\n"), &stdout, &stdout)
	if !handled || !errors.Is(err, admin.ErrAccountExists) {
		t.Fatalf("HandleCLI create duplicate: handled=%v err=%v, want handled=true, ErrAccountExists", handled, err)
	}

	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-resetpassword=no-such-user"}, strings.NewReader("hunter2\n"), &stdout, &stdout)
	if !handled || !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("HandleCLI resetpassword unknown user: handled=%v err=%v, want handled=true, ErrAccountNotFound", handled, err)
	}

	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-deactivate=no-such-user"}, strings.NewReader(""), &stdout, &stdout)
	if !handled || !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("HandleCLI deactivate unknown user: handled=%v err=%v, want handled=true, ErrAccountNotFound", handled, err)
	}

	handled, err = admin.HandleCLI(ctx, store, []string{"-tango-admin-grant-staff=no-such-user"}, strings.NewReader(""), &stdout, &stdout)
	if !handled || !errors.Is(err, admin.ErrAccountNotFound) {
		t.Fatalf("HandleCLI grant-staff unknown user: handled=%v err=%v, want handled=true, ErrAccountNotFound", handled, err)
	}
}

func TestHandleCLIGrantAndRevokeStaffAndSuperuserVerbs(t *testing.T) {
	store := setupAccountStore(t)
	ctx := context.Background()
	var stdout strings.Builder

	if handled, err := admin.HandleCLI(ctx, store, []string{"-tango-admin-create=dave"}, strings.NewReader("hunter2\n"), &stdout, &stdout); !handled || err != nil {
		t.Fatalf("HandleCLI create: handled=%v err=%v", handled, err)
	}

	for _, tc := range []struct {
		flag    string
		message string
	}{
		{"-tango-admin-revoke-staff", "revoked staff access"},
		{"-tango-admin-grant-staff", "granted staff access"},
		{"-tango-admin-revoke-superuser", "revoked superuser access"},
		{"-tango-admin-grant-superuser", "granted superuser access"},
	} {
		stdout.Reset()
		handled, err := admin.HandleCLI(ctx, store, []string{tc.flag + "=dave"}, strings.NewReader(""), &stdout, &stdout)
		if !handled || err != nil {
			t.Fatalf("HandleCLI %s: handled=%v err=%v", tc.flag, handled, err)
		}
		if !strings.Contains(stdout.String(), tc.message) {
			t.Fatalf("HandleCLI %s: stdout = %q, want to contain %q", tc.flag, stdout.String(), tc.message)
		}
	}
}
