package cli

import (
	"encoding/json"
	"fmt"

	"github.com/angvp/tango/migration"
)

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
		switch s := step.(type) {
		case migration.CreateTable:
			encoded[i] = encodedStep{Kind: "CreateTable", Table: s.Table, Columns: s.Columns}
		case migration.DropTable:
			encoded[i] = encodedStep{Kind: "DropTable", Table: s.Table}
		case migration.AddColumn:
			encoded[i] = encodedStep{Kind: "AddColumn", Table: s.Table, Def: s.Column}
		case migration.DropColumn:
			encoded[i] = encodedStep{Kind: "DropColumn", Table: s.Table, Column: s.Column}
		case migration.AlterColumnUnique:
			encoded[i] = encodedStep{Kind: "AlterColumnUnique", Table: s.Table, Column: s.Column, Unique: s.Unique}
		case migration.CreateIndex:
			encoded[i] = encodedStep{Kind: "CreateIndex", Table: s.Table, Column: s.Column}
		case migration.DropIndex:
			encoded[i] = encodedStep{Kind: "DropIndex", Table: s.Table, Column: s.Column}
		default:
			return nil, fmt.Errorf("unsupported migration step type %T", step)
		}
	}
	return encoded, nil
}

func decodeSteps(steps []encodedStep) ([]migration.Step, error) {
	decoded := make([]migration.Step, len(steps))
	for i, step := range steps {
		switch step.Kind {
		case "CreateTable":
			decoded[i] = migration.CreateTable{Table: step.Table, Columns: step.Columns}
		case "DropTable":
			decoded[i] = migration.DropTable{Table: step.Table}
		case "AddColumn":
			decoded[i] = migration.AddColumn{Table: step.Table, Column: step.Def}
		case "DropColumn":
			decoded[i] = migration.DropColumn{Table: step.Table, Column: step.Column}
		case "AlterColumnUnique":
			decoded[i] = migration.AlterColumnUnique{Table: step.Table, Column: step.Column, Unique: step.Unique}
		case "CreateIndex":
			decoded[i] = migration.CreateIndex{Table: step.Table, Column: step.Column}
		case "DropIndex":
			decoded[i] = migration.DropIndex{Table: step.Table, Column: step.Column}
		default:
			return nil, fmt.Errorf("unknown migration step kind %q", step.Kind)
		}
	}
	return decoded, nil
}
