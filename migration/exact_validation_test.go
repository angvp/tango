package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

func TestValidateExactRefusals(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation")
	cases := []struct {
		name string
		col  Column
		want string
	}{
		{"width on text", Column{Name: "c", Type: "text", Width: 32}, "width applies to integer"},
		{"odd width", Column{Name: "c", Type: "integer", Width: 16}, "width must be 0 or 32"},
		{"naive on integer", Column{Name: "c", Type: "integer", Naive: true}, "naive applies to timestamp"},
		{"notnull on pk", Column{Name: "c", Type: "integer", PrimaryKey: true, NotNull: true}, "redundant on a primary key"},
		{"legacy default with notnull", Column{Name: "c", Type: "integer", Default: "0", NotNull: true}, "legacy Default"},
		{"unique name without unique", Column{Name: "c", Type: "text", UniqueName: "u_c"}, "UniqueName needs Unique"},
		{"index name without index", Column{Name: "c", Type: "text", IndexName: "i_c"}, "IndexName needs Indexed"},
		{"bad index name", Column{Name: "c", Type: "text", Unique: true, UniqueName: "1bad"}, "index name"},
		{"value without kind", Column{Name: "c", Type: "text", DefaultValue: "x"}, "DefaultValue needs a DefaultKind"},
		{"unknown kind", Column{Name: "c", Type: "text", DefaultKind: "float", DefaultValue: "1"}, "unknown default kind"},
		{"kind does not fit", Column{Name: "c", Type: "integer", DefaultKind: "string", DefaultValue: "x"}, "does not fit"},
		{"default on pk", Column{Name: "c", Type: "integer", PrimaryKey: true, DefaultKind: "integer", DefaultValue: "1"}, "primary key cannot carry a default"},
		{"backslash", Column{Name: "c", Type: "text", DefaultKind: "string", DefaultValue: `a\b`}, "backslash or NUL"},
		{"longer than varchar", Column{Name: "c", Type: "varchar", Length: 2, DefaultKind: "string", DefaultValue: "abc"}, "longer than varchar(2)"},
		{"bad boolean", Column{Name: "c", Type: "boolean", DefaultKind: "boolean", DefaultValue: "yes"}, "true or false"},
		{"bad integer", Column{Name: "c", Type: "integer", DefaultKind: "integer", DefaultValue: "1.5"}, "base-10 integer"},
		{"int4 out of range", Column{Name: "c", Type: "integer", Width: 32, DefaultKind: "integer", DefaultValue: "3000000000"}, "out of range for a 4-byte"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExact("t", tc.col)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateExact = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateTypedDefaultOnSteps(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation")
	cases := []struct {
		kind, value, want string
	}{
		{"", "x", "DefaultValue needs a DefaultKind"},
		{"string", "a\x00", "backslash or NUL"},
		{"boolean", "maybe", "true or false"},
		{"integer", "x", "base-10 integer"},
		{"float", "1", "unknown default kind"},
	}
	for _, tc := range cases {
		err := validateTypedDefault("t", "c", tc.kind, tc.value)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateTypedDefault(%q, %q) = %v, want %q", tc.kind, tc.value, err, tc.want)
		}
	}
	if err := validateTypedDefault("t", "c", "integer", "-4"); err != nil {
		t.Errorf("a valid integer default was refused: %v", err)
	}
}

func TestIndexStepsRefuseABadNameOnEveryKind(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, CreateTable{Table: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "c", Type: "varchar", Length: 10}}})
	for _, step := range []Step{
		AlterColumnUnique{Table: "t", Column: "c", Unique: true, Name: "bad name"},
		CreateIndex{Table: "t", Column: "c", Name: "bad name"},
		DropIndex{Table: "t", Column: "c", Name: "bad name"},
	} {
		if err := ApplyStep(context.Background(), sqlDB, dialect, step); err == nil || !strings.Contains(err.Error(), "index name") {
			t.Errorf("ApplyStep(%#v) = %v, want an index name refusal", step, err)
		}
	}
}

func TestReplayRefusesNewStepsOnMissingTablesAndColumns(t *testing.T) {
	testdb.SQLiteOnly(t, "pure replay")
	base := CreateTable{Table: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}
	for _, step := range []Step{
		AlterColumnNotNull{Table: "ghost", Column: "c", NotNull: true},
		AlterColumnNotNull{Table: "t", Column: "ghost", NotNull: true},
		AlterColumnDefault{Table: "ghost", Column: "c"},
		AlterColumnDefault{Table: "t", Column: "ghost"},
		RenameIndex{Table: "ghost", Column: "c", From: "a", To: "b"},
		RenameIndex{Table: "t", Column: "ghost", From: "a", To: "b"},
	} {
		_, err := Replay([]Migration{{App: "a", Name: "0001", Up: []Step{base, step}}})
		if err == nil {
			t.Errorf("Replay accepted %#v on a missing table or column", step)
		}
	}
}

func TestValidateModelsAllowsDerivedNamesToCollide(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation")
	// No name here is explicit, so a repeated one is not this check's concern.
	err := ValidateModels([]Model{{App: "a", Name: "tango_migrations", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}})
	if err != nil {
		t.Fatalf("ValidateModels = %v, want nil for names the model did not choose", err)
	}
}

func TestApplyPendingSkipsTheNotNullCheckForWhatTheMigrationCreates(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	m := Migration{App: "a", Name: "0001_notnull", Reversible: true, Up: []Step{
		CreateTable{Table: "made", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "label", Type: "varchar", Length: 10}}},
		// The column and table exist only after the steps above, so there is
		// nothing to count yet; dropping NOT NULL has nothing to check either.
		AlterColumnNotNull{Table: "made", Column: "label", NotNull: true},
		AlterColumnNotNull{Table: "made", Column: "label", NotNull: false},
	}}
	if err := ApplyPending(context.Background(), sqlDB, dialect, []Migration{m}); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
}
