package migration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func applyExactSteps(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, steps ...Step) {
	t.Helper()
	for _, step := range steps {
		if err := ApplyStep(context.Background(), sqlDB, dialect, step); err != nil {
			t.Fatalf("ApplyStep(%#v): %v", step, err)
		}
	}
}

func execExact(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func execFails(sqlDB *sql.DB, query string) bool {
	_, err := sqlDB.ExecContext(context.Background(), query)
	return err != nil
}

// alterTable is a table whose columns each carry the attribute an ALTER step
// must keep: an int4 auto key, a defaulted varchar, a text default and a
// unique named index.
func alterTable() CreateTable {
	return CreateTable{Table: "items", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "label", Type: "varchar", Length: 20},
		{Name: "language", Type: "varchar", Length: 5, DefaultKind: "string", DefaultValue: "en"},
		{Name: "notes", Type: "text", DefaultKind: "string", DefaultValue: "n"},
		{Name: "code", Type: "varchar", Length: 20, Unique: true, UniqueName: "idx_items_code"},
	}}
}

func TestAlterColumnNotNullRefusesNullsThenApplies(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")

	set := AlterColumnNotNull{Table: "items", Column: "label", NotNull: true}
	err := ApplyStep(context.Background(), sqlDB, dialect, set)
	if err == nil || !strings.Contains(err.Error(), "1 row holds NULL") {
		t.Fatalf("ApplyStep with a NULL row = %v, want a refusal naming the NULL row", err)
	}
	if execFails(sqlDB, "INSERT INTO items (code) VALUES ('b')") {
		t.Fatal("the refused step changed the column")
	}

	execExact(t, sqlDB, "UPDATE items SET label = 'x'")
	applyExactSteps(t, sqlDB, dialect, set)
	if !execFails(sqlDB, "INSERT INTO items (code) VALUES ('c')") {
		t.Fatal("NOT NULL not enforced after the step")
	}
	applyExactSteps(t, sqlDB, dialect, AlterColumnNotNull{Table: "items", Column: "label"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('d')")
}

// TestAlterColumnNotNullKeepsWhatMySQLWouldDrop checks the rewrite of one
// column does not lose the key's generated values, a default or an index.
func TestAlterColumnNotNullKeepsWhatMySQLWouldDrop(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	applyExactSteps(t, sqlDB, dialect,
		AlterColumnNotNull{Table: "items", Column: "language", NotNull: true, DefaultKind: "string", DefaultValue: "en"},
		AlterColumnNotNull{Table: "items", Column: "notes", NotNull: true, DefaultKind: "string", DefaultValue: "n"},
		AlterColumnNotNull{Table: "items", Column: "id", NotNull: true},
	)
	execExact(t, sqlDB, "INSERT INTO items (code, label) VALUES ('a', 'x')")
	execExact(t, sqlDB, "INSERT INTO items (code, label) VALUES ('b', 'x')")
	var language, notes string
	var ids int
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT MAX(id), MIN(language), MIN(notes) FROM items").Scan(&ids, &language, &notes); err != nil {
		t.Fatalf("select: %v", err)
	}
	if ids != 2 || language != "en" || notes != "n" {
		t.Fatalf("after the rewrites: max id %d, language %q, notes %q; want 2, en, n", ids, language, notes)
	}
	if !execFails(sqlDB, "INSERT INTO items (code) VALUES ('a')") {
		t.Fatal("unique index idx_items_code lost")
	}
}

func TestAlterColumnDefaultSetChangeDrop(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	read := func() sql.NullString {
		var v sql.NullString
		if err := sqlDB.QueryRowContext(context.Background(), "SELECT label FROM items ORDER BY id DESC LIMIT 1").Scan(&v); err != nil {
			t.Fatalf("select: %v", err)
		}
		return v
	}
	steps := []struct {
		step AlterColumnDefault
		want sql.NullString
	}{
		{AlterColumnDefault{Table: "items", Column: "label", DefaultKind: "string", DefaultValue: "it's"}, sql.NullString{String: "it's", Valid: true}},
		{AlterColumnDefault{Table: "items", Column: "label", DefaultKind: "string", DefaultValue: "ñandú"}, sql.NullString{String: "ñandú", Valid: true}},
		{AlterColumnDefault{Table: "items", Column: "label"}, sql.NullString{}},
	}
	for i, tc := range steps {
		applyExactSteps(t, sqlDB, dialect, tc.step)
		execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('"+string(rune('a'+i))+"')")
		if got := read(); got != tc.want {
			t.Fatalf("step %d: label default = %+v, want %+v", i, got, tc.want)
		}
	}
}

func TestAlterColumnDefaultOnTextColumn(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	applyExactSteps(t, sqlDB, dialect,
		AlterColumnDefault{Table: "items", Column: "notes", DefaultKind: "string", DefaultValue: "ñandú ✓"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")
	var notes sql.NullString
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT notes FROM items").Scan(&notes); err != nil {
		t.Fatalf("select: %v", err)
	}
	if notes.String != "ñandú ✓" {
		t.Fatalf("notes = %+v, want the new default", notes)
	}
	applyExactSteps(t, sqlDB, dialect, AlterColumnDefault{Table: "items", Column: "notes"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('b')")
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT notes FROM items WHERE code = 'b'").Scan(&notes); err != nil || notes.Valid {
		t.Fatalf("notes after DROP DEFAULT = %+v, %v; want NULL", notes, err)
	}
}

func TestAlterColumnDefaultKeepsNotNullAndIncrement(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	table := alterTable()
	table.Columns[3] = Column{Name: "notes", Type: "text", NotNull: true, DefaultKind: "string", DefaultValue: "n"}
	applyExactSteps(t, sqlDB, dialect, table)
	applyExactSteps(t, sqlDB, dialect, AlterColumnDefault{Table: "items", Column: "notes", DefaultKind: "string", DefaultValue: "m"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")
	if !execFails(sqlDB, "INSERT INTO items (code, notes) VALUES ('b', NULL)") {
		t.Fatal("NOT NULL lost when the default changed")
	}
}

func TestRenameIndexKeepsUniqueness(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	applyExactSteps(t, sqlDB, dialect, RenameIndex{Table: "items", Column: "code", Unique: true, From: "idx_items_code", To: "uq_items_code"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")
	if !execFails(sqlDB, "INSERT INTO items (code) VALUES ('a')") {
		t.Fatal("renamed index no longer unique")
	}
	// The old name is gone and the new one is the index to drop.
	if err := ApplyStep(context.Background(), sqlDB, dialect, DropIndex{Table: "items", Column: "code", Name: "idx_items_code"}); err == nil {
		t.Fatal("old index name still exists")
	}
	applyExactSteps(t, sqlDB, dialect, DropIndex{Table: "items", Column: "code", Name: "uq_items_code"})
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")
}

func TestRenameIndexOfAMissingIndexFails(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	err := ApplyStep(context.Background(), sqlDB, dialect, RenameIndex{Table: "items", Column: "code", Unique: true, From: "nope", To: "other"})
	if err == nil {
		t.Fatal("renaming a missing index succeeded, want an error")
	}
}

func TestIndexStepsHonourExplicitNames(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	applyExactSteps(t, sqlDB, dialect,
		CreateIndex{Table: "items", Column: "label", Name: "ix_items_label"},
		AlterColumnUnique{Table: "items", Column: "label", Unique: true, Name: "uq_items_label"},
	)
	execExact(t, sqlDB, "INSERT INTO items (code, label) VALUES ('a', 'x')")
	if !execFails(sqlDB, "INSERT INTO items (code, label) VALUES ('b', 'x')") {
		t.Fatal("explicitly named unique index not enforced")
	}
	applyExactSteps(t, sqlDB, dialect,
		AlterColumnUnique{Table: "items", Column: "label", Name: "uq_items_label"},
		DropIndex{Table: "items", Column: "label", Name: "ix_items_label"},
	)
	execExact(t, sqlDB, "INSERT INTO items (code, label) VALUES ('b', 'x')")
}

func TestIndexStepRefusesABadName(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	err := ApplyStep(context.Background(), sqlDB, dialect, CreateIndex{Table: "items", Column: "label", Name: "bad name;"})
	if err == nil || !strings.Contains(err.Error(), "index name") {
		t.Fatalf("ApplyStep = %v, want an index-name refusal", err)
	}
}

// TestApplyPendingChecksNotNullBeforeAnyDDL checks the whole migration is
// refused, with the schema untouched, when a later step cannot hold.
func TestApplyPendingChecksNotNullBeforeAnyDDL(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, alterTable())
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('a')")
	err := ApplyPending(context.Background(), sqlDB, dialect, []Migration{{
		App: "x", Name: "0001_notnull", Reversible: true,
		Up: []Step{
			AlterColumnDefault{Table: "items", Column: "label", DefaultKind: "string", DefaultValue: "d"},
			AlterColumnNotNull{Table: "items", Column: "label", NotNull: true},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "NULL") {
		t.Fatalf("ApplyPending = %v, want a NULL refusal", err)
	}
	execExact(t, sqlDB, "INSERT INTO items (code) VALUES ('b')")
	var label sql.NullString
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT label FROM items WHERE code = 'b'").Scan(&label); err != nil || label.Valid {
		t.Fatalf("label = %+v, %v; the default step ran although the migration was refused", label, err)
	}
}
