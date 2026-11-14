package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

// exactSteps has one step per case the exact-schema work added, and the
// legacy shape of the steps it extended.
func exactSteps() []migration.Step {
	return []migration.Step{
		migration.CreateIndex{Table: "t", Column: "c", Name: "ix_t_c"},
		migration.CreateIndex{Table: "t", Column: "c"},
		migration.DropIndex{Table: "t", Column: "c", Name: "ix_t_c"},
		migration.DropIndex{Table: "t", Column: "c"},
		migration.AlterColumnUnique{Table: "t", Column: "c", Unique: true, Name: "u_t_c"},
		migration.AlterColumnUnique{Table: "t", Column: "c", Unique: true},
		migration.AlterColumnType{Table: "t", Column: "c", From: "integer", To: "integer", FromWidth: 32},
		migration.AlterColumnType{Table: "t", Column: "c", From: "timestamp", To: "timestamp", FromNaive: true},
		migration.AlterColumnType{Table: "t", Column: "c", From: "varchar", FromLength: 5, To: "text", DefaultKind: "string", DefaultValue: "en"},
		migration.AlterColumnType{Table: "t", Column: "c", From: "integer", To: "text", Default: "'1'"},
		migration.AlterColumnNotNull{Table: "t", Column: "c", NotNull: true, DefaultKind: "boolean", DefaultValue: "false"},
		migration.AlterColumnNotNull{Table: "t", Column: "c"},
		migration.AlterColumnDefault{Table: "t", Column: "c", DefaultKind: "integer", DefaultValue: "0"},
		migration.AlterColumnDefault{Table: "t", Column: "c"},
		migration.RenameIndex{Table: "t", Column: "c", Unique: true, From: "uniq_t_c", To: "idx_t_c_u"},
	}
}

func TestExactStepsRoundTripThroughJSON(t *testing.T) {
	steps := exactSteps()
	encoded, err := encodeSteps(steps)
	if err != nil {
		t.Fatalf("encodeSteps: %v", err)
	}
	decoded, err := decodeSteps(encoded)
	if err != nil {
		t.Fatalf("decodeSteps: %v", err)
	}
	if !reflect.DeepEqual(decoded, steps) {
		t.Fatalf("round trip changed the steps:\n got  %#v\n want %#v", decoded, steps)
	}
}

func TestExactStepKinds(t *testing.T) {
	encoded, err := encodeSteps(exactSteps())
	if err != nil {
		t.Fatalf("encodeSteps: %v", err)
	}
	want := []string{
		"CreateIndexV2", "CreateIndex", "DropIndexV2", "DropIndex",
		"AlterColumnUniqueV2", "AlterColumnUnique",
		"AlterColumnTypeV2", "AlterColumnTypeV2", "AlterColumnTypeV2", "AlterColumnType",
		"AlterColumnNotNull", "AlterColumnNotNull", "AlterColumnDefault", "AlterColumnDefault", "RenameIndex",
	}
	for i, e := range encoded {
		if e.Kind != want[i] {
			t.Errorf("step %d kind = %q, want %q", i, e.Kind, want[i])
		}
	}
}

func TestDecodeAcceptsBothSpellingsOfABaseKind(t *testing.T) {
	for _, kind := range []string{"CreateIndex", "CreateIndexV2"} {
		got, err := decodeSteps([]encodedStep{{Kind: kind, Table: "t", Column: "c"}})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if want := (migration.CreateIndex{Table: "t", Column: "c"}); got[0] != want {
			t.Errorf("%s decoded to %#v, want %#v", kind, got[0], want)
		}
	}
	if _, err := decodeSteps([]encodedStep{{Kind: "NoSuchStepV2"}}); err == nil {
		t.Fatal("decodeSteps accepted an unknown kind")
	}
}

// TestEveryStepTypeHasALiteral guards the silent failure of writeStepLiteral:
// it has no default arm, so a step type it forgot would write nothing.
func TestEveryStepTypeHasALiteral(t *testing.T) {
	all := append(exactSteps(),
		migration.CreateTable{Table: "t", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		migration.DropTable{Table: "t"},
		migration.AddColumn{Table: "t", Column: migration.Column{Name: "c", Type: "text"}},
		migration.DropColumn{Table: "t", Column: "c"},
		migration.RenameColumn{Table: "t", From: "a", To: "b"},
		migration.RenameTable{From: "a", To: "b"},
	)
	for _, step := range all {
		var b strings.Builder
		writeStepLiteral(&b, step)
		name := reflect.TypeOf(step).Name()
		if !strings.Contains(b.String(), "migration."+name+"{") {
			t.Errorf("writeStepLiteral(%T) wrote %q, which does not name migration.%s", step, b.String(), name)
		}
	}
}

func TestExactStepLiteralFields(t *testing.T) {
	var b strings.Builder
	for _, step := range exactSteps() {
		writeStepLiteral(&b, step)
	}
	for _, want := range []string{
		`migration.CreateIndex{Table: "t", Column: "c", Name: "ix_t_c"}`,
		`migration.CreateIndex{Table: "t", Column: "c"}`,
		`migration.AlterColumnUnique{Table: "t", Column: "c", Unique: true, Name: "u_t_c"}`,
		`FromWidth: 32`, `FromNaive: true`, `DefaultKind: "string", DefaultValue: "en"`,
		`migration.AlterColumnNotNull{Table: "t", Column: "c", NotNull: true, DefaultKind: "boolean", DefaultValue: "false"}`,
		`migration.AlterColumnNotNull{Table: "t", Column: "c", NotNull: false}`,
		`migration.AlterColumnDefault{Table: "t", Column: "c", DefaultKind: "integer", DefaultValue: "0"}`,
		`migration.RenameIndex{Table: "t", Column: "c", Unique: true, From: "uniq_t_c", To: "idx_t_c_u"}`,
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("literals lack %s", want)
		}
	}
}
