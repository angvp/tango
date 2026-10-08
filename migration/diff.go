package migration

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
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
	// Struct and Fields are the Go names behind Name and each column's
	// name (column name to Go field name), so makemigrations can name a
	// model or field the way the developer wrote it.
	Struct string
	Fields map[string]string
}

// ErrUnsupportedChange is returned by Diff when the models changed in a way
// no migration step can express: a type change that is not a Widening type
// change (or one to a primary key, or whose default cannot convert), a
// change to which column is the primary key, or a foreign key gained, lost
// or retargeted on an existing column. Generating nothing would leave the
// database silently out of step with the models, so Diff refuses.
var ErrUnsupportedChange = errors.New("tango migration: a model change cannot be expressed as a migration")

// fieldName names column of model the way the developer wrote it
// ("posts.Post.Views"), falling back to the table and column names when
// the Go names are unknown.
func (m Model) fieldName(column string) string {
	return m.App + "." + m.structName() + "." + m.goField(column)
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
		fields := make(map[string]string, len(meta.Fields))
		for j, field := range meta.Fields {
			fields[columns[j].Name] = field.Name
		}
		migrationModels[i] = Model{
			App:     meta.App,
			Name:    db.ColumnName(meta.Name),
			Columns: columns,
			Struct:  meta.Name,
			Fields:  fields,
		}
	}
	return migrationModels
}

// Diff compares the desired schema (derived from models) against state (the
// replayed result of existing migrations) and returns one Migration per app
// with detected changes, keyed by ModelMeta.App. A model whose App is empty
// is grouped under the empty-string key. Returns no migrations if nothing
// changed. A change no step can express fails with ErrUnsupportedChange,
// naming every such field, and returns no migrations. It is exported only
// for the tango CLI's makemigrations machinery.
func Diff(models []model.ModelMeta, state SchemaState) ([]Migration, error) {
	return DiffModels(ModelsFromMeta(models), state)
}

// DiffModels compares desired model shapes against a replayed migration
// state, as Diff does. It is exported only for the tango CLI's
// makemigrations machinery.
func DiffModels(models []Model, state SchemaState, renames ...Rename) ([]Migration, error) {
	if err := validateRenames(models, state, renames); err != nil {
		return nil, err
	}

	byApp := make(map[string]*Migration)

	appMigration := func(app string) *Migration {
		m, ok := byApp[app]
		if !ok {
			m = &Migration{App: app, Reversible: true}
			byApp[app] = m
		}
		return m
	}

	state, columnRenames := renameTables(state, renames, appMigration)

	desiredTables := make(map[string]Model)
	for _, meta := range models {
		desiredTables[meta.Name] = meta
	}

	desiredReferences := make(map[string][]string, len(desiredTables))
	for name, meta := range desiredTables {
		desiredReferences[name] = columnReferences(meta.Columns)
	}
	createOrder := referencedFirst(desiredReferences)

	var unsupported []string
	for _, table := range createOrder {
		meta := desiredTables[table]
		existing, exists := state.Tables[table]

		if !exists {
			m := appMigration(meta.App)
			m.Up = append(m.Up, CreateTable{Table: table, Columns: meta.Columns})
			m.Down = append(m.Down, DropTable{Table: table})
			continue
		}

		m := appMigration(meta.App)
		columns := applyRenames(m, table, existing.Columns, columnRenames)
		unsupported = append(unsupported, unsupportedChanges(meta, columns)...)
		diffColumns(m, table, meta.Columns, columns)
	}
	if len(unsupported) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedChange, strings.Join(unsupported, "; "))
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
	return result, nil
}

// typeChangeProblems explains why current cannot become column's type, or
// returns nothing for a Widening type change whose default converts.
func typeChangeProblems(name string, current ColumnState, column Column) []string {
	switch {
	case current.PrimaryKey || column.PrimaryKey:
		return []string{fmt.Sprintf("%s changes type from %s to %s, but a primary key's type cannot change", name, current.Type, column.Type)}
	case !isWideningTypeChange(current.Type, column.Type):
		return []string{fmt.Sprintf("%s changes type from %s to %s, which is not a widening type change", name, current.Type, column.Type)}
	}
	if _, ok := convertDefault(current.Type, column.Type, current.Default); !ok {
		return []string{fmt.Sprintf("%s changes type from %s to %s, but its default %s cannot be converted", name, current.Type, column.Type, current.Default)}
	}
	return nil
}

// unsupportedChanges describes each change between model's columns and the
// same columns in history that no step can express.
func unsupportedChanges(model Model, existing []ColumnState) []string {
	existingByName := make(map[string]ColumnState, len(existing))
	for _, c := range existing {
		existingByName[c.Name] = c
	}
	var changes []string
	for _, column := range model.Columns {
		current, exists := existingByName[column.Name]
		if !exists {
			continue
		}
		name := model.fieldName(column.Name)
		if current.Type != column.Type {
			changes = append(changes, typeChangeProblems(name, current, column)...)
		}
		switch {
		case current.PrimaryKey && !column.PrimaryKey:
			changes = append(changes, name+" stops being the primary key")
		case !current.PrimaryKey && column.PrimaryKey:
			changes = append(changes, name+" becomes the primary key")
		}
		switch {
		case current.References == column.References:
		case current.References == "":
			changes = append(changes, fmt.Sprintf("%s gains a foreign key to %s", name, column.References))
		case column.References == "":
			changes = append(changes, fmt.Sprintf("%s loses its foreign key to %s", name, current.References))
		default:
			changes = append(changes, fmt.Sprintf("%s changes its foreign key target from %s to %s", name, current.References, column.References))
		}
	}
	return changes
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

		if current.Type != column.Type {
			def, _ := convertDefault(current.Type, column.Type, current.Default)
			m.Up = append(m.Up, AlterColumnType{Table: table, Column: column.Name, From: current.Type, To: column.Type, Default: def})
			m.Reversible = false
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
