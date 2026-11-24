package migration

import "fmt"

// ColumnState is a column's shape as reconstructed by Replay. It is exported
// only for the tango CLI's migration replay/diff machinery.
type ColumnState struct {
	Name       string
	Type       string
	Length     int
	PrimaryKey bool
	Unique     bool
	Indexed    bool
	References string
	// Default is the column's database default as a raw SQL literal, set
	// by an AddColumn that carried one and converted by AlterColumnType.
	// CreateTable never writes a default (see columnDefSQL), so replay
	// never takes one from it.
	Default string

	// The exact-schema fields mirror the Column fields of the same names.
	Width        int    `json:",omitempty"`
	Naive        bool   `json:",omitempty"`
	NotNull      bool   `json:",omitempty"`
	DefaultKind  string `json:",omitempty"`
	DefaultValue string `json:",omitempty"`
	UniqueName   string `json:",omitempty"`
	IndexName    string `json:",omitempty"`
}

// columnStateOf is the replayed state of a column created or added as c.
func columnStateOf(c Column) ColumnState {
	return ColumnState{
		Name: c.Name, Type: c.Type, Length: c.Length, PrimaryKey: c.PrimaryKey,
		Unique: c.Unique, Indexed: c.Indexed, References: c.References,
		Width: c.Width, Naive: c.Naive, NotNull: c.NotNull,
		DefaultKind: c.DefaultKind, DefaultValue: c.DefaultValue,
		UniqueName: c.UniqueName, IndexName: c.IndexName,
	}
}

// TableState is a table's shape as reconstructed by Replay. It is exported
// only for the tango CLI's migration replay/diff machinery.
type TableState struct {
	Name    string
	App     string
	Columns []ColumnState
	// Indexes are the table's composite indexes, sorted by effective name. A
	// Name is the index's physical name: empty while it is the derived one.
	Indexes []CompositeIndex `json:",omitempty"`
}

// SchemaState is the reconstructed shape of every table after replaying a
// sequence of migrations. It is exported only for the tango CLI's
// migration replay/diff machinery.
type SchemaState struct {
	Tables map[string]TableState
}

// Replay applies every migration's Up steps, in order, starting from an
// empty schema, and returns the resulting state. It is exported only for the
// tango CLI's makemigrations machinery.
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
			if err := validateColumn(s.Table, c); err != nil {
				return err
			}
			columns[i] = columnStateOf(c)
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
		if err := validateColumn(s.Table, s.Column); err != nil {
			return err
		}
		added := columnStateOf(s.Column)
		added.Default = s.Column.Default
		table.Columns = append(table.Columns, added)
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
		if i := compositeContaining(table.Indexes, s.Column); i != -1 {
			return fmt.Errorf("tango migration: cannot drop column %q of table %q: index %q still contains it; drop the index first",
				s.Column, s.Table, compositeIndexName(s.Table, table.Indexes[i]))
		}
		table.Columns = append(table.Columns[:index], table.Columns[index+1:]...)
		state.Tables[s.Table] = table

	case AlterColumnUnique:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Unique = s.Unique
		table.Columns[index].UniqueName = explicitName(s.Unique, s.Name, uniqueIndexName(s.Table, s.Column))
		state.Tables[s.Table] = table

	case CreateIndex:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Indexed = true
		table.Columns[index].IndexName = explicitName(true, s.Name, indexName(s.Table, s.Column))
		state.Tables[s.Table] = table

	case DropIndex:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		table.Columns[index].Indexed = false
		table.Columns[index].IndexName = ""
		state.Tables[s.Table] = table

	case RenameTable:
		if _, err := requireTable(state, s.From); err != nil {
			return err
		}
		if _, exists := state.Tables[s.To]; exists {
			return fmt.Errorf("tango migration: table %q already exists", s.To)
		}
		renameTableInState(state, s.From, s.To)

	case AlterColumnType:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		columns := append([]ColumnState(nil), table.Columns...)
		if err := validateAlterLengths(s); err != nil {
			return err
		}
		columns[index].Type = s.To
		columns[index].Length = s.ToLength
		columns[index].Width = s.ToWidth
		columns[index].Naive = s.ToNaive
		columns[index].Default = s.Default
		table.Columns = columns
		state.Tables[s.Table] = table

	case AlterColumnNotNull:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		columns := append([]ColumnState(nil), table.Columns...)
		columns[index].NotNull = s.NotNull
		table.Columns = columns
		state.Tables[s.Table] = table

	case AlterColumnDefault:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		columns := append([]ColumnState(nil), table.Columns...)
		columns[index].DefaultKind = s.DefaultKind
		columns[index].DefaultValue = s.DefaultValue
		table.Columns = columns
		state.Tables[s.Table] = table

	case RenameIndex:
		table, index, err := requireColumn(state, s.Table, s.Column)
		if err != nil {
			return err
		}
		columns := append([]ColumnState(nil), table.Columns...)
		if s.Unique {
			columns[index].UniqueName = explicitName(true, s.To, uniqueIndexName(s.Table, s.Column))
		} else {
			columns[index].IndexName = explicitName(true, s.To, indexName(s.Table, s.Column))
		}
		table.Columns = columns
		state.Tables[s.Table] = table

	case RenameColumn:
		table, index, err := requireColumn(state, s.Table, s.From)
		if err != nil {
			return err
		}
		if columnIndex(table.Columns, s.To) != -1 {
			return fmt.Errorf("tango migration: column %q already exists on table %q", s.To, s.Table)
		}
		columns := append([]ColumnState(nil), table.Columns...)
		columns[index].Name = s.To
		table.Columns = columns
		table.Indexes = renameIndexColumn(s.Table, table.Indexes, s.From, s.To)
		state.Tables[s.Table] = table

	case CreateCompositeIndex:
		return applyCompositeCreate(state, s)

	case DropCompositeIndex:
		return applyCompositeDrop(state, s)

	case RenameCompositeIndex:
		return applyCompositeRename(state, s)

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

// explicitName is the name a column state records for an index: nothing when
// the index is absent or carries the derived name, the explicit name otherwise.
func explicitName(present bool, name, derived string) string {
	if !present || name == derived {
		return ""
	}
	return name
}
