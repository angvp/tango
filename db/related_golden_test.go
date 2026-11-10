package db_test

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// The Related goldens pin the statements Related sends, per dialect, against
// the recording driver: the IN list, its bind order and the statement count.

type relatedIntKeyRow struct {
	OwnerID int
}

type relatedUnexportedKeyRow struct {
	ownerID int64
}

// relatedOwnerSelect is the SELECT list and FROM clause of goldenOwner.
func relatedOwnerSelect(quote string) string {
	return "SELECT " + quote + "id" + quote + ", " + quote + "name" + quote + " FROM " + quote + "golden_owner" + quote
}

// relatedInClause is the WHERE clause of an IN list over n values of column.
func relatedInClause(dialect db.Dialect, quote, column string, n int) string {
	placeholders := make([]string, n)
	for i := range placeholders {
		if dialect == db.Postgres {
			placeholders[i] = "$" + strconv.Itoa(i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	return " WHERE (" + quote + column + quote + " IN (" + strings.Join(placeholders, ", ") + "))"
}

// relatedArgs renders bind values the way the recorder does.
func relatedArgs(values ...int64) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.FormatInt(value, 10)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// relatedKeys returns n rows whose OwnerID is first, first+1, ... in order.
func relatedKeys(first int64, n int) []goldenPost {
	rows := make([]goldenPost, n)
	for i := range rows {
		rows[i] = goldenPost{OwnerID: first + int64(i)}
	}
	return rows
}

func relatedDialects() []struct {
	name    string
	dialect db.Dialect
	quote   string
} {
	return []struct {
		name    string
		dialect db.Dialect
		quote   string
	}{
		{"sqlite", db.SQLite, "`"},
		{"postgres", db.Postgres, `"`},
		{"mysql", db.MySQL, "`"},
	}
}

func relatedOwnerMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	return metaFor(t, registerModels(t, goldenOwner{}, goldenPost{}), goldenOwner{})
}

func TestRelatedSQLGoldens(t *testing.T) {
	owner := relatedOwnerMeta(t)
	for _, tc := range relatedDialects() {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("distinct keys in first-appearance order, zero skipped", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				rows := []goldenPost{{OwnerID: 2}, {OwnerID: 1}, {OwnerID: 2}, {OwnerID: 0}}
				dest := map[int64]goldenOwner{}
				if err := store.Related(context.Background(), rows, "OwnerID", owner, dest); err != nil {
					t.Fatal(err)
				}
				assertStatement(t, rec,
					relatedOwnerSelect(tc.quote)+relatedInClause(tc.dialect, tc.quote, "id", 2),
					relatedArgs(2, 1))
			})

			t.Run("one thousand distinct keys are one statement", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				if err := store.Related(context.Background(), relatedKeys(1, 1000), "OwnerID", owner, map[int64]goldenOwner{}); err != nil {
					t.Fatal(err)
				}
				values := make([]int64, 1000)
				for i := range values {
					values[i] = int64(i + 1)
				}
				assertStatement(t, rec,
					relatedOwnerSelect(tc.quote)+relatedInClause(tc.dialect, tc.quote, "id", 1000),
					relatedArgs(values...))
			})

			t.Run("one thousand and one distinct keys are two statements", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				// Each key appears twice; duplicates must not count toward the split.
				rows := append(relatedKeys(1, 1001), relatedKeys(1, 1001)...)
				if err := store.Related(context.Background(), rows, "OwnerID", owner, map[int64]goldenOwner{}); err != nil {
					t.Fatal(err)
				}
				got := rec.statements()
				if len(got) != 2 {
					t.Fatalf("recorded %d statements, want 2", len(got))
				}
				first := make([]int64, 1000)
				for i := range first {
					first[i] = int64(i + 1)
				}
				wantFirst := relatedOwnerSelect(tc.quote) + relatedInClause(tc.dialect, tc.quote, "id", 1000)
				wantSecond := relatedOwnerSelect(tc.quote) + relatedInClause(tc.dialect, tc.quote, "id", 1)
				if got[0].SQL != wantFirst || got[0].Args != relatedArgs(first...) {
					t.Errorf("first statement differs\n got: %s %s", got[0].SQL, got[0].Args)
				}
				if got[1].SQL != wantSecond || got[1].Args != relatedArgs(1001) {
					t.Errorf("second statement differs\n got: %s %s", got[1].SQL, got[1].Args)
				}
			})
		})
	}
}

