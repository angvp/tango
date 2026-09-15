package security

import "testing"

func TestDeriveCSRFTokenIsDeterministicForSameSecret(t *testing.T) {
	first := DeriveCSRFToken("session-secret")
	second := DeriveCSRFToken("session-secret")

	if first != second {
		t.Fatalf("DeriveCSRFToken() = %q and %q, want identical tokens for the same secret", first, second)
	}
	if first == "" {
		t.Fatal("DeriveCSRFToken() = empty string, want a non-empty hex-encoded token")
	}
}

func TestDeriveCSRFTokenDiffersForDifferentSecrets(t *testing.T) {
	a := DeriveCSRFToken("secret-a")
	b := DeriveCSRFToken("secret-b")

	if a == b {
		t.Fatal("DeriveCSRFToken() produced the same token for different secrets")
	}
}

func TestVerifyCSRFTokenAcceptsMatchingToken(t *testing.T) {
	secret := "session-secret"
	token := DeriveCSRFToken(secret)

	if !VerifyCSRFToken(token, secret) {
		t.Fatal("VerifyCSRFToken() = false, want true for a token derived from the same secret")
	}
}

func TestVerifyCSRFTokenRejectsMismatchedToken(t *testing.T) {
	if VerifyCSRFToken("wrong-token", "session-secret") {
		t.Fatal("VerifyCSRFToken() = true, want false for a token that doesn't match the secret")
	}
}

func TestVerifyCSRFTokenRejectsEmptySubmittedToken(t *testing.T) {
	secret := "session-secret"

	if VerifyCSRFToken("", secret) {
		t.Fatal("VerifyCSRFToken(\"\", secret) = true, want false — empty submitted token must never verify")
	}
}

func TestVerifyCSRFTokenRejectsTokenFromDifferentSecret(t *testing.T) {
	tokenForOther := DeriveCSRFToken("other-secret")

	if VerifyCSRFToken(tokenForOther, "session-secret") {
		t.Fatal("VerifyCSRFToken() = true, want false — token derived from a different secret must not verify")
	}
}
