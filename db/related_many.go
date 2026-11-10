package db

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/angvp/tango/model"
)

// RelatedMany loads, in one statement per MaxInValues distinct keys, the child
// rows whose childField holds each key that keyField of rows carries, and
// stores them in dest, a non-nil map the caller allocated, as one slice per
// key. keyField must be an exported field of rows' struct type, and childField
// a field of child whose type equals it.
//
// query's Where, Any, Filter and OrderBy narrow and order the children; query
// is never modified. Without OrderBy the children of each key come in child
// primary-key order, ascending. Limit and Offset are refused, because they would
// cut across parents. A key with no children adds no entry. dest is written only
// after every statement has succeeded, so on any error it is left as passed.
// RelatedMany reads through the transaction when called on a Store bound to one.
func (s *Store) RelatedMany(ctx context.Context, rows any, keyField string, child model.ModelMeta, childField string, query Query, dest any) error {
	if query.Limit != 0 || query.Offset != 0 {
		return &QueryError{Model: child.Name, Reason: "Limit and Offset are not supported by RelatedMany: they would cut across parents"}
	}
	primaryKey, err := findPrimaryKeyField(child)
	if err != nil {
		return &QueryError{Model: child.Name, Reason: err.Error()}
	}
	rowsValue, err := batchRows(child.Name, rows)
	if err != nil {
		return err
	}
	keyIndex, keyType, err := batchKeyField(child.Name, rowsValue.Type().Elem(), keyField)
	if err != nil {
		return err
	}
	foreignKey, err := fieldNamed(child, childField)
	if err != nil {
		return err
	}
	if err := sameFieldType(child.Name, childField, foreignKey.Type, keyType); err != nil {
		return err
	}
	destValue, err := batchDest(child.Name, dest, keyType, reflect.SliceOf(child.Type))
	if err != nil {
		return err
	}

	if len(query.OrderBy) == 0 {
		query.OrderBy = []string{primaryKey.Name} // query is a copy; the caller's slice is untouched
	}
	loaded := reflect.MakeMap(destValue.Type())
	for _, chunk := range keyChunks(distinctKeys(rowsValue, keyIndex), keyType) {
		chunkQuery := query
		chunkQuery.Where = slices.Concat(query.Where, []Condition{{Field: foreignKey.Name, Op: OpIn, Value: chunk.Interface()}})
		found := reflect.New(reflect.SliceOf(child.Type))
		if err := s.List(ctx, child, chunkQuery, found.Interface()); err != nil {
			return err
		}
		for i := 0; i < found.Elem().Len(); i++ {
			row := found.Elem().Index(i)
			key := row.FieldByName(foreignKey.Name)
			group := loaded.MapIndex(key)
			if !group.IsValid() {
				group = reflect.MakeSlice(reflect.SliceOf(child.Type), 0, 0)
			}
			loaded.SetMapIndex(key, reflect.Append(group, row))
		}
	}

	for _, key := range loaded.MapKeys() {
		destValue.SetMapIndex(key, loaded.MapIndex(key))
	}
	return nil
}

// fieldNamed returns the metadata of the field name on meta, or a QueryError
// when the model has no such field.
func fieldNamed(meta model.ModelMeta, name string) (model.FieldMeta, error) {
	for _, field := range meta.Fields {
		if field.Name == name {
			return field, nil
		}
	}
	return model.FieldMeta{}, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("unknown field %q for model %s", name, meta.Name)}
}
