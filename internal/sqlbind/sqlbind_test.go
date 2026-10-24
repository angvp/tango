package sqlbind

import (
	"reflect"
	"testing"
)

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
		{"VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", "VALUES (?,?,?,?,?,?,?,?,?,?)", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"SELECT 'it''s $1' WHERE x = $1", "SELECT 'it''s $1' WHERE x = ?", []int{1}},
		{"SELECT `a$1`, \"b$1\" FROM t WHERE x = $1", "SELECT `a$1`, \"b$1\" FROM t WHERE x = ?", []int{1}},
		{"SELECT $$ x", "SELECT $$ x", nil},
	}
	for _, tc := range cases {
		gotQuery, gotOrder := RebindArgs(tc.query)
		if gotQuery != tc.wantQuery || !reflect.DeepEqual(gotOrder, tc.wantOrder) {
			t.Errorf("RebindArgs(%q) = %q, %v; want %q, %v", tc.query, gotQuery, gotOrder, tc.wantQuery, tc.wantOrder)
		}
	}
}
