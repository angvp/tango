package migration

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func TestRenameCompositeIndexKeepsColumnsAndUniqueness(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "cr_basic")
	applySteps(t, sqlDB, dialect,
		CreateCompositeIndex{Table: "cr_basic", Columns: []string{"b", "a"}, Unique: true},
		RenameCompositeIndex{Table: "cr_basic", Columns: []string{"b", "a"}, Unique: true, From: "uniq_cr_basic_b_a", To: "ix_cr_pair"},
	)
	want := []liveIndex{{Name: "ix_cr_pair", Columns: []string{"b", "a"}, Unique: true}}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "cr_basic"); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes = %#v, want %#v", got, want)
	}
	mustExec(t, sqlDB, "INSERT INTO cr_basic (a, b, label) VALUES (1, 1, 'x')")
	if _, err := sqlDB.Exec("INSERT INTO cr_basic (a, b, label) VALUES (1, 1, 'y')"); err == nil {
		t.Fatal("the renamed index no longer enforces uniqueness")
	}
}

func TestRenameCompositeIndexRefusesABadName(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	err := ApplyStep(context.Background(), sqlDB, dialect, RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "x", To: "bad-name"})
	if err == nil || !strings.Contains(err.Error(), "bad-name") {
		t.Fatalf("error = %v, want one naming bad-name", err)
	}
	err = ApplyStep(context.Background(), sqlDB, dialect, RenameCompositeIndex{Table: "t", Columns: []string{"a"}, From: "x", To: "y"})
	if err == nil {
		t.Fatal("a one-column rename was accepted")
	}
}

func TestRenameTableLeavesCompositeIndexNamesAndRenamesSingleOnes(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applySteps(t, sqlDB, dialect, CreateTable{Table: "cr_old", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "a", Type: "integer", Indexed: true},
		{Name: "b", Type: "integer"},
		{Name: "a_b", Type: "integer"},
	}},
		CreateCompositeIndex{Table: "cr_old", Columns: []string{"a", "b"}},
		RenameTable{From: "cr_old", To: "cr_new"},
	)
	want := []liveIndex{{Name: "idx_cr_old_a_b", Columns: []string{"a", "b"}}}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "cr_new"); !reflect.DeepEqual(got, want) {
		t.Fatalf("composite after the table rename = %#v, want %#v: its name is renamed by its own step", got, want)
	}
	if got := indexNames(t, sqlDB, dialect, "cr_new"); !reflect.DeepEqual(got, []string{"idx_cr_new_a", "idx_cr_old_a_b"}) {
		t.Fatalf("index names = %v, want the single-column one renamed and the composite not", got)
	}
	applySteps(t, sqlDB, dialect, RenameCompositeIndex{Table: "cr_new", Columns: []string{"a", "b"}, From: "idx_cr_old_a_b", To: "idx_cr_new_a_b"})
	if got := indexNames(t, sqlDB, dialect, "cr_new"); !reflect.DeepEqual(got, []string{"idx_cr_new_a", "idx_cr_new_a_b"}) {
		t.Fatalf("index names after the rename step = %v", got)
	}
}

func TestRenameColumnKeepsTheCompositeIndexUsable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "cr_col")
	applySteps(t, sqlDB, dialect,
		CreateCompositeIndex{Table: "cr_col", Columns: []string{"a", "b"}, Unique: true},
		RenameColumn{Table: "cr_col", From: "a", To: "x"},
	)
	want := []liveIndex{{Name: "uniq_cr_col_a_b", Columns: []string{"x", "b"}, Unique: true}}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "cr_col"); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes = %#v, want %#v", got, want)
	}
	applySteps(t, sqlDB, dialect, RenameCompositeIndex{Table: "cr_col", Columns: []string{"x", "b"}, Unique: true, From: "uniq_cr_col_a_b", To: "uniq_cr_col_x_b"})
	if got := indexNames(t, sqlDB, dialect, "cr_col"); !reflect.DeepEqual(got, []string{"uniq_cr_col_x_b"}) {
		t.Fatalf("index names = %v", got)
	}
}

