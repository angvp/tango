package migration

import (
	"maps"
	"reflect"
	"slices"
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

	desiredReferences := make(map[string][]string, len(desiredTables))
	for name, meta := range desiredTables {
		desiredReferences[name] = columnReferences(meta.Columns)
	}
	createOrder := referencedFirst(desiredReferences)

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

	removedReferences := make(map[string][]string)
	for name, existing := range state.Tables {
		if _, wanted := desiredTables[name]; !wanted {
			removedReferences[name] = columnReferences(existing.Columns)
		}
	}
	removedCreationOrder := referencedFirst(removedReferences)

	// Drop in reverse creation order, so a table goes only after every
	// removed table that references it.
	for i := len(removedCreationOrder) - 1; i >= 0; i-- {
		table := removedCreationOrder[i]
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

// referencedFirst orders the tables of references (each table mapped to
// the tables it references) by name, except that a table comes after every
// other table in the set it references: PostgreSQL rejects a REFERENCES
// clause naming a table that does not exist yet. A reference to a table
// outside the set or to the table itself does not constrain the order.
// Tables in a reference cycle, and tables waiting on one, come last in
// name order: no order creates them referenced-first, so Diff leaves the
// cycle for the database to accept (SQLite) or reject (PostgreSQL) rather
// than failing makemigrations.
func referencedFirst(references map[string][]string) []string {
	tables := slices.Sorted(maps.Keys(references))
	index := make(map[string]int, len(tables))
	for i, table := range tables {
		index[table] = i
	}

	deps := make([][]int, len(tables))
	for i, table := range tables {
		for _, target := range references[table] {
			if j, inSet := index[target]; inSet && j != i {
				deps[i] = append(deps[i], j)
			}
		}
	}

	order, stuck := dependencyOrder(deps)
	ordered := make([]string, 0, len(tables))
	for _, i := range slices.Concat(order, stuck) {
		ordered = append(ordered, tables[i])
	}
	return ordered
}

// referencingColumn is a column shape that may hold a foreign key: a
// desired Column or a replayed ColumnState.
type referencingColumn interface {
	referencedTable() string
}

func (c Column) referencedTable() string      { return c.References }
func (c ColumnState) referencedTable() string { return c.References }

// columnReferences lists the tables columns reference.
func columnReferences[C referencingColumn](columns []C) []string {
	var references []string
	for _, c := range columns {
		if table := c.referencedTable(); table != "" {
			references = append(references, table)
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
