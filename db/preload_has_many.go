package db

import (
	"context"
	"reflect"

	"github.com/angvp/tango/model"
)

// preloadHasMany sets each row's slice to its children and returns every
// child as one slice, grouped by row in row order. A row with no children gets
// an empty, non-nil slice, so a requested relation is distinguishable from one
// never asked for. Each row's slice is a view of the returned slice, so a
// nested read on the returned slice fills the children the rows hold.
func (s *Store) preloadHasMany(ctx context.Context, meta model.ModelMeta, rows reflect.Value, node *preloadNode) (reflect.Value, error) {
	primaryKey, err := findPrimaryKeyField(meta)
	if err != nil {
		return reflect.Value{}, &QueryError{Model: meta.Name, Reason: err.Error()}
	}
	loaded := reflect.MakeMap(reflect.MapOf(primaryKey.Type, reflect.SliceOf(node.target.Type)))
	if err := s.RelatedMany(ctx, rows.Interface(), primaryKey.Name, node.target, node.relation.ForeignKey, Query{}, loaded.Interface()); err != nil {
		return reflect.Value{}, err
	}
	total := 0
	for i := 0; i < rows.Len(); i++ {
		if group := loaded.MapIndex(rows.Index(i).FieldByName(primaryKey.Name)); group.IsValid() {
			total += group.Len()
		}
	}
	related := reflect.MakeSlice(reflect.SliceOf(node.target.Type), total, total)
	offset := 0
	for i := 0; i < rows.Len(); i++ {
		row := rows.Index(i)
		group := loaded.MapIndex(row.FieldByName(primaryKey.Name))
		n := 0
		if group.IsValid() {
			n = group.Len()
			reflect.Copy(related.Slice(offset, offset+n), group)
		}
		row.FieldByName(node.relation.Name).Set(related.Slice3(offset, offset+n, offset+n))
		offset += n
	}
	return related, nil
}
