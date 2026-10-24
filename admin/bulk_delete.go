package admin

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/model"
)

// bulkPKField is the repeated form field carrying the selected primary keys.
const bulkPKField = "pk"

var errBulkKeyMissing = errors.New("tango admin: a selected row does not exist")

// bulkKey is one submitted primary key converted to the model's own key type.
// canon is the same value as int64, uint64 or string, so keys compare equal
// whatever width the field has.
type bulkKey struct {
	value reflect.Value
	canon any
}

func parseBulkKey(field model.FieldMeta, raw string) (bulkKey, bool) {
	value := reflect.New(field.Type).Elem()
	switch field.Type.Kind() {
	case reflect.String:
		value.SetString(raw)
		return bulkKey{value: value, canon: raw}, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value.OverflowInt(n) {
			return bulkKey{}, false
		}
		value.SetInt(n)
		return bulkKey{value: value, canon: n}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value.OverflowUint(n) {
			return bulkKey{}, false
		}
		value.SetUint(n)
		return bulkKey{value: value, canon: n}, true
	}
	return bulkKey{}, false
}

// canonicalKey is the comparable form of a primary-key field value.
func canonicalKey(value reflect.Value) any {
	switch value.Kind() {
	case reflect.String:
		return value.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	default:
		return value.Uint()
	}
}

func parseBulkKeys(field model.FieldMeta, raws []string) ([]bulkKey, bool) {
	keys := make([]bulkKey, 0, len(raws))
	for _, raw := range raws {
		key, ok := parseBulkKey(field, raw)
		if !ok {
			return nil, false
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bulkKeyLess(keys[i].canon, keys[j].canon) })
	return keys, true
}

func bulkKeyLess(a, b any) bool {
	switch x := a.(type) {
	case int64:
		return x < b.(int64)
	case uint64:
		return x < b.(uint64)
	default:
		return a.(string) < b.(string)
	}
}

// existingBulkKeys returns the canonical form of every selected key that has
// a row. It compares keys in Go instead of trusting a row count: a collation
// that pads trailing spaces can match "a " to the row "a".
func existingBulkKeys(ctx context.Context, tx *db.Store, meta model.ModelMeta, field model.FieldMeta, keys []bulkKey) (map[any]struct{}, error) {
	list := reflect.MakeSlice(reflect.SliceOf(field.Type), 0, len(keys))
	for _, key := range keys {
		list = reflect.Append(list, key.value)
	}
	rows := reflect.New(reflect.SliceOf(meta.Type))
	query := db.Query{Where: []db.Condition{{Field: field.Name, Op: db.OpIn, Value: list.Interface()}}}
	if err := tx.List(ctx, meta, query, rows.Interface()); err != nil {
		return nil, err
	}
	found := make(map[any]struct{}, rows.Elem().Len())
	for i := 0; i < rows.Elem().Len(); i++ {
		found[canonicalKey(rows.Elem().Index(i).FieldByName(field.Name))] = struct{}{}
	}
	return found, nil
}

// deleteBulkKeys removes the selected rows, and every row that references
// them, inside the transaction tx. A selected row that an earlier selected
// row's cascade already removed is not a failure, so the outcome does not
// depend on the order of the keys.
func deleteBulkKeys(ctx context.Context, tx *db.Store, meta model.ModelMeta, field model.FieldMeta, keys []bulkKey) error {
	found, err := existingBulkKeys(ctx, tx, meta, field, keys)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, ok := found[key.canon]; !ok {
			return errBulkKeyMissing
		}
	}
	for _, key := range keys {
		if err := tx.Delete(ctx, meta, key.value.Interface()); err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	left, err := existingBulkKeys(ctx, tx, meta, field, keys)
	if err != nil {
		return err
	}
	if len(left) != 0 {
		return errors.New("tango admin: bulk delete left a selected row in place")
	}
	return nil
}

func rejectBulk(ctx *tango.Context, key, fallback string) error {
	return ctx.JSON(http.StatusBadRequest, map[string]string{"error": i18n.T(ctx.Context(), key, fallback)})
}

func bulkDeleteView(store *db.Store, registration ModelRegistration) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"

	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodPost {
			return methodNotAllowed(ctx)
		}
		field, err := primaryKeyField(meta)
		if err != nil {
			return err
		}
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		if !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}

		keys, ok := parseBulkKeys(field, ctx.Request().PostForm[bulkPKField])
		if !ok {
			return rejectBulk(ctx, "admin.error.bulk_invalid", "the selection is not valid; nothing was deleted")
		}

		err = store.InTx(ctx.Context(), func(tx *db.Store) error {
			return deleteBulkKeys(ctx.Context(), tx, meta, field, keys)
		})
		switch {
		case errors.Is(err, errBulkKeyMissing):
			return rejectBulk(ctx, "admin.error.bulk_missing", "a selected row no longer exists; nothing was deleted")
		case err != nil:
			return err
		}
		return ctx.Redirect(basePath)
	}
}
