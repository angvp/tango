package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

func seedWidgets(t *testing.T) (*db.Store, *sql.DB) {
	t.Helper()
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	for _, widget := range []createWidget{
		{Name: "a", Active: true, Count: 1},
		{Name: "b", Active: false, Count: 2},
		{Name: "c", Active: true, Count: 3},
	} {
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}
	return store, sqlDB
}

func TestFirstReturnsTheFirstRowByPrimaryKey(t *testing.T) {
	store, _ := seedWidgets(t)
	meta := registerModel(t, createWidget{})
	ctx := context.Background()
	first := func(query db.Query) (createWidget, error) {
		var got createWidget
		err := store.First(ctx, meta, query, &got)
		return got, err
	}

	if got, err := first(db.Query{}); err != nil || got.Name != "a" {
		t.Errorf("First(Query{}) = %+v, %v, want the lowest primary key", got, err)
	}
	if got, err := first(db.Query{Where: []db.Condition{{Field: "Active", Op: db.OpEq, Value: true}}, Offset: 1}); err != nil || got.Name != "c" {
		t.Errorf("First with Offset 1 = %+v, %v, want c", got, err)
	}
	if got, err := first(db.Query{OrderBy: []string{"-Count"}}); err != nil || got.Name != "c" {
		t.Errorf("First ordered by -Count = %+v, %v, want c", got, err)
	}
	if got, err := first(db.Query{Filter: ptr(db.And(db.Not(db.Cond("Name", db.OpEq, "a")), db.In("Count", []int{2, 3})))}); err != nil || got.Name != "b" {
		t.Errorf("First with Filter = %+v, %v, want b", got, err)
	}
	if got, err := first(db.Query{Limit: 5}); err != nil || got.Name != "a" {
		t.Errorf("First must always read one row, got %+v, %v", got, err)
	}
}

func TestFirstLeavesDestUntouchedOnFailure(t *testing.T) {
	store, _ := seedWidgets(t)
	meta := registerModel(t, createWidget{})
	ctx := context.Background()
	sentinel := createWidget{ID: 99, Name: "keep"}

	for name, query := range map[string]db.Query{
		"no match":         {Where: []db.Condition{{Field: "Name", Op: db.OpEq, Value: "zzz"}}},
		"validation error": {Where: []db.Condition{{Field: "Nope", Op: db.OpEq, Value: 1}}},
	} {
		dest := sentinel
		err := store.First(ctx, meta, query, &dest)
		if err == nil {
			t.Fatalf("%s: First succeeded", name)
		}
		if !reflect.DeepEqual(dest, sentinel) {
			t.Errorf("%s: dest changed to %+v", name, dest)
		}
		if name == "no match" {
			if !errors.Is(err, db.ErrNotFound) || !strings.Contains(err.Error(), "createWidget") {
				t.Errorf("no match error = %v, want ErrNotFound naming the model", err)
			}
		} else if !errors.Is(err, db.ErrInvalidQuery) {
			t.Errorf("validation error = %v, want ErrInvalidQuery", err)
		}
	}
}

func TestFirstRejectsABadDestination(t *testing.T) {
	store, _ := seedWidgets(t)
	meta := registerModel(t, createWidget{})
	var rows []createWidget
	var wrong struct{ ID int64 }
	for name, dest := range map[string]any{"nil": nil, "not a pointer": createWidget{}, "slice": &rows, "wrong struct": &wrong, "nil pointer": (*createWidget)(nil)} {
		err := store.First(context.Background(), meta, db.Query{}, dest)
		if err == nil || errors.Is(err, db.ErrInvalidQuery) || errors.Is(err, db.ErrNotFound) {
			t.Errorf("%s: err = %v, want a plain destination error", name, err)
		}
	}
}

func TestFirstNeverMutatesTheQuery(t *testing.T) {
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	meta := registerGoldenModels(t)
	build := func() db.Query {
		return db.Query{Where: []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}}, Filter: ptr(db.IsNull("OwnerID")), Limit: 9, Offset: 2}
	}
	query := build()
	var row goldenPost
	_ = store.First(context.Background(), meta, query, &row)
	if !reflect.DeepEqual(query, build()) {
		t.Errorf("query changed: %+v", query)
	}
	query.OrderBy = make([]string, 0, 4)
	_ = store.First(context.Background(), meta, query, &row)
	if len(query.OrderBy) != 0 || len(query.OrderBy[:cap(query.OrderBy)][0]) != 0 {
		t.Errorf("caller's OrderBy backing array was written: %v", query.OrderBy[:cap(query.OrderBy)])
	}
}