// TestRelatedSendsNothingWithoutKeys pairs each empty input with a call that
// sends a statement on the same recorder, so the zero is a measurement.
func TestRelatedSendsNothingWithoutKeys(t *testing.T) {
	owner := relatedOwnerMeta(t)
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)
	ctx := context.Background()

	if err := store.Related(ctx, relatedKeys(7, 1), "OwnerID", owner, map[int64]goldenOwner{}); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.statements()); n != 1 {
		t.Fatalf("control call recorded %d statements, want 1", n)
	}

	inputs := map[string]any{
		"empty slice": []goldenPost{},
		"nil slice":   []goldenPost(nil),
		"all zero":    []goldenPost{{}, {}},
	}
	for name, rows := range inputs {
		if err := store.Related(ctx, rows, "OwnerID", owner, map[int64]goldenOwner{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := len(rec.statements()); n != 1 {
		t.Errorf("empty inputs recorded %d more statements, want none", n-1)
	}
}

// TestRelatedRefusals pairs each refusal with a valid control on a fresh
// recorder: the control must send one statement, every refusal none, and a
// refused call must leave dest as it was.
func TestRelatedRefusals(t *testing.T) {
	owner := relatedOwnerMeta(t)
	rows := []goldenPost{{OwnerID: 3}}
	filled := func() map[int64]goldenOwner {
		return map[int64]goldenOwner{99: {ID: 99, Name: "kept"}}
	}

	tests := []struct {
		name      string
		rows      any
		keyField  string
		dest      any
		wantError bool
	}{
		{"control: valid rows and dest", rows, "OwnerID", filled(), false},
		{"rows is a struct, not a slice", goldenPost{OwnerID: 3}, "OwnerID", filled(), true},
		{"rows is nil", nil, "OwnerID", filled(), true},
		{"rows is a slice of pointers", []*goldenPost{{OwnerID: 3}}, "OwnerID", filled(), true},
		{"key field missing", rows, "Missing", filled(), true},
		{"key field of another type", []relatedIntKeyRow{{OwnerID: 3}}, "OwnerID", filled(), true},
		{"key field unexported", []relatedUnexportedKeyRow{{ownerID: 3}}, "ownerID", filled(), true},
		{"dest key type differs", rows, "OwnerID", map[int]goldenOwner{99: {ID: 99}}, true},
		{"dest value type differs", rows, "OwnerID", map[int64]goldenPost{99: {ID: 99}}, true},
		{"dest is not a map", rows, "OwnerID", []goldenOwner{{ID: 99}}, true},
		{"dest is a nil map", rows, "OwnerID", map[int64]goldenOwner(nil), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			store := db.NewStore(sql.OpenDB(rec), db.SQLite)
			err := store.Related(context.Background(), tc.rows, tc.keyField, owner, tc.dest)
			statements := len(rec.statements())
			if !tc.wantError {
				if err != nil {
					t.Fatalf("Related: %v", err)
				}
				if statements != 1 {
					t.Fatalf("control recorded %d statements, want 1", statements)
				}
				return
			}
			var queryError *db.QueryError
			if !errors.As(err, &queryError) || !errors.Is(err, db.ErrInvalidQuery) {
				t.Fatalf("Related error = %v, want *db.QueryError", err)
			}
			if statements != 0 {
				t.Errorf("refused call recorded %d statements, want none", statements)
			}
			if dest, ok := tc.dest.(map[int64]goldenOwner); ok && dest != nil {
				if len(dest) != 1 || dest[99].Name != "kept" {
					t.Errorf("dest changed on error: %v", dest)
				}
			}
		})
	}
}
