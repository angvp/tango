package migration

import (
	"fmt"
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
	sort.SliceStable(indexes, func(a, b int) bool {
		left, right := compositeIndexName(table, indexes[a]), compositeIndexName(table, indexes[b])
		if lower, other := strings.ToLower(left), strings.ToLower(right); lower != other {
			return lower < other
		}
		return left < right
	})
	return indexes
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
