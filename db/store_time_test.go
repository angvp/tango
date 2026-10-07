package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

type timeEvent struct {
	ID       int64 `tango:"pk"`
	Name     string
	StartsAt time.Time
}

// useLocalZone sets the process's local time zone for the rest of the test,
// so a database driver that reads times back in the local zone (pgx does,
// for TIMESTAMPTZ) is caught even when the machine itself runs in UTC.
func useLocalZone(t *testing.T, zone *time.Location) {
	t.Helper()
	previous := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = previous })
}

// TestStoreReadsTimesBackInUTC pins down that every Store read path returns
// a time.Time in UTC on every dialect, whatever the process's local zone:
// the same stored instant must come back identical (and so serialise
// identically, e.g. to JSON) through Get, List, QueryRow and Query.
func TestStoreReadsTimesBackInUTC(t *testing.T) {
	useLocalZone(t, time.FixedZone("UTC-6", -6*60*60))
	sqlDB, dialect, registry := openTables(t, timeEvent{})
	meta := metaFor(t, registry, timeEvent{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	startsAt := time.Date(2026, 10, 6, 18, 30, 0, 0, time.UTC)
	event := timeEvent{Name: "launch", StartsAt: startsAt}
	if err := store.Create(ctx, meta, &event); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var got timeEvent
	if err := store.Get(ctx, meta, event.ID, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	var listed []timeEvent
	if err := store.List(ctx, meta, db.Query{}, &listed); err != nil || len(listed) != 1 {
		t.Fatalf("List = %d rows, %v; want 1 row", len(listed), err)
	}
	var row timeEvent
	if err := store.QueryRow(ctx, &row, bind(dialect, "SELECT id, name, starts_at FROM time_event WHERE id = ?"), event.ID); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	var rows []timeEvent
	if err := store.Query(ctx, &rows, "SELECT id, name, starts_at FROM time_event"); err != nil || len(rows) != 1 {
		t.Fatalf("Query = %d rows, %v; want 1 row", len(rows), err)
	}

	var pointerRow struct{ StartsAt *time.Time }
	if err := store.QueryRow(ctx, &pointerRow, "SELECT starts_at FROM time_event"); err != nil || pointerRow.StartsAt == nil {
		t.Fatalf("QueryRow into *time.Time = %v, %v; want a time", pointerRow.StartsAt, err)
	}

	for path, value := range map[string]time.Time{
		"Get":                      got.StartsAt,
		"List":                     listed[0].StartsAt,
		"QueryRow":                 row.StartsAt,
		"Query":                    rows[0].StartsAt,
		"QueryRow into *time.Time": *pointerRow.StartsAt,
	} {
		if value != startsAt {
			t.Errorf("%s StartsAt = %v (%s), want %v in UTC", path, value, value.Location(), startsAt)
		}
	}
}
