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

// dateType is the Go type of model.Date, whose column is a DATE.
var dateType = reflect.TypeFor[model.Date]()

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
	case t == dateType:
		return "date"
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
	columnType, length := sqlType(field.Type), 0
	if columnType == "text" && field.MaxLength > 0 {
		columnType, length = "varchar", field.MaxLength
	}
	return Column{
		Name:         db.ColumnName(field.Name),
		Type:         columnType,
		Length:       length,
		PrimaryKey:   field.PrimaryKey,
		Unique:       field.Unique,
		Indexed:      field.Indexed,
		References:   references,
		Width:        int4WidthOf(field.Int4),
		Naive:        field.Naive,
		NotNull:      field.NotNull,
		DefaultKind:  field.DefaultKind,
		DefaultValue: field.DefaultValue,
		UniqueName:   field.UniqueName,
		IndexName:    field.IndexName,
	}
}

func int4WidthOf(int4 bool) int {
	if int4 {
		return int4Width
	}
	return 0
}

// normalizeIndexNames clears an explicit index name equal to the derived
// one, so declaring the default name changes nothing.
func normalizeIndexNames(table string, c Column) Column {
	if c.UniqueName == uniqueIndexName(table, c.Name) {
		c.UniqueName = ""
	}
	if c.IndexName == indexName(table, c.Name) {
		c.IndexName = ""
	}
	return c
}

// ModelsFromMeta converts registered model metadata into the dialect- and
// reflect-free Model shape the CLI works with (e.g. to serialize as JSON for
// the "-tango-dump-models" flag convention `tango makemigrations` relies on).
// It is exported only for tango's CLI/app-side convention.
func ModelsFromMeta(models []model.ModelMeta) []Model {
	migrationModels := make([]Model, len(models))
	for i, meta := range models {
		table := db.ColumnName(meta.Name)
		columns := make([]Column, len(meta.Fields))
		for j, field := range meta.Fields {
			columns[j] = normalizeIndexNames(table, desiredColumn(field))
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

	referenced := referencedBy(state)
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
		unsupported = append(unsupported, unsupportedChanges(meta, columns, referenced)...)
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
			if !m.Reversible {
				// Any irreversible step makes the whole migration
				// irreversible: no partial rollback is generated.
				m.Down = nil
			}
			m.Down = reversedSteps(m.Down)
			result = append(result, *m)
		}
	}
	return result, nil
}

// referencedBy maps each table some column of the state names in References
// to its first referrer as "table.column", taking tables and columns in name
// order so a message is deterministic.
func referencedBy(state SchemaState) map[string]string {
	tables := make([]string, 0, len(state.Tables))
	for name := range state.Tables {
		tables = append(tables, name)
	}
	sort.Strings(tables)
	referenced := map[string]string{}
	for _, table := range tables {
		columns := append([]ColumnState(nil), state.Tables[table].Columns...)
		sort.Slice(columns, func(i, j int) bool { return columns[i].Name < columns[j].Name })
		for _, c := range columns {
			if _, seen := referenced[c.References]; c.References != "" && !seen {
				referenced[c.References] = table + "." + c.Name
			}
		}
	}
	return referenced
}

// typeChangeProblems explains why current cannot become column's type, or
// returns nothing for a Widening type change whose default converts.
func typeChangeProblems(name, table string, current ColumnState, column Column, referenced map[string]string) []string {
	from, to := stateToken(current), columnToken(column)
	fromLabel, toLabel := tokenLabel(from, current.Length), tokenLabel(to, column.Length)
	if current.PrimaryKey || column.PrimaryKey {
		if !(current.PrimaryKey && column.PrimaryKey && from == "int4" && to == "integer") {
			return []string{fmt.Sprintf("%s changes type from %s to %s, but a primary key's type cannot change", name, fromLabel, toLabel)}
		}
		if referrer := referenced[table]; referrer != "" {
			return []string{fmt.Sprintf("%s widens its primary key from int4, but %s references it and a foreign key must match the key's width; widen %s first", name, referrer, referrer)}
		}
		return nil
	}
	if !isWidening(from, current.Length, to, column.Length) && !isNarrowing(from, current.Length, to, column.Length) {
		return []string{fmt.Sprintf("%s changes type from %s to %s, which is not a widening type change", name, fromLabel, toLabel)}
	}
	if current.DefaultKind != "" && defaultKindForType[column.Type] != current.DefaultKind {
		return []string{fmt.Sprintf("%s changes type from %s to %s, but its default kind %s does not fit the new type; drop the default first", name, fromLabel, toLabel, current.DefaultKind)}
	}
	if _, ok := convertDefault(from, to, current.Default); !ok {
		return []string{fmt.Sprintf("%s changes type from %s to %s, but its default %s cannot be converted", name, fromLabel, toLabel, current.Default)}
	}
	return nil
}

