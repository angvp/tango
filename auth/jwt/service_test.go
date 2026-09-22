package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var (
	testSecret  = []byte("0123456789abcdef0123456789abcdef")
	otherSecret = []byte("abcdef0123456789abcdef0123456789")
)

func TestNewServiceValidation(t *testing.T) {
	valid := Key{ID: "active", Secret: testSecret}
	tests := []struct {
		name       string
		active     Key
		retired    []Key
		issuer     string
		audience   string
		opts       []ServiceOption
		wantErr    bool
		wantPhrase string
	}{
		{"valid", valid, nil, "issuer", "audience", nil, false, ""},
		{"empty active id", Key{Secret: testSecret}, nil, "issuer", "audience", nil, true, "key ID"},
		{"short active secret", Key{ID: "active", Secret: []byte("short")}, nil, "issuer", "audience", nil, true, "at least"},
		{"short retired secret", valid, []Key{{ID: "old", Secret: []byte("short")}}, "issuer", "audience", nil, true, "at least"},
		{"active duplicate", valid, []Key{{ID: "active", Secret: otherSecret}}, "issuer", "audience", nil, true, "duplicate"},
		{"retired duplicate", valid, []Key{{ID: "old", Secret: otherSecret}, {ID: "old", Secret: testSecret}}, "issuer", "audience", nil, true, "duplicate"},
		{"empty issuer", valid, nil, "", "audience", nil, true, "issuer"},
		{"empty audience", valid, nil, "issuer", "", nil, true, "audience"},
		{"zero ttl", valid, nil, "issuer", "audience", []ServiceOption{WithMaxTTL(0)}, true, "TTL"},
		{"negative skew", valid, nil, "issuer", "audience", []ServiceOption{WithClockSkew(-time.Second)}, true, "clock skew"},
		{"large skew", valid, nil, "issuer", "audience", []ServiceOption{WithClockSkew(5*time.Minute + time.Nanosecond)}, true, "clock skew"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(test.active, test.retired, test.issuer, test.audience, test.opts...)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), test.wantPhrase) {
					t.Fatalf("NewService() = %v, %v; want error containing %q", service, err, test.wantPhrase)
				}
				return
			}
			if err != nil || service == nil {
				t.Fatalf("NewService() = %v, %v", service, err)
			}
		})
	}
}

func TestIssueAndVerify(t *testing.T) {
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	service.now = func() time.Time { return fixed }
	encoded, err := service.Issue("user-42", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.Verify(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-42" || claims.Issuer != "issuer" || claims.Audience != "audience" || !claims.IssuedAt.Equal(fixed) || !claims.ExpiresAt.Equal(fixed.Add(time.Hour)) {
		t.Fatalf("Claims = %+v", claims)
	}
}

func TestIssueValidation(t *testing.T) {
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	for _, test := range []struct {
		name    string
		subject string
		ttl     time.Duration
	}{
		{"empty subject", "", time.Minute},
		{"blank subject", "  ", time.Minute},
		{"zero ttl", "user", 0},
		{"negative ttl", "user", -time.Second},
		{"over max", "user", DefaultMaxTTL + time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Issue(test.subject, test.ttl); err == nil {
				t.Fatal("Issue returned nil error")
			}
		})
	}
}

