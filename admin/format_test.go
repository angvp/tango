package admin_test

import (
	"testing"

	"github.com/angvp/tango/admin"
)

func TestByteSizeFormatsWithBinaryUnits(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1.5 KiB"},
		{1 << 20, "1 MiB"},
		{1572864, "1.5 MiB"},
		{4 << 20, "4 MiB"},
		{1 << 30, "1 GiB"},
		{1 << 40, "1 TiB"},
		{1 << 50, "1 PiB"},
		{1 << 60, "1 EiB"},
		{1048527, "1 MiB"},
		{-5, "0 B"},
	}
	for _, tt := range tests {
		if got := admin.ByteSize(tt.size); got != tt.want {
			t.Errorf("ByteSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestShortHashAbbreviatesADigest(t *testing.T) {
	const digest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"full sha256 hex", digest, "9f86d081884c"},
		{"already short", "9f86d08", "9f86d08"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := admin.ShortHash(tt.in); got != tt.want {
				t.Fatalf("ShortHash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
