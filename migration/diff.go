package migration

import (
	"reflect"
	"sort"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

var timeType = reflect.TypeOf(time.Time{})

// Model is the SQL-relevant model shape makemigrations needs from an app. It
// is exported so tango's app-side "-tango-dump-models" JSON payload and CLI can
// share a type; do not treat it as a hand-authored application API.
type Model struct {
	App     string
	Name    string
	Columns []Column
}

// sqlType returns the dialect-agnostic type token Diff stores in Column.Type
// for a Go field type.
func sqlType(t reflect.Type) string {
	switch {
	case t == timeType:
		return "timestamp"
	case t.Kind() == reflect.String:
		return "text"
	case t.Kind() == reflect.Bool:
		return "boolean"
	case isFloatKind(t.Kind()):
		return "real"
	default:
		return "integer"
	}
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

func desiredColumn(field model.FieldMeta) Column {
	var references string
	if field.ForeignKey != "" {
		references = db.ColumnName(field.ForeignKey)
	}
	return Column{
		Name:       db.ColumnName(field.Name),
		Type:       sqlType(field.Type),
		PrimaryKey: field.PrimaryKey,
		Unique:     field.Unique,
		Indexed:    field.Indexed,
		References: references,
	}
}

// ModelsFromMeta converts registered model metadata into the dialect- and
// reflect-free Model shape the CLI works with (e.g. to serialize as JSON for
// the "-tango-dump-models" flag convention `tango makemigrations` relies on).
// It is exported only for tango's CLI/app-side convention.
func ModelsFromMeta(models []model.ModelMeta) []Model {
	migrationModels := make([]Model, len(models))
	for i, meta := range models {
		columns := make([]Column, len(meta.Fields))
		for j, field := range meta.Fields {
			columns[j] = desiredColumn(field)
		}
		migrationModels[i] = Model{
			App:     meta.App,
			Name:    db.ColumnName(meta.Name),
			Columns: columns,
		}
	}
	return migrationModels
}

// Diff compares the desired schema (derived from models) against state (the
// replayed result of existing migrations) and returns one Migration per app
// with detected changes, keyed by ModelMeta.App. A model whose App is empty
// is grouped under the empty-string key. Returns no migrations if nothing
// changed. It is exported only for the tango CLI's makemigrations machinery.
func Diff(models []model.ModelMeta, state SchemaState) []Migration {
	return DiffModels(ModelsFromMeta(models), state)
}

// DiffModels compares desired model shapes against a replayed migration state.
// It is exported only for the tango CLI's makemigrations machinery.
func DiffModels(models []Model, state SchemaState) []Migration {
	byApp := make(map[string]*Migration)

	appMigration := func(app string) *Migration {
		m, ok := byApp[app]
		if !ok {
			m = &Migration{App: app, Reversible: true}
			byApp[app] = m
		}
		return m
	}

	desiredTables := make(map[string]Model)
	for _, meta := range models {
		desiredTables[meta.Name] = meta
	}

	desiredNames := make([]string, 0, len(desiredTables))
	for name := range desiredTables {
		desiredNames = append(desiredNames, name)
	}
	createOrder := referencedFirst(desiredNames, func(table string) []string {
		return columnReferences(desiredTables[table].Columns)
	})

	for _, table := range createOrder {
		meta := desiredTables[table]
		existing, exists := state.Tables[table]

		if !exists {
			m := appMigration(meta.App)
			m.Up = append(m.Up, CreateTable{Table: table, Columns: meta.Columns})
			m.Down = append(m.Down, DropTable{Table: table})
			continue
		}

		diffColumns(appMigration(meta.App), table, meta.Columns, existing.Columns)
	}

	var removedTables []string
	for name := range state.Tables {
		if _, wanted := desiredTables[name]; !wanted {
			removedTables = append(removedTables, name)
		}
	}
	createdOrder := referencedFirst(removedTables, func(table string) []string {
		var references []string
		for _, c := range state.Tables[table].Columns {
			if c.References != "" {
				references = append(references, c.References)
			}
		}
		return references
	})

	// Drop in reverse creation order, so a table goes only after every
	// removed table that references it.
	for i := len(createdOrder) - 1; i >= 0; i-- {
		table := createdOrder[i]
		existing := state.Tables[table]
		m := appMigration(existing.App)
		m.Up = append(m.Up, DropTable{Table: table})
		m.Reversible = false
	}

	apps := make([]string, 0, len(byApp))
	for app := range byApp {
		apps = append(apps, app)
	}
	sort.Strings(apps)

	var result []Migration
	for _, app := range apps {
		m := byApp[app]
		if len(m.Up) > 0 {
			m.Down = reversedSteps(m.Down)
			result = append(result, *m)
		}
	}
	return result
}

// referencedFirst orders tables alphabetically, except that a table always
// comes after every table in the set it references: PostgreSQL rejects a
// REFERENCES clause naming a table that does not exist yet. A reference to
// a table outside the set, to the table itself, or around a cycle does not
// constrain the order.
func referencedFirst(tables []string, references func(table string) []string) []string {
	sorted := append([]string(nil), tables...)
	sort.Strings(sorted)
	inSet := make(map[string]bool, len(sorted))
	for _, table := range sorted {
		inSet[table] = true
	}

	ordered := make([]string, 0, len(sorted))
	visited := make(map[string]bool, len(sorted))
	var visit func(table string)
	visit = func(table string) {
		if visited[table] {
			return
		}
		visited[table] = true
		referenced := append([]string(nil), references(table)...)
		sort.Strings(referenced)
		for _, target := range referenced {
			if inSet[target] {
				visit(target)
			}
		}
		ordered = append(ordered, table)
	}
	for _, table := range sorted {
		visit(table)
	}
	return ordered
}

func columnReferences(columns []Column) []string {
	var references []string
	for _, c := range columns {
		if c.References != "" {
			references = append(references, c.References)
		}
	}
	return references
}

// reversedSteps returns steps in reverse order. Diff records each Up step's
// inverse as it goes, so a migration's Down undoes its Up last-first:
// a table is dropped only after the tables and columns referencing it.
func reversedSteps(steps []Step) []Step {
	if len(steps) == 0 {
		return steps
	}
	reversed := make([]Step, len(steps))
	for i, step := range steps {
		reversed[len(steps)-1-i] = step
	}
	return reversed
}

func diffColumns(m *Migration, table string, columns []Column, existing []ColumnState) {
	existingByName := make(map[string]ColumnState, len(existing))
	for _, c := range existing {
		existingByName[c.Name] = c
	}

	desiredNames := make(map[string]struct{}, len(columns))

	for _, column := range columns {
		desiredNames[column.Name] = struct{}{}

		current, exists := existingByName[column.Name]
		if !exists {
			m.Up = append(m.Up, AddColumn{Table: table, Column: column})
			m.Down = append(m.Down, DropColumn{Table: table, Column: column.Name})
			continue
		}

		if current.Unique != column.Unique {
			m.Up = append(m.Up, AlterColumnUnique{Table: table, Column: column.Name, Unique: column.Unique})
			m.Down = append(m.Down, AlterColumnUnique{Table: table, Column: column.Name, Unique: current.Unique})
		}

		if current.Indexed != column.Indexed {
			if column.Indexed {
				m.Up = append(m.Up, CreateIndex{Table: table, Column: column.Name})
				m.Down = append(m.Down, DropIndex{Table: table, Column: column.Name})
			} else {
				m.Up = append(m.Up, DropIndex{Table: table, Column: column.Name})
				m.Down = append(m.Down, CreateIndex{Table: table, Column: column.Name})
			}
		}
	}

	columnNames := make([]string, 0, len(existing))
	for _, c := range existing {
		columnNames = append(columnNames, c.Name)
	}
	sort.Strings(columnNames)

	for _, name := range columnNames {
		if _, wanted := desiredNames[name]; wanted {
			continue
		}
		m.Up = append(m.Up, DropColumn{Table: table, Column: name})
		m.Reversible = false
	}
}
