package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/angvp/tango/db"
)

// author and book are the models behind the raw queries' tables; rawAuthor,
// rawAuthorBookCount and rawBook are plain result shapes for those queries.
type author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type book struct {
	ID       int64 `tango:"pk"`
	AuthorID int64 `tango:"fk=author"`
	Title    string
}

type rawAuthor struct {
	ID   int64
	Name string
}

type rawAuthorBookCount struct {
	Name      string
	BookCount int
}

type rawBook struct {
	AuthorID int64
	Title    string
}

func TestStoreQueryRowScansSingleResult(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var result rawAuthorBookCount
	err := store.QueryRow(
		context.Background(),
		&result,
		bind(dialect, `SELECT author.name AS name, COUNT(book.id) AS bookcount
		 FROM author JOIN book ON book.author_id = author.id
		 WHERE author.name = ?
		 GROUP BY author.name`),
		"Ada",
	)
	if err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}

	if result.Name != "Ada" || result.BookCount != 2 {
		t.Fatalf("QueryRow = %+v, want Name=Ada BookCount=2", result)
	}
}

func TestStoreQueryRowNoResultsReturnsErrNotFound(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var result rawAuthor
	err := store.QueryRow(context.Background(), &result, bind(dialect, "SELECT id, name FROM author WHERE name = ?"), "Missing")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap db.ErrNotFound", err)
	}
}

func TestStoreQueryScansMultipleResults(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var results []rawAuthor
	err := store.Query(context.Background(), &results, "SELECT id, name FROM author ORDER BY name")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Query returned %d rows, want 2", len(results))
	}
	if results[0].Name != "Ada" || results[1].Name != "Grace" {
		t.Fatalf("Query = %+v, want names Ada, Grace", results)
	}
}

func TestStoreQueryNoResultsReturnsEmptySliceNotError(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	results := []rawAuthor{}
	err := store.Query(context.Background(), &results, bind(dialect, "SELECT id, name FROM author WHERE name = ?"), "Missing")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if results == nil {
		t.Fatal("Query left dest nil, want empty non-nil slice")
	}
	if len(results) != 0 {
		t.Fatalf("Query returned %d rows, want 0", len(results))
	}
}

// TestStoreQueryMatchesGeneratedSnakeCaseColumnsWithoutAliasing proves a raw
// query selecting tanGO's own generated column names (author_id, no "AS")
// scans straight into the corresponding Go field (AuthorID) — the fix for
// the gap the booktore dogfooding app hit twice as a runtime failure.
func TestStoreQueryMatchesGeneratedSnakeCaseColumnsWithoutAliasing(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var results []rawBook
	err := store.Query(context.Background(), &results, "SELECT author_id, title FROM book ORDER BY title")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Query returned %d rows, want 3", len(results))
	}
	if results[0].Title != "COBOL Reflections" || results[0].AuthorID != 2 {
		t.Fatalf("Query[0] = %+v, want Title=COBOL Reflections AuthorID=2", results[0])
	}
}

// TestStoreQueryRowMatchesGeneratedSnakeCaseColumnWithoutAliasing is
// QueryRow's equivalent of the Query case above.
func TestStoreQueryRowMatchesGeneratedSnakeCaseColumnWithoutAliasing(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var result rawBook
	err := store.QueryRow(context.Background(), &result, bind(dialect, "SELECT author_id, title FROM book WHERE title = ?"), "Notes")
	if err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}
	if result.AuthorID != 1 {
		t.Fatalf("AuthorID = %d, want 1", result.AuthorID)
	}
}

func TestStoreQueryStillMatchesExactFieldNameAliases(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var results []rawBook
	err := store.Query(context.Background(), &results, `SELECT author_id AS AuthorID, title AS Title FROM book ORDER BY title`)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Query returned %d rows, want 3", len(results))
	}
}

func TestStoreQueryUnmatchedColumnFails(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var results []rawAuthor
	err := store.Query(
		context.Background(),
		&results,
		`SELECT author.id AS id, author.name AS name, book.title AS title
		 FROM author JOIN book ON book.author_id = author.id`,
	)
	if err == nil {
		t.Fatal("Query returned nil error for an unmatched column, want non-nil")
	}
}

// openRawSQLTestDB returns a fresh database holding the author and book
// tables, seeded with two authors (Ada is 1, Grace is 2) and three books.
func openRawSQLTestDB(t *testing.T) (*sql.DB, db.Dialect) {
	t.Helper()
	sqlDB, dialect, registry := openTables(t, author{}, book{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	authorMeta, bookMeta := metaFor(t, registry, author{}), metaFor(t, registry, book{})
	for _, a := range []author{{ID: 1, Name: "Ada"}, {ID: 2, Name: "Grace"}} {
		if err := store.Create(ctx, authorMeta, &a); err != nil {
			t.Fatalf("seed author: %v", err)
		}
	}
	for _, b := range []book{{AuthorID: 1, Title: "Notes"}, {AuthorID: 1, Title: "Sketches"}, {AuthorID: 2, Title: "COBOL Reflections"}} {
		if err := store.Create(ctx, bookMeta, &b); err != nil {
			t.Fatalf("seed book: %v", err)
		}
	}
	return sqlDB, dialect
}
