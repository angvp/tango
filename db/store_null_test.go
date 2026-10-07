package db_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// These tests pin down how the Store treats SQL NULL. tanGO has no nullable
// field type, so NULL and a field's Go zero value are the same thing: a
// NULL column reads back as the zero value, and a foreign key field left at
// zero ("unset") is written as NULL so the database's REFERENCES constraint
// accepts it.

type nullAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type nullPost struct {
	ID        int64 `tango:"pk"`
	AuthorID  int64 `tango:"fk=nullAuthor"`
	Title     string
	Views     int
	Score     float64
	Published bool
	CreatedAt time.Time
}

// nullSchema is the tables the way migrations build them for each dialect —
// the foreign key is a real REFERENCES constraint, enforced on SQLite
// because testdb turns foreign keys on — with every column but the key
// nullable, as after `tango migrate` adds columns to a table with data.
var nullSchema = map[db.Dialect][]string{
	db.SQLite: {
		`CREATE TABLE null_author (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`,
		`CREATE TABLE null_post (id INTEGER PRIMARY KEY AUTOINCREMENT, author_id INTEGER REFERENCES null_author,
			title TEXT, views INTEGER, score REAL, published BOOLEAN, created_at TIMESTAMP)`,
	},
	db.Postgres: {
		`CREATE TABLE null_author (id BIGSERIAL PRIMARY KEY, name TEXT)`,
		`CREATE TABLE null_post (id BIGSERIAL PRIMARY KEY, author_id BIGINT REFERENCES null_author,
			title TEXT, views BIGINT, score DOUBLE PRECISION, published BOOLEAN, created_at TIMESTAMPTZ)`,
	},
}

// openNullTestDB builds nullSchema in a fresh database for the run's Test
// dialect and returns a Store with the models registered.
func openNullTestDB(t *testing.T) (*sql.DB, *db.Store, model.ModelMeta, model.ModelMeta) {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	for _, stmt := range nullSchema[dialect] {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	registry := model.NewRegistry()
	for _, v := range []any{nullAuthor{}, nullPost{}} {
		if err := registry.Register(v); err != nil {
			t.Fatalf("register %T: %v", v, err)
		}
	}
	authorMeta, _ := registry.Get("nullAuthor")
	postMeta, _ := registry.Get("nullPost")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)
	return sqlDB, store, authorMeta, postMeta
}

// readAuthorID reads null_post.author_id for id straight from the table,
// bypassing the Store's NULL handling.
func readAuthorID(t *testing.T, sqlDB *sql.DB, id int64) sql.NullInt64 {
	t.Helper()
	query := `SELECT author_id FROM null_post WHERE id = ?`
	if testdb.Dialect() == db.Postgres {
		query = `SELECT author_id FROM null_post WHERE id = $1`
	}
	var authorID sql.NullInt64
	if err := sqlDB.QueryRow(query, id).Scan(&authorID); err != nil {
		t.Fatalf("read author_id: %v", err)
	}
	return authorID
}

