package admin

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/angvp/tango/model"
)

type uniqueErrorBook struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"unique"`
	Slug  string
}

// TestUniqueViolationFieldAttributesEachDialectsShape checks the attribution
// on the error shape each driver reports. The dialects are not run here; the
// shapes are the driver's documented messages and fields.
func TestUniqueViolationFieldAttributesEachDialectsShape(t *testing.T) {
	registry := model.NewRegistry()
	if err := registry.Register(uniqueErrorBook{}); err != nil {
		t.Fatal(err)
	}
	meta, ok := registry.Get("uniqueErrorBook")
	if !ok {
		t.Fatal("uniqueErrorBook is not registered")
	}
	tests := []struct {
		name      string
		err       error
		wantField string
		wantOK    bool
	}{
		{"sqlite column", errors.New("constraint failed: UNIQUE constraint failed: unique_error_book.title (2067)"), "Title", true},
		{"sqlite composite is ambiguous", errors.New("UNIQUE constraint failed: unique_error_book.title, unique_error_book.slug (2067)"), "", false},
		{"postgres constraint", &pgconn.PgError{Code: "23505", ConstraintName: "uniq_unique_error_book_title"}, "Title", true},
		{"mysql key with table prefix", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'unique_error_book.uniq_unique_error_book_title'"}, "Title", true},
		{"mysql key of an undeclared index", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'unique_error_book.uniq_unique_error_book_slug'"}, "", false},
		{"not a unique violation", errors.New("no such table: unique_error_book"), "", false},
		{"postgres foreign key is not a unique violation", &pgconn.PgError{Code: "23503", ConstraintName: "uniq_unique_error_book_title"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, ok := uniqueViolationField(meta, fmt.Errorf("wrapped: %w", tt.err))
			if ok != tt.wantOK || field.Name != tt.wantField {
				t.Fatalf("uniqueViolationField = (%q, %v), want (%q, %v)", field.Name, ok, tt.wantField, tt.wantOK)
			}
		})
	}
}
