package sqlbind

import (
	"reflect"
	"strings"
	"testing"
)

func TestRebind(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		numbered bool
		want     string
	}{
		{"numbered keeps $N", "SELECT a FROM t WHERE x = $1 AND y = $2", true, "SELECT a FROM t WHERE x = $1 AND y = $2"},
		{"positional rewrites in order", "SELECT a FROM t WHERE x = $1 AND y = $2", false, "SELECT a FROM t WHERE x = ? AND y = ?"},
		{"no placeholders", "SELECT 1", false, "SELECT 1"},
		{"two digit index", "VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", false, "VALUES (?,?,?,?,?,?,?,?,?,?)"},
		{"string literal untouched", "SELECT '$1' FROM t WHERE x = $1", false, "SELECT '$1' FROM t WHERE x = ?"},
		{"escaped quote in literal", "SELECT 'it''s $1' WHERE x = $1", false, "SELECT 'it''s $1' WHERE x = ?"},
		{"quoted identifiers untouched", "SELECT `a$1`, \"b$1\" FROM t WHERE x = $1", false, "SELECT `a$1`, \"b$1\" FROM t WHERE x = ?"},
		{"dollar without digits untouched", "SELECT $$ x", false, "SELECT $$ x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Rebind(tc.query, tc.numbered); got != tc.want {
				t.Fatalf("Rebind(%q, %v) = %q, want %q", tc.query, tc.numbered, got, tc.want)
			}
		})
	}
}

// A positional dialect binds arguments in the order the "?" appear, so a
// query that reuses or reorders $N cannot be rewritten without changing its
// meaning.
func TestRebindRefusesWhatPositionalCannotExpress(t *testing.T) {
	for _, query := range []string{
		"SELECT 1 WHERE a = $1 OR b = $1",
		"SELECT 1 WHERE a = $2 AND b = $1",
		"SELECT 1 WHERE a = $2",
	} {
		t.Run(query, func(t *testing.T) {
			defer func() {
				got := recover()
				msg, _ := got.(string)
				if !strings.Contains(msg, "sqlbind") {
					t.Fatalf("Rebind(%q, false) panic = %v, want a sqlbind panic", query, got)
				}
			}()
			Rebind(query, false)
		})
	}
	// Numbered dialects take any of these unchanged.
	if got := Rebind("SELECT 1 WHERE a = $1 OR b = $1", true); got != "SELECT 1 WHERE a = $1 OR b = $1" {
		t.Fatalf("numbered rewrite changed the query: %q", got)
	}
}

func TestRebindArgsReportsWhichArgumentEachPlaceholderTakes(t *testing.T) {
	cases := []struct {
		query     string
		wantQuery string
		wantOrder []int
	}{
		{"SELECT 1", "SELECT 1", nil},
		{"a = $1 AND b = $2", "a = ? AND b = ?", []int{1, 2}},
		{"a = $2 AND b = $1", "a = ? AND b = ?", []int{2, 1}},
		{"a = $1 OR b = $1", "a = ? OR b = ?", []int{1, 1}},
		{"'$1' = $3", "'$1' = ?", []int{3}},
	}
	for _, tc := range cases {
		gotQuery, gotOrder := RebindArgs(tc.query)
		if gotQuery != tc.wantQuery || !reflect.DeepEqual(gotOrder, tc.wantOrder) {
			t.Errorf("RebindArgs(%q) = %q, %v; want %q, %v", tc.query, gotQuery, gotOrder, tc.wantQuery, tc.wantOrder)
		}
	}
}
