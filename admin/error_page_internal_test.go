package admin

import "testing"

func TestShortReferenceKeepsEightHexCharactersOrSixteenOthers(t *testing.T) {
	tests := []struct{ name, id, want string }{
		{"generated hexadecimal ID", "6c901321398a11c48237db6a32f33d51", "6c901321"},
		{"short hexadecimal ID", "abcd", "abcd"},
		{"long non-hexadecimal ID", "trace-id-with-many-characters", "trace-id-with-ma"},
		{"short non-hexadecimal ID", "req-7", "req-7"},
		{"empty ID", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortReference(tt.id); got != tt.want {
				t.Fatalf("shortReference(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

// TestSafeReturnPathIsEmptyForAnythingOutsideTheAdmin pins the helper the
// expired-form link relies on: an unsafe path yields no link.
func TestSafeReturnPathIsEmptyForAnythingOutsideTheAdmin(t *testing.T) {
	for _, raw := range []string{"https://evil.example/", "//evil.example/", `/\evil.example`, "/elsewhere/"} {
		if got := safeAdminNext(raw, ""); got != "" {
			t.Fatalf("safeAdminNext(%q) = %q, want no return path", raw, got)
		}
	}
	if got := safeAdminNext("/admin/x/new/", ""); got != "/admin/x/new/" {
		t.Fatalf("safeAdminNext for an admin path = %q", got)
	}
}