// unsupportedChanges describes each change between model's columns and the
// same columns in history that no step can express.
func unsupportedChanges(model Model, existing []ColumnState, referenced map[string]string) []string {
	existingByName := make(map[string]ColumnState, len(existing))
	for _, c := range existing {
		existingByName[c.Name] = c
	}
	var changes []string
	for _, column := range model.Columns {
		current, exists := existingByName[column.Name]
		name := model.fieldName(column.Name)
		if !exists {
			if column.NotNull && column.DefaultKind == "" {
				changes = append(changes, name+" is a new NOT NULL column with no default, so existing rows cannot be filled; give it a default= or add it nullable first")
			}
			continue
		}
		if typeChanged(current, column) {
			changes = append(changes, typeChangeProblems(name, model.Name, current, column, referenced)...)
		}
		if current.Default != "" && (current.NotNull != column.NotNull || current.DefaultKind != column.DefaultKind || current.DefaultValue != column.DefaultValue) {
			changes = append(changes, name+" changes NOT NULL or its default while it carries a legacy default, which the migration could not restate; clear the legacy default first")
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

		diffExistingColumn(m, table, column, current)
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

// diffExistingColumn appends the steps that bring current to column, in the
// order type, unique, index, NOT NULL, default, and each step's inverse.
func diffExistingColumn(m *Migration, table string, column Column, current ColumnState) {
	if typeChanged(current, column) {
		diffTypeChange(m, table, column, current)
	}
	diffIndexAttribute(m, table, column.Name, true, current.Unique, current.UniqueName, column.Unique, column.UniqueName)
	diffIndexAttribute(m, table, column.Name, false, current.Indexed, current.IndexName, column.Indexed, column.IndexName)
	if current.NotNull != column.NotNull {
		m.Up = append(m.Up, AlterColumnNotNull{Table: table, Column: column.Name, NotNull: column.NotNull, DefaultKind: current.DefaultKind, DefaultValue: current.DefaultValue})
		m.Down = append(m.Down, AlterColumnNotNull{Table: table, Column: column.Name, NotNull: current.NotNull, DefaultKind: current.DefaultKind, DefaultValue: current.DefaultValue})
	}
	if current.DefaultKind != column.DefaultKind || current.DefaultValue != column.DefaultValue {
		m.Up = append(m.Up, AlterColumnDefault{Table: table, Column: column.Name, DefaultKind: column.DefaultKind, DefaultValue: column.DefaultValue})
		m.Down = append(m.Down, AlterColumnDefault{Table: table, Column: column.Name, DefaultKind: current.DefaultKind, DefaultValue: current.DefaultValue})
	}
}

func diffTypeChange(m *Migration, table string, column Column, current ColumnState) {
	from, to := stateToken(current), columnToken(column)
	def, _ := convertDefault(from, to, current.Default)
	step := AlterColumnType{
		Table: table, Column: column.Name,
		From: current.Type, FromLength: current.Length, FromWidth: current.Width, FromNaive: current.Naive,
		To: column.Type, ToLength: column.Length, ToWidth: column.Width, ToNaive: column.Naive,
		Default: def, DefaultKind: current.DefaultKind, DefaultValue: current.DefaultValue,
	}
	m.Up = append(m.Up, step)
	if isNarrowing(from, step.FromLength, to, step.ToLength) {
		// Widening again is always safe, so a narrowing reverses.
		m.Down = append(m.Down, AlterColumnType{
			Table: table, Column: column.Name,
			From: step.To, FromLength: step.ToLength, FromWidth: step.ToWidth, FromNaive: step.ToNaive,
			To: step.From, ToLength: step.FromLength, ToWidth: step.FromWidth, ToNaive: step.FromNaive,
			Default: def, DefaultKind: current.DefaultKind, DefaultValue: current.DefaultValue,
		})
		return
	}
	m.Reversible = false
}

// diffIndexAttribute handles one of a column's two index attributes, unique
// (a unique index) or plain: gaining or losing it, or, when it stays, a
// change of its explicit name. fromName and toName are "" for the derived name.
func diffIndexAttribute(m *Migration, table, column string, unique, had bool, fromName string, has bool, toName string) {
	create := func(name string) Step {
		if unique {
			return AlterColumnUnique{Table: table, Column: column, Unique: true, Name: name}
		}
		return CreateIndex{Table: table, Column: column, Name: name}
	}
	drop := func(name string) Step {
		if unique {
			return AlterColumnUnique{Table: table, Column: column, Unique: false, Name: name}
		}
		return DropIndex{Table: table, Column: column, Name: name}
	}
	switch {
	case !had && has:
		m.Up = append(m.Up, create(toName))
		m.Down = append(m.Down, drop(toName))
	case had && !has:
		m.Up = append(m.Up, drop(fromName))
		m.Down = append(m.Down, create(fromName))
	case had && has && fromName != toName:
		from, to := effectiveIndexName(table, column, unique, fromName), effectiveIndexName(table, column, unique, toName)
		m.Up = append(m.Up, RenameIndex{Table: table, Column: column, Unique: unique, From: from, To: to})
		m.Down = append(m.Down, RenameIndex{Table: table, Column: column, Unique: unique, From: to, To: from})
	}
}

func effectiveIndexName(table, column string, unique bool, explicit string) string {
	switch {
	case explicit != "":
		return explicit
	case unique:
		return uniqueIndexName(table, column)
	}
	return indexName(table, column)
}
