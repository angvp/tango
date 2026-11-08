package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// uniqueTime has a unique time column: several unset values must not collide.
type uniqueTime struct {
	ID int64     `tango:"pk"`
	At time.Time `tango:"unique"`
}

// orderTime is an ordered time column with unset and set rows.
type orderTime struct {
	ID int64 `tango:"pk"`
	At time.Time
}

// TestUniqueTimeAcceptsSeveralUnsetValuesOnEveryDialect: a zero time is NULL,
// and several NULLs do not conflict under a unique constraint.
func TestUniqueTimeAcceptsSeveralUnsetValuesOnEveryDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, uniqueTime{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, uniqueTime{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		row := uniqueTime{}
		if err := store.Create(ctx, meta, &row); err != nil {
			t.Fatalf("unset time %d: %v", i, err)
		}
	}
	set := uniqueTime{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.Create(ctx, meta, &set); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, meta, &uniqueTime{At: set.At}); err == nil {
		t.Error("a duplicate set time was accepted by the unique constraint")
	}
}

// TestNullOrderingIsNativeToEachDialect: ascending order puts an unset time
// first on SQLite and MySQL and last on PostgreSQL. The expected order is the
// one the migrations guide states.
func TestNullOrderingIsNativeToEachDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, orderTime{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, orderTime{})
	ctx := context.Background()
	rows := []orderTime{
		{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, // id 1
		{}, // id 2, unset
		{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, // id 3
	}
	for i := range rows {
		if err := store.Create(ctx, meta, &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	query := "SELECT " + quoteFor(dialect, "id") + " FROM " + quoteFor(dialect, "order_time") + " ORDER BY " + quoteFor(dialect, "at") + " ASC"
	result, err := sqlDB.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	var got []int64
	for result.Next() {
		var id int64
		if err := result.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	want := []int64{2, 3, 1}
	if dialect == db.Postgres {
		want = []int64{3, 1, 2}
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("dialect %d: ascending order = %v, want %v", dialect, got, want)
	}
}

// TestCleanupSQLConvertsHistoricYearOneRows runs the optional cleanup statement
// from the migrations guide on a historic year-0001 row and a real date. The
// year-0001 row becomes NULL, and the real date is untouched. SQLite and
// PostgreSQL only: MySQL cannot store year 0001.
func TestCleanupSQLConvertsHistoricYearOneRows(t *testing.T) {
	testdb.NotMySQL(t, "MySQL has no year 0001 to clean up")
	sqlDB, dialect, _ := openTables(t, orderTime{})
	ctx := context.Background()
	year1 := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	insert := "INSERT INTO " + quoteFor(dialect, "order_time") + " (" + quoteFor(dialect, "id") + ", " + quoteFor(dialect, "at") + ") VALUES (1, " + bindFor(dialect) + ")"
	if _, err := sqlDB.ExecContext(ctx, insert, year1); err != nil {
		t.Fatal(err)
	}
	insert = "INSERT INTO " + quoteFor(dialect, "order_time") + " (" + quoteFor(dialect, "id") + ", " + quoteFor(dialect, "at") + ") VALUES (2, " + bindFor(dialect) + ")"
	if _, err := sqlDB.ExecContext(ctx, insert, recorded); err != nil {
		t.Fatal(err)
	}
	cleanup := `UPDATE "order_time" SET "at" = NULL WHERE "at" LIKE '0001-01-01%'`
	if dialect == db.Postgres {
		cleanup = `UPDATE "order_time" SET "at" = NULL WHERE "at" < '0002-01-01'`
	}
	if _, err := sqlDB.ExecContext(ctx, cleanup); err != nil {
		t.Fatal(err)
	}
	var nulls, kept int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "order_time" WHERE "at" IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "order_time" WHERE "at" IS NOT NULL`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if nulls != 1 || kept != 1 {
		t.Errorf("after cleanup: %d NULL and %d set rows, want 1 and 1", nulls, kept)
	}
}

// TestZeroForeignKeyInListMatchesOnlyTheAssignedOwner: IN with a zero foreign
// key is not rejected, and an unset row is NULL, so OwnerID IN (0, 3) matches
// owner 3 only. The persistence guide states this.
func TestZeroForeignKeyInListMatchesOnlyTheAssignedOwner(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, zeroOwner{}, zeroKinds{})
	store := db.NewStore(sqlDB, dialect)
	ownerMeta := metaFor(t, registry, zeroOwner{})
	meta := metaFor(t, registry, zeroKinds{})
	ctx := context.Background()
	var owner3 zeroOwner
	for i := 1; i <= 3; i++ {
		owner := zeroOwner{Name: "o"}
		if err := store.Create(ctx, ownerMeta, &owner); err != nil {
			t.Fatal(err)
		}
		owner3 = owner
	}
	unset := zeroKinds{Text: "unset"}
	if err := store.Create(ctx, meta, &unset); err != nil {
		t.Fatal(err)
	}
	assigned := zeroKinds{Text: "assigned", Owner: owner3.ID}
	if err := store.Create(ctx, meta, &assigned); err != nil {
		t.Fatal(err)
	}
	var rows []zeroKinds
	query := db.Query{Filter: ptr(db.In("Owner", []int64{0, owner3.ID}))}
	if err := store.List(ctx, meta, query, &rows); err != nil {
		t.Fatalf("IN with a zero foreign key: %v", err)
	}
	if len(rows) != 1 || rows[0].Text != "assigned" {
		t.Errorf("IN (0, %d) matched %d rows; want only the owner-%d row", owner3.ID, len(rows), owner3.ID)
	}
}

// TestIsNullFindsUnsetTimesOnEveryDialect is the time-column counterpart of
// the date test.
func TestIsNullFindsUnsetTimesOnEveryDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, orderTime{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, orderTime{})
	ctx := context.Background()
	set := orderTime{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	unset := orderTime{}
	for _, row := range []*orderTime{&set, &unset} {
		if err := store.Create(ctx, meta, row); err != nil {
			t.Fatal(err)
		}
	}
	var nullRows []orderTime
	if err := store.List(ctx, meta, db.Query{Filter: ptr(db.IsNull("At"))}, &nullRows); err != nil {
		t.Fatal(err)
	}
	if len(nullRows) != 1 || nullRows[0].ID != unset.ID {
		t.Errorf("IsNull matched %v, want only the unset row %d", nullRows, unset.ID)
	}
	var setRows []orderTime
	if err := store.List(ctx, meta, db.Query{Filter: ptr(db.IsNotNull("At"))}, &setRows); err != nil {
		t.Fatal(err)
	}
	if len(setRows) != 1 || setRows[0].ID != set.ID {
		t.Errorf("IsNotNull matched %v, want only the set row %d", setRows, set.ID)
	}
}