func TestVerifyFailures(t *testing.T) {
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	service := mustService(t, Key{ID: "active", Secret: testSecret}, nil)
	service.now = func() time.Time { return fixed }
	valid := signRegistered(t, testSecret, "active", jwtlib.RegisteredClaims{
		Subject: "user", Issuer: "issuer", Audience: jwtlib.ClaimStrings{"audience"},
		IssuedAt: jwtlib.NewNumericDate(fixed), ExpiresAt: jwtlib.NewNumericDate(fixed.Add(time.Hour)),
	})
	tampered := valid[:len(valid)-1] + "x"
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"malformed", "not-a-token", ErrInvalidToken},
		{"tampered", tampered, ErrInvalidToken},
		{"unknown kid", signRegistered(t, testSecret, "missing", standardClaims(fixed)), ErrInvalidToken},
		{"wrong issuer", signRegistered(t, testSecret, "active", claimsWith(fixed, "other", "audience")), ErrInvalidToken},
		{"wrong audience", signRegistered(t, testSecret, "active", claimsWith(fixed, "issuer", "other")), ErrInvalidToken},
		{"missing subject", signRegistered(t, testSecret, "active", jwtlib.RegisteredClaims{Issuer: "issuer", Audience: jwtlib.ClaimStrings{"audience"}, IssuedAt: jwtlib.NewNumericDate(fixed), ExpiresAt: jwtlib.NewNumericDate(fixed.Add(time.Hour))}), ErrInvalidToken},
		{"future issued-at", signRegistered(t, testSecret, "active", claimsAt(fixed.Add(time.Minute), fixed.Add(time.Hour))), ErrInvalidToken},
		{"expiry before issued-at", signRegistered(t, testSecret, "active", claimsAt(fixed, fixed.Add(-time.Minute))), ErrInvalidToken},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Verify(test.token)
			if !errors.Is(err, test.want) {
				t.Fatalf("Verify error = %v, want %v", err, test.want)
			}
		})
	}

	none := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, standardClaims(fixed))
	none.Header["kid"] = "active"
	encodedNone, err := none.SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(encodedNone); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("none algorithm error = %v", err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaToken := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, standardClaims(fixed))
	rsaToken.Header["kid"] = "active"
	encodedRSA, err := rsaToken.SignedString(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(encodedRSA); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("RS256 algorithm error = %v", err)
	}
}

func TestExpiryAndClockSkew(t *testing.T) {
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	service, err := NewService(Key{ID: "active", Secret: testSecret}, nil, "issuer", "audience", WithClockSkew(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return fixed }
	within := signRegistered(t, testSecret, "active", claimsAt(fixed.Add(-time.Hour), fixed.Add(-30*time.Second)))
	if _, err := service.Verify(within); err != nil {
		t.Fatalf("within skew: %v", err)
	}
	beyond := signRegistered(t, testSecret, "active", claimsAt(fixed.Add(-time.Hour), fixed.Add(-time.Minute)))
	if _, err := service.Verify(beyond); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("beyond skew error = %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	old := mustService(t, Key{ID: "old", Secret: testSecret}, nil)
	old.now = func() time.Time { return fixed }
	token, err := old.Issue("user", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rotated := mustService(t, Key{ID: "new", Secret: otherSecret}, []Key{{ID: "old", Secret: testSecret}})
	rotated.now = func() time.Time { return fixed }
	if _, err := rotated.Verify(token); err != nil {
		t.Fatalf("retired key token failed: %v", err)
	}
	removed := mustService(t, Key{ID: "new", Secret: otherSecret}, nil)
	removed.now = func() time.Time { return fixed }
	if _, err := removed.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("removed key token error = %v", err)
	}
}

func mustService(t *testing.T, active Key, retired []Key) *Service {
	t.Helper()
	service, err := NewService(active, retired, "issuer", "audience")
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func standardClaims(now time.Time) jwtlib.RegisteredClaims {
	return claimsAt(now, now.Add(time.Hour))
}

func claimsAt(issuedAt, expiresAt time.Time) jwtlib.RegisteredClaims {
	return claimsWithTimes(issuedAt, expiresAt, "issuer", "audience")
}

func claimsWith(now time.Time, issuer, audience string) jwtlib.RegisteredClaims {
	return claimsWithTimes(now, now.Add(time.Hour), issuer, audience)
}

func claimsWithTimes(issuedAt, expiresAt time.Time, issuer, audience string) jwtlib.RegisteredClaims {
	return jwtlib.RegisteredClaims{Subject: "user", Issuer: issuer, Audience: jwtlib.ClaimStrings{audience}, IssuedAt: jwtlib.NewNumericDate(issuedAt), ExpiresAt: jwtlib.NewNumericDate(expiresAt)}
}

func signRegistered(t *testing.T, secret []byte, kid string, claims jwtlib.RegisteredClaims) string {
	t.Helper()
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	encoded, err := token.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
