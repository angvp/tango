package migration

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// indexNames lists table's indexes from the dialect's own catalog. Indexes
// have no other observable behavior a test can check without timing, so
// this is the one place the rebuild tests read the catalog.
func indexNames(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table string) []string {
	t.Helper()
	query := "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = $1 AND sql IS NOT NULL"
	if dialect == db.Postgres {
		query = "SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1 AND indexname NOT LIKE '%_pkey'"
	}
	rows, err := sqlDB.Query(query, table)
	if err != nil {
		t.Fatalf("list indexes of %q: %v", table, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list indexes of %q: %v", table, err)
	}
	slices.Sort(names)
	return names
}

func applySteps(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, steps ...Step) {
	t.Helper()
	for _, step := range steps {
		if err := ApplyStep(context.Background(), sqlDB, dialect, step); err != nil {
			t.Fatalf("ApplyStep(%#v): %v", step, err)
		}
	}
}

func mustExec(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func mustFail(t *testing.T, sqlDB *sql.DB, why, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err == nil {
		t.Fatalf("%s succeeded, want it rejected: %s", query, why)
	}
}

// rebuildFixture creates an author table with every kind of structure a
// migration can give a column (a unique column, an indexed column, a
// NOT NULL DEFAULT column added later, a column about to be dropped), and a
// post table that references it, with rows in both.
func rebuildFixture(t *testing.T) (*sql.DB, db.Dialect) {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect,
		CreateTable{Table: "author", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Unique: true},
			{Name: "nick", Type: "text", Indexed: true},
			{Name: "bio", Type: "text"},
		}},
		AddColumn{Table: "author", Column: Column{Name: "status", Type: "text", Default: "'active'"}},
		CreateTable{Table: "post", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", Indexed: true, References: "author"},
			{Name: "title", Type: "text"},
			{Name: "junk", Type: "text"},
		}},
	)
	mustExec(t, sqlDB, `INSERT INTO author (email, nick, bio) VALUES ('ada@example.com', 'ada', 'math')`)
	mustExec(t, sqlDB, `INSERT INTO author (email, nick, bio, status) VALUES ('alan@example.com', 'alan', 'logic', 'away')`)
	mustExec(t, sqlDB, `INSERT INTO post (author_id, title, junk) VALUES (1, 'Notes', 'x'), (2, 'Machines', 'y')`)
	return sqlDB, dialect
}

func TestDropColumnKeepsTheRestOfAReferencedTable(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "author")

	applySteps(t, sqlDB, dialect, DropColumn{Table: "author", Column: "bio"})

	type author struct {
		id                  int64
		email, nick, status string
	}
	var got []author
	rows, err := sqlDB.Query(`SELECT id, email, nick, status FROM author ORDER BY id`)
	if err != nil {
		t.Fatalf("read authors: %v", err)
	}
	for rows.Next() {
		var a author
		if err := rows.Scan(&a.id, &a.email, &a.nick, &a.status); err != nil {
			t.Fatalf("scan author: %v", err)
		}
		got = append(got, a)
	}
	rows.Close()
	want := []author{{1, "ada@example.com", "ada", "active"}, {2, "alan@example.com", "alan", "away"}}
	if !slices.Equal(got, want) {
		t.Fatalf("authors = %v, want %v", got, want)
	}

	if after := indexNames(t, sqlDB, dialect, "author"); !slices.Equal(after, indexesBefore) {
		t.Fatalf("author indexes = %v, want %v", after, indexesBefore)
	}
	mustFail(t, sqlDB, "email is unique", `INSERT INTO author (email, nick) VALUES ('ada@example.com', 'other')`)
	mustFail(t, sqlDB, "status is NOT NULL", `INSERT INTO author (email, nick, status) VALUES ('n@example.com', 'n', NULL)`)
	mustFail(t, sqlDB, "post rows still reference author 1", `DELETE FROM author WHERE id = 1`)
	mustFail(t, sqlDB, "foreign keys are enforced again after the rebuild", `INSERT INTO post (author_id, title) VALUES (999, 'orphan')`)

	mustExec(t, sqlDB, `INSERT INTO author (email, nick) VALUES ('grace@example.com', 'grace')`)
	var id int64
	var status string
	if err := sqlDB.QueryRow(`SELECT id, status FROM author WHERE email = 'grace@example.com'`).Scan(&id, &status); err != nil {
		t.Fatalf("read new author: %v", err)
	}
	// Rejected inserts above may have used up ids on PostgreSQL, so only
	// require an id past every existing row.
	if id <= 2 || status != "active" {
		t.Fatalf("new author id, status = %d, %q; want an id past 2, %q (the default)", id, status, "active")
	}
}

