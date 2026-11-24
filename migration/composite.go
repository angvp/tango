package migration

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// CompositeIndex is a composite unique constraint or ordinary index over an
// ordered tuple of two or more columns of a model. It is exported so the
// "-tango-dump-models" payload and the CLI share one type; do not treat it as
// a hand-authored application API.
type CompositeIndex struct {
	// Columns are the column names of the tuple, in order.
	Columns []string
	// Unique is true for a unique constraint.
	Unique bool
	// Name is the explicit index name; empty means the derived
	// uniq_<table>_<columns> or idx_<table>_<columns>.
	Name string `json:",omitempty"`
}

// compositeIndexName is the effective name of ix on table: its explicit name,
// or the derived one that joins every column.
func compositeIndexName(table string, ix CompositeIndex) string {
	if ix.Name != "" {
		return ix.Name
	}
	prefix := "idx_"
	if ix.Unique {
		prefix = "uniq_"
	}
	return prefix + table + "_" + strings.Join(ix.Columns, "_")
}

// compositeIndexesOf converts the declarations of meta into column-named
// composite indexes, sorted by effective name so that the order they were
// declared in never shows in a dump or a migration. A name equal to the
// derived one is stored as empty, so spelling it out changes nothing.
func compositeIndexesOf(table string, meta model.ModelMeta) []CompositeIndex {
	if len(meta.Indexes) == 0 {
		return nil
	}
	indexes := make([]CompositeIndex, len(meta.Indexes))
	for i, declared := range meta.Indexes {
		columns := make([]string, len(declared.Fields))
		for j, field := range declared.Fields {
			columns[j] = db.ColumnName(field)
		}
		ix := CompositeIndex{Columns: columns, Unique: declared.Unique, Name: declared.Name}
		if ix.Name == compositeIndexName(table, CompositeIndex{Columns: columns, Unique: declared.Unique}) {
			ix.Name = ""
		}
		indexes[i] = ix
	}
	return sortedComposite(table, indexes)
}

// maxCompositeNameBytes is the longest index name PostgreSQL keeps whole.
const maxCompositeNameBytes = maxIndexNameBytes

// compositeProblems reports what cannot be told from one model alone or from
// its Register call: a derived name past the limit, a tuple declared twice,
// and an ordinary index that repeats a unique tuple.
func compositeProblems(models []Model) []string {
	var problems []string
	for _, m := range models {
		for i, ix := range m.Indexes {
			owner := compositeOwner(m.Name, ix)
			if name := compositeIndexName(m.Name, ix); ix.Name == "" && len(name) > maxCompositeNameBytes {
				problems = append(problems, fmt.Sprintf("%s: the derived index name %q is %d bytes, over the limit of %d; give the index an explicit name",
					owner, name, len(name), maxCompositeNameBytes))
			}
			for _, other := range m.Indexes[:i] {
				if !sameTuple(ix.Columns, other.Columns) {
					continue
				}
				if ix.Unique == other.Unique {
					problems = append(problems, owner+" is declared twice")
				} else {
					problems = append(problems, owner+": the ordinary index repeats the unique tuple, which already provides that index")
				}
			}
		}
	}
	return problems
}

func compositeOwner(table string, ix CompositeIndex) string {
	return table + "(" + strings.Join(ix.Columns, ", ") + ")"
}

func sameTuple(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// normalizeComposite stores a name equal to the derived one as empty.
func normalizeComposite(table string, ix CompositeIndex) CompositeIndex {
	if ix.Name == compositeIndexName(table, CompositeIndex{Columns: ix.Columns, Unique: ix.Unique}) {
		ix.Name = ""
	}
	return ix
}

// sortedComposite returns a copy of indexes ordered by effective name.
func sortedComposite(table string, indexes []CompositeIndex) []CompositeIndex {
	sorted := slices.Clone(indexes)
	sort.SliceStable(sorted, func(a, b int) bool {
		left, right := compositeIndexName(table, sorted[a]), compositeIndexName(table, sorted[b])
		if lower, other := strings.ToLower(left), strings.ToLower(right); lower != other {
			return lower < other
		}
		return left < right
	})
	return sorted
}

// compositeNamed is the position of the index called name, or -1.
func compositeNamed(table string, indexes []CompositeIndex, name string) int {
	for i, ix := range indexes {
		if compositeIndexName(table, ix) == name {
			return i
		}
	}
	return -1
}

// compositeContaining is the position of the first index that holds column,
// or -1.
func compositeContaining(indexes []CompositeIndex, column string) int {
	for i, ix := range indexes {
		if slices.Contains(ix.Columns, column) {
			return i
		}
	}
	return -1
}

// pinDerivedNames gives every index that carries the derived name of table
// that name explicitly. A table or column rename leaves the database's index
// names as they were, so the state keeps the physical name until a
// RenameCompositeIndex changes it.
func pinDerivedNames(table string, indexes []CompositeIndex) []CompositeIndex {
	if len(indexes) == 0 {
		return indexes
	}
	pinned := make([]CompositeIndex, len(indexes))
	for i, ix := range indexes {
		ix.Columns = slices.Clone(ix.Columns)
		ix.Name = compositeIndexName(table, ix)
		pinned[i] = ix
	}
	return pinned
}

// renameIndexColumn returns indexes with column from renamed to to. An index
// that holds it and has the derived name keeps that name as its physical one.
func renameIndexColumn(table string, indexes []CompositeIndex, from, to string) []CompositeIndex {
	if compositeContaining(indexes, from) == -1 {
		return indexes
	}
	renamed := make([]CompositeIndex, len(indexes))
	for i, ix := range indexes {
		if !slices.Contains(ix.Columns, from) {
			renamed[i] = ix
			continue
		}
		ix.Name = compositeIndexName(table, ix)
		columns := slices.Clone(ix.Columns)
		for j, c := range columns {
			if c == from {
				columns[j] = to
			}
		}
		ix.Columns = columns
		renamed[i] = normalizeComposite(table, ix)
	}
	return sortedComposite(table, renamed)
}

// validateCompositeColumns checks the tuple of a composite step against the
// table: two or more columns, each present once, and a well-formed name.
func validateCompositeColumns(table TableState, columns []string, name string) error {
	if len(columns) < 2 {
		return fmt.Errorf("tango migration: an index on table %q needs at least two columns, got %d", table.Name, len(columns))
	}
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		switch {
		case seen[column]:
			return fmt.Errorf("tango migration: column %q is repeated in an index on table %q", column, table.Name)
		case columnIndex(table.Columns, column) == -1:
			return fmt.Errorf("tango migration: column %q does not exist on table %q", column, table.Name)
		}
		seen[column] = true
	}
	if name != "" && (!indexNameRE.MatchString(name) || len(name) > maxIndexNameBytes) {
		return fmt.Errorf("tango migration: index name %q must match [A-Za-z_][A-Za-z0-9_]* and be at most %d bytes", name, maxIndexNameBytes)
	}
	return nil
}

