package admin

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// EventBulkDelete is the structured log event emitted once for each bulk
// delete that committed. It carries the model name, selected_count (the number
// of selected rows accepted, not the rows physically deleted, which cascades
// can raise) and the Admin account's username, never keys or row values.
const EventBulkDelete = "tango.admin.bulk_delete"

const (
	bulkPKField       = "pk"
	bulkCountField    = "count"
	bulkReturnToField = "return_to"
)

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
		if raw == "" {
			return bulkKey{}, false
		}
		value.SetString(raw)
		return bulkKey{value: value, canon: raw}, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != raw || value.OverflowInt(n) {
			return bulkKey{}, false
		}
		value.SetInt(n)
		return bulkKey{value: value, canon: n}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != raw || n > math.MaxInt64 || value.OverflowUint(n) {
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
	seen := make(map[any]struct{}, len(raws))
	for _, raw := range raws {
		key, ok := parseBulkKey(field, raw)
		if !ok {
			return nil, false
		}
		if _, dup := seen[key.canon]; dup {
			return nil, false
		}
		seen[key.canon] = struct{}{}
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

// deleteBulkKeys re-reads every selected key inside the transaction tx and
// refuses the whole action when one has no row, then removes the selection.
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
	return removeBulkKeys(ctx, tx, meta, field, keys)
}

// removeBulkKeys deletes the selected rows, and every row that references
// them, in ascending key order. A selected row that is already gone when its
// turn comes (an earlier selected row's cascade removed it, or another
// transaction did after the re-read) is not a failure, so the outcome does
// not depend on the order of the keys. Every selected key must be absent at
// the end.
func removeBulkKeys(ctx context.Context, tx *db.Store, meta model.ModelMeta, field model.FieldMeta, keys []bulkKey) error {
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

// rejectBulk answers a refused bulk delete with an admin page, because the
// request comes from a form submission a person is looking at.
func rejectBulk(ctx *tango.Context, nav []navItem, brand Branding, meta model.ModelMeta, opts adminregistry.Options, basePath, key, fallback string, args ...any) error {
	section := sectionName(meta, opts)
	current := i18n.T(ctx.Context(), "admin.bulk.delete_selected", "Delete selected")
	return render(ctx, http.StatusBadRequest, bulkRejectedTemplate, bulkRejectedPageData{
		chrome:   chrome{Nav: nav, Active: meta.Name, Brand: brand}.withContext(ctx.Context()).withCrumbs(crumb{Label: section, Href: basePath}, crumb{Label: current}),
		Title:    section,
		Message:  i18n.T(ctx.Context(), key, fallback, args...),
		Plural:   pluralNoun(meta, opts),
		BasePath: basePath,
	})
}

// bulkReturnURL rebuilds the list URL to return to from the submitted
// return_to. Only a local URL of this model's changelist carrying search,
// page and registered filter parameters, each once, is honoured; anything
// else falls back to the plain list. The result is assembled from parsed
// parts, never from the submitted string.
func bulkReturnURL(raw, basePath string, registration ModelRegistration) url.Values {
	allowed := map[string]struct{}{"q": {}, "page": {}}
	for _, name := range registration.Options.Filters {
		allowed[filterKey(name)] = struct{}{}
	}
	if strings.ContainsAny(raw, "#\\") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return url.Values{}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Fragment != "" || parsed.Path != basePath {
		return url.Values{}
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return url.Values{}
	}
	for key, values := range query {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			return url.Values{}
		}
	}
	if page, ok := query["page"]; ok {
		if n, err := strconv.Atoi(page[0]); err != nil || n < 1 || strconv.Itoa(n) != page[0] {
			return url.Values{}
		}
	}
	return query
}

func bulkDeleteView(store *db.Store, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
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

		form := ctx.Request().PostForm
		raws := form[bulkPKField]
		count, countErr := strconv.Atoi(form.Get(bulkCountField))
		if len(raws) == 0 || len(raws) > pageSize || countErr != nil || count != len(raws) {
			return rejectBulk(ctx, nav, brand, meta, registration.Options, basePath, "admin.error.bulk_invalid", "Nothing was deleted. The selection is not valid.")
		}
		keys, ok := parseBulkKeys(field, raws)
		if !ok {
			return rejectBulk(ctx, nav, brand, meta, registration.Options, basePath, "admin.error.bulk_invalid", "Nothing was deleted. The selection is not valid.")
		}

		err = store.InTx(ctx.Context(), func(tx *db.Store) error {
			return deleteBulkKeys(ctx.Context(), tx, meta, field, keys)
		})
		switch {
		case errors.Is(err, errBulkKeyMissing):
			return rejectBulk(ctx, nav, brand, meta, registration.Options, basePath, "admin.error.bulk_selected_missing", "Nothing was deleted. One selected %s no longer exists.", displayNoun(meta, registration.Options))
		case err != nil:
			return err
		}

		username := ""
		if who := signedInFrom(ctx.Context()); who != nil {
			username = who.Username
		}
		ctx.Logger().Info(EventBulkDelete, "model", meta.Name, "selected_count", len(keys), "username", username)

		if len(keys) == 1 {
			setOutcome(ctx, "admin.bulk.outcome_one", displayNoun(meta, registration.Options))
		} else {
			setOutcome(ctx, "admin.bulk.outcome_many", strconv.Itoa(len(keys)), pluralNoun(meta, registration.Options))
		}

		location := basePath
		if query := bulkReturnURL(form.Get(bulkReturnToField), basePath, registration).Encode(); query != "" {
			location += "?" + query
		}
		return ctx.Redirect(location)
	}
}
