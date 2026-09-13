package auth_test

import (
	"testing"

	"github.com/angvp/tango/auth"
)

func TestCSRFHelpersDeriveAndVerifyToken(t *testing.T) {
	token := auth.DeriveCSRFToken("session-secret")
	if token == "" || token == "session-secret" {
		t.Fatalf("derived token = %q, want non-empty and not the raw secret", token)
	}
	if !auth.VerifyCSRFToken(token, "session-secret") {
		t.Fatal("VerifyCSRFToken returned false for the derived token")
	}
	if auth.VerifyCSRFToken(token, "different-secret") {
		t.Fatal("VerifyCSRFToken returned true for a different secret")
	}
	if auth.VerifyCSRFToken("", "session-secret") {
		t.Fatal("VerifyCSRFToken returned true for an empty submitted token")
	}
}

func TestSafeRedirectHelpersUseArbitraryPrefix(t *testing.T) {
	if got := auth.SafeRedirect("/app/dashboard/?tab=1", "/app/", "/app/"); got != "/app/dashboard/?tab=1" {
		t.Fatalf("SafeRedirect valid = %q", got)
	}
	for _, raw := range []string{"https://evil.example/", "//evil.example/", "/admin/", "/application/"} {
		if auth.IsSafeRedirect(raw, "/app/") {
			t.Fatalf("IsSafeRedirect(%q, /app/) = true, want false", raw)
		}
		if got := auth.SafeRedirect(raw, "/app/", "/app/"); got != "/app/" {
			t.Fatalf("SafeRedirect(%q) = %q, want fallback /app/", raw, got)
		}
	}
}
