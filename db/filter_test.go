package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
)

func listWidgetIDs(t *testing.T, store *db.Store, query db.Query) []string {
	t.Helper()
	meta := registerModel(t, createWidget{})
	var rows []createWidget
	if err := store.List(context.Background(), meta, withOrder(query), &rows); err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.Name
	}
	return names
}

func withOrder(query db.Query) db.Query {
	query.OrderBy = []string{"ID"}
	return query
}

func TestFilterGroupsBooleanConditions(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	for _, widget := range []createWidget{
		{Name: "a", Active: true, Count: 1},
		{Name: "b", Active: false, Count: 2},
		{Name: "c", Active: true, Count: 3},
		{Name: "d", Active: false, Count: 4},
	} {
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		query db.Query
		want  []string
	}{
		{"single condition", db.Query{Filter: ptr(db.Cond("Name", db.OpEq, "b"))}, []string{"b"}},
		{"or inside and", db.Query{Filter: ptr(db.And(
			db.Or(db.Cond("Name", db.OpEq, "a"), db.Cond("Name", db.OpEq, "d")),
			db.Cond("Count", db.OpGt, 1),
		))}, []string{"d"}},
		{"and inside or", db.Query{Filter: ptr(db.Or(
			db.And(db.Cond("Active", db.OpEq, true), db.Cond("Count", db.OpGt, 1)),
			db.Cond("Name", db.OpEq, "b"),
		))}, []string{"b", "c"}},
		{"not", db.Query{Filter: ptr(db.Not(db.Cond("Active", db.OpEq, true)))}, []string{"b", "d"}},
		{"not over a group", db.Query{Filter: ptr(db.Not(db.Or(db.Cond("Name", db.OpEq, "a"), db.Cond("Name", db.OpEq, "b"))))}, []string{"c", "d"}},
		{"one-child groups mean the child", db.Query{Filter: ptr(db.And(db.Or(db.Cond("Name", db.OpEq, "c"))))}, []string{"c"}},
		{"filter ANDs with where", db.Query{
			Where:  []db.Condition{{Field: "Active", Op: db.OpEq, Value: true}},
			Filter: ptr(db.Not(db.Cond("Name", db.OpEq, "a"))),
		}, []string{"c"}},
		{"filter ANDs with any", db.Query{
			Any:    []db.Condition{{Field: "Name", Op: db.OpEq, Value: "a"}, {Field: "Name", Op: db.OpEq, Value: "b"}},
			Filter: ptr(db.Cond("Active", db.OpEq, false)),
		}, []string{"b"}},
		{"or root is contained by the other parts", db.Query{
			Where:  []db.Condition{{Field: "Count", Op: db.OpLte, Value: 2}},
			Filter: ptr(db.Or(db.Cond("Name", db.OpEq, "a"), db.Cond("Name", db.OpEq, "d"))),
		}, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := listWidgetIDs(t, store, tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List = %v, want %v", got, tc.want)
			}
			count, err := store.Count(context.Background(), meta, tc.query)
			if err != nil || count != len(tc.want) {
				t.Errorf("Count = %d, %v, want %d", count, err, len(tc.want))
			}
		})
	}
}

func ptr(e db.Expr) *db.Expr { return &e }

func TestFilterSQLGoldens(t *testing.T) {
	query := db.Query{
		Where: []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}},
		Any:   []db.Condition{{Field: "Title", Op: db.OpEq, Value: "x"}, {Field: "Title", Op: db.OpNe, Value: "y"}},
		Filter: ptr(db.And(
			db.Cond("Published", db.OpEq, true),
			db.Or(db.Cond("Title", db.OpLike, "a%"), db.Not(db.Cond("Score", db.OpEq, 7))),
		)),
	}
	tests := []struct {
		name     string
		query    db.Query
		sqlite   string
		postgres string
		args     string
	}{
		{
			"filter after where and any",
			query,
			" WHERE (`score` > ?) AND (`title` = ? OR `title` <> ?) AND (`published` = ? AND (`title` LIKE ? OR NOT (`score` = ?)))",
			` WHERE ("score" > $1) AND ("title" = $2 OR "title" <> $3) AND ("published" = $4 AND ("title" LIKE $5 OR NOT ("score" = $6)))`,
			`[1, "x", "y", true, "a%", 7]`,
		},
		{
			"leaf root is parenthesized",
			db.Query{Filter: ptr(db.Cond("Score", db.OpEq, 3))},
			" WHERE (`score` = ?)",
			` WHERE ("score" = $1)`,
			`[3]`,
		},
		{
			"not root is parenthesized",
			db.Query{Filter: ptr(db.Not(db.Cond("Score", db.OpEq, 3)))},
			" WHERE (NOT (`score` = ?))",
			` WHERE (NOT ("score" = $1))`,
			`[3]`,
		},
		{
			"one-child group",
			db.Query{Filter: ptr(db.Or(db.Cond("Score", db.OpEq, 3)))},
			" WHERE (`score` = ?)",
			` WHERE ("score" = $1)`,
			`[3]`,
		},
	}
	for _, tc := range tests {
		for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
			t.Run(tc.name, func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect)
				var rows []goldenPost
				if err := store.List(context.Background(), registerGoldenModels(t), tc.query, &rows); err != nil {
					t.Fatal(err)
				}
				want, quote := tc.sqlite, "`"
				if dialect == db.Postgres {
					want, quote = tc.postgres, `"`
				}
				assertStatement(t, rec, goldenSelect(quote)+want, tc.args)
			})
		}
	}
}

