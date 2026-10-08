package migration

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Rename is a Rename mapping. With Column set, the column migration history
// knows as Table.Column, in App, is the column the models now call To; with
// Column empty, the table history knows as Table is the model's table the
// models now call To. Diff turns it into a RenameColumn or RenameTable step
// instead of a drop and an add, so the data is kept. A field of a renamed
// model is named by the model's old table name. It is exported for the
// tango CLI's makemigrations machinery.
type Rename struct {
	App    string
	Table  string
	Column string
	To     string
}

func (r Rename) isTable() bool { return r.Column == "" }

func (r Rename) source() string {
	if r.isTable() {
		return r.App + "." + r.Table
	}
	return r.App + "." + r.Table + "." + r.Column
}

func (r Rename) destination() string {
	if r.isTable() {
		return r.App + "." + r.To
	}
	return r.App + "." + r.Table + "." + r.To
}

// splitRenames separates model mappings from field mappings.
func splitRenames(renames []Rename) (tables, columns []Rename) {
	for _, r := range renames {
		if r.isTable() {
			tables = append(tables, r)
		} else {
			columns = append(columns, r)
		}
	}
	return tables, columns
}

// ErrInvalidRename is returned by DiffModels when a Rename mapping does not
// describe a column history has and the models have under a new name.
var ErrInvalidRename = errors.New("tango migration: invalid rename")

// validateRenames checks every mapping against history and the models
// before Diff generates anything, reporting every problem at once. Model
// mappings are checked first; a field mapping is then checked against
// history under its model's old name and against the models under the
// model's new one.
func validateRenames(models []Model, state SchemaState, renames []Rename) error {
	modelsByTable := make(map[string]Model, len(models))
	for _, m := range models {
		modelsByTable[m.Name] = m
	}
	tableRenames, columnRenames := splitRenames(renames)
	sources := make(map[string]bool)
	destinations := make(map[string]bool)
	newTable := make(map[string]string, len(tableRenames))
	renamedTo := make(map[string]Rename, len(tableRenames))
	var problems []string
	for _, r := range renames {
		if sources[r.source()] {
			problems = append(problems, fmt.Sprintf("%s is renamed more than once", r.source()))
			continue
		}
		sources[r.source()] = true
		if destinations[r.destination()] {
			problems = append(problems, fmt.Sprintf("%s is named as a rename destination more than once", r.destination()))
			continue
		}
		destinations[r.destination()] = true
	}
	for _, r := range tableRenames {
		newTable[r.Table] = r.To
		renamedTo[r.To] = r
	}
	for _, r := range tableRenames {
		if _, chained := newTable[r.To]; chained {
			problems = append(problems, fmt.Sprintf("%s is both renamed and the destination of a rename; rename each model once, from its name in migration history", r.App+"."+r.To))
			continue
		}
		problems = append(problems, tableRenameProblems(r, modelsByTable, state)...)
	}
	for _, r := range columnRenames {
		if renamed, ok := renamedTo[r.Table]; ok {
			problems = append(problems, fmt.Sprintf("%s: name the model by its old name, %s, in a field rename", r.source(), renamed.source()))
			continue
		}
		modelTable := r.Table
		if to, ok := newTable[r.Table]; ok {
			modelTable = to
		}
		problems = append(problems, renameProblems(r, modelTable, modelsByTable, state)...)
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidRename, strings.Join(problems, "; "))
}

func tableRenameProblems(r Rename, modelsByTable map[string]Model, state SchemaState) []string {
	table, inHistory := state.Tables[r.Table]
	if !inHistory {
		return []string{fmt.Sprintf("%s: migration history has no table %s", r.source(), r.Table)}
	}
	if table.App != r.App {
		return []string{fmt.Sprintf("%s: table %s belongs to app %s", r.source(), r.Table, table.App)}
	}
	var problems []string
	if old, ok := modelsByTable[r.Table]; ok {
		problems = append(problems, fmt.Sprintf("%s: the %s model still has table %s, so it was not renamed", r.source(), old.structName(), r.Table))
	}
	if _, ok := state.Tables[r.To]; ok {
		problems = append(problems, fmt.Sprintf("%s is already in migration history, so nothing can be renamed to it", r.destination()))
	}
	if model, ok := modelsByTable[r.To]; !ok || model.App != r.App {
		problems = append(problems, fmt.Sprintf("%s: the models have no model with table %s in app %s (it has tables %s)", r.destination(), r.To, r.App, appTableList(modelsByTable, r.App)))
	}
	return problems
}

