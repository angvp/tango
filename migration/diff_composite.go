package migration

import (
	"fmt"
	"slices"
	"strings"
)

// stepPair is a step and its inverse; Diff records an Up step's inverse in
// Down as it goes.
type stepPair struct{ up, down Step }

// compositePlan is what it takes to bring a table's composite indexes to the
// declared ones: indexes to drop, to rename and to create. They are applied
// in that order, with the table's column changes after the drops, which frees
// the columns and the names, and before the creates, which need the columns.
type compositePlan struct {
	drops   []stepPair
	renames []stepPair
	creates []stepPair
}

func compositeKey(ix CompositeIndex) string {
	return fmt.Sprintf("%t:%s", ix.Unique, strings.Join(ix.Columns, ","))
}

// planComposite compares the declared indexes of table with the existing ones
// (already carrying the column and table renames of this migration). Two
// indexes are the same when they have the same uniqueness and ordered columns;
// when their names differ the existing one is renamed. problems lists renames
// and creates that need a name another index still holds, which no order of
// steps can resolve in one migration.
func planComposite(table string, desired, existing []CompositeIndex) (plan compositePlan, problems []string) {
	desired = sortedComposite(table, desired)
	existingByKey := make(map[string]CompositeIndex, len(existing))
	for _, ix := range existing {
		existingByKey[compositeKey(ix)] = ix
	}
	desiredKeys := make(map[string]bool, len(desired))
	for _, ix := range desired {
		desiredKeys[compositeKey(ix)] = true
	}

	remaining := make(map[string]bool, len(existing))
	for _, ix := range sortedComposite(table, existing) {
		if desiredKeys[compositeKey(ix)] {
			remaining[compositeIndexName(table, ix)] = true
			continue
		}
		plan.drops = append(plan.drops, stepPair{
			up:   DropCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, Name: ix.Name},
			down: CreateCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, Name: ix.Name},
		})
	}

	for _, ix := range desired {
		have, found := existingByKey[compositeKey(ix)]
		name := compositeIndexName(table, ix)
		if !found {
			if remaining[name] {
				problems = append(problems, nameInUse(table, name))
			}
			plan.creates = append(plan.creates, stepPair{
				up:   CreateCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, Name: ix.Name},
				down: DropCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, Name: ix.Name},
			})
			continue
		}
		from := compositeIndexName(table, have)
		if from == name {
			continue
		}
		if remaining[name] {
			problems = append(problems, nameInUse(table, name))
		}
		plan.renames = append(plan.renames, stepPair{
			up:   RenameCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, From: from, To: name},
			down: RenameCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, From: name, To: from},
		})
	}
	return plan, problems
}

func nameInUse(table, name string) string {
	return fmt.Sprintf("table %s: the index name %q is the current name of one composite index and the new name of another; give the indexes distinct names in two migrations", table, name)
}

func appendPairs(m *Migration, pairs []stepPair) {
	for _, p := range pairs {
		m.Up = append(m.Up, p.up)
		m.Down = append(m.Down, p.down)
	}
}

func (p compositePlan) emitDrops(m *Migration)   { appendPairs(m, p.drops) }
func (p compositePlan) emitRenames(m *Migration) { appendPairs(m, p.renames) }
func (p compositePlan) emitCreates(m *Migration) { appendPairs(m, p.creates) }

// newTableComposite is the steps that create the declared indexes of a table
// the migration creates. They need no inverse: dropping the table drops them.
func newTableComposite(table string, declared []CompositeIndex) []Step {
	var steps []Step
	for _, ix := range sortedComposite(table, declared) {
		steps = append(steps, CreateCompositeIndex{Table: table, Columns: slices.Clone(ix.Columns), Unique: ix.Unique, Name: ix.Name})
	}
	return steps
}

// renameIndexColumns applies this migration's column renames of table to its
// composite indexes, as applyRenames does to its columns.
func renameIndexColumns(table string, indexes []CompositeIndex, renames []Rename) []CompositeIndex {
	for _, r := range renames {
		if r.Table == table {
			indexes = renameIndexColumn(table, indexes, r.Column, r.To)
		}
	}
	return indexes
}
