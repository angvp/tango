package db

import (
	"context"
	"reflect"

	"github.com/angvp/tango/model"
)

// preloadHasMany sets each row's slice to its children. A row with no children
// gets an empty, non-nil slice, so a requested relation is distinguishable
// from one never asked for.
func (s *Store) preloadHasMany(ctx context.Context, meta model.ModelMeta, rows reflect.Value, plan preloadPlan) error {
	primaryKey, err := findPrimaryKeyField(meta)
	if err != nil {
		return &QueryError{Model: meta.Name, Reason: err.Error()}
	}
	loaded := reflect.MakeMap(reflect.MapOf(primaryKey.Type, reflect.SliceOf(plan.target.Type)))
	if err := s.RelatedMany(ctx, rows.Interface(), primaryKey.Name, plan.target, plan.relation.ForeignKey, Query{}, loaded.Interface()); err != nil {
		return err
	}
	for i := 0; i < rows.Len(); i++ {
		row := rows.Index(i)
		children := loaded.MapIndex(row.FieldByName(primaryKey.Name))
		if !children.IsValid() {
			children = reflect.MakeSlice(reflect.SliceOf(plan.target.Type), 0, 0)
		}
		row.FieldByName(plan.relation.Name).Set(children)
	}
	return nil
}
