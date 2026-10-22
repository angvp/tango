package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

type namedCount int

func TestInMatchesListedValues(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range []string{"a", "b", "c", "d"} {
		widget := createWidget{Name: name, Count: i + 1, CreatedAt: base.Add(time.Duration(i) * time.Hour)}
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatal(err)
		}
	}
	names := func(query db.Query) []string {
		t.Helper()
		query.OrderBy = []string{"ID"}
		var rows []createWidget
		if err := store.List(context.Background(), meta, query, &rows); err != nil {
			t.Fatalf("List: %v", err)
		}
		out := make([]string, len(rows))
		for i, row := range rows {
			out[i] = row.Name
		}
		return out
	}

	var nilInts []int
	tests := []struct {
		name  string
		query db.Query
		want  []string
	}{
		{"strings", db.Query{Filter: ptr(db.In("Name", []string{"b", "d", "zzz"}))}, []string{"b", "d"}},
		{"ints", db.Query{Filter: ptr(db.In("Count", []int{1, 3}))}, []string{"a", "c"}},
		{"duplicates are allowed", db.Query{Filter: ptr(db.In("Count", []int{2, 2, 2}))}, []string{"b"}},
		{"an array", db.Query{Filter: ptr(db.In("Name", [2]string{"a", "c"}))}, []string{"a", "c"}},
		{"[]any with matching elements", db.Query{Filter: ptr(db.In("Count", []any{2, 4}))}, []string{"b", "d"}},
		{"times", db.Query{Filter: ptr(db.In("CreatedAt", []time.Time{base, base.Add(3 * time.Hour)}))}, []string{"a", "d"}},
		{"named element types pass by kind", db.Query{Filter: ptr(db.In("Count", []namedCount{2, 3}))}, []string{"b", "c"}},
		{"in Where", db.Query{Where: []db.Condition{{Field: "Name", Op: db.OpIn, Value: []string{"a", "b"}}}}, []string{"a", "b"}},
		{"in Any", db.Query{Any: []db.Condition{{Field: "Name", Op: db.OpIn, Value: []string{"a"}}, {Field: "Count", Op: db.OpIn, Value: []int{4}}}}, []string{"a", "d"}},
		{"in a group", db.Query{Filter: ptr(db.And(db.In("Count", []int{1, 2, 3}), db.Not(db.In("Name", []string{"b"}))))}, []string{"a", "c"}},
		{"nil matches nothing", db.Query{Filter: ptr(db.In("Name", nil))}, []string{}},
		{"typed nil slice matches nothing", db.Query{Filter: ptr(db.In("Count", nilInts))}, []string{}},
		{"empty slice matches nothing", db.Query{Filter: ptr(db.In("Count", []int{}))}, []string{}},
		{"NOT of an empty list matches every row", db.Query{Filter: ptr(db.Not(db.In("Count", []int{})))}, []string{"a", "b", "c", "d"}},
		{"empty list in Or adds nothing", db.Query{Filter: ptr(db.Or(db.In("Count", []int{}), db.Cond("Name", db.OpEq, "a")))}, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := names(tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List = %v, want %v", got, tc.want)
			}
			count, err := store.Count(context.Background(), meta, tc.query)
			if err != nil || count != len(tc.want) {
				t.Errorf("Count = %d, %v, want %d", count, err, len(tc.want))
			}
		})
	}
}

func TestInSQLGoldens(t *testing.T) {
	query := db.Query{
		Where:  []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a"}},
		Filter: ptr(db.And(db.In("Score", []int{1, 2, 3}), db.In("Title", []string{}), db.Not(db.In("ID", []int64{9})), db.IsNull("OwnerID"), db.Cond("Title", db.OpLike, "z%"))),
	}
	for dialect, want := range map[db.Dialect]struct{ quote, where string }{
		db.SQLite:   {"`", " WHERE (`title` = ?) AND (`score` IN (?, ?, ?) AND (1 = 0) AND NOT (`id` IN (?)) AND `owner_id` IS NULL AND `title` LIKE ?)"},
		db.Postgres: {`"`, ` WHERE ("title" = $1) AND ("score" IN ($2, $3, $4) AND (1 = 0) AND NOT ("id" IN ($5)) AND "owner_id" IS NULL AND "title" LIKE $6)`},
		db.MySQL:    {"`", " WHERE (`title` = ?) AND (`score` IN (?, ?, ?) AND (1 = 0) AND NOT (`id` IN (?)) AND `owner_id` IS NULL AND `title` LIKE ?)"},
	} {
		rec := &recorder{}
		store := db.NewStore(sql.OpenDB(rec), dialect)
		var rows []goldenPost
		if err := store.List(context.Background(), registerGoldenModels(t), query, &rows); err != nil {
			t.Fatal(err)
		}
		assertStatement(t, rec, goldenSelect(want.quote)+want.where, `["a", 1, 2, 3, 9, "z%"]`)
	}
}

