package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type dateEvent struct {
	ID    int64 `tango:"pk"`
	Title string
	Day   model.Date
}

type dateSlot struct {
	ID  int64      `tango:"pk"`
	Day model.Date `tango:"unique"`
}

func TestDateRoundTripsAndAnUnsetDateIsNULL(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	ctx := context.Background()

	set := dateEvent{Title: "set", Day: model.MustDate(2026, time.March, 1)}
	unset := dateEvent{Title: "unset"}
	for _, row := range []*dateEvent{&set, &unset} {
		if err := store.Create(ctx, meta, row); err != nil {
			t.Fatal(err)
		}
	}
	var gotSet, gotUnset dateEvent
	if err := store.Get(ctx, meta, set.ID, &gotSet); err != nil {
		t.Fatal(err)
	}
	if err := store.Get(ctx, meta, unset.ID, &gotUnset); err != nil {
		t.Fatal(err)
	}
	if gotSet.Day != set.Day {
		t.Errorf("Day = %v, want %v", gotSet.Day, set.Day)
	}
	if !gotUnset.Day.IsZero() {
		t.Errorf("unset Day read back as %v, want the zero date", gotUnset.Day)
	}
	var nulls int
	query := "SELECT CASE WHEN " + quoteFor(dialect, "day") + " IS NULL THEN 1 ELSE 0 END FROM " + quoteFor(dialect, "date_event") + " WHERE " + quoteFor(dialect, "id") + " = " + bindFor(dialect)
	if err := sqlDB.QueryRowContext(ctx, query, unset.ID).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 1 {
		t.Errorf("an unset Date is not stored as NULL")
	}

	updated := gotSet
	updated.Day = model.MustDate(2026, time.April, 2)
	if err := store.Update(ctx, meta, &updated); err != nil {
		t.Fatal(err)
	}
	var again dateEvent
	if err := store.Get(ctx, meta, set.ID, &again); err != nil {
		t.Fatal(err)
	}
	if again.Day != updated.Day {
		t.Errorf("after update Day = %v, want %v", again.Day, updated.Day)
	}
}

func TestDateRangeINAndOrderAcrossMonthAndYearBoundaries(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	ctx := context.Background()

	dates := []struct {
		title string
		day   model.Date
	}{
		{"leap", model.MustDate(2024, time.February, 29)},
		{"march", model.MustDate(2024, time.March, 1)},
		{"last", model.MustDate(2025, time.December, 31)},
		{"new", model.MustDate(2026, time.January, 1)},
	}
	for _, d := range dates {
		row := dateEvent{Title: d.title, Day: d.day}
		if err := store.Create(ctx, meta, &row); err != nil {
			t.Fatal(err)
		}
	}

	var inRange []dateEvent
	query := db.Query{
		Where: []db.Condition{
			{Field: "Day", Op: db.OpGte, Value: model.MustDate(2024, time.February, 29)},
			{Field: "Day", Op: db.OpLt, Value: model.MustDate(2026, time.January, 1)},
		},
		OrderBy: []string{"Day"},
	}
	if err := store.List(ctx, meta, query, &inRange); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, inRange, "leap", "march", "last")

	var listed []dateEvent
	if err := store.List(ctx, meta, db.Query{Filter: ptr(db.In("Day", []model.Date{model.MustDate(2026, time.January, 1), model.MustDate(2024, time.March, 1)}))}, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Errorf("IN matched %d rows, want 2", len(listed))
	}

	var ordered []dateEvent
	if err := store.List(ctx, meta, db.Query{OrderBy: []string{"-Day"}}, &ordered); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, ordered, "new", "last", "march", "leap")
}

func assertTitles(t *testing.T, rows []dateEvent, want ...string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		if row.Title != want[i] {
			t.Errorf("row %d = %q, want %q", i, row.Title, want[i])
		}
	}
}

func TestDateConditionRejectsAValueOfAnotherType(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	for _, value := range []any{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026-01-01"} {
		var rows []dateEvent
		err := store.List(context.Background(), meta, db.Query{Where: []db.Condition{{Field: "Day", Op: db.OpEq, Value: value}}}, &rows)
		if !errors.Is(err, db.ErrInvalidQuery) {
			t.Errorf("a %T value for a Date: err = %v, want ErrInvalidQuery", value, err)
		}
	}
}