func TestFirstAndExistsSQLGoldens(t *testing.T) {
	where := []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}}
	for dialect, quote := range map[db.Dialect]string{db.SQLite: "`", db.Postgres: `"`, db.MySQL: "`"} {
		ph := "?"
		if dialect == db.Postgres {
			ph = "$1"
		}
		rec := &recorder{}
		store := db.NewStore(sql.OpenDB(rec), dialect)
		meta := registerGoldenModels(t)
		var row goldenPost
		if err := store.First(context.Background(), meta, db.Query{Where: where, Offset: 3}, &row); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("First on an empty recorder: %v", err)
		}
		if _, err := store.Exists(context.Background(), meta, db.Query{Where: where, Limit: 7, Offset: 3, OrderBy: []string{"-Score"}}); err != nil {
			t.Fatal(err)
		}
		got := rec.statements()
		if len(got) != 2 {
			t.Fatalf("recorded %d statements", len(got))
		}
		wantFirst := goldenSelect(quote) + " WHERE (" + quote + "score" + quote + " > " + ph + ") ORDER BY " + quote + "id" + quote + " ASC LIMIT 1 OFFSET 3"
		wantExists := "SELECT 1 FROM " + quote + "golden_post" + quote + " WHERE (" + quote + "score" + quote + " > " + ph + ") LIMIT 1"
		if got[0].SQL != wantFirst {
			t.Errorf("First SQL\n got: %s\nwant: %s", got[0].SQL, wantFirst)
		}
		if got[1].SQL != wantExists {
			t.Errorf("Exists SQL\n got: %s\nwant: %s", got[1].SQL, wantExists)
		}
	}
}

func TestExistsReportsWhetherAnyRowMatches(t *testing.T) {
	store, _ := seedWidgets(t)
	meta := registerModel(t, createWidget{})
	ctx := context.Background()
	tests := []struct {
		name  string
		query db.Query
		want  bool
	}{
		{"empty query", db.Query{}, true},
		{"match", db.Query{Where: []db.Condition{{Field: "Name", Op: db.OpEq, Value: "b"}}}, true},
		{"no match", db.Query{Where: []db.Condition{{Field: "Name", Op: db.OpEq, Value: "zzz"}}}, false},
		{"filter match", db.Query{Filter: ptr(db.And(db.In("Count", []int{3}), db.Cond("Active", db.OpEq, true)))}, true},
		{"filter no match", db.Query{Filter: ptr(db.And(db.In("Count", []int{2}), db.Cond("Active", db.OpEq, true)))}, false},
		{"empty IN", db.Query{Filter: ptr(db.In("Count", []int{}))}, false},
		{"any", db.Query{Any: []db.Condition{{Field: "Name", Op: db.OpEq, Value: "zzz"}, {Field: "Name", Op: db.OpEq, Value: "c"}}}, true},
		{"limit offset and order are ignored", db.Query{Where: []db.Condition{{Field: "Name", Op: db.OpEq, Value: "a"}}, Limit: 1, Offset: 50, OrderBy: []string{"-Count"}}, true},
	}
	for _, tc := range tests {
		got, err := store.Exists(ctx, meta, tc.query)
		if err != nil || got != tc.want {
			t.Errorf("%s: Exists = %v, %v, want %v", tc.name, got, err, tc.want)
		}
	}

	if _, err := store.Exists(ctx, meta, db.Query{Where: []db.Condition{{Field: "Nope", Op: db.OpEq, Value: 1}}}); !errors.Is(err, db.ErrInvalidQuery) {
		t.Errorf("a validation error must be returned, got %v", err)
	}
}

func TestExistsOnAnEmptyTable(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	got, err := store.Exists(context.Background(), metaFor(t, registry, createWidget{}), db.Query{})
	if err != nil || got {
		t.Errorf("Exists on an empty table = %v, %v, want false", got, err)
	}
}

func TestExistsNeverMutatesTheQuery(t *testing.T) {
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	build := func() db.Query {
		return db.Query{Where: []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}}, Limit: 9, Offset: 2, OrderBy: []string{"-ID"}}
	}
	query := build()
	if _, err := store.Exists(context.Background(), registerGoldenModels(t), query); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(query, build()) {
		t.Errorf("query changed: %+v", query)
	}
}
