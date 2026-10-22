package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
)

func TestNullPredicatesFindUnsetForeignKeys(t *testing.T) {
	_, store, authorMeta, postMeta := openNullTestDB(t)
	ctx := context.Background()
	author := nullAuthor{Name: "Jane"}
	if err := store.Create(ctx, authorMeta, &author); err != nil {
		t.Fatal(err)
	}
	for _, post := range []nullPost{
		{Title: "orphan one"},
		{Title: "owned", AuthorID: author.ID},
		{Title: "orphan two"},
	} {
		if err := store.Create(ctx, postMeta, &post); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(query db.Query) []string {
		t.Helper()
		query.OrderBy = []string{"ID"}
		var rows []nullPost
		if err := store.List(ctx, postMeta, query, &rows); err != nil {
			t.Fatalf("List: %v", err)
		}
		out := make([]string, len(rows))
		for i, row := range rows {
			out[i] = row.Title
		}
		return out
	}

	tests := []struct {
		name  string
		query db.Query
		want  []string
	}{
		{"IS NULL in Where", db.Query{Where: []db.Condition{{Field: "AuthorID", Op: db.OpIsNull}}}, []string{"orphan one", "orphan two"}},
		{"IS NOT NULL in Where", db.Query{Where: []db.Condition{{Field: "AuthorID", Op: db.OpIsNotNull}}}, []string{"owned"}},
		{"IS NULL in Any", db.Query{Any: []db.Condition{{Field: "AuthorID", Op: db.OpIsNull}, {Field: "Title", Op: db.OpEq, Value: "owned"}}}, []string{"orphan one", "owned", "orphan two"}},
		{"IsNull in Filter", db.Query{Filter: ptr(db.IsNull("AuthorID"))}, []string{"orphan one", "orphan two"}},
		{"IsNotNull in Filter", db.Query{Filter: ptr(db.IsNotNull("AuthorID"))}, []string{"owned"}},
		{"the zero foreign key literal never matches", db.Query{Where: []db.Condition{{Field: "AuthorID", Op: db.OpEq, Value: int64(0)}}}, []string{}},
		{"NOT equals leaves NULL rows out", db.Query{Filter: ptr(db.Not(db.Cond("AuthorID", db.OpEq, author.ID)))}, []string{}},
		{"not-equal leaves NULL rows out", db.Query{Where: []db.Condition{{Field: "AuthorID", Op: db.OpNe, Value: author.ID + 100}}}, []string{"owned"}},
		{"everything except x including unset", db.Query{Filter: ptr(db.Or(db.Not(db.Cond("AuthorID", db.OpEq, author.ID)), db.IsNull("AuthorID")))}, []string{"orphan one", "orphan two"}},
		{"IsNull combined with another condition", db.Query{Filter: ptr(db.And(db.IsNull("AuthorID"), db.Cond("Title", db.OpEq, "orphan two")))}, []string{"orphan two"}},
		{"IsNull on a non-foreign-key column", db.Query{Filter: ptr(db.IsNull("Title"))}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := titles(tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List = %v, want %v", got, tc.want)
			}
			count, err := store.Count(ctx, postMeta, tc.query)
			if err != nil || count != len(tc.want) {
				t.Errorf("Count = %d, %v, want %d", count, err, len(tc.want))
			}
		})
	}
}

func TestNullPredicateSQLGoldens(t *testing.T) {
	query := db.Query{
		Where:  []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a"}, {Field: "OwnerID", Op: db.OpIsNull}},
		Any:    []db.Condition{{Field: "OwnerID", Op: db.OpIsNotNull}, {Field: "Score", Op: db.OpGt, Value: 2}},
		Filter: ptr(db.And(db.IsNull("OwnerID"), db.Cond("Title", db.OpLike, "b%"))),
	}
	for dialect, want := range map[db.Dialect]struct{ quote, where string }{
		db.SQLite:   {"`", " WHERE (`title` = ? AND `owner_id` IS NULL) AND (`owner_id` IS NOT NULL OR `score` > ?) AND (`owner_id` IS NULL AND `title` LIKE ?)"},
		db.Postgres: {`"`, ` WHERE ("title" = $1 AND "owner_id" IS NULL) AND ("owner_id" IS NOT NULL OR "score" > $2) AND ("owner_id" IS NULL AND "title" LIKE $3)`},
		db.MySQL:    {"`", " WHERE (`title` = ? AND `owner_id` IS NULL) AND (`owner_id` IS NOT NULL OR `score` > ?) AND (`owner_id` IS NULL AND `title` LIKE ?)"},
	} {
		rec := &recorder{}
		store := db.NewStore(sql.OpenDB(rec), dialect)
		var rows []goldenPost
		if err := store.List(context.Background(), registerGoldenModels(t), query, &rows); err != nil {
			t.Fatal(err)
		}
		assertStatement(t, rec, goldenSelect(want.quote)+want.where, `["a", 2, "b%"]`)
	}
}

func TestNullOperatorsTakeNoValue(t *testing.T) {
	meta := registerGoldenModels(t)
	store := db.NewStore(sql.OpenDB(&recorder{}), db.SQLite)
	tests := []struct {
		name  string
		query db.Query
		want  string
	}{
		{"filter IS NULL with a value", db.Query{Filter: ptr(db.Cond("OwnerID", db.OpIsNull, int64(1)))}, `tango db: invalid query: operator "IS NULL" takes no value (field "OwnerID")`},
		{"filter IS NOT NULL with a value", db.Query{Filter: ptr(db.Cond("OwnerID", db.OpIsNotNull, 0))}, `tango db: invalid query: operator "IS NOT NULL" takes no value (field "OwnerID")`},
		{"where IS NULL with a value", db.Query{Where: []db.Condition{{Field: "OwnerID", Op: db.OpIsNull, Value: 1}}}, `tango db: operator "IS NULL" takes no value (field "OwnerID")`},
		{"equality still needs a value", db.Query{Filter: ptr(db.Cond("Title", db.OpEq, nil))}, `tango db: invalid query: operator "=" needs a value (field "Title")`},
		{"unknown field", db.Query{Filter: ptr(db.IsNull("Nope"))}, `tango db: invalid query: unknown field "Nope" for model goldenPost`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rows []goldenPost
			err := store.List(context.Background(), meta, tc.query, &rows)
			if err == nil || err.Error() != tc.want || !errors.Is(err, db.ErrInvalidQuery) {
				t.Errorf("err = %v, want %q matching ErrInvalidQuery", err, tc.want)
			}
		})
	}
}
