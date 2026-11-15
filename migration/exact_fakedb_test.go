package migration

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func applyOn(t *testing.T, dialect db.Dialect, step Step, answers ...fakeAnswer) ([]string, error) {
	t.Helper()
	testdb.SQLiteOnly(t, "runs against a fake driver, whatever the configured database")
	sqlDB, fake := newFakeDB(t, answers...)
	err := ApplyStep(context.Background(), sqlDB, dialect, step)
	return fake.executed(), err
}

func wantSQL(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statements =\n%q\nwant\n%q", got, want)
	}
}

func TestAlterColumnNotNullSQL(t *testing.T) {
	t.Run("postgres sets and drops", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, AlterColumnNotNull{Table: "items", Column: "label", NotNull: true}, nullCountRow(0))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, `ALTER TABLE "items" ALTER COLUMN "label" SET NOT NULL`)
		got, err = applyOn(t, db.Postgres, AlterColumnNotNull{Table: "items", Column: "label"})
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, `ALTER TABLE "items" ALTER COLUMN "label" DROP NOT NULL`)
	})
	t.Run("postgres refuses NULL rows before any DDL", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, AlterColumnNotNull{Table: "items", Column: "label", NotNull: true}, nullCountRow(3))
		if err == nil || !strings.Contains(err.Error(), "3 rows hold NULL") {
			t.Fatalf("error = %v, want a refusal counting 3 rows", err)
		}
		wantSQL(t, got)
	})
	t.Run("a failing count is reported with its context", func(t *testing.T) {
		_, err := applyOn(t, db.Postgres, AlterColumnNotNull{Table: "items", Column: "label", NotNull: true},
			fakeAnswer{match: "COUNT(*)", err: context.DeadlineExceeded})
		if err == nil || !strings.Contains(err.Error(), "checking the data before NOT NULL") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("a bad typed default is refused", func(t *testing.T) {
		_, err := applyOn(t, db.Postgres, AlterColumnNotNull{Table: "items", Column: "label", DefaultKind: "boolean", DefaultValue: "maybe"})
		if err == nil || !strings.Contains(err.Error(), "must be true or false") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("mysql restates type, default and auto increment", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL,
			AlterColumnNotNull{Table: "items", Column: "id", NotNull: true},
			nullCountRow(0), mysqlColumnRow("int", "NO", nil, "int", "auto_increment"))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `items` MODIFY COLUMN `id` int NOT NULL AUTO_INCREMENT")
	})
	t.Run("mysql carries the typed default through MODIFY", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL,
			AlterColumnNotNull{Table: "items", Column: "language", DefaultKind: "string", DefaultValue: "en"},
			mysqlColumnRow("varchar", "NO", "en", "varchar(5)", ""))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `items` MODIFY COLUMN `language` varchar(5) DEFAULT 'en'")
	})
	t.Run("mysql refuses to drop a default the step does not carry", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL,
			AlterColumnNotNull{Table: "items", Column: "language", NotNull: true},
			nullCountRow(0), mysqlColumnRow("varchar", "YES", "en", "varchar(5)", ""))
		if err == nil || !strings.Contains(err.Error(), "has a default the step does not carry") {
			t.Fatalf("error = %v", err)
		}
		wantSQL(t, got)
	})
	t.Run("mysql reports a missing column", func(t *testing.T) {
		_, err := applyOn(t, db.MySQL, AlterColumnNotNull{Table: "items", Column: "ghost"})
		if err == nil || !strings.Contains(err.Error(), `column "ghost" does not exist`) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("an unknown dialect is refused", func(t *testing.T) {
		_, err := applyOn(t, db.Dialect(99), AlterColumnNotNull{Table: "items", Column: "label"})
		if err == nil {
			t.Fatal("no error for an unknown dialect")
		}
	})
}

func TestAlterColumnDefaultSQL(t *testing.T) {
	set := AlterColumnDefault{Table: "items", Column: "language", DefaultKind: "string", DefaultValue: "it's"}
	drop := AlterColumnDefault{Table: "items", Column: "language"}
	t.Run("postgres", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, set)
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, `ALTER TABLE "items" ALTER COLUMN "language" SET DEFAULT 'it''s'`)
		got, err = applyOn(t, db.Postgres, drop)
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, `ALTER TABLE "items" ALTER COLUMN "language" DROP DEFAULT`)
	})
	t.Run("mysql sets a non-text default in place", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL, set, mysqlColumnRow("varchar", "YES", nil, "varchar(5)", ""))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `items` ALTER COLUMN `language` SET DEFAULT 'it''s'")
	})
	t.Run("mysql takes MODIFY for a text default", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL, AlterColumnDefault{Table: "items", Column: "notes", DefaultKind: "string", DefaultValue: "n"},
			mysqlColumnRow("longtext", "NO", nil, "longtext", ""))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `items` MODIFY COLUMN `notes` longtext NOT NULL DEFAULT ('n')")
	})
	t.Run("mysql drops a default with MODIFY, keeping NOT NULL", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL, drop, mysqlColumnRow("varchar", "NO", "en", "varchar(5)", ""))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `items` MODIFY COLUMN `language` varchar(5) NOT NULL")
	})
	t.Run("a bad default and an unknown dialect are refused", func(t *testing.T) {
		if _, err := applyOn(t, db.Postgres, AlterColumnDefault{Table: "t", Column: "c", DefaultKind: "integer", DefaultValue: "x"}); err == nil {
			t.Fatal("a non-integer integer default was accepted")
		}
		if _, err := applyOn(t, db.Dialect(99), drop); err == nil {
			t.Fatal("unknown dialect accepted")
		}
	})
}

