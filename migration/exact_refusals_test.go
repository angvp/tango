package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

func TestDiffRefusalsFromTheDesign(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	id := Column{Name: "id", Type: "integer", PrimaryKey: true}
	int4ID := Column{Name: "id", Type: "integer", PrimaryKey: true, Width: int4Width}
	cases := []struct {
		name    string
		history []Step
		models  []Model
		renames []Rename
		want    string
		is      error
	}{
		{"int4 to text",
			[]Step{CreateTable{Table: "t", Columns: []Column{id, {Name: "n", Type: "integer", Width: int4Width}}}},
			[]Model{{App: "a", Name: "t", Columns: []Column{id, {Name: "n", Type: "text"}}}},
			nil, "changes type from int4 to text, which is not a widening type change", ErrUnsupportedChange},
		{"bigint to int4",
			[]Step{CreateTable{Table: "t", Columns: []Column{id, {Name: "n", Type: "integer"}}}},
			[]Model{{App: "a", Name: "t", Columns: []Column{id, {Name: "n", Type: "integer", Width: int4Width}}}},
			nil, "changes type from integer to int4, which is not a widening type change", ErrUnsupportedChange},
		{"tz to naive",
			[]Step{CreateTable{Table: "t", Columns: []Column{id, {Name: "at", Type: "timestamp"}}}},
			[]Model{{App: "a", Name: "t", Columns: []Column{id, {Name: "at", Type: "timestamp", Naive: true}}}},
			nil, "changes type from timestamp to naive timestamp, which is not a widening type change", ErrUnsupportedChange},
		{"referenced int4 key",
			[]Step{
				CreateTable{Table: "u", Columns: []Column{int4ID}},
				CreateTable{Table: "p", Columns: []Column{id, {Name: "u_id", Type: "integer", Width: int4Width, References: "u"}}},
			},
			[]Model{
				{App: "a", Name: "u", Columns: []Column{id}},
				{App: "a", Name: "p", Columns: []Column{id, {Name: "u_id", Type: "integer", Width: int4Width, References: "u"}}},
			},
			nil, "p.u_id", ErrUnsupportedChange},
		{"legacy default",
			[]Step{
				CreateTable{Table: "t", Columns: []Column{id}},
				AddColumn{Table: "t", Column: Column{Name: "n", Type: "integer", Default: "0"}},
			},
			[]Model{{App: "a", Name: "t", Columns: []Column{id, {Name: "n", Type: "integer", NotNull: true}}}},
			nil, "legacy default", ErrUnsupportedChange},
		{"rename of an explicit index",
			[]Step{CreateTable{Table: "t", Columns: []Column{id, {Name: "email", Type: "text", Unique: true, UniqueName: "idx_t_email"}}}},
			[]Model{{App: "a", Name: "t", Columns: []Column{id, {Name: "mail", Type: "text", Unique: true, UniqueName: "idx_t_email"}}}},
			[]Rename{{App: "a", Table: "t", Column: "email", To: "mail"}}, "idx_t_email", ErrInvalidRename},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := replayOrFatal(t, []Migration{{App: "a", Name: "0001", Up: c.history}})
			_, err := DiffModels(c.models, state, c.renames...)
			if !errors.Is(err, c.is) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("DiffModels error = %v, want %v containing %q", err, c.is, c.want)
			}
		})
	}
}

func TestAlterColumnTypeKeepsATypedDefaultAcrossAWidening(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	applyExactSteps(t, sqlDB, dialect, CreateTable{Table: "langs", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "code", Type: "varchar", Length: 5, NotNull: true, DefaultKind: "string", DefaultValue: "en"},
	}})
	applyExactSteps(t, sqlDB, dialect, AlterColumnType{Table: "langs", Column: "code",
		From: "varchar", FromLength: 5, To: "text", DefaultKind: "string", DefaultValue: "en"})
	execExact(t, sqlDB, "INSERT INTO langs (id) VALUES (1)")
	var code string
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT code FROM langs WHERE id = 1").Scan(&code); err != nil {
		t.Fatalf("select: %v", err)
	}
	if code != "en" {
		t.Fatalf("default after the widening = %q, want en", code)
	}
	if !execFails(sqlDB, "INSERT INTO langs (id, code) VALUES (2, NULL)") {
		t.Fatal("NOT NULL lost in the widening")
	}
}
