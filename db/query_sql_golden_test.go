package db_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// These goldens pin the SQL text, placeholder order and bind arguments that
// List and Count emit, per dialect, so later query features can prove they
// leave existing queries byte-identical. They run against a recording driver
// and need no database.

type goldenOwner struct {
	ID   int64 `tango:"pk"`
	Name string
}

type goldenPost struct {
	ID        int64 `tango:"pk"`
	Title     string
	Score     int
	Published bool
	OwnerID   int64 `tango:"fk=goldenOwner"`
	CreatedAt time.Time
}

type recordedStatement struct {
	SQL  string
	Args string
}

// recorder is a database/sql driver that records each query and answers with
// no rows, or with a single zero for COUNT(*).
type recorder struct {
	mu   sync.Mutex
	seen []recordedStatement
}

func (r *recorder) Connect(context.Context) (driver.Conn, error) { return &recorderConn{r}, nil }
func (r *recorder) Driver() driver.Driver                        { return r }
func (r *recorder) Open(string) (driver.Conn, error)             { return &recorderConn{r}, nil }

func (r *recorder) statements() []recordedStatement {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedStatement(nil), r.seen...)
}

type recorderConn struct{ rec *recorder }

func (c *recorderConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unsupported") }
func (c *recorderConn) Close() error                        { return nil }
func (c *recorderConn) Begin() (driver.Tx, error)           { return nil, fmt.Errorf("unsupported") }

func (c *recorderConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	values := make([]string, len(args))
	for i, arg := range args {
		values[i] = fmt.Sprintf("%#v", arg.Value)
	}
	c.rec.mu.Lock()
	c.rec.seen = append(c.rec.seen, recordedStatement{SQL: query, Args: "[" + strings.Join(values, ", ") + "]"})
	c.rec.mu.Unlock()
	return &recorderRows{count: strings.HasPrefix(query, "SELECT COUNT(*)")}, nil
}

type recorderRows struct {
	count bool
	done  bool
}

func (r *recorderRows) Columns() []string { return []string{"c"} }
func (r *recorderRows) Close() error      { return nil }
func (r *recorderRows) Next(dest []driver.Value) error {
	if !r.count || r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(0)
	return nil
}

// goldenSelect is the SELECT list and FROM clause for the dialect's quoting.
func goldenSelect(quote string) string {
	names := []string{"id", "title", "score", "published", "owner_id", "created_at"}
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quote + name + quote
	}
	return "SELECT " + strings.Join(quoted, ", ") + " FROM " + quote + "golden_post" + quote
}

func TestQuerySQLGoldens(t *testing.T) {
	zeroTime := time.Time{}
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	type want struct{ sql, args string }
	tests := []struct {
		name     string
		query    db.Query
		sqlite   want
		postgres want
		mysql    want
	}{
		{
			name:     "empty query",
			query:    db.Query{},
			sqlite:   want{"", "[]"},
			postgres: want{"", "[]"},
			mysql:    want{"", "[]"},
		},
		{
			name:     "where only",
			query:    db.Query{Where: []db.Condition{{Field: "Title", Op: db.OpEq, Value: "a"}, {Field: "Score", Op: db.OpGte, Value: 3}}},
			sqlite:   want{" WHERE (`title` = ? AND `score` >= ?)", `["a", 3]`},
			postgres: want{` WHERE ("title" = $1 AND "score" >= $2)`, `["a", 3]`},
			mysql:    want{" WHERE (`title` = ? AND `score` >= ?)", `["a", 3]`},
		},
		{
			name:     "any only",
			query:    db.Query{Any: []db.Condition{{Field: "Title", Op: db.OpLike, Value: "a%"}, {Field: "Published", Op: db.OpEq, Value: true}}},
			sqlite:   want{" WHERE (`title` LIKE ? OR `published` = ?)", `["a%", true]`},
			postgres: want{` WHERE ("title" LIKE $1 OR "published" = $2)`, `["a%", true]`},
			mysql:    want{" WHERE (`title` LIKE ? OR `published` = ?)", `["a%", true]`},
		},
		{
			name: "where and any",
			query: db.Query{
				Where: []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}, {Field: "Score", Op: db.OpLt, Value: 9}},
				Any:   []db.Condition{{Field: "Title", Op: db.OpEq, Value: "x"}, {Field: "Title", Op: db.OpNe, Value: "y"}},
			},
			sqlite:   want{" WHERE (`score` > ? AND `score` < ?) AND (`title` = ? OR `title` <> ?)", `[1, 9, "x", "y"]`},
			postgres: want{` WHERE ("score" > $1 AND "score" < $2) AND ("title" = $3 OR "title" <> $4)`, `[1, 9, "x", "y"]`},
			mysql:    want{" WHERE (`score` > ? AND `score` < ?) AND (`title` = ? OR `title` <> ?)", `[1, 9, "x", "y"]`},
		},
		{
			name:     "zero foreign key comparison keeps its literal SQL",
			query:    db.Query{Where: []db.Condition{{Field: "OwnerID", Op: db.OpEq, Value: int64(0)}}},
			sqlite:   want{" WHERE (`owner_id` = ?)", `[0]`},
			postgres: want{` WHERE ("owner_id" = $1)`, `[0]`},
			mysql:    want{" WHERE (`owner_id` = ?)", `[0]`},
		},
		{
			name:     "time arguments",
			query:    db.Query{Where: []db.Condition{{Field: "CreatedAt", Op: db.OpGte, Value: stamp}, {Field: "CreatedAt", Op: db.OpLt, Value: zeroTime}}},
			sqlite:   want{" WHERE (`created_at` >= ? AND `created_at` < ?)", `[time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)]`},
			postgres: want{` WHERE ("created_at" >= $1 AND "created_at" < $2)`, `[time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)]`},
			mysql:    want{" WHERE (`created_at` >= ? AND `created_at` < ?)", `[time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), <nil>]`},
		},
	}

	for _, dialect := range []struct {
		name  string
		d     db.Dialect
		quote string
	}{
		{"sqlite", db.SQLite, "`"},
		{"postgres", db.Postgres, `"`},
		{"mysql", db.MySQL, "`"},
	} {
		for _, tc := range tests {
			t.Run(dialect.name+"/count/"+tc.name, func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect.d)
				meta := registerGoldenModels(t)
				if _, err := store.Count(context.Background(), meta, tc.query); err != nil {
					t.Fatal(err)
				}
				w := pickWant(dialect.d, tc.sqlite, tc.postgres, tc.mysql)
				assertStatement(t, rec, "SELECT COUNT(*) FROM "+dialect.quote+"golden_post"+dialect.quote+w.sql, w.args)
			})
			t.Run(dialect.name+"/list/"+tc.name, func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect.d)
				meta := registerGoldenModels(t)
				var rows []goldenPost
				if err := store.List(context.Background(), meta, tc.query, &rows); err != nil {
					t.Fatal(err)
				}
				w := pickWant(dialect.d, tc.sqlite, tc.postgres, tc.mysql)
				assertStatement(t, rec, goldenSelect(dialect.quote)+w.sql, w.args)
			})
		}
	}
}