func TestRenameIndexSQL(t *testing.T) {
	step := RenameIndex{Table: "items", Column: "code", Unique: true, From: "idx_items_code", To: "uniq_items_code"}
	got, err := applyOn(t, db.Postgres, step)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, `ALTER INDEX "idx_items_code" RENAME TO "uniq_items_code"`)
	got, err = applyOn(t, db.MySQL, step)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, "ALTER TABLE `items` RENAME INDEX `idx_items_code` TO `uniq_items_code`")
	if _, err := applyOn(t, db.Postgres, RenameIndex{Table: "items", Column: "code", From: "ok", To: "bad name"}); err == nil {
		t.Fatal("an invalid index name was accepted")
	}
	if _, err := applyOn(t, db.Dialect(99), step); err == nil {
		t.Fatal("unknown dialect accepted")
	}
}

func TestAlterColumnReshapeSQL(t *testing.T) {
	widen := AlterColumnType{Table: "counters", Column: "id", From: "integer", To: "integer", FromWidth: 32}
	t.Run("postgres widens the column and its sequence in one transaction", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, widen, fakeAnswer{match: "pg_get_serial_sequence", rows: rowsOf(`public.counters_id_seq`)})
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "BEGIN",
			`ALTER TABLE "counters" ALTER COLUMN "id" TYPE BIGINT USING "id"`,
			`ALTER SEQUENCE public.counters_id_seq AS bigint`,
			"COMMIT")
	})
	t.Run("postgres leaves a column with no sequence alone", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, widen, fakeAnswer{match: "pg_get_serial_sequence", rows: rowsOf(nil)})
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "BEGIN", `ALTER TABLE "counters" ALTER COLUMN "id" TYPE BIGINT USING "id"`, "COMMIT")
	})
	t.Run("postgres gives a timestamp its time zone as UTC", func(t *testing.T) {
		got, err := applyOn(t, db.Postgres, AlterColumnType{Table: "events", Column: "at", From: "timestamp", To: "timestamp", FromNaive: true})
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, `ALTER TABLE "events" ALTER COLUMN "at" TYPE TIMESTAMPTZ USING "at" AT TIME ZONE 'UTC'`)
	})
	t.Run("mysql restates NOT NULL, the default and AUTO_INCREMENT", func(t *testing.T) {
		step := AlterColumnType{Table: "counters", Column: "id", From: "integer", To: "integer", FromWidth: 32, DefaultKind: "integer", DefaultValue: "7"}
		got, err := applyOn(t, db.MySQL, step, mysqlColumnRow("int", "NO", "7", "int", "auto_increment"))
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got, "ALTER TABLE `counters` MODIFY COLUMN `id` BIGINT NOT NULL DEFAULT 7 AUTO_INCREMENT")
	})
	t.Run("mysql has nothing to do for a time zone", func(t *testing.T) {
		got, err := applyOn(t, db.MySQL, AlterColumnType{Table: "events", Column: "at", From: "timestamp", To: "timestamp", FromNaive: true})
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got)
	})
	t.Run("sqlite has nothing to do", func(t *testing.T) {
		got, err := applyOn(t, db.SQLite, widen)
		if err != nil {
			t.Fatal(err)
		}
		wantSQL(t, got)
	})
	t.Run("a lossy direction is refused on every dialect", func(t *testing.T) {
		for name, step := range map[string]AlterColumnType{
			"int8 to int4": {Table: "t", Column: "c", From: "integer", To: "integer", ToWidth: 32},
			"tz to naive":  {Table: "t", Column: "c", From: "timestamp", To: "timestamp", ToNaive: true},
		} {
			if _, err := applyOn(t, db.Postgres, step); err == nil || !strings.Contains(err.Error(), "not a widening type change") {
				t.Errorf("%s: error = %v", name, err)
			}
		}
	})
	t.Run("an unknown dialect is refused", func(t *testing.T) {
		if _, err := applyOn(t, db.Dialect(99), widen); err == nil {
			t.Fatal("unknown dialect accepted")
		}
	})
}

func TestAlterColumnTypeCarriesATypedDefault(t *testing.T) {
	step := AlterColumnType{Table: "langs", Column: "code", From: "varchar", FromLength: 5, To: "text", DefaultKind: "string", DefaultValue: "en"}
	got, err := applyOn(t, db.Postgres, step)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, `ALTER TABLE "langs" ALTER COLUMN "code" DROP DEFAULT, ALTER COLUMN "code" TYPE TEXT USING "code", ALTER COLUMN "code" SET DEFAULT 'en'`)
	got, err = applyOn(t, db.MySQL, step, mysqlColumnRow("varchar", "NO", "en", "varchar(5)", ""),
		fakeAnswer{match: "information_schema.statistics", rows: rowsOf(int64(0))})
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, "ALTER TABLE `langs` MODIFY COLUMN `code` LONGTEXT NOT NULL DEFAULT ('en')")
}
