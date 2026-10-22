package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/angvp/tango/model"
)

// First reads the first row matching query into dest, a non-nil pointer to a
// struct of the model. With no OrderBy it orders by the primary key,
// ascending, so the result is deterministic; it always reads one row, honours
// Offset and ignores Limit. It returns an error matching ErrNotFound (naming
// the model) when no row matches. dest is left untouched on any error. The
// caller's query is never modified.
func (s *Store) First(ctx context.Context, meta model.ModelMeta, query Query, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Type() != meta.Type {
		return fmt.Errorf("tango db: First destination must be a non-nil pointer to %s", meta.Type)
	}

	if len(query.OrderBy) == 0 {
		primaryKey, err := findPrimaryKeyField(meta)
		if err != nil {
			return err
		}
		query.OrderBy = []string{primaryKey.Name} // query is a copy; the caller's slice is untouched
	}
	query.Limit = 1

	sqlQuery, args, err := s.selectSQL(meta, query)
	if err != nil {
		return err
	}
	rows, err := s.conn().QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%w: no %s matches the query", ErrNotFound, meta.Name)
	}
	row := reflect.New(meta.Type).Elem()
	if err := scanFieldsInto(rows, row, meta.Fields); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	value.Elem().Set(row)
	return nil
}

// Exists reports whether any row matches query's Where, Any and Filter.
// Limit, Offset and OrderBy are ignored.
func (s *Store) Exists(ctx context.Context, meta model.ModelMeta, query Query) (bool, error) {
	whereSQL, args, err := s.whereClause(meta, query)
	if err != nil {
		return false, err
	}
	sqlQuery := "SELECT 1 FROM " + s.ident(meta.Name) + whereSQL + " LIMIT 1"
	rows, err := s.conn().QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := rows.Next()
	if err := rows.Err(); err != nil {
		return false, err
	}
	return found, nil
}
