package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/angvp/tango/internal/schemaattr"
	"github.com/angvp/tango/migration"
)

// v2Suffix marks a step kind that carries an exact-schema attribute.
const v2Suffix = "V2"

type encodedMigration struct {
	App        string        `json:"app"`
	Name       string        `json:"name"`
	Up         []encodedStep `json:"up"`
	Down       []encodedStep `json:"down"`
	Reversible bool          `json:"reversible"`
}

type encodedStep struct {
	Kind    string             `json:"kind"`
	Table   string             `json:"table,omitempty"`
	Column  string             `json:"column,omitempty"`
	Unique  bool               `json:"unique,omitempty"`
	Columns []migration.Column `json:"columns,omitempty"`
	Def     migration.Column   `json:"def,omitempty"`
	// To is a rename's new name (Column holds the old one) or a type
	// change's new type (From holds the old one).
	To         string `json:"to,omitempty"`
	From       string `json:"from,omitempty"`
	ToLength   int    `json:"toLength,omitempty"`
	FromLength int    `json:"fromLength,omitempty"`
	Default    string `json:"default,omitempty"`

	// The fields below are the exact-schema additions; an older reader
	// refuses the V2 kinds that carry them rather than ignore them.
	Name         string `json:"name,omitempty"`
	NotNull      bool   `json:"notNull,omitempty"`
	DefaultKind  string `json:"defaultKind,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
	FromWidth    int    `json:"fromWidth,omitempty"`
	ToWidth      int    `json:"toWidth,omitempty"`
	FromNaive    bool   `json:"fromNaive,omitempty"`
	ToNaive      bool   `json:"toNaive,omitempty"`

	// IndexColumns is the ordered tuple of a composite index step. Those
	// steps have kinds of their own, which an older reader refuses by name.
	IndexColumns []string `json:"indexColumns,omitempty"`
}

func encodeMigrations(migrations []migration.Migration) ([]encodedMigration, error) {
	encoded := make([]encodedMigration, len(migrations))
	for i, m := range migrations {
		up, err := encodeSteps(m.Up)
		if err != nil {
			return nil, err
		}
		down, err := encodeSteps(m.Down)
		if err != nil {
			return nil, err
		}
		encoded[i] = encodedMigration{
			App:        m.App,
			Name:       m.Name,
			Up:         up,
			Down:       down,
			Reversible: m.Reversible,
		}
	}
	return encoded, nil
}

func decodeMigrations(data []byte) ([]migration.Migration, error) {
	var encoded []encodedMigration
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil, err
	}

	migrations := make([]migration.Migration, len(encoded))
	for i, m := range encoded {
		up, err := decodeSteps(m.Up)
		if err != nil {
			return nil, err
		}
		down, err := decodeSteps(m.Down)
		if err != nil {
			return nil, err
		}
		migrations[i] = migration.Migration{
			App:        m.App,
			Name:       m.Name,
			Up:         up,
			Down:       down,
			Reversible: m.Reversible,
		}
	}
	return migrations, nil
}

func encodeSteps(steps []migration.Step) ([]encodedStep, error) {
	encoded := make([]encodedStep, len(steps))
	for i, step := range steps {
		e, err := encodeStep(step)
		if err != nil {
			return nil, err
		}
		if schemaattr.StepUses(step) && !isNewStepKind(e.Kind) {
			e.Kind += v2Suffix
		}
		encoded[i] = e
	}
	return encoded, nil
}

// isNewStepKind reports whether kind is a step type of its own that an older
// reader does not know, so it needs no V2 suffix to be refused by name.
func isNewStepKind(kind string) bool {
	switch kind {
	case "AlterColumnNotNull", "AlterColumnDefault", "RenameIndex",
		"CreateCompositeIndex", "DropCompositeIndex", "RenameCompositeIndex":
		return true
	}
	return false
}

func encodeStep(step migration.Step) (encodedStep, error) {
	switch s := step.(type) {
	case migration.CreateTable:
		return encodedStep{Kind: "CreateTable", Table: s.Table, Columns: s.Columns}, nil
	case migration.DropTable:
		return encodedStep{Kind: "DropTable", Table: s.Table}, nil
	case migration.AddColumn:
		return encodedStep{Kind: "AddColumn", Table: s.Table, Def: s.Column}, nil
	case migration.DropColumn:
		return encodedStep{Kind: "DropColumn", Table: s.Table, Column: s.Column}, nil
	case migration.AlterColumnUnique:
		return encodedStep{Kind: "AlterColumnUnique", Table: s.Table, Column: s.Column, Unique: s.Unique, Name: s.Name}, nil
	case migration.CreateIndex:
		return encodedStep{Kind: "CreateIndex", Table: s.Table, Column: s.Column, Name: s.Name}, nil
	case migration.DropIndex:
		return encodedStep{Kind: "DropIndex", Table: s.Table, Column: s.Column, Name: s.Name}, nil
	case migration.RenameColumn:
		return encodedStep{Kind: "RenameColumn", Table: s.Table, Column: s.From, To: s.To}, nil
	case migration.RenameTable:
		return encodedStep{Kind: "RenameTable", Table: s.From, To: s.To}, nil
	case migration.AlterColumnType:
		return encodedStep{
			Kind: "AlterColumnType", Table: s.Table, Column: s.Column, From: s.From, To: s.To,
			FromLength: s.FromLength, ToLength: s.ToLength, Default: s.Default,
			FromWidth: s.FromWidth, ToWidth: s.ToWidth, FromNaive: s.FromNaive, ToNaive: s.ToNaive,
			DefaultKind: s.DefaultKind, DefaultValue: s.DefaultValue,
		}, nil
	case migration.AlterColumnNotNull:
		return encodedStep{Kind: "AlterColumnNotNull", Table: s.Table, Column: s.Column, NotNull: s.NotNull, DefaultKind: s.DefaultKind, DefaultValue: s.DefaultValue}, nil
	case migration.AlterColumnDefault:
		return encodedStep{Kind: "AlterColumnDefault", Table: s.Table, Column: s.Column, DefaultKind: s.DefaultKind, DefaultValue: s.DefaultValue}, nil
	case migration.RenameIndex:
		return encodedStep{Kind: "RenameIndex", Table: s.Table, Column: s.Column, Unique: s.Unique, From: s.From, To: s.To}, nil
	case migration.CreateCompositeIndex:
		return encodedStep{Kind: "CreateCompositeIndex", Table: s.Table, IndexColumns: s.Columns, Unique: s.Unique, Name: s.Name}, nil
	case migration.DropCompositeIndex:
		return encodedStep{Kind: "DropCompositeIndex", Table: s.Table, IndexColumns: s.Columns, Unique: s.Unique, Name: s.Name}, nil
	case migration.RenameCompositeIndex:
		return encodedStep{Kind: "RenameCompositeIndex", Table: s.Table, IndexColumns: s.Columns, Unique: s.Unique, From: s.From, To: s.To}, nil
	}
	return encodedStep{}, fmt.Errorf("unsupported migration step type %T", step)
}

func decodeSteps(steps []encodedStep) ([]migration.Step, error) {
	decoded := make([]migration.Step, len(steps))
	for i, step := range steps {
		d, err := decodeStep(step)
		if err != nil {
			return nil, err
		}
		decoded[i] = d
	}
	return decoded, nil
}

func decodeStep(step encodedStep) (migration.Step, error) {
	switch strings.TrimSuffix(step.Kind, v2Suffix) {
	case "CreateTable":
		return migration.CreateTable{Table: step.Table, Columns: step.Columns}, nil
	case "DropTable":
		return migration.DropTable{Table: step.Table}, nil
	case "AddColumn":
		return migration.AddColumn{Table: step.Table, Column: step.Def}, nil
	case "DropColumn":
		return migration.DropColumn{Table: step.Table, Column: step.Column}, nil
	case "AlterColumnUnique":
		return migration.AlterColumnUnique{Table: step.Table, Column: step.Column, Unique: step.Unique, Name: step.Name}, nil
	case "CreateIndex":
		return migration.CreateIndex{Table: step.Table, Column: step.Column, Name: step.Name}, nil
	case "DropIndex":
		return migration.DropIndex{Table: step.Table, Column: step.Column, Name: step.Name}, nil
	case "RenameColumn":
		return migration.RenameColumn{Table: step.Table, From: step.Column, To: step.To}, nil
	case "RenameTable":
		return migration.RenameTable{From: step.Table, To: step.To}, nil
	case "AlterColumnType":
		return migration.AlterColumnType{
			Table: step.Table, Column: step.Column, From: step.From, To: step.To,
			FromLength: step.FromLength, ToLength: step.ToLength, Default: step.Default,
			FromWidth: step.FromWidth, ToWidth: step.ToWidth, FromNaive: step.FromNaive, ToNaive: step.ToNaive,
			DefaultKind: step.DefaultKind, DefaultValue: step.DefaultValue,
		}, nil
	case "AlterColumnNotNull":
		return migration.AlterColumnNotNull{Table: step.Table, Column: step.Column, NotNull: step.NotNull, DefaultKind: step.DefaultKind, DefaultValue: step.DefaultValue}, nil
	case "AlterColumnDefault":
		return migration.AlterColumnDefault{Table: step.Table, Column: step.Column, DefaultKind: step.DefaultKind, DefaultValue: step.DefaultValue}, nil
	case "RenameIndex":
		return migration.RenameIndex{Table: step.Table, Column: step.Column, Unique: step.Unique, From: step.From, To: step.To}, nil
	case "CreateCompositeIndex":
		return migration.CreateCompositeIndex{Table: step.Table, Columns: step.IndexColumns, Unique: step.Unique, Name: step.Name}, nil
	case "DropCompositeIndex":
		return migration.DropCompositeIndex{Table: step.Table, Columns: step.IndexColumns, Unique: step.Unique, Name: step.Name}, nil
	case "RenameCompositeIndex":
		return migration.RenameCompositeIndex{Table: step.Table, Columns: step.IndexColumns, Unique: step.Unique, From: step.From, To: step.To}, nil
	}
	return nil, fmt.Errorf("unknown migration step kind %q", step.Kind)
}