func TestDateStoredAsExactTextOnSQLite(t *testing.T) {
	testdb.SQLiteOnly(t, "reads the raw text SQLite stores for a DATE column")
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	ctx := context.Background()
	row := dateEvent{Title: "raw", Day: model.MustDate(2026, time.March, 1)}
	if err := store.Create(ctx, meta, &row); err != nil {
		t.Fatal(err)
	}
	// CAST keeps the stored text: a bare DATE column is converted by the driver
	// to a time.Time on read, which would hide the stored form.
	var text string
	if err := sqlDB.QueryRowContext(ctx, `SELECT CAST("day" AS TEXT) FROM "date_event" WHERE "id" = ?`, row.ID).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "2026-03-01" {
		t.Errorf("stored text = %q, want 2026-03-01", text)
	}
}

func TestStoreQueryScansADateColumn(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	ctx := context.Background()
	row := dateEvent{Title: "raw", Day: model.MustDate(2026, time.March, 1)}
	if err := store.Create(ctx, meta, &row); err != nil {
		t.Fatal(err)
	}
	var rows []dateEvent
	sqlText := "SELECT " + quoteFor(dialect, "id") + ", " + quoteFor(dialect, "title") + ", " + quoteFor(dialect, "day") + " FROM " + quoteFor(dialect, "date_event")
	if err := store.Query(ctx, &rows, sqlText); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Day != row.Day {
		t.Errorf("Store.Query scanned %v, want the stored date %v", rows, row.Day)
	}
}

func TestDateUniqueRejectsADuplicateDateAndAcceptsSeveralUnset(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateSlot{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateSlot{})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := store.Create(ctx, meta, &dateSlot{}); err != nil {
			t.Fatalf("zero Date %d: %v", i, err)
		}
	}
	day := model.MustDate(2026, time.May, 5)
	if err := store.Create(ctx, meta, &dateSlot{Day: day}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, meta, &dateSlot{Day: day}); err == nil {
		t.Error("a duplicate nonzero Date was accepted by a unique constraint")
	}
}

// TestIsNullFindsUnsetDatesOnEveryDialect: the supported way to find unset
// rows is IsNull, and IsNotNull finds set ones, on every dialect.
func TestIsNullFindsUnsetDatesOnEveryDialect(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, dateEvent{})
	store := db.NewStore(sqlDB, dialect)
	meta := metaFor(t, registry, dateEvent{})
	ctx := context.Background()
	for _, row := range []dateEvent{
		{Title: "set", Day: model.MustDate(2026, time.March, 1)},
		{Title: "unset"},
	} {
		row := row
		if err := store.Create(ctx, meta, &row); err != nil {
			t.Fatal(err)
		}
	}
	var unset []dateEvent
	if err := store.List(ctx, meta, db.Query{Filter: ptr(db.IsNull("Day"))}, &unset); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, unset, "unset")
	var set []dateEvent
	if err := store.List(ctx, meta, db.Query{Filter: ptr(db.IsNotNull("Day"))}, &set); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, set, "set")
}

// TestDateColumnIsDeclaredDATEOnEveryDialect reads the type the database
// recorded for the day column from its catalog. A value check cannot see it:
// a column of the wrong type can still hold and return a date.
func TestDateColumnIsDeclaredDATEOnEveryDialect(t *testing.T) {
	sqlDB, dialect, _ := openTables(t, dateEvent{})
	var query string
	switch dialect {
	case db.Postgres:
		query = "SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'date_event' AND column_name = 'day'"
	case db.MySQL:
		query = "SELECT DATA_TYPE FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'date_event' AND column_name = 'day'"
	default:
		query = "SELECT type FROM pragma_table_info('date_event') WHERE name = 'day'"
	}
	var declared string
	if err := sqlDB.QueryRow(query).Scan(&declared); err != nil {
		t.Fatalf("read the declared type: %v", err)
	}
	want := "DATE"
	if dialect == db.Postgres || dialect == db.MySQL {
		want = "date" // information_schema reports the standard name
	}
	if !strings.EqualFold(declared, want) {
		t.Errorf("dialect %d: day is declared %q, want a date column (%s)", dialect, declared, want)
	}
}