// tableIndexNameTaken reports whether table already has an index called name,
// composite or on one column.
func tableIndexNameTaken(table TableState, name string) bool {
	if compositeNamed(table.Name, table.Indexes, name) != -1 {
		return true
	}
	for _, c := range table.Columns {
		if (c.Unique && columnUniqueName(table.Name, Column{Name: c.Name, UniqueName: c.UniqueName}) == name) ||
			(c.Indexed && columnIndexName(table.Name, Column{Name: c.Name, IndexName: c.IndexName}) == name) {
			return true
		}
	}
	return false
}

func applyCompositeCreate(state *SchemaState, s CreateCompositeIndex) error {
	table, err := requireTable(state, s.Table)
	if err != nil {
		return err
	}
	if err := validateCompositeColumns(table, s.Columns, s.Name); err != nil {
		return err
	}
	ix := normalizeComposite(s.Table, CompositeIndex{Columns: slices.Clone(s.Columns), Unique: s.Unique, Name: s.Name})
	if name := compositeIndexName(s.Table, ix); tableIndexNameTaken(table, name) {
		return fmt.Errorf("tango migration: index %q already exists on table %q", name, s.Table)
	}
	table.Indexes = sortedComposite(s.Table, append(slices.Clone(table.Indexes), ix))
	state.Tables[s.Table] = table
	return nil
}

// requireComposite finds the index a drop or rename names and checks that it
// covers the tuple the step states.
func requireComposite(table TableState, name string, columns []string, unique bool) (int, error) {
	i := compositeNamed(table.Name, table.Indexes, name)
	if i == -1 {
		return -1, fmt.Errorf("tango migration: index %q does not exist on table %q", name, table.Name)
	}
	if ix := table.Indexes[i]; ix.Unique != unique || !slices.Equal(ix.Columns, columns) {
		return -1, fmt.Errorf("tango migration: index %q on table %q does not cover columns (%s)%s",
			name, table.Name, strings.Join(columns, ", "), map[bool]string{true: ", unique", false: ""}[unique])
	}
	return i, nil
}

func applyCompositeDrop(state *SchemaState, s DropCompositeIndex) error {
	table, err := requireTable(state, s.Table)
	if err != nil {
		return err
	}
	name := compositeIndexName(s.Table, CompositeIndex{Columns: s.Columns, Unique: s.Unique, Name: s.Name})
	i, err := requireComposite(table, name, s.Columns, s.Unique)
	if err != nil {
		return err
	}
	table.Indexes = slices.Delete(slices.Clone(table.Indexes), i, i+1)
	state.Tables[s.Table] = table
	return nil
}

func applyCompositeRename(state *SchemaState, s RenameCompositeIndex) error {
	table, err := requireTable(state, s.Table)
	if err != nil {
		return err
	}
	i, err := requireComposite(table, s.From, s.Columns, s.Unique)
	if err != nil {
		return err
	}
	if err := validateCompositeColumns(table, s.Columns, s.To); err != nil {
		return err
	}
	if tableIndexNameTaken(table, s.To) {
		return fmt.Errorf("tango migration: index %q already exists on table %q", s.To, s.Table)
	}
	indexes := slices.Clone(table.Indexes)
	indexes[i].Name = s.To
	indexes[i] = normalizeComposite(s.Table, indexes[i])
	table.Indexes = sortedComposite(s.Table, indexes)
	state.Tables[s.Table] = table
	return nil
}