func TestListSQLOrderingAndPagination(t *testing.T) {
	where := []db.Condition{{Field: "Score", Op: db.OpGt, Value: 1}}
	tests := []struct {
		name  string
		query db.Query
		tail  map[db.Dialect]string // appended after the SELECT ... FROM
	}{
		{"order ascending", db.Query{OrderBy: []string{"Title"}}, map[db.Dialect]string{
			db.SQLite: " ORDER BY `title` ASC", db.Postgres: ` ORDER BY "title" ASC`, db.MySQL: " ORDER BY `title` ASC"}},
		{"order descending then ascending", db.Query{OrderBy: []string{"-Score", "ID"}}, map[db.Dialect]string{
			db.SQLite: " ORDER BY `score` DESC, `id` ASC", db.Postgres: ` ORDER BY "score" DESC, "id" ASC`, db.MySQL: " ORDER BY `score` DESC, `id` ASC"}},
		{"limit", db.Query{Limit: 5}, map[db.Dialect]string{
			db.SQLite: " LIMIT 5", db.Postgres: " LIMIT 5", db.MySQL: " LIMIT 5"}},
		{"limit and offset", db.Query{Limit: 5, Offset: 10}, map[db.Dialect]string{
			db.SQLite: " LIMIT 5 OFFSET 10", db.Postgres: " LIMIT 5 OFFSET 10", db.MySQL: " LIMIT 5 OFFSET 10"}},
		{"offset without limit", db.Query{Offset: 10}, map[db.Dialect]string{
			db.SQLite: " LIMIT -1 OFFSET 10", db.Postgres: " OFFSET 10", db.MySQL: " LIMIT 18446744073709551615 OFFSET 10"}},
		{"where order limit offset together", db.Query{Where: where, OrderBy: []string{"-ID"}, Limit: 2, Offset: 4}, map[db.Dialect]string{
			db.SQLite:   " WHERE (`score` > ?) ORDER BY `id` DESC LIMIT 2 OFFSET 4",
			db.Postgres: ` WHERE ("score" > $1) ORDER BY "id" DESC LIMIT 2 OFFSET 4`,
			db.MySQL:    " WHERE (`score` > ?) ORDER BY `id` DESC LIMIT 2 OFFSET 4"}},
	}
	quotes := map[db.Dialect]string{db.SQLite: "`", db.Postgres: `"`, db.MySQL: "`"}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%d/%s", dialect, tc.name), func(t *testing.T) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect)
				var rows []goldenPost
				if err := store.List(context.Background(), registerGoldenModels(t), tc.query, &rows); err != nil {
					t.Fatal(err)
				}
				args := "[]"
				if len(tc.query.Where) > 0 {
					args = "[1]"
				}
				assertStatement(t, rec, goldenSelect(quotes[dialect])+tc.tail[dialect], args)
			})
		}
	}
}

func TestCountIgnoresOrderingAndPagination(t *testing.T) {
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)
	query := db.Query{OrderBy: []string{"-ID"}, Limit: 3, Offset: 2}
	if _, err := store.Count(context.Background(), registerGoldenModels(t), query); err != nil {
		t.Fatal(err)
	}
	assertStatement(t, rec, "SELECT COUNT(*) FROM `golden_post`", "[]")
}

func pickWant[T any](d db.Dialect, sqlite, postgres, mysql T) T {
	switch d {
	case db.Postgres:
		return postgres
	case db.MySQL:
		return mysql
	}
	return sqlite
}

func registerGoldenModels(t *testing.T) (meta model.ModelMeta) {
	t.Helper()
	registry := registerModels(t, goldenOwner{}, goldenPost{})
	return metaFor(t, registry, goldenPost{})
}

func assertStatement(t *testing.T, rec *recorder, wantSQL, wantArgs string) {
	t.Helper()
	got := rec.statements()
	if len(got) != 1 {
		t.Fatalf("recorded %d statements, want 1: %v", len(got), got)
	}
	if got[0].SQL != wantSQL {
		t.Errorf("SQL differs\n got: %s\nwant: %s", got[0].SQL, wantSQL)
	}
	if got[0].Args != wantArgs {
		t.Errorf("args differ\n got: %s\nwant: %s", got[0].Args, wantArgs)
	}
}
