package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/angvp/tango/model"
)

// Related loads the rows of related that rows point at through keyField, in
// one statement per MaxInValues distinct keys, and stores each row in dest
// under its primary key. keyField must be an exported field of rows' struct
// type whose type equals related's primary key type. Zero keys are never
// sent, and a key with no related row adds no entry. dest, a non-nil map the
// caller allocated, is written only after every statement has succeeded, so on
// any error it is left as passed. Related reads through the transaction when
// called on a Store bound to one.
func (s *Store) Related(ctx context.Context, rows any, keyField string, related model.ModelMeta, dest any) error {
	primaryKey, err := findPrimaryKeyField(related)
	if err != nil {
		return &QueryError{Model: related.Name, Reason: err.Error()}
	}
	rowsValue, err := batchRows(related.Name, rows)
	if err != nil {
		return err
	}
	keyIndex, keyType, err := batchKeyField(related.Name, rowsValue.Type().Elem(), keyField)
	if err != nil {
		return err
	}
	if err := sameFieldType(related.Name, keyField, keyType, primaryKey.Type); err != nil {
		return err
	}
	destValue, err := batchDest(related.Name, dest, primaryKey.Type, related.Type)
	if err != nil {
		return err
	}

	loaded := reflect.MakeMap(destValue.Type())
	err = s.listChunks(ctx, related, distinctKeys(rowsValue, keyIndex), primaryKey.Type, func(chunk reflect.Value) Query {
		return Query{Where: []Condition{{Field: primaryKey.Name, Op: OpIn, Value: chunk.Interface()}}}
	}, func(row reflect.Value) {
		loaded.SetMapIndex(row.FieldByName(primaryKey.Name), row)
	})
	if err != nil {
		return err
	}

	for _, key := range loaded.MapKeys() {
		destValue.SetMapIndex(key, loaded.MapIndex(key))
	}
	return nil
}

// listChunks runs one List of meta per chunk of at most MaxInValues keys, in
// order. queryFor builds the query of a chunk, and visit receives every row
// each chunk returns. The first failed List stops the loop and is returned.
func (s *Store) listChunks(ctx context.Context, meta model.ModelMeta, keys []reflect.Value, keyType reflect.Type, queryFor func(chunk reflect.Value) Query, visit func(row reflect.Value)) error {
	for _, chunk := range keyChunks(keys, keyType) {
		found := reflect.New(reflect.SliceOf(meta.Type))
		if err := s.List(ctx, meta, queryFor(chunk), found.Interface()); err != nil {
			return err
		}
		for i := 0; i < found.Elem().Len(); i++ {
			visit(found.Elem().Index(i))
		}
	}
	return nil
}

// batchRows checks that rows is a slice of structs, or a non-nil pointer to
// one, and returns the slice. An empty or nil slice is valid.
func batchRows(modelName string, rows any) (reflect.Value, error) {
	value := reflect.ValueOf(rows)
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.Struct {
		return reflect.Value{}, &QueryError{Model: modelName, Reason: fmt.Sprintf("rows must be a slice of structs or a pointer to one, got %T", rows)}
	}
	return value, nil
}

// batchKeyField returns the index and type of the direct, exported field name
// on the row type.
func batchKeyField(modelName string, rowType reflect.Type, name string) (int, reflect.Type, error) {
	field, ok := rowType.FieldByName(name)
	if !ok || len(field.Index) != 1 {
		return 0, nil, &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("key field %q is not a direct field of %s", name, rowType)}
	}
	if !field.IsExported() {
		return 0, nil, &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("key field %q of %s is not exported", name, rowType)}
	}
	return field.Index[0], field.Type, nil
}

// sameFieldType reports a QueryError unless got is want.
func sameFieldType(modelName, name string, got, want reflect.Type) error {
	if got != want {
		return &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("field %q has type %s, want %s", name, got, want)}
	}
	return nil
}

// batchDest checks that dest is a non-nil map from keyType to valueType and
// returns it.
func batchDest(modelName string, dest any, keyType, valueType reflect.Type) (reflect.Value, error) {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Map || value.IsNil() || value.Type().Key() != keyType || value.Type().Elem() != valueType {
		return reflect.Value{}, &QueryError{Model: modelName, Reason: fmt.Sprintf("destination must be a non-nil map[%s]%s, got %T", keyType, valueType, dest)}
	}
	return value, nil
}

// distinctKeys returns the non-zero values of the key field at index across
// rows, each once, in order of first appearance.
func distinctKeys(rows reflect.Value, index int) []reflect.Value {
	seen := make(map[any]bool)
	var keys []reflect.Value
	for i := 0; i < rows.Len(); i++ {
		key := rows.Index(i).Field(index)
		if key.IsZero() || seen[key.Interface()] {
			continue
		}
		seen[key.Interface()] = true
		keys = append(keys, key)
	}
	return keys
}

// keyChunks splits keys into slices of keyType, each at most MaxInValues long,
// in order.
func keyChunks(keys []reflect.Value, keyType reflect.Type) []reflect.Value {
	var chunks []reflect.Value
	for start := 0; start < len(keys); start += MaxInValues {
		end := min(start+MaxInValues, len(keys))
		chunk := reflect.MakeSlice(reflect.SliceOf(keyType), 0, end-start)
		chunks = append(chunks, reflect.Append(chunk, keys[start:end]...))
	}
	return chunks
}
