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

// ErrInvalidForeignKey is returned by Create and Update when a set (non-zero)
// foreign key field does not reference an existing row of its related model.
// This is referential-integrity validation: a preflight SELECT, not run
// inside a shared transaction with the write it guards, and only performed
// when UseModels has been called. It is distinct from
// model.ErrUnknownForeignKeyTarget, which validates that the related *model*
// exists at all, once, at registration time.
var ErrInvalidForeignKey = errors.New("tango db: invalid foreign key")

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
	models  *model.Registry
}

// NewStore wraps sqlDB in a Store that generates SQL for dialect.
func NewStore(sqlDB *sql.DB, dialect Dialect) *Store {
	return &Store{db: sqlDB, dialect: dialect}
}

// UseModels attaches the model registry Delete needs to cascade: when set,
// deleting a row also deletes, recursively, every row of every other
// registered model that references it through a foreign key field (a
// tango:"fk=X" tag), matching Django's ORM-level cascade. This is
// application-level cascade, not a database ON DELETE CASCADE constraint.
// Without calling UseModels, Delete only removes the target row, exactly as
// before this method existed. tango.Registry.SetStore calls this
// automatically with its own Models(), so apps using the standard
// Registry/Serve flow get cascading deletes with no code change.
func (s *Store) UseModels(models *model.Registry) {
	s.models = models
}

// execer is the subset of *sql.DB / *sql.Tx that Delete's cascade needs, so
// the same query-building code runs whether or not a transaction is active.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// validateForeignKeys checks that every set (non-zero) foreign key field on
// structValue references an existing row of its related model, returning
// ErrInvalidForeignKey for the first one that doesn't. A no-op unless
// UseModels has been called. See ErrInvalidForeignKey.
func (s *Store) validateForeignKeys(ctx context.Context, meta model.ModelMeta, structValue reflect.Value) error {
	if s.models == nil {
		return nil
	}

	for _, field := range meta.Fields {
		if field.ForeignKey == "" {
			continue
		}

		fieldValue := structValue.FieldByName(field.Name)
		if !fieldValue.IsValid() || fieldValue.IsZero() {
			continue // zero-value foreign key fields are treated as unset, not a reference to PK 0
		}

		relatedMeta, ok := s.models.Get(field.ForeignKey)
		if !ok {
			continue // schema validation (Registry.ValidateForeignKeys) already covers an unregistered target
		}
		relatedPKField, err := findPrimaryKeyField(relatedMeta)
		if err != nil {
			continue
		}

		query := fmt.Sprintf(
			"SELECT 1 FROM %s WHERE %s = %s",
			ColumnName(relatedMeta.Name),
			ColumnName(relatedPKField.Name),
			placeholder(s.dialect, 1),
		)

		var exists int
		err = s.db.QueryRowContext(ctx, query, fieldValue.Interface()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s.%s references nonexistent %s %v", ErrInvalidForeignKey, meta.Name, field.Name, field.ForeignKey, fieldValue.Interface())
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// Create inserts dest using metadata-derived table and column names.
func (s *Store) Create(ctx context.Context, meta model.ModelMeta, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("tango db: Create destination must be a non-nil pointer")
	}

	structValue := value.Elem()

	if err := s.validateForeignKeys(ctx, meta, structValue); err != nil {
		return err
	}

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

	if err := s.validateForeignKeys(ctx, meta, structValue); err != nil {
		return err
	}

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

// Delete removes the row matching pk. If UseModels has been called, it first
// cascades: every row of every other registered model that references this
// one via a foreign key field is deleted first (recursively), all inside one
// transaction, before the target row itself is removed. See UseModels.
func (s *Store) Delete(ctx context.Context, meta model.ModelMeta, pk any) error {
	pkField, err := findPrimaryKeyField(meta)
	if err != nil {
		return err
	}

	if s.models == nil {
		return s.execDelete(ctx, s.db, meta, pkField, pk, true)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	visited := map[string]bool{cascadeKey(meta.Name, pk): true}
	if err := s.cascadeDependents(ctx, tx, meta, pk, visited); err != nil {
		return err
	}
	if err := s.execDelete(ctx, tx, meta, pkField, pk, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	return nil
}

// cascadeDependents deletes, recursively, every row of every other
// registered model that references (meta, pk) through a foreign key field —
// but not (meta, pk) itself, which the caller deletes once every dependent
// is gone. visited guards against circular or self-referential foreign keys
// looping forever.
func (s *Store) cascadeDependents(ctx context.Context, tx execer, meta model.ModelMeta, pk any, visited map[string]bool) error {
	for _, other := range s.models.All() {
		otherPKField, err := findPrimaryKeyField(other)
		if err != nil {
			continue // a model with no primary key can't be deleted from at all
		}

		for _, field := range other.Fields {
			if field.ForeignKey != meta.Name {
				continue
			}

			childPKs, err := s.referencingPrimaryKeys(ctx, tx, other, otherPKField, field, pk)
			if err != nil {
				return err
			}

			for _, childPK := range childPKs {
				key := cascadeKey(other.Name, childPK)
				if visited[key] {
					continue
				}
				visited[key] = true

				if err := s.cascadeDependents(ctx, tx, other, childPK, visited); err != nil {
					return err
				}
				if err := s.execDelete(ctx, tx, other, otherPKField, childPK, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// referencingPrimaryKeys returns the primary key of every row in model other
// whose foreign key field equals pk.
func (s *Store) referencingPrimaryKeys(ctx context.Context, tx execer, other model.ModelMeta, otherPKField model.FieldMeta, fkField model.FieldMeta, pk any) ([]any, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s",
		ColumnName(otherPKField.Name),
		ColumnName(other.Name),
		ColumnName(fkField.Name),
		placeholder(s.dialect, 1),
	)
	rows, err := tx.QueryContext(ctx, query, pk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pks []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		pks = append(pks, v)
	}
	return pks, rows.Err()
}

// execDelete issues one DELETE statement via exec (either s.db or an active
// transaction). When checkAffected is true, it returns ErrNotFound if no row
// matched — used only for the caller-specified target row; cascaded rows
// are known to exist (found by referencingPrimaryKeys) so skip the check.
func (s *Store) execDelete(ctx context.Context, exec execer, meta model.ModelMeta, pkField model.FieldMeta, pk any, checkAffected bool) error {
	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = %s",
		ColumnName(meta.Name),
		ColumnName(pkField.Name),
		placeholder(s.dialect, 1),
	)

	result, err := exec.ExecContext(ctx, query, pk)
	if err != nil {
		return err
	}
	if !checkAffected {
		return nil
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

func cascadeKey(modelName string, pk any) string {
	return modelName + ":" + fmt.Sprint(pk)
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
// column against each exported field's Go name and its ColumnName-derived
// snake_case form, both case-insensitively — so a raw query selecting
// tanGO's own generated column names (e.g. session_key) matches the
// corresponding Go field (SessionKey) without requiring an "AS FieldName"
// alias, while a query that already aliases to (or happens to match) the
// bare Go field name keeps working exactly as before.
func findFieldByColumn(structValue reflect.Value, structType reflect.Type, column string) (reflect.Value, error) {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		if strings.EqualFold(field.Name, column) || strings.EqualFold(ColumnName(field.Name), column) {
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
