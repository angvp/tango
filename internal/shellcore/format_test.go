package shellcore

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type post struct {
	Title string
	Views int
}

func TestFormatPrintsValuesDeterministically(t *testing.T) {
	when := time.Date(2026, 10, 9, 12, 30, 0, 0, time.FixedZone("x", 2*3600))
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"nil prints nothing", nil, ""},
		{"string is quoted", "hi", `"hi"`},
		{"int", 42, "42"},
		{"float", 1.5, "1.5"},
		{"bool", true, "true"},
		{"time is RFC 3339", when, "2026-10-09T12:30:00+02:00"},
		{"short bytes", []byte("hello"), `[]byte(5) "hello"`},
		{"long bytes keep a prefix only", []byte(strings.Repeat("a", 100)), `[]byte(100) "aaaaaaaaaaaaaaaa…"`},
		{"map keys are sorted", map[string]any{"b": 2, "a": "x", "c": nil}, `map[a:"x" b:2 c:nil]`},
		{"slice of scalars", []int{1, 2, 3}, "[1 2 3]"},
		{"empty slice", []string{}, "[]"},
		{"error", errors.New("boom"), `error("boom")`},
		{"struct", post{"T", 3}, `{Title:"T" Views:3}`},
		{"pointer to struct", &post{"T", 3}, `&{Title:"T" Views:3}`},
		{"nested time in a map", map[string]any{"at": when}, `map[at:2026-10-09T12:30:00+02:00]`},
		{
			"rows print one per line",
			[]map[string]any{{"ID": 1, "Title": "a"}, {"ID": 2, "Title": "b"}},
			"map[ID:1 Title:\"a\"]\nmap[ID:2 Title:\"b\"]",
		},
		{
			"rows held as any print one per line too",
			[]any{map[string]any{"ID": 1}, map[string]any{"ID": 2}},
			"map[ID:1]\nmap[ID:2]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.value, 0); got != tt.want {
				t.Fatalf("Format = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatIsStableAcrossCalls(t *testing.T) {
	value := map[string]any{"k1": 1, "k2": 2, "k3": 3, "k4": 4, "k5": 5, "k6": 6}
	first := Format(value, 0)
	for range 50 {
		if got := Format(value, 0); got != first {
			t.Fatalf("Format changed between calls: %q vs %q", got, first)
		}
	}
}

func TestFormatTruncatesEachValueOnlyWhenAskedTo(t *testing.T) {
	long := strings.Repeat("x", 300)
	if got := Format(long, 0); !strings.HasSuffix(got, `x"`) || len(got) != 302 {
		t.Fatalf("limit 0 must not truncate, got %d bytes", len(got))
	}
	got := Format(long, 200)
	if want := 200; len([]rune(got)) != want+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated to %d runes ending %q, want %d runes plus a visible ellipsis", len([]rune(got)), got[len(got)-4:], want)
	}
	rows := []map[string]any{{"a": long}, {"a": "short"}}
	lines := strings.Split(Format(rows, 200), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "…") || strings.Contains(lines[1], "…") {
		t.Fatalf("each row line is truncated on its own, got %q", lines)
	}
}
