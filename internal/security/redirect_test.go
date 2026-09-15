package security

import "testing"

func TestIsSafeRedirect(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		allowedPrefix string
		want          bool
	}{
		{
			name:          "same-app relative path under prefix",
			raw:           "/admin/posts",
			allowedPrefix: "/admin",
			want:          true,
		},
		{
			name:          "path equal to the prefix itself",
			raw:           "/admin",
			allowedPrefix: "/admin",
			want:          true,
		},
		{
			name:          "path outside the allowed prefix",
			raw:           "/other/posts",
			allowedPrefix: "/admin",
			want:          false,
		},
		{
			name:          "empty allowed prefix always rejects",
			raw:           "/admin",
			allowedPrefix: "",
			want:          false,
		},
		{
			name:          "protocol-relative URL is rejected even if it starts with the prefix",
			raw:           "//evil.example.com",
			allowedPrefix: "//evil.example.com",
			want:          false,
		},
		{
			name:          "absolute URL with scheme is rejected",
			raw:           "/adminhttps://evil.example.com",
			allowedPrefix: "/admin",
			want:          false,
		},
		{
			name:          "absolute URL not matching the prefix is rejected",
			raw:           "https://evil.example.com/admin",
			allowedPrefix: "/admin",
			want:          false,
		},
		{
			name:          "empty raw target is rejected",
			raw:           "",
			allowedPrefix: "/admin",
			want:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSafeRedirect(tc.raw, tc.allowedPrefix); got != tc.want {
				t.Fatalf("IsSafeRedirect(%q, %q) = %v, want %v", tc.raw, tc.allowedPrefix, got, tc.want)
			}
		})
	}
}

func TestSafeRedirectReturnsRawWhenSafe(t *testing.T) {
	got := SafeRedirect("/admin/posts", "/admin", "/admin")
	if got != "/admin/posts" {
		t.Fatalf("SafeRedirect() = %q, want the original safe target", got)
	}
}

func TestSafeRedirectReturnsFallbackWhenUnsafe(t *testing.T) {
	got := SafeRedirect("https://evil.example.com", "/admin", "/admin/default")
	if got != "/admin/default" {
		t.Fatalf("SafeRedirect() = %q, want fallback %q", got, "/admin/default")
	}
}

func TestSafeRedirectReturnsFallbackForProtocolRelativeTarget(t *testing.T) {
	got := SafeRedirect("//evil.example.com", "/admin", "/admin/default")
	if got != "/admin/default" {
		t.Fatalf("SafeRedirect() = %q, want fallback %q", got, "/admin/default")
	}
}
