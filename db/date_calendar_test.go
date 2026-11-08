package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// calendarZones are process zones on both sides of UTC. The Date stored must
// keep its calendar day in each of them.
var calendarZones = []*time.Location{
	time.FixedZone("UTC-11", -11*3600),
	time.UTC,
	time.FixedZone("UTC+13", 13*3600),
}

// storedDayText reads the stored day column with the dialect's own formatting,
// not through Date.Scan, so a wrong Scan cannot cancel a wrong store.
func storedDayText(dialect db.Dialect) string {
	switch dialect {
	case db.Postgres:
		return "to_char(" + quoteFor(dialect, "day") + ", 'YYYY-MM-DD')"
	case db.MySQL:
		return "DATE_FORMAT(" + quoteFor(dialect, "day") + ", '%Y-%m-%d')"
	}
	return "CAST(" + quoteFor(dialect, "day") + " AS TEXT)"
}

// TestDateCalendarComponentsAreStoredAndReadBackInEveryZone writes a Date
// taken from times around each boundary, in each zone, and checks the stored
// text and the returned value against literal calendar dates.
func TestDateCalendarComponentsAreStoredAndReadBackInEveryZone(t *testing.T) {
	boundaries := []struct {
		name string
		at   func(zone *time.Location) time.Time
		want string
	}{
		{"month end late", func(z *time.Location) time.Time { return time.Date(2026, 1, 31, 23, 30, 0, 0, z) }, "2026-01-31"},
		{"month start early", func(z *time.Location) time.Time { return time.Date(2026, 2, 1, 0, 30, 0, 0, z) }, "2026-02-01"},
		{"year end late", func(z *time.Location) time.Time { return time.Date(2025, 12, 31, 23, 30, 0, 0, z) }, "2025-12-31"},
		{"leap day late", func(z *time.Location) time.Time { return time.Date(2024, 2, 29, 23, 30, 0, 0, z) }, "2024-02-29"},
		{"march first early", func(z *time.Location) time.Time { return time.Date(2026, 3, 1, 0, 30, 0, 0, z) }, "2026-03-01"},
	}
	for _, zone := range calendarZones {
		for _, boundary := range boundaries {
			t.Run(zone.String()+"/"+boundary.name, func(t *testing.T) {
				useLocalZone(t, zone)
				sqlDB, dialect, registry := openTables(t, dateEvent{})
				store := db.NewStore(sqlDB, dialect)
				meta := metaFor(t, registry, dateEvent{})
				ctx := context.Background()

				moment := boundary.at(zone)
				row := dateEvent{Title: "boundary", Day: model.DateOf(moment)}
				if err := store.Create(ctx, meta, &row); err != nil {
					t.Fatal(err)
				}

				var stored string
				query := "SELECT " + storedDayText(dialect) + " FROM " + quoteFor(dialect, "date_event") + " WHERE " + quoteFor(dialect, "id") + " = " + bindFor(dialect)
				if err := sqlDB.QueryRowContext(ctx, query, row.ID).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored != boundary.want {
					t.Errorf("stored day = %q, want %q (the time %v shows that date)", stored, boundary.want, moment)
				}

				var got dateEvent
				if err := store.Get(ctx, meta, row.ID, &got); err != nil {
					t.Fatal(err)
				}
				if got.Day != model.DateOf(moment) || got.Day.String() != boundary.want {
					t.Errorf("read back %v, want %s", got.Day, boundary.want)
				}
			})
		}
	}
}

// TestRawInsertedDatesReadThroughTheORMInEveryZone is the reverse direction:
// the literal is written with SQL and must read back with the same calendar
// components in every zone.
func TestRawInsertedDatesReadThroughTheORMInEveryZone(t *testing.T) {
	for _, zone := range calendarZones {
		for _, literal := range []string{"2026-03-01", "2024-02-29"} {
			t.Run(zone.String()+"/"+literal, func(t *testing.T) {
				useLocalZone(t, zone)
				sqlDB, dialect, registry := openTables(t, dateEvent{})
				store := db.NewStore(sqlDB, dialect)
				meta := metaFor(t, registry, dateEvent{})
				ctx := context.Background()

				insert := "INSERT INTO " + quoteFor(dialect, "date_event") + " (" + quoteFor(dialect, "id") + ", " + quoteFor(dialect, "title") + ", " + quoteFor(dialect, "day") + ") VALUES (1, 'raw', " + bindFor(dialect) + ")"
				if _, err := sqlDB.ExecContext(ctx, insert, literal); err != nil {
					t.Fatal(err)
				}
				var got dateEvent
				if err := store.Get(ctx, meta, int64(1), &got); err != nil {
					t.Fatal(err)
				}
				if got.Day.String() != literal {
					t.Errorf("read back %s, want %s", got.Day, literal)
				}
			})
		}
	}
}
