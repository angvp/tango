package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func TestAlterColumnTypeWidensInt4KeepingKeyDefaultAndSequence(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, CreateTable{Table: "counters", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "qty", Type: "integer", Width: 32, NotNull: true, DefaultKind: "integer", DefaultValue: "7"},
	}})
	execExact(t, sqlDB, "INSERT INTO counters (qty) VALUES (1)")

	applyExactSteps(t, sqlDB, dialect,
		AlterColumnType{Table: "counters", Column: "id", From: "integer", To: "integer", FromWidth: 32},
		AlterColumnType{Table: "counters", Column: "qty", From: "integer", To: "integer", FromWidth: 32,
			DefaultKind: "integer", DefaultValue: "7"},
	)

	// Past the int4 range: only a widened column and sequence accept these.
	execExact(t, sqlDB, "INSERT INTO counters (id, qty) VALUES (3000000000, 3000000000)")
	if dialect != db.SQLite {
		var maxValue int64
		if dialect == db.Postgres {
			if err := sqlDB.QueryRowContext(context.Background(), "SELECT seqmax FROM pg_sequence WHERE seqrelid = pg_get_serial_sequence('counters', 'id')::regclass").Scan(&maxValue); err != nil {
				t.Fatalf("sequence: %v", err)
			}
			if maxValue <= 2147483647 {
				t.Fatalf("sequence max = %d, want a bigint sequence", maxValue)
			}
		}
	}
	execExact(t, sqlDB, "INSERT INTO counters (id) VALUES (3000000001)")
	var qty int64
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT qty FROM counters WHERE id = 3000000001").Scan(&qty); err != nil {
		t.Fatalf("select: %v", err)
	}
	if qty != 7 {
		t.Fatalf("qty default = %d after the widening, want 7", qty)
	}
	if dialect != db.SQLite {
		// The key still generates values: a MySQL MODIFY must restate
		// AUTO_INCREMENT, and a PostgreSQL sequence must survive the type change.
		execExact(t, sqlDB, "INSERT INTO counters (qty) VALUES (2)")
	}
	if execFails(sqlDB, "INSERT INTO counters (id, qty) VALUES (3000000001, NULL)") {
		return
	}
	t.Fatal("NOT NULL or the primary key was lost")
}

func TestAlterColumnTypeNaiveToTimeZoneReadsValuesAsUTC(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, CreateTable{Table: "events", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "at", Type: "timestamp", Naive: true, NotNull: true},
	}})
	execExact(t, sqlDB, "INSERT INTO events (id, at) VALUES (1, '2026-03-01 10:30:00')")
	applyExactSteps(t, sqlDB, dialect,
		AlterColumnType{Table: "events", Column: "at", From: "timestamp", To: "timestamp", FromNaive: true})
	if dialect == db.Postgres {
		var got string
		if err := sqlDB.QueryRowContext(context.Background(),
			"SELECT to_char(at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS') FROM events WHERE id = 1").Scan(&got); err != nil {
			t.Fatalf("select: %v", err)
		}
		if got != "2026-03-01 10:30:00" {
			t.Fatalf("value read as UTC = %q, want 2026-03-01 10:30:00", got)
		}
		var typ string
		if err := sqlDB.QueryRowContext(context.Background(),
			"SELECT data_type FROM information_schema.columns WHERE table_name = 'events' AND column_name = 'at'").Scan(&typ); err != nil {
			t.Fatalf("type: %v", err)
		}
		if typ != "timestamp with time zone" {
			t.Fatalf("type = %q, want timestamp with time zone", typ)
		}
	}
	if !execFails(sqlDB, "INSERT INTO events (id, at) VALUES (2, NULL)") {
		t.Fatal("NOT NULL lost")
	}
}

func TestAlterColumnTypeRefusesLossyReshapes(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, CreateTable{Table: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}})
	for _, s := range []AlterColumnType{
		{Table: "t", Column: "id", From: "integer", To: "integer", ToWidth: 32},
		{Table: "t", Column: "id", From: "timestamp", To: "timestamp", ToNaive: true},
	} {
		err := ApplyStep(context.Background(), sqlDB, dialect, s)
		if err == nil || !strings.Contains(err.Error(), "not a widening type change") {
			t.Fatalf("ApplyStep(%+v) = %v, want a widening refusal", s, err)
		}
	}
}