func TestDropColumnKeepsTheRestOfAReferencingTable(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "post")

	applySteps(t, sqlDB, dialect, DropColumn{Table: "post", Column: "junk"})

	var titles []string
	rows, err := sqlDB.Query(`SELECT title FROM post ORDER BY id`)
	if err != nil {
		t.Fatalf("read posts: %v", err)
	}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan post: %v", err)
		}
		titles = append(titles, title)
	}
	rows.Close()
	if want := []string{"Notes", "Machines"}; !slices.Equal(titles, want) {
		t.Fatalf("post titles = %v, want %v", titles, want)
	}
	if after := indexNames(t, sqlDB, dialect, "post"); !slices.Equal(after, indexesBefore) {
		t.Fatalf("post indexes = %v, want %v", after, indexesBefore)
	}
	mustFail(t, sqlDB, "author_id references author", `INSERT INTO post (author_id, title) VALUES (999, 'orphan')`)
	mustExec(t, sqlDB, `INSERT INTO post (author_id, title) VALUES (1, 'More notes')`)
}

func postTitles(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT title FROM post ORDER BY id`)
	if err != nil {
		t.Fatalf("read posts: %v", err)
	}
	defer rows.Close()
	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan post: %v", err)
		}
		titles = append(titles, title)
	}
	return titles
}

// assertPostUntouched checks post still has every column, row and index it
// had before a rebuild that failed.
func assertPostUntouched(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, wantTitles, wantIndexes []string) {
	t.Helper()
	if got, want := columnNames(t, sqlDB, dialect, "post"), []string{"id", "author_id", "title", "junk"}; !slices.Equal(got, want) {
		t.Fatalf("post columns = %v, want %v", got, want)
	}
	if got := postTitles(t, sqlDB); !slices.Equal(got, wantTitles) {
		t.Fatalf("post titles = %v, want %v", got, wantTitles)
	}
	if got := indexNames(t, sqlDB, dialect, "post"); !slices.Equal(got, wantIndexes) {
		t.Fatalf("post indexes = %v, want %v", got, wantIndexes)
	}
}

func TestFailedRebuildAtTheFirstStatementLeavesTheTableIntact(t *testing.T) {
	testdb.SQLiteOnly(t, "only SQLite rebuilds a table to drop a column; PostgreSQL drops it in one statement")
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "post")
	mustExec(t, sqlDB, `CREATE TABLE post_tango_rebuild (id INTEGER)`)

	if err := ApplyStep(context.Background(), sqlDB, dialect, DropColumn{Table: "post", Column: "junk"}); err == nil {
		t.Fatal("DropColumn succeeded although the rebuild's temporary table name was taken, want an error")
	}

	assertPostUntouched(t, sqlDB, dialect, []string{"Notes", "Machines"}, indexesBefore)
	mustFail(t, sqlDB, "foreign keys are enforced again after a failed rebuild", `INSERT INTO post (author_id, title) VALUES (999, 'orphan')`)
}

func TestFailedRebuildAtTheForeignKeyCheckLeavesTheTableIntact(t *testing.T) {
	testdb.SQLiteOnly(t, "only SQLite rebuilds a table to drop a column, and only SQLite can hold an orphan row")
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "post")

	// Plant an orphan with enforcement off on one pinned connection, so the
	// rebuild gets through create, copy, drop and rename before its final
	// foreign-key check fails.
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("pin connection: %v", err)
	}
	for _, statement := range []string{
		"PRAGMA foreign_keys = OFF",
		"INSERT INTO post (author_id, title, junk) VALUES (999, 'Orphan', 'z')",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := conn.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	conn.Close()

	if err := ApplyStep(context.Background(), sqlDB, dialect, DropColumn{Table: "post", Column: "junk"}); err == nil {
		t.Fatal("DropColumn succeeded although post holds an orphan row, want the foreign-key check to fail it")
	}

	assertPostUntouched(t, sqlDB, dialect, []string{"Notes", "Machines", "Orphan"}, indexesBefore)
	mustFail(t, sqlDB, "foreign keys are enforced again after a failed rebuild", `INSERT INTO post (author_id, title) VALUES (998, 'another orphan')`)
}