func appTableList(modelsByTable map[string]Model, app string) string {
	var names []string
	for name, m := range modelsByTable {
		if m.App == app {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func renameProblems(r Rename, modelTable string, modelsByTable map[string]Model, state SchemaState) []string {
	table, inHistory := state.Tables[r.Table]
	if !inHistory {
		return []string{fmt.Sprintf("%s.%s: migration history has no table %s", r.App, r.Table, r.Table)}
	}
	if table.App != r.App {
		return []string{fmt.Sprintf("%s: table %s belongs to app %s", r.source(), r.Table, table.App)}
	}
	model, inModels := modelsByTable[modelTable]
	if !inModels {
		return []string{fmt.Sprintf("%s: the models have no table %s", r.source(), modelTable)}
	}

	var problems []string
	if columnIndex(table.Columns, r.Column) == -1 {
		problems = append(problems, fmt.Sprintf("%s is not in migration history (table %s has columns %s)", r.source(), r.Table, stateColumnList(table.Columns)))
	}
	if columnIndex(table.Columns, r.To) != -1 {
		problems = append(problems, fmt.Sprintf("%s is already in migration history, so nothing can be renamed to it", r.destination()))
	}
	if hasColumn(model.Columns, r.Column) {
		problems = append(problems, fmt.Sprintf("%s: the %s model still has %s, so it was not renamed", r.source(), model.structName(), model.goField(r.Column)))
	}
	if !hasColumn(model.Columns, r.To) {
		problems = append(problems, fmt.Sprintf("%s.%s.%s: the %s model has no field for column %s (it has fields %s)", r.App, modelTable, r.To, model.structName(), r.To, model.fieldList()))
	}
	return problems
}

func hasColumn(columns []Column, name string) bool {
	for _, c := range columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func stateColumnList(columns []ColumnState) string {
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

func (m Model) structName() string {
	if m.Struct == "" {
		return m.Name
	}
	return m.Struct
}

func (m Model) goField(column string) string {
	if field := m.Fields[column]; field != "" {
		return field
	}
	return column
}

func (m Model) fieldList() string {
	names := make([]string, len(m.Columns))
	for i, c := range m.Columns {
		names[i] = m.goField(c.Name)
	}
	return strings.Join(names, ", ")
}

// applyRenames returns existing with each of table's renamed columns under
// its new name, and records the RenameColumn steps on m.
func applyRenames(m *Migration, table string, existing []ColumnState, renames []Rename) []ColumnState {
	var renamed []ColumnState
	for _, r := range renames {
		if r.Table != table {
			continue
		}
		if renamed == nil {
			renamed = slices.Clone(existing)
		}
		renamed[columnIndex(renamed, r.Column)].Name = r.To
		m.Up = append(m.Up, RenameColumn{Table: table, From: r.Column, To: r.To})
		m.Down = append(m.Down, RenameColumn{Table: table, From: r.To, To: r.Column})
	}
	if renamed == nil {
		return existing
	}
	return renamed
}

// renameTables returns state with each model mapping applied, as replaying
// the RenameTable steps it records on each model's app migration would
// leave it, and the field mappings with their tables resolved to the new
// names.
func renameTables(state SchemaState, renames []Rename, appMigration func(app string) *Migration) (SchemaState, []Rename) {
	tableRenames, columnRenames := splitRenames(renames)
	if len(tableRenames) == 0 {
		return state, columnRenames
	}
	renamed := SchemaState{Tables: maps.Clone(state.Tables)}
	newTable := make(map[string]string, len(tableRenames))
	for _, r := range tableRenames {
		newTable[r.Table] = r.To
		renameTableInState(&renamed, r.Table, r.To)
		m := appMigration(r.App)
		m.Up = append(m.Up, RenameTable{From: r.Table, To: r.To})
		m.Down = append(m.Down, RenameTable{From: r.To, To: r.Table})
	}
	resolved := make([]Rename, len(columnRenames))
	for i, r := range columnRenames {
		if to, ok := newTable[r.Table]; ok {
			r.Table = to
		}
		resolved[i] = r
	}
	return renamed, resolved
}

// renameTableInState moves table from to to in state and points every
// foreign key that referenced from at to. state's table map must not be
// shared with another state.
func renameTableInState(state *SchemaState, from, to string) {
	table := state.Tables[from]
	delete(state.Tables, from)
	table.Name = to
	state.Tables[to] = table
	for name, t := range state.Tables {
		var columns []ColumnState
		for i, c := range t.Columns {
			if c.References != from {
				continue
			}
			if columns == nil {
				columns = slices.Clone(t.Columns)
			}
			columns[i].References = to
		}
		if columns != nil {
			t.Columns = columns
			state.Tables[name] = t
		}
	}
}
