package admin

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

const bulkLeftInPlaceText = "tango admin: bulk delete left a selected row in place"

// bulkStoreFixture migrates bulkInternalRow on a fresh database and seeds
// rows 1, 2 and 3, returning the store with the model's metadata and its
// primary-key field.
func bulkStoreFixture(t *testing.T) (*sql.DB, db.Dialect, *db.Store, model.ModelMeta, model.FieldMeta) {
	t.Helper()
	models := model.NewRegistry()
	if err := models.Register(bulkInternalRow{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	meta, ok := models.Get("bulkInternalRow")
	if !ok {
		t.Fatal("bulkInternalRow is not registered")
	}
	field, err := primaryKeyField(meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3} {
		if err := store.Create(context.Background(), meta, &bulkInternalRow{ID: id, Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	return sqlDB, dialect, store, meta, field
}

// blockBulkInternalDelete installs a BEFORE DELETE trigger on bulk_internal_row.
// With ignore set, deleting the row with id onlyID is skipped silently; without
// it, every delete is refused with an error. The statements are written for
// SQLite and PostgreSQL; other dialects skip.
func blockBulkInternalDelete(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, onlyID int64, ignore bool) {
	t.Helper()
	var stmts []string
	switch dialect {
	case db.SQLite:
		when := ""
		if onlyID != 0 {
			when = fmt.Sprintf(" WHEN OLD.id = %d", onlyID)
		}
		action := `SELECT RAISE(ABORT, 'delete blocked for test');`
		if ignore {
			action = `SELECT RAISE(IGNORE);`
		}
		stmts = []string{`CREATE TRIGGER block_bulk_internal_delete BEFORE DELETE ON bulk_internal_row` + when + ` BEGIN ` + action + ` END;`}
	case db.Postgres:
		body := `RAISE EXCEPTION 'delete blocked for test';`
		if ignore {
			body = `RETURN NULL;`
		}
		when := ""
		if onlyID != 0 {
			when = fmt.Sprintf(" WHEN (OLD.id = %d)", onlyID)
		}
		stmts = []string{
			`CREATE FUNCTION block_bulk_internal_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + body + ` END; $$`,
			`CREATE TRIGGER block_bulk_internal_delete BEFORE DELETE ON bulk_internal_row FOR EACH ROW` + when + ` EXECUTE FUNCTION block_bulk_internal_delete()`,
		}
	default:
		t.Skipf("no delete-blocking trigger is written for dialect %v in this test", dialect)
	}
	for _, stmt := range stmts {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("install trigger: %v", err)
		}
	}
}

func bulkInternalRowCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM bulk_internal_row`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestParseBulkKeyRejectsAKeyKindItDoesNotParse(t *testing.T) {
	cases := []struct {
		name string
		kind reflect.Type
		raw  string
	}{
		{name: "float", kind: reflect.TypeOf(float64(0)), raw: "1"},
		{name: "bool", kind: reflect.TypeOf(false), raw: "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := parseBulkKey(model.FieldMeta{Name: "ID", Type: tc.kind}, tc.raw)
			if ok {
				t.Fatalf("parseBulkKey accepted %q for a %s key: %+v", tc.raw, tc.name, key)
			}
		})
	}
}

func TestRemoveBulkKeysReportsADeleteTheDatabaseRefuses(t *testing.T) {
	sqlDB, dialect, store, meta, field := bulkStoreFixture(t)
	blockBulkInternalDelete(t, sqlDB, dialect, 0, false)
	keys, ok := parseBulkKeys(field, []string{"1", "2"})
	if !ok {
		t.Fatal("keys did not parse")
	}

	err := store.InTx(context.Background(), func(tx *db.Store) error {
		return removeBulkKeys(context.Background(), tx, meta, field, keys)
	})

	if err == nil || !strings.Contains(err.Error(), "delete blocked for test") {
		t.Fatalf("err = %v, want the database's refusal", err)
	}
	if got := bulkInternalRowCount(t, sqlDB); got != 3 {
		t.Fatalf("rows left = %d, want 3", got)
	}
}

func TestRemoveBulkKeysRefusesARowThatSurvivesItsDelete(t *testing.T) {
	sqlDB, dialect, store, meta, field := bulkStoreFixture(t)
	blockBulkInternalDelete(t, sqlDB, dialect, 2, true)
	keys, ok := parseBulkKeys(field, []string{"1", "2"})
	if !ok {
		t.Fatal("keys did not parse")
	}

	err := store.InTx(context.Background(), func(tx *db.Store) error {
		return removeBulkKeys(context.Background(), tx, meta, field, keys)
	})

	if err == nil || err.Error() != bulkLeftInPlaceText {
		t.Fatalf("err = %v, want %q", err, bulkLeftInPlaceText)
	}
	if got := bulkInternalRowCount(t, sqlDB); got != 3 {
		t.Fatalf("rows left = %d, want 3: the transaction must roll back the delete of row 1", got)
	}
}

func TestRemoveBulkKeysRollsBackWhenTheReReadFails(t *testing.T) {
	sqlDB, _, store, meta, field := bulkStoreFixture(t)
	keys, ok := parseBulkKeys(field, []string{"1", "2"})
	if !ok {
		t.Fatal("keys did not parse")
	}
	// The delete goes by the real key, but the re-read filters on a column
	// that does not exist, so the re-read fails after the deletes ran.
	brokenField := field
	brokenField.Name = "NoSuchColumn"

	err := store.InTx(context.Background(), func(tx *db.Store) error {
		return removeBulkKeys(context.Background(), tx, meta, brokenField, keys)
	})

	if err == nil || err.Error() == bulkLeftInPlaceText {
		t.Fatalf("err = %v, want the re-read's own failure", err)
	}
	if got := bulkInternalRowCount(t, sqlDB); got != 3 {
		t.Fatalf("rows left = %d, want 3: a failed re-read must roll back the deletes", got)
	}
}
