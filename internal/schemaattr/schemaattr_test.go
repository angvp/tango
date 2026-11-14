package schemaattr

import (
	"testing"

	"github.com/angvp/tango/migration"
)

func TestHandshakeArgWireValue(t *testing.T) {
	if HandshakeArg != "tango-exact-schema-v1" {
		t.Fatalf("HandshakeArg = %q, want the fixed wire value", HandshakeArg)
	}
}

func TestColumnUses(t *testing.T) {
	legacy := migration.Column{Name: "a", Type: "varchar", Length: 5, PrimaryKey: true, Unique: true, Indexed: true, References: "x", Default: "TRUE"}
	if ColumnUses(legacy) {
		t.Errorf("ColumnUses(%+v) = true, want false for a v0.9.0 column", legacy)
	}
	for name, c := range map[string]migration.Column{
		"width":        {Width: 32},
		"naive":        {Naive: true},
		"notnull":      {NotNull: true},
		"default kind": {DefaultKind: "string"},
		"default val":  {DefaultValue: "x"},
		"unique name":  {UniqueName: "u"},
		"index name":   {IndexName: "i"},
	} {
		if !ColumnUses(c) {
			t.Errorf("ColumnUses(%s) = false, want true", name)
		}
	}
}

func TestStepUses(t *testing.T) {
	exact := migration.Column{Name: "a", NotNull: true}
	legacyCols := []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}
	cases := []struct {
		name string
		step migration.Step
		want bool
	}{
		{"legacy create table", migration.CreateTable{Table: "t", Columns: legacyCols}, false},
		{"exact create table", migration.CreateTable{Table: "t", Columns: append([]migration.Column{exact}, legacyCols...)}, true},
		{"legacy add column", migration.AddColumn{Table: "t", Column: legacyCols[0]}, false},
		{"exact add column", migration.AddColumn{Table: "t", Column: exact}, true},
		{"drop table", migration.DropTable{Table: "t"}, false},
		{"drop column", migration.DropColumn{Table: "t", Column: "c"}, false},
		{"legacy create index", migration.CreateIndex{Table: "t", Column: "c"}, false},
		{"named create index", migration.CreateIndex{Table: "t", Column: "c", Name: "n"}, true},
		{"named drop index", migration.DropIndex{Table: "t", Column: "c", Name: "n"}, true},
		{"legacy drop index", migration.DropIndex{Table: "t", Column: "c"}, false},
		{"named unique", migration.AlterColumnUnique{Table: "t", Column: "c", Unique: true, Name: "n"}, true},
		{"legacy unique", migration.AlterColumnUnique{Table: "t", Column: "c", Unique: true}, false},
		{"legacy type change", migration.AlterColumnType{Table: "t", Column: "c", From: "integer", To: "text", Default: "'1'"}, false},
		{"width type change", migration.AlterColumnType{Table: "t", Column: "c", From: "integer", To: "integer", FromWidth: 32}, true},
		{"naive type change", migration.AlterColumnType{Table: "t", Column: "c", From: "timestamp", To: "timestamp", FromNaive: true}, true},
		{"typed default type change", migration.AlterColumnType{Table: "t", Column: "c", From: "varchar", To: "text", DefaultKind: "string"}, true},
		{"rename column", migration.RenameColumn{Table: "t", From: "a", To: "b"}, false},
		{"rename table", migration.RenameTable{From: "a", To: "b"}, false},
		{"not null", migration.AlterColumnNotNull{Table: "t", Column: "c"}, true},
		{"default", migration.AlterColumnDefault{Table: "t", Column: "c"}, true},
		{"rename index", migration.RenameIndex{Table: "t", Column: "c", From: "a", To: "b"}, true},
	}
	for _, tc := range cases {
		if got := StepUses(tc.step); got != tc.want {
			t.Errorf("%s: StepUses = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestModelField(t *testing.T) {
	legacy := migration.Model{App: "a", Struct: "Old", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}}
	if field, ok := ModelField([]migration.Model{legacy}); ok {
		t.Fatalf("ModelField on legacy models = %q, true; want not found", field)
	}
	exact := migration.Model{App: "blog", Struct: "Post", Columns: []migration.Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "created_at", Type: "timestamp", Naive: true},
	}, Fields: map[string]string{"created_at": "CreatedAt"}}
	field, ok := ModelField([]migration.Model{legacy, exact})
	if !ok || field != "blog.Post.CreatedAt" {
		t.Fatalf("ModelField = %q, %v; want blog.Post.CreatedAt, true", field, ok)
	}
	bare := migration.Model{App: "a", Struct: "S", Columns: []migration.Column{{Name: "x", NotNull: true}}}
	if field, _ := ModelField([]migration.Model{bare}); field != "a.S.x" {
		t.Fatalf("ModelField without a Fields entry = %q, want a.S.x", field)
	}
}
