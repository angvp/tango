package admin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"golang.org/x/crypto/bcrypt"
)

const envSecret = "env-secret-value"

func TestHandleCLIUsesTangoAdminPasswordWithoutPrompting(t *testing.T) {
	store := setupAccountStore(t)
	t.Setenv("TANGO_ADMIN_PASSWORD", envSecret)
	var stdout, stderr strings.Builder

	// stdin is empty: reading it would fail with "no password provided".
	handled, err := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=bob"}, strings.NewReader(""), &stdout, &stderr)
	if !handled || err != nil {
		t.Fatalf("create: handled=%v err=%v", handled, err)
	}
	if strings.Contains(stdout.String(), "New password:") {
		t.Fatalf("prompted although TANGO_ADMIN_PASSWORD is set:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "using the password from TANGO_ADMIN_PASSWORD") {
		t.Fatalf("no notice naming the variable:\n%s", stdout.String())
	}
	if !loginWorks(t, store, "bob", envSecret) {
		t.Fatal("the account does not accept the password from the environment")
	}

	t.Setenv("TANGO_ADMIN_PASSWORD", "reset-"+envSecret)
	stdout.Reset()
	handled, err = admin.HandleCLI(context.Background(), store, []string{"-tango-admin-resetpassword=bob"}, strings.NewReader(""), &stdout, &stderr)
	if !handled || err != nil {
		t.Fatalf("resetpassword: handled=%v err=%v", handled, err)
	}
	if !loginWorks(t, store, "bob", "reset-"+envSecret) {
		t.Fatal("resetpassword did not use the environment password")
	}
}

func TestHandleCLIEmptyTangoAdminPasswordFallsBackToThePrompt(t *testing.T) {
	store := setupAccountStore(t)
	t.Setenv("TANGO_ADMIN_PASSWORD", "")
	var stdout, stderr strings.Builder

	handled, err := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=bob"}, strings.NewReader("typed-password\n"), &stdout, &stderr)
	if !handled || err != nil {
		t.Fatalf("create: handled=%v err=%v", handled, err)
	}
	if !strings.Contains(stdout.String(), "New password:") || strings.Contains(stdout.String(), "TANGO_ADMIN_PASSWORD") {
		t.Fatalf("want the plain prompt and no notice:\n%s", stdout.String())
	}
	if !loginWorks(t, store, "bob", "typed-password") {
		t.Fatal("the account does not accept the typed password")
	}
}

func TestHandleCLITangoAdminPasswordWinsOverStdin(t *testing.T) {
	store := setupAccountStore(t)
	t.Setenv("TANGO_ADMIN_PASSWORD", envSecret)
	var out strings.Builder

	handled, err := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=bob"}, strings.NewReader("piped-password\n"), &out, &out)
	if !handled || err != nil {
		t.Fatalf("create: handled=%v err=%v", handled, err)
	}
	if !loginWorks(t, store, "bob", envSecret) || loginWorks(t, store, "bob", "piped-password") {
		t.Fatal("the environment password must win over stdin")
	}
}

func TestHandleCLITangoAdminPasswordIsNeverPrinted(t *testing.T) {
	store := setupAccountStore(t)
	var stdout, stderr strings.Builder

	// Accepted password.
	t.Setenv("TANGO_ADMIN_PASSWORD", envSecret)
	if _, err := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=bob"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	// A password bcrypt rejects (over 72 bytes) must fail without echoing it.
	tooLong := strings.Repeat("x", 80)
	t.Setenv("TANGO_ADMIN_PASSWORD", tooLong)
	_, err := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=carol"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("an over-long password was accepted")
	}
	// A duplicate account fails after the password was read.
	t.Setenv("TANGO_ADMIN_PASSWORD", envSecret)
	_, dupErr := admin.HandleCLI(context.Background(), store, []string{"-tango-admin-create=bob"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(dupErr, admin.ErrAccountExists) {
		t.Fatalf("duplicate = %v, want ErrAccountExists", dupErr)
	}

	for _, secret := range []string{envSecret, tooLong} {
		for name, text := range map[string]string{"stdout": stdout.String(), "stderr": stderr.String(), "error": err.Error() + dupErr.Error()} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s contains the password:\n%s", name, text)
			}
		}
	}
}

// loginWorks reports whether the stored hash of username verifies password.
func loginWorks(t *testing.T, store *db.Store, username, password string) bool {
	t.Helper()
	var stored struct{ PasswordHash string }
	if err := store.QueryRow(context.Background(), &stored, "SELECT password_hash AS PasswordHash FROM admin_user WHERE username = $1", username); err != nil {
		t.Fatalf("query stored hash for %q: %v", username, err)
	}
	return bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(password)) == nil
}