func TestStoreCreateWritesZeroForeignKeyAsNull(t *testing.T) {
	sqlDB, store, _, postMeta := openNullTestDB(t)
	ctx := context.Background()

	post := nullPost{Title: "No author"}
	if err := store.Create(ctx, postMeta, &post); err != nil {
		t.Fatalf("Create with an unset foreign key: %v", err)
	}

	authorID := readAuthorID(t, sqlDB, post.ID)
	if authorID.Valid {
		t.Fatalf("author_id = %d, want NULL for a zero foreign key", authorID.Int64)
	}

	var got nullPost
	if err := store.Get(ctx, postMeta, post.ID, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AuthorID != 0 {
		t.Fatalf("AuthorID = %d, want 0 read back from NULL", got.AuthorID)
	}
}

func TestStoreUpdateWritesZeroForeignKeyAsNull(t *testing.T) {
	sqlDB, store, authorMeta, postMeta := openNullTestDB(t)
	ctx := context.Background()

	author := nullAuthor{Name: "Ada"}
	if err := store.Create(ctx, authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := nullPost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(ctx, postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	post.AuthorID = 0
	if err := store.Update(ctx, postMeta, &post); err != nil {
		t.Fatalf("Update clearing the foreign key: %v", err)
	}

	authorID := readAuthorID(t, sqlDB, post.ID)
	if authorID.Valid {
		t.Fatalf("author_id = %d, want NULL after clearing the foreign key", authorID.Int64)
	}
}

func TestStoreReadsNullColumnsAsZeroValues(t *testing.T) {
	sqlDB, store, _, postMeta := openNullTestDB(t)
	ctx := context.Background()

	// A row as it looks after `tango migrate` added columns to a table that
	// already had data: every column but the key is NULL.
	var id int64
	if err := sqlDB.QueryRow(`INSERT INTO null_post DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got nullPost
	if err := store.Get(ctx, postMeta, id, &got); err != nil {
		t.Fatalf("Get a row with NULL columns: %v", err)
	}
	if want := (nullPost{ID: id}); got != want {
		t.Fatalf("Get = %+v, want zero values %+v", got, want)
	}

	var listed []nullPost
	if err := store.List(ctx, postMeta, db.Query{}, &listed); err != nil {
		t.Fatalf("List rows with NULL columns: %v", err)
	}
	if len(listed) != 1 || listed[0] != (nullPost{ID: id}) {
		t.Fatalf("List = %+v, want one row of zero values", listed)
	}
}

func TestStoreRawQueryReadsNullAsZeroValue(t *testing.T) {
	_, store, authorMeta, postMeta := openNullTestDB(t)
	ctx := context.Background()

	author := nullAuthor{Name: "Ada"}
	if err := store.Create(ctx, authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	if err := store.Create(ctx, postMeta, &nullPost{Title: "Orphan"}); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	// The LEFT JOIN yields a NULL author name for the post without one.
	type row struct {
		Title      string
		AuthorName string
	}
	var rows []row
	err := store.Query(ctx, &rows, `
		SELECT p.title, a.name AS author_name
		FROM null_post p LEFT JOIN null_author a ON a.id = p.author_id`)
	if err != nil {
		t.Fatalf("Query with a NULL column: %v", err)
	}
	if len(rows) != 1 || rows[0] != (row{Title: "Orphan"}) {
		t.Fatalf("Query = %+v, want [{Orphan }]", rows)
	}

	var one row
	if err := store.QueryRow(ctx, &one, `SELECT 'x' AS title, NULL AS author_name`); err != nil {
		t.Fatalf("QueryRow with a NULL column: %v", err)
	}
	if one != (row{Title: "x"}) {
		t.Fatalf("QueryRow = %+v, want {x }", one)
	}
}

func TestStoreNullScanKeepsScannerAndPointerFieldsWorking(t *testing.T) {
	_, store, _, _ := openNullTestDB(t)
	ctx := context.Background()

	type row struct {
		Name     sql.NullString
		Nickname *string
		Count    int
	}
	var got row
	if err := store.QueryRow(ctx, &got, `SELECT NULL AS name, NULL AS nickname, 3 AS count`); err != nil {
		t.Fatalf("QueryRow NULLs: %v", err)
	}
	if got.Name.Valid || got.Nickname != nil || got.Count != 3 {
		t.Fatalf("QueryRow = %+v, want invalid NullString, nil pointer, Count 3", got)
	}

	if err := store.QueryRow(ctx, &got, `SELECT 'ada' AS name, 'a' AS nickname, 4 AS count`); err != nil {
		t.Fatalf("QueryRow values: %v", err)
	}
	if !got.Name.Valid || got.Name.String != "ada" || got.Nickname == nil || *got.Nickname != "a" || got.Count != 4 {
		t.Fatalf("QueryRow = %+v, want the selected values", got)
	}
}
