package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// The RelatedMany goldens pin the statements RelatedMany sends, per dialect,
// against the recording driver: the child SELECT, the IN list on the child's
// foreign key, the default primary-key order, and the statement count.

// relatedManyOwners returns n parent rows whose primary keys are first,
// first+1, ... in order.
func relatedManyOwners(first int64, n int) []goldenOwner {
	rows := make([]goldenOwner, n)
	for i := range rows {
		rows[i] = goldenOwner{ID: first + int64(i)}
	}
	return rows
}

func relatedManyMetas(t *testing.T) (owner, post model.ModelMeta) {
	t.Helper()
	registry := registerModels(t, goldenOwner{}, goldenPost{})
	return metaFor(t, registry, goldenOwner{}), metaFor(t, registry, goldenPost{})
}

// relatedManyOrder is the default child ORDER BY: primary key, ascending.
func relatedManyOrder(quote string) string {
	return " ORDER BY " + quote + "id" + quote + " ASC"
}

func TestRelatedManySQLGoldens(t *testing.T) {
	_, post := relatedManyMetas(t)
	for _, tc := range relatedDialects() {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("distinct keys in first-appearance order, zero skipped, default order", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				rows := []goldenOwner{{ID: 2}, {ID: 1}, {ID: 2}, {ID: 0}}
				dest := map[int64][]goldenPost{}
				if err := store.RelatedMany(context.Background(), rows, "ID", post, "OwnerID", db.Query{}, dest); err != nil {
					t.Fatal(err)
				}
				assertStatement(t, rec,
					goldenSelect(tc.quote)+relatedInClause(tc.dialect, tc.quote, "owner_id", 2)+relatedManyOrder(tc.quote),
					relatedArgs(2, 1))
			})

			t.Run("caller conditions come first, caller order replaces the default", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				query := db.Query{
					Where:   []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a"}},
					OrderBy: []string{"-Title"},
				}
				if err := store.RelatedMany(context.Background(), relatedManyOwners(3, 1), "ID", post, "OwnerID", query, map[int64][]goldenPost{}); err != nil {
					t.Fatal(err)
				}
				placeholders := "(" + tc.quote + "title" + tc.quote + " = ? AND " + tc.quote + "owner_id" + tc.quote + " IN (?))"
				if tc.dialect == db.Postgres {
					placeholders = "(" + tc.quote + "title" + tc.quote + " = $1 AND " + tc.quote + "owner_id" + tc.quote + " IN ($2))"
				}
				assertStatement(t, rec,
					goldenSelect(tc.quote)+" WHERE "+placeholders+" ORDER BY "+tc.quote+"title"+tc.quote+" DESC",
					`["a", 3]`)
			})

			t.Run("one thousand distinct keys are one statement", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				if err := store.RelatedMany(context.Background(), relatedManyOwners(1, 1000), "ID", post, "OwnerID", db.Query{}, map[int64][]goldenPost{}); err != nil {
					t.Fatal(err)
				}
				values := make([]int64, 1000)
				for i := range values {
					values[i] = int64(i + 1)
				}
				assertStatement(t, rec,
					goldenSelect(tc.quote)+relatedInClause(tc.dialect, tc.quote, "owner_id", 1000)+relatedManyOrder(tc.quote),
					relatedArgs(values...))
			})

			t.Run("one thousand and one distinct keys are two statements", func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), tc.dialect)
				rows := append(relatedManyOwners(1, 1001), relatedManyOwners(1, 1001)...)
				if err := store.RelatedMany(context.Background(), rows, "ID", post, "OwnerID", db.Query{}, map[int64][]goldenPost{}); err != nil {
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
				wantFirst := goldenSelect(tc.quote) + relatedInClause(tc.dialect, tc.quote, "owner_id", 1000) + relatedManyOrder(tc.quote)
				wantSecond := goldenSelect(tc.quote) + relatedInClause(tc.dialect, tc.quote, "owner_id", 1) + relatedManyOrder(tc.quote)
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

// TestRelatedManySendsNothingWithoutKeys pairs each empty input with a call
// that sends a statement on the same recorder.
func TestRelatedManySendsNothingWithoutKeys(t *testing.T) {
	_, post := relatedManyMetas(t)
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)
	ctx := context.Background()

	if err := store.RelatedMany(ctx, relatedManyOwners(7, 1), "ID", post, "OwnerID", db.Query{}, map[int64][]goldenPost{}); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.statements()); n != 1 {
		t.Fatalf("control call recorded %d statements, want 1", n)
	}
	inputs := map[string]any{
		"empty slice": []goldenOwner{},
		"nil slice":   []goldenOwner(nil),
		"all zero":    []goldenOwner{{}, {}},
	}
	for name, rows := range inputs {
		if err := store.RelatedMany(ctx, rows, "ID", post, "OwnerID", db.Query{}, map[int64][]goldenPost{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := len(rec.statements()); n != 1 {
		t.Errorf("empty inputs recorded %d more statements, want none", n-1)
	}
}

// TestRelatedManyRefusals pairs each refusal with a valid control on a fresh
// recorder: the control sends one statement, every refusal none, and a refused
// call leaves dest as it was.
func TestRelatedManyRefusals(t *testing.T) {
	_, post := relatedManyMetas(t)
	rows := relatedManyOwners(3, 1)
	filled := func() map[int64][]goldenPost {
		return map[int64][]goldenPost{99: {{ID: 99, Title: "kept"}}}
	}

	tests := []struct {
		name       string
		rows       any
		keyField   string
		childField string
		query      db.Query
		dest       any
		wantError  bool
	}{
		{"control: valid rows, field and dest", rows, "ID", "OwnerID", db.Query{}, filled(), false},
		{"rows is a struct, not a slice", goldenOwner{ID: 3}, "ID", "OwnerID", db.Query{}, filled(), true},
		{"rows is nil", nil, "ID", "OwnerID", db.Query{}, filled(), true},
		{"key field missing", rows, "Missing", "OwnerID", db.Query{}, filled(), true},
		{"key field unexported", []relatedUnexportedKeyRow{{ownerID: 3}}, "ownerID", "OwnerID", db.Query{}, filled(), true},
		{"child field missing", rows, "ID", "Missing", db.Query{}, filled(), true},
		{"child field of another type", rows, "ID", "Title", db.Query{}, filled(), true},
		{"query has a limit", rows, "ID", "OwnerID", db.Query{Limit: 1}, filled(), true},
		{"query has an offset", rows, "ID", "OwnerID", db.Query{Offset: 1}, filled(), true},
		{"unknown order field", rows, "ID", "OwnerID", db.Query{OrderBy: []string{"Missing"}}, filled(), true},
		{"dest key type differs", rows, "ID", "OwnerID", db.Query{}, map[int]([]goldenPost){99: {{ID: 99}}}, true},
		{"dest value is not a slice of the child", rows, "ID", "OwnerID", db.Query{}, map[int64]goldenPost{99: {ID: 99}}, true},
		{"dest is not a map", rows, "ID", "OwnerID", db.Query{}, []goldenPost{{ID: 99}}, true},
		{"dest is a nil map", rows, "ID", "OwnerID", db.Query{}, map[int64][]goldenPost(nil), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			store := db.NewStore(sql.OpenDB(rec), db.SQLite)
			err := store.RelatedMany(context.Background(), tc.rows, tc.keyField, post, tc.childField, tc.query, tc.dest)
			statements := len(rec.statements())
			if !tc.wantError {
				if err != nil {
					t.Fatalf("RelatedMany: %v", err)
				}
				if statements != 1 {
					t.Fatalf("control recorded %d statements, want 1", statements)
				}
				return
			}
			var queryError *db.QueryError
			if !errors.As(err, &queryError) || !errors.Is(err, db.ErrInvalidQuery) {
				t.Fatalf("RelatedMany error = %v, want *db.QueryError", err)
			}
			if statements != 0 {
				t.Errorf("refused call recorded %d statements, want none", statements)
			}
			if dest, ok := tc.dest.(map[int64][]goldenPost); ok && dest != nil {
				if len(dest) != 1 || dest[99][0].Title != "kept" {
					t.Errorf("dest changed on error: %v", dest)
				}
			}
		})
	}
}

// TestRelatedManyQueryLimit pairs an over-limit call with a control at the
// edge of MaxQueryArgs: the parent keys and the caller's conditions must
// together stay within it, and the refusal is the existing QueryLimitError.
func TestRelatedManyQueryLimit(t *testing.T) {
	_, post := relatedManyMetas(t)
	conditions := func(n int) []db.Condition {
		out := make([]db.Condition, n)
		for i := range out {
			out[i] = db.Condition{Field: "Title", Op: db.OpEq, Value: "x"}
		}
		return out
	}

	tests := []struct {
		name       string
		keys       int
		conditions int
		wantLimit  bool
	}{
		{"control: exactly MaxQueryArgs", 2, db.MaxQueryArgs - 2, false},
		{"one bind value over MaxQueryArgs", db.MaxInValues, db.MaxQueryArgs - db.MaxInValues + 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			store := db.NewStore(sql.OpenDB(rec), db.SQLite)
			query := db.Query{Where: conditions(tc.conditions)}
			err := store.RelatedMany(context.Background(), relatedManyOwners(1, tc.keys), "ID", post, "OwnerID", query, map[int64][]goldenPost{})
			statements := len(rec.statements())
			if !tc.wantLimit {
				if err != nil {
					t.Fatalf("RelatedMany: %v", err)
				}
				if statements != 1 {
					t.Fatalf("control recorded %d statements, want 1", statements)
				}
				return
			}
			var limitError *db.QueryLimitError
			if !errors.As(err, &limitError) || limitError.Limit != "bind values" {
				t.Fatalf("RelatedMany error = %v, want a bind values *db.QueryLimitError", err)
			}
			if statements != 0 {
				t.Errorf("refused call recorded %d statements, want none", statements)
			}
		})
	}
}
