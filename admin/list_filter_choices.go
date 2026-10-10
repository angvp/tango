package admin

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// maxFilterChoices caps how many related rows a foreign key filter offers.
// Beyond it the filter explains itself and offers nothing, rather than
// rendering an unbounded select.
const maxFilterChoices = 100

// filterChoice is one related row a foreign key filter can select: its
// primary key as a query value, and its display text.
type filterChoice struct {
	Value string
	Text  string
}

// loadFilterChoices lists the related model target in primary key order, the
// same order as the foreign key select, in one query. It reports overCap, and
// no choices, when the model has more than maxFilterChoices rows.
func loadFilterChoices(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, target string) ([]filterChoice, bool, error) {
	relatedMeta, ok := models.Get(target)
	if !ok {
		return nil, false, fmt.Errorf("tango admin: filter target %q is not a registered model", target)
	}
	pkField, err := primaryKeyField(relatedMeta)
	if err != nil {
		return nil, false, err
	}

	destPtr := reflect.New(reflect.SliceOf(relatedMeta.Type))
	query := db.Query{OrderBy: []string{pkField.Name}, Limit: maxFilterChoices + 1}
	if err := store.List(ctx, relatedMeta, query, destPtr.Interface()); err != nil {
		return nil, false, fmt.Errorf("tango admin: list %s filter choices: %w", target, err)
	}

	rows := destPtr.Elem()
	if rows.Len() > maxFilterChoices {
		return nil, true, nil
	}

	labelField := labelFieldFor(adminReg, target)
	choices := make([]filterChoice, rows.Len())
	for i := range choices {
		row := rows.Index(i)
		value := fmt.Sprint(row.FieldByName(pkField.Name).Interface())
		choices[i] = filterChoice{Value: value, Text: relatedChoiceText(row, labelField, value)}
	}
	return choices, false, nil
}

// foreignKeyValue converts a related primary key, as it appears in a query
// value, to the Go value the foreign key field stores.
func foreignKeyValue(field model.FieldMeta, raw string) (any, error) {
	value := reflect.New(field.Type).Elem()
	switch field.Type.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, field.Type.Bits())
		if err != nil {
			return nil, fmt.Errorf("tango admin: filter value %q for %s: %w", raw, field.Name, err)
		}
		value.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, field.Type.Bits())
		if err != nil {
			return nil, fmt.Errorf("tango admin: filter value %q for %s: %w", raw, field.Name, err)
		}
		value.SetUint(n)
	default:
		return nil, fmt.Errorf("tango admin: filter on %s has unsupported kind %s", field.Name, field.Type.Kind())
	}
	return value.Interface(), nil
}
