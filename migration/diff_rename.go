package migration

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Rename is a Rename mapping: a column that migration history knows as
// Table.Column, in App, is the column the models now call To. Diff turns it
// into a RenameColumn step instead of a drop and an add, so the data is
// kept. It is exported for the tango CLI's makemigrations machinery.
type Rename struct {
	App    string
	Table  string
	Column string
	To     string
}

func (r Rename) source() string      { return r.App + "." + r.Table + "." + r.Column }
func (r Rename) destination() string { return r.App + "." + r.Table + "." + r.To }

// ErrInvalidRename is returned by DiffModels when a Rename mapping does not
// describe a column history has and the models have under a new name.
var ErrInvalidRename = errors.New("tango migration: invalid rename")

// validateRenames checks every mapping against history and the models
// before Diff generates anything, reporting every problem at once.
func validateRenames(models []Model, state SchemaState, renames []Rename) error {
	modelsByTable := make(map[string]Model, len(models))
	for _, m := range models {
		modelsByTable[m.Name] = m
	}
	sources := make(map[string]bool)
	destinations := make(map[string]bool)
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
		problems = append(problems, renameProblems(r, modelsByTable, state)...)
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidRename, strings.Join(problems, "; "))
}

func renameProblems(r Rename, modelsByTable map[string]Model, state SchemaState) []string {
	table, inHistory := state.Tables[r.Table]
	if !inHistory {
		return []string{fmt.Sprintf("%s.%s: migration history has no table %s", r.App, r.Table, r.Table)}
	}
	if table.App != r.App {
		return []string{fmt.Sprintf("%s: table %s belongs to app %s", r.source(), r.Table, table.App)}
	}
	model, inModels := modelsByTable[r.Table]
	if !inModels {
		return []string{fmt.Sprintf("%s: the models have no table %s", r.source(), r.Table)}
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
		problems = append(problems, fmt.Sprintf("%s: the %s model has no field for column %s (it has fields %s)", r.destination(), model.structName(), r.To, model.fieldList()))
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
