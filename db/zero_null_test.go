package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// zeroKinds holds one field of each kind a zero value may take. Only the time
// field's zero is stored as NULL; the others keep their normal storage.
type zeroKinds struct {
	ID    int64 `tango:"pk"`
	Text  string
	Count int64
	Flag  bool
	At    time.Time
	Owner int64 `tango:"fk=zeroOwner"`
}

// zeroOwner is the model zeroKinds.Owner references.
type zeroOwner struct {
	ID   int64 `tango:"pk"`
	Name string
}

// quoteFor quotes a SQL identifier the way the dialect expects.
func quoteFor(dialect db.Dialect, name string) string {
	if dialect == db.MySQL {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}

// bindFor is the placeholder for the first bind argument on dialect.
func bindFor(dialect db.Dialect) string {
	if dialect == db.Postgres {
		return "$1"
	}
	return "?"
}

// TestZeroValuesAreStoredAsNULLOnEveryDialect writes unset values and reads
// each column's NULL-ness through the dialect's own SQL, not the ORM. The
// zero time must be NULL on every dialect; the zero string, number and
// boolean must keep their storage.
func TestZeroValuesAreStoredAsNULLOnEveryDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, zeroOwner{}, zeroKinds{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, zeroKinds{})
	ctx := context.Background()

	row := zeroKinds{}
	if err := store.Create(ctx, meta, &row); err != nil {
		t.Fatal(err)
	}
	table := quoteFor(dialect, "zero_kinds")
	cases := []struct {
		column string
		isNull bool
	}{
		{"at", true},
		{"owner", true},
		{"text", false},
		{"count", false},
		{"flag", false},
	}
	for _, c := range cases {
		column := quoteFor(dialect, c.column)
		query := "SELECT CASE WHEN " + column + " IS NULL THEN 1 ELSE 0 END FROM " + table + " WHERE " + quoteFor(dialect, "id") + " = " + bindFor(dialect)
		var nulls int
		if err := sqlDB.QueryRowContext(ctx, query, row.ID).Scan(&nulls); err != nil {
			t.Fatalf("%s: %v", c.column, err)
		}
		if got := nulls == 1; got != c.isNull {
			t.Errorf("dialect %d: column %s NULL = %v, want %v", dialect, c.column, got, c.isNull)
		}
	}
}

// TestSettingATimeToZeroStoresNULLOnEveryDialect checks the update path: a
// stored time that is set back to zero is written as NULL, as on create.
func TestSettingATimeToZeroStoresNULLOnEveryDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, zeroOwner{}, zeroKinds{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, zeroKinds{})
	ctx := context.Background()

	row := zeroKinds{At: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)}
	if err := store.Create(ctx, meta, &row); err != nil {
		t.Fatal(err)
	}
	row.At = time.Time{}
	if err := store.Update(ctx, meta, &row); err != nil {
		t.Fatal(err)
	}
	var nulls int
	query := "SELECT CASE WHEN " + quoteFor(dialect, "at") + " IS NULL THEN 1 ELSE 0 END FROM " + quoteFor(dialect, "zero_kinds") + " WHERE " + quoteFor(dialect, "id") + " = " + bindFor(dialect)
	if err := sqlDB.QueryRowContext(ctx, query, row.ID).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 1 {
		t.Errorf("dialect %d: a time set to zero is not NULL", dialect)
	}
}

// TestHistoricYearOneRowsStillReadAsZeroTime inserts a year-0001 value by raw
// SQL, as rows written before the zero-as-NULL rule hold it. It must keep
// reading as the zero time, and nothing rewrites it.
func TestHistoricYearOneRowsStillReadAsZeroTime(t *testing.T) {
	testdb.NotMySQL(t, "MySQL has no year 0001 to store")
	sqlDB, dialect, registry := openTables(t, zeroOwner{}, zeroKinds{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, zeroKinds{})
	ctx := context.Background()

	literal := "0001-01-01 00:00:00"
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO "+quoteFor(dialect, "zero_kinds")+" ("+quoteFor(dialect, "id")+", "+quoteFor(dialect, "at")+") VALUES (1, "+bindFor(dialect)+")", literal); err != nil {
		t.Fatal(err)
	}
	var got zeroKinds
	if err := store.Get(ctx, meta, int64(1), &got); err != nil {
		t.Fatal(err)
	}
	if !got.At.IsZero() {
		t.Errorf("historic year-0001 row reads as %v, want the zero time", got.At)
	}
}

// TestZeroForeignKeyIsStoredAsNULLOnCreateAndUpdate: an unset foreign key is
// NULL on every dialect, both when created unset and when set back to zero.
func TestZeroForeignKeyIsStoredAsNULLOnCreateAndUpdate(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, zeroOwner{}, zeroKinds{})
	store := db.NewStore(sqlDB, dialect)
	ownerMeta := metaFor(t, registry, zeroOwner{})
	meta := metaFor(t, registry, zeroKinds{})
	ctx := context.Background()

	owner := zeroOwner{}
	if err := store.Create(ctx, ownerMeta, &owner); err != nil {
		t.Fatal(err)
	}
	unset := zeroKinds{}
	if err := store.Create(ctx, meta, &unset); err != nil {
		t.Fatal(err)
	}
	set := zeroKinds{Owner: owner.ID}
	if err := store.Create(ctx, meta, &set); err != nil {
		t.Fatal(err)
	}
	set.Owner = 0
	if err := store.Update(ctx, meta, &set); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{unset.ID, set.ID} {
		var nulls int
		query := "SELECT CASE WHEN " + quoteFor(dialect, "owner") + " IS NULL THEN 1 ELSE 0 END FROM " + quoteFor(dialect, "zero_kinds") + " WHERE " + quoteFor(dialect, "id") + " = " + bindFor(dialect)
		if err := sqlDB.QueryRowContext(ctx, query, id).Scan(&nulls); err != nil {
			t.Fatal(err)
		}
		if nulls != 1 {
			t.Errorf("dialect %d: zero foreign key on row %d is not NULL", dialect, id)
		}
	}
}