func TestDropColumnRefusesWhileACompositeIndexHoldsIt(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "cr_drop")
	applySteps(t, sqlDB, dialect, CreateCompositeIndex{Table: "cr_drop", Columns: []string{"a", "b"}})
	err := ApplyStep(context.Background(), sqlDB, dialect, DropColumn{Table: "cr_drop", Column: "b"})
	if err == nil || !strings.Contains(err.Error(), `"idx_cr_drop_a_b"`) || !strings.Contains(err.Error(), "drop the index first") {
		t.Fatalf("error = %v, want a refusal naming idx_cr_drop_a_b", err)
	}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "cr_drop"); len(got) != 1 {
		t.Fatalf("the refused step changed the index: %#v", got)
	}
	applySteps(t, sqlDB, dialect,
		DropCompositeIndex{Table: "cr_drop", Columns: []string{"a", "b"}},
		DropColumn{Table: "cr_drop", Column: "b"},
	)
	// A column no composite index holds still drops, and so does one that a
	// single-column index holds, as before.
	applySteps(t, sqlDB, dialect, DropColumn{Table: "cr_drop", Column: "label"})
}

func TestCompositeRenameAndDropGuardsSQLOnPostgresAndMySQL(t *testing.T) {
	rename := RenameCompositeIndex{Table: "t", Columns: []string{"a", "b"}, From: "idx_t_a_b", To: "ix_pair"}
	got, err := applyOn(t, db.Postgres, rename)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, `ALTER INDEX "idx_t_a_b" RENAME TO "ix_pair"`)
	got, err = applyOn(t, db.MySQL, rename)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, "ALTER TABLE `t` RENAME INDEX `idx_t_a_b` TO `ix_pair`")

	drop := DropColumn{Table: "t", Column: "b"}
	held := fakeAnswer{match: "pg_index", rows: rowsOf("idx_t_a_b")}
	got, err = applyOn(t, db.Postgres, drop, held)
	if err == nil || !strings.Contains(err.Error(), `index "idx_t_a_b" still contains it`) {
		t.Fatalf("postgres error = %v, want a refusal", err)
	}
	for _, s := range got {
		if strings.HasPrefix(s, "ALTER TABLE") {
			t.Fatalf("a refused drop ran %q", s)
		}
	}
	got, err = applyOn(t, db.MySQL, drop, fakeAnswer{match: "information_schema.statistics", rows: rowsOf("idx_t_a_b")})
	if err == nil || !strings.Contains(err.Error(), `index "idx_t_a_b" still contains it`) {
		t.Fatalf("mysql error = %v, want a refusal", err)
	}
	for _, s := range got {
		if strings.HasPrefix(s, "ALTER TABLE") {
			t.Fatalf("a refused drop ran %q", s)
		}
	}
	got, err = applyOn(t, db.Postgres, drop, fakeAnswer{match: "pg_index", err: context.DeadlineExceeded})
	if err == nil {
		t.Fatalf("a failing catalog read did not fail the step (ran %q)", got)
	}
	got, err = applyOn(t, db.Postgres, drop)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL(t, got, `ALTER TABLE "t" DROP COLUMN "b"`)
}

func TestRenameTableOnlyRenamesSingleColumnIndexesOnPostgresAndMySQL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns int64
		renamed bool
	}{{"one column", 1, true}, {"two columns", 2, false}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyOn(t, db.Postgres, RenameTable{From: "old", To: "new"},
				fakeAnswer{match: "pg_indexes", rows: [][]driver.Value{{"idx_old_a_b", ""}}},
				fakeAnswer{match: "indnatts", rows: rowsOf(tc.columns)})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"BEGIN", `ALTER TABLE "old" RENAME TO "new"`, "COMMIT"}
			if tc.renamed {
				want = []string{"BEGIN", `ALTER TABLE "old" RENAME TO "new"`, `ALTER INDEX IF EXISTS "idx_old_a_b" RENAME TO "idx_new_a_b"`, "COMMIT"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("statements = %q, want %q", got, want)
			}
		})
	}
	got, err := applyOn(t, db.MySQL, RenameTable{From: "old", To: "new"},
		fakeAnswer{match: "DISTINCT index_name", rows: [][]driver.Value{{"idx_old_a_b", ""}}},
		fakeAnswer{match: "COUNT(*)", rows: rowsOf(int64(2))})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if strings.Contains(s, "RENAME INDEX") {
			t.Fatalf("a multi-column index was renamed on MySQL: %q", s)
		}
	}
}
