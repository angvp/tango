package admin

import (
	"context"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type bulkInternalRow struct {
	ID   int64 `tango:"pk"`
	Name string
}

func TestRemoveBulkKeysAcceptsARowGoneAfterTheReRead(t *testing.T) {
	models := model.NewRegistry()
	if err := models.Register(bulkInternalRow{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	meta, _ := models.Get("bulkInternalRow")
	field, err := primaryKeyField(meta)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, id := range []int64{1, 2, 3} {
		if err := store.Create(ctx, meta, &bulkInternalRow{ID: id, Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	keys, ok := parseBulkKeys(field, []string{"1", "2"})
	if !ok {
		t.Fatal("keys did not parse")
	}

	// Another transaction removes key 2 after this one re-read the selection.
	if _, err := sqlDB.Exec(`DELETE FROM bulk_internal_row WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	err = store.InTx(ctx, func(tx *db.Store) error {
		return removeBulkKeys(ctx, tx, meta, field, keys)
	})
	if err != nil {
		t.Fatalf("removeBulkKeys = %v, want success: a selected row already gone is not a failure", err)
	}
	var left int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM bulk_internal_row`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("rows left = %d (err %v), want only the unselected row", left, err)
	}

	// The same selection at re-read time, with key 2 already gone, is refused.
	err = store.InTx(ctx, func(tx *db.Store) error {
		return deleteBulkKeys(ctx, tx, meta, field, keys)
	})
	if err != errBulkKeyMissing {
		t.Fatalf("deleteBulkKeys = %v, want errBulkKeyMissing", err)
	}
}
