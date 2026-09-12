package migration

import "fmt"

// ColumnState is a column's shape as reconstructed by Replay.
type ColumnState struct {
	Name       string
	Type       string
	PrimaryKey bool
	Unique     bool
	Indexed    bool
}

// TableState is a table's shape as reconstructed by Replay.
type TableState struct {
	Name    string
	App     string
	Columns []ColumnState
}

// SchemaState is the reconstructed shape of every table after replaying a
// sequence of migrations.
type SchemaState struct {
	Tables map[string]TableState
}

// Replay applies every migration's Up steps, in order, starting from an
// empty schema, and returns the resulting state.
func Replay(migrations []Migration) (SchemaState, error) {
	state := SchemaState{Tables: make(map[string]TableState)}

	for _, m := range migrations {
		for _, step := range m.Up {
			if err := applyStepToState(&state, step, m.App); err != nil {
				return SchemaState{}, fmt.Errorf("migration %q: %w", m.Name, err)
			}
		}
	}

	return state, nil
}

func applyStepToState(state *SchemaState, step Step, app string) error {
	switch s := step.(type) {
	case CreateTable:
		if _, exists := state.Tables[s.Table]; exists {
			return fmt.Errorf("tango migration: table %q already exists", s.Table)
		}
		columns := make([]ColumnState, len(s.Columns))
		for i, c := range s.Columns {
			columns[i] = ColumnState{Name: c.Name, Type: c.Type, PrimaryKey: c.PrimaryKey, Unique: c.Unique, Indexed: c.Indexed}
		}
		state.Tables[s.Table] = TableState{Name: s.Table, App: app, Columns: columns}

	case DropTable:
		if _, exists := state.Tables[s.Table]; !exists {
			return fmt.Errorf("tango migration: table %q does not exist", s.Table)
		}
		delete(state.Tables, s.Table)

	case AddColumn:
		table, err := requireTable(state, s.Table)
		if err != nil {
			return err
		}
		if columnIndex(table.Columns, s.Column.Name) != -1 {
			return fmt.Errorf("tango migration: column %q already exists on table %q", s.Column.Name, s.Table)
		}
		table.Columns = append(table.Columns, ColumnState{
			Name: s.Column.Name, Type: s.Column.Type, PrimaryKey: s.Column.PrimaryKey,
			Unique: s.Column.Unique, Indexed: s.Column.Indexed,
		})
		state.Tables[s.Table] = table

	case DropColumn:
		table, err := requireTable(state, s.Table)
		if err != nil {
			return err
		}
		index := columnIndex(table.Columns, s.Column)
		if index == -1 {
			return fmt.Errorf("tango migration: column %q does not exist on table %q", s.Column, s.Table)
		}
		table.Columns = append(table.Columns[:index], table.Columns[index+1:]...)
		state.Tables[s.Table] = table

	case AlterColumnUnique:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Unique = s.Unique
		state.Tables[s.Table] = table

	case CreateIndex:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Indexed = true
		state.Tables[s.Table] = table

	case DropIndex:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Indexed = false
		state.Tables[s.Table] = table

	default:
		return fmt.Errorf("tango migration: unknown step type %T", step)
	}

	return nil
}

func requireTable(state *SchemaState, table string) (TableState, error) {
	t, exists := state.Tables[table]
	if !exists {
		return TableState{}, fmt.Errorf("tango migration: table %q does not exist", table)
	}
	return t, nil
}

func requireColumn(state *SchemaState, table, column string) (TableState, int, error) {
	t, err := requireTable(state, table)
	if err != nil {
		return TableState{}, -1, err
	}
	index := columnIndex(t.Columns, column)
	if index == -1 {
		return TableState{}, -1, fmt.Errorf("tango migration: column %q does not exist on table %q", column, table)
	}
	return t, index, nil
}

func columnIndex(columns []ColumnState, name string) int {
	for i, c := range columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}