func TestInErrors(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	tooMany := make([]int64, db.MaxInValues+1)
	atLimit := make([]int64, db.MaxInValues)
	var nilTime *time.Time

	tests := []struct {
		name  string
		query db.Query
		want  string
		limit bool
	}{
		{"scalar", db.Query{Filter: ptr(db.In("ID", int64(3)))}, `tango db: invalid query: operator "IN" needs a slice or array for field "ID", got int64`, false},
		{"string is not a list", db.Query{Filter: ptr(db.In("Title", "abc"))}, `tango db: invalid query: operator "IN" needs a slice or array for field "Title", got string`, false},
		{"wrong element type", db.Query{Filter: ptr(db.In("ID", []any{int64(1), int64(2), "3"}))}, `tango db: invalid query: IN value 2 for field "ID" has type string, want int64`, false},
		{"wrong slice type", db.Query{Filter: ptr(db.In("ID", []string{"a"}))}, `tango db: invalid query: IN value 0 for field "ID" has type string, want int64`, false},
		{"nil element", db.Query{Filter: ptr(db.In("Title", []any{"a", nil}))}, `tango db: invalid query: IN value 1 for field "Title" is nil`, false},
		{"nil pointer element", db.Query{Filter: ptr(db.In("CreatedAt", []*time.Time{nilTime}))}, `tango db: invalid query: IN value 0 for field "CreatedAt" has type *time.Time, want time.Time`, false},
		{"zero time", db.Query{Filter: ptr(db.In("CreatedAt", []time.Time{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), {}}))}, `tango db: invalid query: IN value 1 for field "CreatedAt" is the zero time`, false},
		{"too many values", db.Query{Filter: ptr(db.In("ID", tooMany))}, `tango db: invalid query: IN list for field "ID" has 1001 values, limit 1000`, true},
		{"too many values in Where", db.Query{Where: []db.Condition{{Field: "ID", Op: db.OpIn, Value: tooMany}}}, `tango db: invalid query: IN list for field "ID" has 1001 values, limit 1000`, true},
		{"unknown field", db.Query{Filter: ptr(db.In("Nope", []int{1}))}, `tango db: invalid query: unknown field "Nope" for model goldenPost`, false},
		{"too many bind values overall", db.Query{Filter: ptr(manyLists(11, atLimit))}, fmt.Sprintf(`tango db: invalid query: query has %d bind values, limit %d`, 11*db.MaxInValues, db.MaxQueryArgs), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rows []goldenPost
			err := store.List(context.Background(), meta, tc.query, &rows)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %q", err, tc.want)
			}
			if !errors.Is(err, db.ErrInvalidQuery) || errors.Is(err, db.ErrQueryLimit) != tc.limit {
				t.Errorf("sentinels: invalid=%v limit=%v, want limit=%v", errors.Is(err, db.ErrInvalidQuery), errors.Is(err, db.ErrQueryLimit), tc.limit)
			}
		})
	}

	var rows []goldenPost
	if err := store.List(context.Background(), meta, db.Query{Filter: ptr(manyLists(10, atLimit))}, &rows); err != nil {
		t.Errorf("exactly %d bind values must be accepted: %v", db.MaxQueryArgs, err)
	}
}

// manyLists is an Or of n IN lists, each holding list.
func manyLists(n int, list []int64) db.Expr {
	children := make([]db.Expr, n)
	for i := range children {
		children[i] = db.In("ID", list)
	}
	return db.Or(children...)
}

func TestInNeverMutatesTheCallersList(t *testing.T) {
	store := db.NewStore(sql.OpenDB(&recorder{}), db.Postgres)
	ids := []int64{3, 1, 2}
	anys := []any{int64(5), int64(4)}
	query := db.Query{Where: []db.Condition{{Field: "ID", Op: db.OpIn, Value: ids}}, Filter: ptr(db.In("ID", anys))}
	var rows []goldenPost
	if err := store.List(context.Background(), registerGoldenModels(t), query, &rows); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{3, 1, 2}) || !reflect.DeepEqual(anys, []any{int64(5), int64(4)}) {
		t.Errorf("lists changed: %v %v", ids, anys)
	}
}

func TestZeroTimeInInIsRejectedOnEveryDialect(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		rec := &recorder{}
		store := db.NewStore(sql.OpenDB(rec), dialect)
		var rows []goldenPost
		err := store.List(context.Background(), registerGoldenModels(t), db.Query{Filter: ptr(db.In("CreatedAt", []time.Time{{}}))}, &rows)
		if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
			t.Errorf("dialect %d: err = %v, statements = %d; want a validation error before any SQL", dialect, err, len(rec.statements()))
		}
	}
}
