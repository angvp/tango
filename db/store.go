package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/angvp/tango/model"
)

// ErrNotFound is returned by Get, Update, and Delete when no row matches
// the given primary key.
var ErrNotFound = errors.New("tango db: not found")

// Query carries pagination and ordering options for List.
type Query struct {
	Limit   int
	Offset  int
	OrderBy []string
}

// Store is tanGO's persistence boundary over a database/sql connection.
type Store struct {
	db      *sql.DB
	dialect Dialect
}

// NewStore wraps sqlDB in a Store that generates SQL for dialect.
func NewStore(sqlDB *sql.DB, dialect Dialect) *Store {
	return &Store{db: sqlDB, dialect: dialect}
}

// Create inserts dest using metadata-derived table and column names.
func (s *Store) Create(ctx context.Context, meta model.ModelMeta, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("tango db: Create destination must be a non-nil pointer")
	}

	structValue := value.Elem()
	tableName := ColumnName(meta.Name)

	var primaryKeyField model.FieldMeta
	var primaryKeyValue reflect.Value
	var columns []string
	var placeholders []string
	var args []any

	for _, field := range meta.Fields {
		fieldValue := structValue.FieldByName(field.Name)
		if !fieldValue.IsValid() {
			return fmt.Errorf("tango db: field %q not found on %s", field.Name, meta.Name)
		}

		if field.PrimaryKey {
			primaryKeyField = field
			primaryKeyValue = fieldValue
			if fieldValue.IsZero() {
				continue
			}
		}

		columns = append(columns, ColumnName(field.Name))
		placeholders = append(placeholders, placeholder(s.dialect, len(placeholders)+1))
		args = append(args, fieldValue.Interface())
	}

	needsBackfill := primaryKeyField.Name != "" && primaryKeyValue.IsZero()

	if s.dialect == Postgres && needsBackfill {
		query := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
			tableName,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
			ColumnName(primaryKeyField.Name),
		)

		var id int64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return err
		}

		return setPrimaryKeyValue(primaryKeyValue, id)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	if !needsBackfill {
		return nil
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	return setPrimaryKeyValue(primaryKeyValue, id)
}

// Get selects the row matching pk and scans it into dest.
func (s *Store) Get(ctx context.Context, meta model.ModelMeta, pk any, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("tango db: Get destination must be a non-nil pointer")
	}
	structValue := value.Elem()

	primaryKeyField, err := findPrimaryKeyField(meta)
	if err != nil {
		return err
	}

	columns := make([]string, len(meta.Fields))
	for i, field := range meta.Fields {
		columns[i] = ColumnName(field.Name)
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s",
		strings.Join(columns, ", "),
		ColumnName(meta.Name),
		ColumnName(primaryKeyField.Name),
		placeholder(s.dialect, 1),
	)

	row := s.db.QueryRowContext(ctx, query, pk)
	if err := scanFieldsInto(row, structValue, meta.Fields); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %v", ErrNotFound, pk)
		}
		return err
	}

	return nil
}

// Update writes dest's current field values to the row matching its
// primary key.
func (s *Store) Update(ctx context.Context, meta model.ModelMeta, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("tango db: Update destination must be a non-nil pointer")
	}
	structValue := value.Elem()

	primaryKeyField, err := findPrimaryKeyField(meta)
	if err != nil {
		return err
	}
	primaryKeyValue := structValue.FieldByName(primaryKeyField.Name).Interface()

	var assignments []string
	var args []any
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			continue
		}
		fieldValue := structValue.FieldByName(field.Name)
		if !fieldValue.IsValid() {
			return fmt.Errorf("tango db: field %q not found on %s", field.Name, meta.Name)
		}
		assignments = append(assignments, ColumnName(field.Name)+" = "+placeholder(s.dialect, len(assignments)+1))
		args = append(args, fieldValue.Interface())
	}
	args = append(args, primaryKeyValue)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = %s",
		ColumnName(meta.Name),
		strings.Join(assignments, ", "),
		ColumnName(primaryKeyField.Name),
		placeholder(s.dialect, len(assignments)+1),
	)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: %v", ErrNotFound, primaryKeyValue)
	}

	return nil
}

// Delete removes the row matching pk.
func (s *Store) Delete(ctx context.Context, meta model.ModelMeta, pk any) error {
	primaryKeyField, err := findPrimaryKeyField(meta)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = %s",
		ColumnName(meta.Name),
		ColumnName(primaryKeyField.Name),
		placeholder(s.dialect, 1),
	)

	result, err := s.db.ExecContext(ctx, query, pk)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: %v", ErrNotFound, pk)
	}

	return nil
}

