package gcs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/storage/gcs"
)

// Credential-construction errors carry no wrapped chain, so errors.Is and
// errors.As cannot reach the SDK or the operating system error behind them.
func TestCredentialConstructionErrorsCarryNoChain(t *testing.T) {
	isolateAmbientCredentials(t)
	dir := t.TempDir()
	fileWithSecret := filepath.Join(dir, "key.json")
	if err := os.WriteFile(fileWithSecret, []byte(`{"type":"`+secretMarker+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]gcs.Config{
		"file read fails":         {Bucket: testBucket, CredentialsFile: filepath.Join(dir, "missing.json")},
		"malformed JSON":          {Bucket: testBucket, CredentialsJSON: []byte(`{"type":"service_account","private_key":"` + secretMarker + `"`)},
		"type echoed by the SDK":  {Bucket: testBucket, CredentialsJSON: []byte(`{"type":"` + secretMarker + `"}`)},
		"type echoed from a file": {Bucket: testBucket, CredentialsFile: fileWithSecret},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			cfg.TempDir = t.TempDir()
			s, err := gcs.New(context.Background(), cfg)
			if err != nil {
				t.Fatalf("New failed at construction: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err = s.Stat(ctx, testKey)
			if err == nil {
				t.Fatal("Stat with unusable credentials succeeded")
			}
			if errors.Unwrap(err) != nil {
				t.Fatalf("credential error carries a chain: %v", err)
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Fatalf("credential error echoes a credential: %v", err)
			}
		})
	}
}

// An application-default lookup that fails at construction carries no chain
// either. The variable names a file that does not exist, which makes the
// lookup fail deterministically, unlike a bare host, whose GCE detection the
// SDK caches across tests.
func TestApplicationDefaultLookupFailureCarriesNoChain(t *testing.T) {
	isolateAmbientCredentials(t)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New failed at construction: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = s.Stat(ctx, testKey)
	if err == nil {
		t.Fatal("Stat with a missing application-default credentials file succeeded")
	}
	if errors.Unwrap(err) != nil {
		t.Fatalf("credential error carries a chain: %v", err)
	}
}

// The SDK quotes its input with escapes in some messages. A credential value
// with real newline and tab characters then appears in the message in its
// JSON-escaped form, which a match on the decoded value does not find.
func TestCredentialErrorsNeverEchoAnEscapedForm(t *testing.T) {
	isolateAmbientCredentials(t)
	const value = "bad\nSECRET-CREDENTIAL-MARKER-91c2\tline"
	escaped := strings.TrimSuffix(strings.TrimPrefix(mustJSONString(t, value), `"`), `"`)
	body := []byte(`{"type":` + mustJSONString(t, value) + `}`)

	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, CredentialsJSON: body, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New failed at construction: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = s.Stat(ctx, testKey)
	if err == nil {
		t.Fatal("Stat with an unusable credential type succeeded")
	}
	msg := err.Error()
	if strings.Contains(msg, value) {
		t.Fatalf("error echoes the decoded credential value: %v", err)
	}
	if strings.Contains(msg, escaped) {
		t.Fatalf("error echoes the JSON-escaped credential value: %v", err)
	}
}

func mustJSONString(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
