package db

import "testing"

func TestPlaceholderSQLiteAlwaysReturnsQuestionMark(t *testing.T) {
	for n := 1; n <= 3; n++ {
		if got := placeholder(SQLite, n); got != "?" {
			t.Fatalf("placeholder(SQLite, %d) = %q, want \"?\"", n, got)
		}
	}
}

func TestPlaceholderPostgresReturnsPositionalSyntax(t *testing.T) {
	cases := map[int]string{1: "$1", 2: "$2", 3: "$3", 10: "$10"}
	for n, want := range cases {
		if got := placeholder(Postgres, n); got != want {
			t.Fatalf("placeholder(Postgres, %d) = %q, want %q", n, got, want)
		}
	}
}