// List selects rows into dest (a pointer to a slice of the model struct),
// applying query's Limit, Offset, and OrderBy.
func (s *Store) List(ctx context.Context, meta model.ModelMeta, query Query, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("tango db: List destination must be a non-nil pointer to a slice")
	}
	sliceValue := value.Elem()
	elementType := sliceValue.Type().Elem()

	columns := make([]string, len(meta.Fields))
	for i, field := range meta.Fields {
		columns[i] = ColumnName(field.Name)
	}

	sqlQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), ColumnName(meta.Name))

	if len(query.OrderBy) > 0 {
		orderClauses := make([]string, len(query.OrderBy))
		for i, entry := range query.OrderBy {
			fieldName := entry
			direction := "ASC"
			if strings.HasPrefix(entry, "-") {
				fieldName = entry[1:]
				direction = "DESC"
			}

			if !hasField(meta, fieldName) {
				return fmt.Errorf("tango db: unknown OrderBy field %q for model %s", fieldName, meta.Name)
			}

			orderClauses[i] = ColumnName(fieldName) + " " + direction
		}
		sqlQuery += " ORDER BY " + strings.Join(orderClauses, ", ")
	}

	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	if query.Offset > 0 {
		sqlQuery += fmt.Sprintf(" OFFSET %d", query.Offset)
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	result := reflect.MakeSlice(sliceValue.Type(), 0, 0)
	for rows.Next() {
		element := reflect.New(elementType).Elem()
		if err := scanFieldsInto(rows, element, meta.Fields); err != nil {
			return err
		}
		result = reflect.Append(result, element)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	sliceValue.Set(result)

	return nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanFieldsInto scans one row's columns, in meta.Fields order, into the
// corresponding fields of structValue.
func scanFieldsInto(scanner rowScanner, structValue reflect.Value, fields []model.FieldMeta) error {
	pointers := make([]any, len(fields))
	for i, field := range fields {
		fieldValue := structValue.FieldByName(field.Name)
		if !fieldValue.IsValid() {
			return fmt.Errorf("tango db: field %q not found on %s", field.Name, structValue.Type())
		}
		pointers[i] = fieldValue.Addr().Interface()
	}

	return scanner.Scan(pointers...)
}

// findPrimaryKeyField returns the FieldMeta flagged PrimaryKey in meta.
func findPrimaryKeyField(meta model.ModelMeta) (model.FieldMeta, error) {
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			return field, nil
		}
	}
	return model.FieldMeta{}, fmt.Errorf("tango db: model %s has no primary key field", meta.Name)
}

// hasField reports whether meta has a field with the given Go name.
func hasField(meta model.ModelMeta, name string) bool {
	for _, field := range meta.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func setPrimaryKeyValue(value reflect.Value, id int64) error {
	if !value.CanSet() {
		return fmt.Errorf("tango db: primary key field cannot be set")
	}

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.OverflowInt(id) {
			return fmt.Errorf("tango db: generated id %d overflows %s", id, value.Type())
		}
		value.SetInt(id)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if id < 0 || value.OverflowUint(uint64(id)) {
			return fmt.Errorf("tango db: generated id %d overflows %s", id, value.Type())
		}
		value.SetUint(uint64(id))
	case reflect.String:
		value.SetString(strconv.FormatInt(id, 10))
	default:
		return fmt.Errorf("tango db: cannot backfill generated id into %s", value.Type())
	}

	return nil
}

// QueryRow executes sql with args and scans the single resulting row into
// dest (a pointer to a struct), matching returned columns to struct fields
// by name (case-insensitively). It returns an error wrapping ErrNotFound
// when the query yields no rows.
//
// sqlQuery must use the placeholder syntax matching this Store's configured
// Dialect ("?" for SQLite, "$1", "$2", ... for Postgres) — QueryRow does not
// translate or validate placeholder syntax between dialects.
func (s *Store) QueryRow(ctx context.Context, dest any, sqlQuery string, args ...any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("tango db: QueryRow destination must be a non-nil pointer")
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%w: query returned no rows", ErrNotFound)
	}

	if err := scanColumnsInto(rows, value.Elem()); err != nil {
		return err
	}

	return rows.Err()
}

// Query executes sql with args and appends one scanned element per
// resulting row into dest (a pointer to a slice of structs), matching
// returned columns to struct fields by name (case-insensitively). A query
// matching no rows leaves dest an empty, non-nil slice with a nil error.
//
// sqlQuery must use the placeholder syntax matching this Store's configured
// Dialect ("?" for SQLite, "$1", "$2", ... for Postgres) — Query does not
// translate or validate placeholder syntax between dialects.
func (s *Store) Query(ctx context.Context, dest any, sqlQuery string, args ...any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("tango db: Query destination must be a non-nil pointer to a slice")
	}
	sliceValue := value.Elem()
	elementType := sliceValue.Type().Elem()

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	result := reflect.MakeSlice(sliceValue.Type(), 0, 0)
	for rows.Next() {
		element := reflect.New(elementType).Elem()
		if err := scanColumnsInto(rows, element); err != nil {
			return err
		}
		result = reflect.Append(result, element)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	sliceValue.Set(result)

	return nil
}

// scanColumnsInto scans one row of rows into structValue, matching each
// returned column name to a struct field by name (case-insensitively).
// It returns an error if a column has no corresponding field.
func scanColumnsInto(rows *sql.Rows, structValue reflect.Value) error {
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	structType := structValue.Type()
	pointers := make([]any, len(columns))
	for i, column := range columns {
		fieldValue, err := findFieldByColumn(structValue, structType, column)
		if err != nil {
			return err
		}
		pointers[i] = fieldValue.Addr().Interface()
	}

	return rows.Scan(pointers...)
}

// findFieldByColumn locates the struct field matching column, comparing
// names case-insensitively.
func findFieldByColumn(structValue reflect.Value, structType reflect.Type, column string) (reflect.Value, error) {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.IsExported() && strings.EqualFold(field.Name, column) {
			return structValue.Field(i), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("tango db: column %q has no corresponding field on %s", column, structType)
}

// ColumnName derives the database table or column name for a Go identifier.
func ColumnName(name string) string {
	var builder strings.Builder
	runes := []rune(name)

	for index, r := range runes {
		if unicode.IsUpper(r) {
			if index > 0 && shouldInsertUnderscore(runes, index) {
				builder.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		builder.WriteRune(r)
	}

	return builder.String()
}

func shouldInsertUnderscore(runes []rune, index int) bool {
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}

	if index+1 < len(runes) && unicode.IsLower(runes[index+1]) {
		return true
	}

	return false
}
