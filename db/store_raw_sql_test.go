package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

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
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var result rawAuthorBookCount
	err := store.QueryRow(
		context.Background(),
		&result,
		`SELECT authors.name AS name, COUNT(books.id) AS bookcount
		 FROM authors JOIN books ON books.author_id = authors.id
		 WHERE authors.name = ?
		 GROUP BY authors.name`,
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
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var result rawAuthor
	err := store.QueryRow(context.Background(), &result, "SELECT id, name FROM authors WHERE name = ?", "Missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreQueryScansMultipleResults(t *testing.T) {
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var results []rawAuthor
	err := store.Query(context.Background(), &results, "SELECT id, name FROM authors ORDER BY name")
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
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	results := []rawAuthor{}
	err := store.Query(context.Background(), &results, "SELECT id, name FROM authors WHERE name = ?", "Missing")
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
// the gap the bookstore dogfooding app hit twice as a runtime failure.
func TestStoreQueryMatchesGeneratedSnakeCaseColumnsWithoutAliasing(t *testing.T) {
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var results []rawBook
	err := store.Query(context.Background(), &results, "SELECT author_id, title FROM books ORDER BY title")
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
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var result rawBook
	err := store.QueryRow(context.Background(), &result, "SELECT author_id, title FROM books WHERE title = ?", "Notes")
	if err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}
	if result.AuthorID != 1 {
		t.Fatalf("AuthorID = %d, want 1", result.AuthorID)
	}
}

func TestStoreQueryStillMatchesExactFieldNameAliases(t *testing.T) {
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var results []rawBook
	err := store.Query(context.Background(), &results, `SELECT author_id AS AuthorID, title AS Title FROM books ORDER BY title`)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Query returned %d rows, want 3", len(results))
	}
}

func TestStoreQueryUnmatchedColumnFails(t *testing.T) {
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var results []rawAuthor
	err := store.Query(
		context.Background(),
		&results,
		`SELECT authors.id AS id, authors.name AS name, books.title AS title
		 FROM authors JOIN books ON books.author_id = authors.id`,
	)
	if err == nil {
		t.Fatal("Query returned nil error for an unmatched column, want non-nil")
	}
}

func openRawSQLTestDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	ctx := context.Background()
	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE authors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create authors table: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			author_id INTEGER NOT NULL,
			title TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create books table: %v", err)
	}

	_, err = sqlDB.ExecContext(ctx, `INSERT INTO authors (id, name) VALUES (1, 'Ada'), (2, 'Grace')`)
	if err != nil {
		t.Fatalf("seed authors: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO books (author_id, title) VALUES
			(1, 'Notes'),
			(1, 'Sketches'),
			(2, 'COBOL Reflections')
	`)
	if err != nil {
		t.Fatalf("seed books: %v", err)
	}

	return sqlDB
}
