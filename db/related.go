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
	keyIndex, err := batchKeyField(related.Name, rowsValue.Type().Elem(), keyField, primaryKey.Type)
	if err != nil {
		return err
	}
	destValue, err := batchDest(related.Name, dest, primaryKey.Type, related.Type)
	if err != nil {
		return err
	}

	loaded := reflect.MakeMap(destValue.Type())
	for _, chunk := range keyChunks(distinctKeys(rowsValue, keyIndex), primaryKey.Type) {
		query := Query{Where: []Condition{{Field: primaryKey.Name, Op: OpIn, Value: chunk.Interface()}}}
		found := reflect.New(reflect.SliceOf(related.Type))
		if err := s.List(ctx, related, query, found.Interface()); err != nil {
			return err
		}
		for i := 0; i < found.Elem().Len(); i++ {
			row := found.Elem().Index(i)
			loaded.SetMapIndex(row.FieldByName(primaryKey.Name), row)
		}
	}

	for _, key := range loaded.MapKeys() {
		destValue.SetMapIndex(key, loaded.MapIndex(key))
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

// batchKeyField returns the index of the direct, exported field name on the
// row type, checking that its type is keyType.
func batchKeyField(modelName string, rowType reflect.Type, name string, keyType reflect.Type) (int, error) {
	field, ok := rowType.FieldByName(name)
	if !ok || len(field.Index) != 1 {
		return 0, &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("key field %q is not a direct field of %s", name, rowType)}
	}
	if !field.IsExported() {
		return 0, &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("key field %q of %s is not exported", name, rowType)}
	}
	if field.Type != keyType {
		return 0, &QueryError{Model: modelName, Field: name, Reason: fmt.Sprintf("key field %q has type %s, want %s", name, field.Type, keyType)}
	}
	return field.Index[0], nil
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
