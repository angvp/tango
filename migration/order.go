package migration

import (
	"fmt"
	"sort"
	"strings"
)

// applyOrder returns migrations in the order ApplyPending runs them: by Name
// then App, except that a migration referencing another app's table (a
// CreateTable or AddColumn column with References) runs after the other
// app's migration that creates that table, and each app's own migrations
// keep their Name order. This makes a cross-app foreign key independent of
// app names and InstalledApps order. It fails, naming every migration
// involved, when the cross-app foreign keys form a cycle.
func applyOrder(migrations []Migration) ([]Migration, error) {
	base := append([]Migration(nil), migrations...)
	sort.SliceStable(base, func(i, j int) bool {
		if base[i].Name == base[j].Name {
			return base[i].App < base[j].App
		}
		return base[i].Name < base[j].Name
	})

	order, stuck := dependencyOrder(migrationDependencies(base))
	if len(stuck) > 0 {
		return nil, orderCycleError(base, stuck)
	}
	ordered := make([]Migration, len(order))
	for i, index := range order {
		ordered[i] = base[index]
	}
	return ordered, nil
}

// dependencyOrder is the one ordering primitive behind both ApplyPending's
// migration order and Diff's table order. It orders the indexes of deps so
// that each comes after every index deps lists for it, and otherwise keeps
// index order: each step takes the lowest index whose dependencies are all
// placed. When the indexes left all wait on a cycle, none is ready; it
// stops there and returns them, in index order, as stuck.
func dependencyOrder(deps [][]int) (order, stuck []int) {
	done := make([]bool, len(deps))
	for len(order) < len(deps) {
		next := firstReady(deps, done)
		if next < 0 {
			for i := range deps {
				if !done[i] {
					stuck = append(stuck, i)
				}
			}
			return order, stuck
		}
		done[next] = true
		order = append(order, next)
	}
	return order, nil
}

// migrationDependencies returns, for each migration in base, the indexes of
// the migrations that must run before it: the previous migration of the
// same app, and every other app's migration creating a table it references.
func migrationDependencies(base []Migration) [][]int {
	creators := make(map[string][]int)
	for i, m := range base {
		for _, step := range m.Up {
			switch s := step.(type) {
			case CreateTable:
				creators[s.Table] = append(creators[s.Table], i)
			case RenameTable:
				// After a rename, the table exists under its new name only
				// once this migration has run.
				creators[s.To] = append(creators[s.To], i)
			}
		}
	}

	deps := make([][]int, len(base))
	previousOfApp := make(map[string]int)
	for i, m := range base {
		if prev, ok := previousOfApp[m.App]; ok {
			deps[i] = append(deps[i], prev)
		}
		previousOfApp[m.App] = i
		for _, table := range referencedTables(m) {
			for _, creator := range creators[table] {
				if base[creator].App != m.App {
					deps[i] = append(deps[i], creator)
				}
			}
		}
	}
	return deps
}

// referencedTables lists the tables a migration's Up steps add foreign keys
// to.
func referencedTables(m Migration) []string {
	var tables []string
	for _, step := range m.Up {
		switch s := step.(type) {
		case CreateTable:
			tables = append(tables, columnReferences(s.Columns)...)
		case AddColumn:
			if s.Column.References != "" {
				tables = append(tables, s.Column.References)
			}
		}
	}
	return tables
}

// firstReady returns the first not-yet-done index whose dependencies are all
// done, or -1 if there is none.
func firstReady(deps [][]int, done []bool) int {
	for i := range deps {
		if done[i] {
			continue
		}
		ready := true
		for _, dep := range deps[i] {
			if !done[dep] {
				ready = false
				break
			}
		}
		if ready {
			return i
		}
	}
	return -1
}

func orderCycleError(base []Migration, stuck []int) error {
	names := make([]string, len(stuck))
	for i, index := range stuck {
		names[i] = fmt.Sprintf("%s/%s", base[index].App, base[index].Name)
	}
	return fmt.Errorf("tango migration: cannot order migrations: foreign keys between apps form a cycle among %s; "+
		"move one of the foreign keys into a later migration of its app", strings.Join(names, ", "))
}

// stepOrder returns steps, one of m's step lists, in the order ApplyPending
// and RollbackLast run them: as written, except that within each run of
// consecutive CreateTable steps a table comes after the tables it
// references, and within each run of consecutive DropTable steps a table
// comes before the tables it references. PostgreSQL refuses a REFERENCES
// clause naming a table that doesn't exist yet, and refuses to drop a table
// another table still references. Today's generator already writes steps in
// this order; releases before v0.1.0 wrote them in table-name order, and
// their files must keep applying and rolling back. Which table references
// which comes from m's own CreateTable and AddColumn steps, Up and Down.
func stepOrder(m Migration, steps []Step) []Step {
	references := make(map[string][]string)
	for _, step := range append(append([]Step(nil), m.Up...), m.Down...) {
		switch s := step.(type) {
		case CreateTable:
			references[s.Table] = append(references[s.Table], columnReferences(s.Columns)...)
		case AddColumn:
			if s.Column.References != "" {
				references[s.Table] = append(references[s.Table], s.Column.References)
			}
		}
	}

	ordered := make([]Step, 0, len(steps))
	for start := 0; start < len(steps); {
		end := start + 1
		for end < len(steps) && sameTableStepKind(steps[start], steps[end]) {
			end++
		}
		ordered = append(ordered, tableRunOrder(steps[start:end], references)...)
		start = end
	}
	return ordered
}

// sameTableStepKind reports whether a and b are both CreateTable or both
// DropTable steps, so they belong to one run stepOrder may reorder.
func sameTableStepKind(a, b Step) bool {
	switch a.(type) {
	case CreateTable:
		_, ok := b.(CreateTable)
		return ok
	case DropTable:
		_, ok := b.(DropTable)
		return ok
	}
	return false
}

// tableRunOrder orders one run of CreateTable steps referenced-first, or one
// run of DropTable steps referencing-first. Any other single step, and any
// tables whose foreign keys form a cycle, keep their written order.
func tableRunOrder(run []Step, references map[string][]string) []Step {
	if len(run) < 2 {
		return run
	}
	_, creating := run[0].(CreateTable)
	index := make(map[string]int, len(run))
	for i, step := range run {
		index[stepTable(step)] = i
	}
	deps := make([][]int, len(run))
	for i, step := range run {
		for _, referenced := range references[stepTable(step)] {
			j, ok := index[referenced]
			if !ok || j == i {
				continue
			}
			if creating {
				deps[i] = append(deps[i], j) // create the referenced table first
			} else {
				deps[j] = append(deps[j], i) // drop the referencing table first
			}
		}
	}
	order, stuck := dependencyOrder(deps)
	ordered := make([]Step, 0, len(run))
	for _, i := range append(order, stuck...) {
		ordered = append(ordered, run[i])
	}
	return ordered
}

// stepTable is the table a CreateTable or DropTable step names.
func stepTable(step Step) string {
	switch s := step.(type) {
	case CreateTable:
		return s.Table
	case DropTable:
		return s.Table
	}
	return ""
}