func TestFilterErrors(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)

	deep := db.Cond("Score", db.OpEq, 1)
	for i := 0; i < 8; i++ { // nine levels: eight groups around one leaf
		deep = db.And(deep)
	}
	wide := make([]db.Expr, 100) // one group plus 100 leaves is 101 nodes
	for i := range wide {
		wide[i] = db.Cond("Score", db.OpEq, i)
	}

	tests := []struct {
		name  string
		query db.Query
		want  string
		limit bool
	}{
		{"unknown field", db.Query{Filter: ptr(db.Cond("Titel", db.OpEq, "x"))}, `tango db: invalid query: unknown field "Titel" for model goldenPost`, false},
		{"unsupported operator", db.Query{Filter: ptr(db.Cond("Title", db.Op("~"), "x"))}, `tango db: invalid query: unsupported operator "~"`, false},
		{"missing value", db.Query{Filter: ptr(db.Cond("Title", db.OpEq, nil))}, `tango db: invalid query: operator "=" needs a value (field "Title")`, false},
		{"wrong value type", db.Query{Filter: ptr(db.Cond("Score", db.OpEq, "x"))}, `tango db: invalid query: value for field "Score" has type string, want int`, false},
		{"empty and", db.Query{Filter: ptr(db.And())}, `tango db: invalid query: AND group has no children`, false},
		{"empty or", db.Query{Filter: ptr(db.Or())}, `tango db: invalid query: OR group has no children`, false},
		{"not with two children", db.Query{Filter: &db.Expr{Op: db.ExprNot, Children: []db.Expr{db.Cond("Score", db.OpEq, 1), db.Cond("Score", db.OpEq, 2)}}}, `tango db: invalid query: NOT needs exactly one child, got 2`, false},
		{"not with no child", db.Query{Filter: &db.Expr{Op: db.ExprNot}}, `tango db: invalid query: NOT needs exactly one child, got 0`, false},
		{"zero operator", db.Query{Filter: &db.Expr{}}, `tango db: invalid query: expression has no operator`, false},
		{"unknown operator", db.Query{Filter: &db.Expr{Op: "XOR"}}, `tango db: invalid query: unknown expression operator "XOR"`, false},
		{"leaf with children", db.Query{Filter: &db.Expr{Op: db.ExprCond, Cond: db.Condition{Field: "Score", Op: db.OpEq, Value: 1}, Children: []db.Expr{{}}}}, `tango db: invalid query: condition has children`, false},
		{"group with condition", db.Query{Filter: &db.Expr{Op: db.ExprAnd, Cond: db.Condition{Field: "Score"}, Children: []db.Expr{db.Cond("Score", db.OpEq, 1)}}}, `tango db: invalid query: AND group carries a condition`, false},
		{"too deep", db.Query{Filter: &deep}, `tango db: invalid query: filter is 9 levels deep, limit 8`, true},
		{"too many nodes", db.Query{Filter: ptr(db.And(wide...))}, `tango db: invalid query: filter has 101 nodes, limit 100`, true},
		{"legacy unknown where field keeps its text", db.Query{Where: []db.Condition{{Field: "Nope", Op: db.OpEq, Value: 1}}}, `tango db: unknown Where field "Nope" for model goldenPost`, false},
		{"unknown OrderBy field keeps its text and matches ErrInvalidQuery", db.Query{OrderBy: []string{"-Nope"}}, `tango db: unknown OrderBy field "Nope" for model goldenPost`, false},
		{"legacy any operator keeps its text", db.Query{Any: []db.Condition{{Field: "Score", Op: db.Op("~"), Value: 1}}}, `tango db: unsupported Where operator "~"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rows []goldenPost
			err := store.List(context.Background(), meta, tc.query, &rows)
			if err == nil {
				t.Fatal("List succeeded, want an error")
			}
			if err.Error() != tc.want {
				t.Errorf("error = %q\nwant    %q", err, tc.want)
			}
			if !errors.Is(err, db.ErrInvalidQuery) {
				t.Errorf("errors.Is(err, ErrInvalidQuery) = false for %v", err)
			}
			var queryErr *db.QueryError
			var limitErr *db.QueryLimitError
			if gotLimit := errors.As(err, &limitErr); gotLimit != tc.limit {
				t.Errorf("errors.As QueryLimitError = %v, want %v", gotLimit, tc.limit)
			}
			if tc.limit != errors.Is(err, db.ErrQueryLimit) {
				t.Errorf("errors.Is(err, ErrQueryLimit) = %v, want %v", !tc.limit, tc.limit)
			}
			if !tc.limit && !errors.As(err, &queryErr) {
				t.Errorf("errors.As QueryError = false for %v", err)
			}
			if _, countErr := store.Count(context.Background(), meta, tc.query); len(tc.query.OrderBy) == 0 && (countErr == nil || countErr.Error() != tc.want) { // Count ignores OrderBy
				t.Errorf("Count error = %v, want the same as List", countErr)
			}
		})
	}
}

func TestFilterLimitsAreExactlyAtTheBoundary(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)

	atDepth := db.Cond("Score", db.OpEq, 1)
	for i := 1; i < db.MaxFilterDepth; i++ {
		atDepth = db.And(atDepth)
	}
	leaves := make([]db.Expr, db.MaxFilterNodes-1) // plus the group is exactly the limit
	for i := range leaves {
		leaves[i] = db.Cond("Score", db.OpEq, i)
	}
	for name, filter := range map[string]db.Expr{"depth": atDepth, "nodes": db.Or(leaves...)} {
		var rows []goldenPost
		if err := store.List(context.Background(), meta, db.Query{Filter: &filter}, &rows); err != nil {
			t.Errorf("%s at the limit: %v", name, err)
		}
	}
}

func TestFilterLimitsHoldForSelfReferencingTrees(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	children := make([]db.Expr, 1)
	children[0] = db.Expr{Op: db.ExprAnd, Children: children} // a cycle through the slice
	filter := db.Expr{Op: db.ExprOr, Children: children}
	var rows []goldenPost
	err := store.List(context.Background(), meta, db.Query{Filter: &filter}, &rows)
	if !errors.Is(err, db.ErrQueryLimit) {
		t.Fatalf("err = %v, want a query-limit error and no endless walk", err)
	}
}

func TestQueryValidationAndCompilationNeverMutateTheQuery(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	build := func() db.Query {
		return db.Query{
			Where:   []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}},
			Any:     []db.Condition{{Field: "Title", Op: db.OpEq, Value: "x"}},
			Filter:  ptr(db.And(db.Cond("Title", db.OpLike, "a%"), db.Not(db.Or(db.Cond("Score", db.OpEq, 2))))),
			OrderBy: []string{"-ID"},
			Limit:   3,
			Offset:  1,
		}
	}
	query := build()
	var rows []goldenPost
	if err := store.List(context.Background(), meta, query, &rows); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Count(context.Background(), meta, query); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(query, build()) {
		t.Errorf("query changed:\n got %+v\nwant %+v", query, build())
	}
}

func TestQueryErrorsMatchTheirSentinels(t *testing.T) {
	err := error(&db.QueryError{Model: "Post", Field: "Title", Reason: "boom"})
	if !errors.Is(err, db.ErrInvalidQuery) || errors.Is(err, db.ErrQueryLimit) {
		t.Errorf("QueryError matches: invalid=%v limit=%v", errors.Is(err, db.ErrInvalidQuery), errors.Is(err, db.ErrQueryLimit))
	}
	if got, want := err.Error(), "tango db: invalid query: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	limit := error(&db.QueryLimitError{Limit: "filter nodes", Max: 100, Got: 101})
	if !errors.Is(limit, db.ErrQueryLimit) || !errors.Is(limit, db.ErrInvalidQuery) {
		t.Errorf("QueryLimitError must match both sentinels")
	}
}
