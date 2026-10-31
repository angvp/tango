package admin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// TestForeignKeyViolationShapes checks the refusal each driver reports for a
// row another row still references. The dialects are not run here; the shapes
// are the drivers' documented codes and messages.
func TestForeignKeyViolationShapes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sqlite refuses a referenced delete", errors.New("constraint failed: FOREIGN KEY constraint failed (787)"), true},
		{"postgres foreign_key_violation", &pgconn.PgError{Code: "23503"}, true},
		{"mysql row is referenced", &mysql.MySQLError{Number: 1451, Message: "Cannot delete or update a parent row"}, true},
		{"mysql no referenced row", &mysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row"}, true},
		{"postgres unique violation is not a foreign key", &pgconn.PgError{Code: "23505"}, false},
		{"no such table", errors.New("no such table: fk_post"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isForeignKeyViolation(fmt.Errorf("wrapped: %w", tt.err)); got != tt.want {
				t.Fatalf("isForeignKeyViolation = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRacedForeignKeyOnSaveIsTheFormLevelMessage covers a save whose foreign
// key the database refused after the preflight passed (a concurrent delete).
func TestRacedForeignKeyOnSaveIsTheFormLevelMessage(t *testing.T) {
	meta := model.ModelMeta{Name: "Post"}
	raced := &pgconn.PgError{Code: "23503"}
	fields, formError, refused := storeFailure(context.Background(), raced, meta, adminregistry.Options{})
	if !refused || fields != nil {
		t.Fatalf("refused = %v, fields = %v; want a refusal with no field message", refused, fields)
	}
	if formError != "The item you chose no longer exists. Choose another and save again." {
		t.Fatalf("formError = %q", formError)
	}
}
