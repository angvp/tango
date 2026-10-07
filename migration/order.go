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

	deps := migrationDependencies(base)
	ordered := make([]Migration, 0, len(base))
	done := make([]bool, len(base))
	for len(ordered) < len(base) {
		next := firstReady(deps, done)
		if next < 0 {
			return nil, orderCycleError(base, done)
		}
		done[next] = true
		ordered = append(ordered, base[next])
	}
	return ordered, nil
}

// migrationDependencies returns, for each migration in base, the indexes of
// the migrations that must run before it: the previous migration of the
// same app, and every other app's migration creating a table it references.
func migrationDependencies(base []Migration) [][]int {
	creators := make(map[string][]int)
	for i, m := range base {
		for _, step := range m.Up {
			if create, ok := step.(CreateTable); ok {
				creators[create.Table] = append(creators[create.Table], i)
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

func orderCycleError(base []Migration, done []bool) error {
	var stuck []string
	for i, m := range base {
		if !done[i] {
			stuck = append(stuck, fmt.Sprintf("%s/%s", m.App, m.Name))
		}
	}
	return fmt.Errorf("tango migration: cannot order migrations: foreign keys between apps form a cycle among %s; "+
		"move one of the foreign keys into a later migration of its app", strings.Join(stuck, ", "))
}
