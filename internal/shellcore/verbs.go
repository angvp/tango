package shellcore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// ErrReadOnly is wrapped by every write verb of a read-only session.
var ErrReadOnly = errors.New("the shell is read-only (--readonly)")

// Verbs are the built-in, map-based helpers a session can call by bare name:
// Models, Describe, Get, List, Count, Create, Update, Delete and Context.
// Rows are map[string]any keyed by Go field name; models are addressed as
// app.Model.
type Verbs struct {
	ctx      context.Context
	models   *model.Registry
	store    *db.Store
	readOnly bool
}

// NewVerbs returns the verbs over models and store. readOnly makes the
// verbs that write fail.
func NewVerbs(ctx context.Context, models *model.Registry, store *db.Store, readOnly bool) *Verbs {
	return &Verbs{ctx: ctx, models: models, store: store, readOnly: readOnly}
}

// Names lists the verbs in the order help() shows them.
var verbNames = []string{"Models", "Describe", "Get", "List", "Count", "Create", "Update", "Delete", "Context"}

// Functions are the verbs by the bare name a session calls them by.
func (v *Verbs) Functions() map[string]any {
	return map[string]any{
		"Models":   v.Models,
		"Describe": v.Describe,
		"Get":      v.Get,
		"List":     v.List,
		"Count":    v.Count,
		"Create":   v.Create,
		"Update":   v.Update,
		"Delete":   v.Delete,
		"Context":  v.Context,
	}
}

// Context is the context of the shell session.
func (v *Verbs) Context() context.Context { return v.ctx }

// Models lists every registered model as app.Model, sorted.
func (v *Verbs) Models() []string {
	names := make([]string, 0)
	for _, meta := range v.models.All() {
		names = append(names, qualified(meta))
	}
	sort.Strings(names)
	return names
}

// Describe lists the fields of a model, one row per field.
func (v *Verbs) Describe(name string) ([]map[string]any, error) {
	meta, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, len(meta.Fields))
	for i, f := range meta.Fields {
		rows[i] = map[string]any{
			"Name":       f.Name,
			"Type":       f.Type.String(),
			"PrimaryKey": f.PrimaryKey,
			"Unique":     f.Unique,
			"Indexed":    f.Indexed,
			"ForeignKey": v.qualifiedForeignKey(f.ForeignKey),
		}
	}
	return rows, nil
}

// qualifiedForeignKey names the model a foreign key points at as app.Model.
func (v *Verbs) qualifiedForeignKey(target string) string {
	if target == "" {
		return ""
	}
	if meta, ok := v.models.Get(target); ok {
		return qualified(meta)
	}
	return target
}

// Get returns the row of model whose primary key is pk.
func (v *Verbs) Get(name string, pk any) (map[string]any, error) {
	meta, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	return v.get(meta, pk)
}

func (v *Verbs) get(meta model.ModelMeta, pk any) (map[string]any, error) {
	key, err := primaryKeyValue(meta, pk)
	if err != nil {
		return nil, err
	}
	dest := reflect.New(meta.Type)
	if err := v.store.Get(v.ctx, meta, key, dest.Interface()); err != nil {
		return nil, fmt.Errorf("get %s: %w", qualified(meta), err)
	}
	return rowOf(meta, dest.Elem()), nil
}

// List returns the rows of model matching an optional query map with the
// keys where (field to value, equality only), order (field names, "-" prefix
// for descending), limit and offset.
func (v *Verbs) List(name string, query ...map[string]any) ([]map[string]any, error) {
	meta, q, err := v.query(name, query)
	if err != nil {
		return nil, err
	}
	dest := reflect.New(reflect.SliceOf(meta.Type))
	if err := v.store.List(v.ctx, meta, q, dest.Interface()); err != nil {
		return nil, fmt.Errorf("list %s: %w", qualified(meta), err)
	}
	slice := dest.Elem()
	rows := make([]map[string]any, slice.Len())
	for i := range rows {
		rows[i] = rowOf(meta, slice.Index(i))
	}
	return rows, nil
}

// Count returns how many rows of model match the where key of an optional
// query map; order, limit and offset do not affect it.
func (v *Verbs) Count(name string, query ...map[string]any) (int, error) {
	meta, q, err := v.query(name, query)
	if err != nil {
		return 0, err
	}
	n, err := v.store.Count(v.ctx, meta, q)
	if err != nil {
		return 0, fmt.Errorf("count %s: %w", qualified(meta), err)
	}
	return n, nil
}

// Create stores a new row built from the field-to-value map and returns the
// stored row, including the primary key the database assigned.
func (v *Verbs) Create(name string, row map[string]any) (map[string]any, error) {
	if err := v.refuseWrite("Create"); err != nil {
		return nil, err
	}
	meta, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	dest := reflect.New(meta.Type)
	if err := setFields(meta, dest.Elem(), row, true); err != nil {
		return nil, fmt.Errorf("create %s: %w", qualified(meta), err)
	}
	if err := v.store.Create(v.ctx, meta, dest.Interface()); err != nil {
		return nil, fmt.Errorf("create %s: %w", qualified(meta), err)
	}
	return rowOf(meta, dest.Elem()), nil
}

// Update changes the given fields of the row whose primary key is pk and
// returns the stored row.
func (v *Verbs) Update(name string, pk any, changes map[string]any) (map[string]any, error) {
	if err := v.refuseWrite("Update"); err != nil {
		return nil, err
	}
	meta, err := v.lookup(name)
	if err != nil {
		return nil, err
	}
	key, err := primaryKeyValue(meta, pk)
	if err != nil {
		return nil, err
	}
	dest := reflect.New(meta.Type)
	if err := v.store.Get(v.ctx, meta, key, dest.Interface()); err != nil {
		return nil, fmt.Errorf("update %s: %w", qualified(meta), err)
	}
	if err := setFields(meta, dest.Elem(), changes, false); err != nil {
		return nil, fmt.Errorf("update %s: %w", qualified(meta), err)
	}
	if err := v.store.Update(v.ctx, meta, dest.Interface()); err != nil {
		return nil, fmt.Errorf("update %s: %w", qualified(meta), err)
	}
	return rowOf(meta, dest.Elem()), nil
}

// Delete removes the row whose primary key is pk.
func (v *Verbs) Delete(name string, pk any) error {
	if err := v.refuseWrite("Delete"); err != nil {
		return err
	}
	meta, err := v.lookup(name)
	if err != nil {
		return err
	}
	key, err := primaryKeyValue(meta, pk)
	if err != nil {
		return err
	}
	if err := v.store.Delete(v.ctx, meta, key); err != nil {
		return fmt.Errorf("delete %s: %w", qualified(meta), err)
	}
	return nil
}

func (v *Verbs) refuseWrite(verb string) error {
	if v.readOnly {
		return fmt.Errorf("%s refused: %w", verb, ErrReadOnly)
	}
	return nil
}

func qualified(meta model.ModelMeta) string { return meta.App + "." + meta.Name }

// lookup finds a model by its app.Model name. A bare or misspelled name is
// an error that offers the names it may have meant.
func (v *Verbs) lookup(name string) (model.ModelMeta, error) {
	if app, modelName, ok := strings.Cut(name, "."); ok {
		if meta, found := v.models.Get(modelName); found && meta.App == app {
			return meta, nil
		}
	}
	hint := ""
	if near := v.nearNames(name); len(near) > 0 {
		hint = "; did you mean " + strings.Join(near, ", ") + "?"
	}
	return model.ModelMeta{}, fmt.Errorf("unknown model %q: models are addressed as app.Model, Models() lists them%s", name, hint)
}

func (v *Verbs) nearNames(name string) []string {
	bare := name
	if _, after, ok := strings.Cut(name, "."); ok {
		bare = after
	}
	var near []string
	for _, meta := range v.models.All() {
		if strings.EqualFold(meta.Name, bare) || strings.EqualFold(qualified(meta), name) {
			near = append(near, qualified(meta))
		}
	}
	sort.Strings(near)
	if len(near) > 3 {
		near = near[:3]
	}
	return near
}

// query resolves the model and the optional query map of List and Count.
func (v *Verbs) query(name string, maps []map[string]any) (model.ModelMeta, db.Query, error) {
	meta, err := v.lookup(name)
	if err != nil {
		return meta, db.Query{}, err
	}
	if len(maps) > 1 {
		return meta, db.Query{}, fmt.Errorf("takes one query map, got %d", len(maps))
	}
	var q db.Query
	if len(maps) == 0 || maps[0] == nil {
		return meta, q, nil
	}
	for key, value := range maps[0] {
		switch key {
		case "where":
			where, ok := value.(map[string]any)
			if !ok {
				return meta, q, fmt.Errorf(`query "where" must be a map[string]any, got %T`, value)
			}
			keys := make([]string, 0, len(where))
			for field := range where {
				keys = append(keys, field)
			}
			sort.Strings(keys)
			for _, field := range keys {
				fm, err := fieldOf(meta, field)
				if err != nil {
					return meta, q, fmt.Errorf("query where: %w", err)
				}
				val, err := coerce(where[field], fm.Type)
				if err != nil {
					return meta, q, fmt.Errorf("query where %s: %w", field, err)
				}
				q.Where = append(q.Where, db.Condition{Field: field, Op: db.OpEq, Value: val.Interface()})
			}
		case "order":
			order, err := stringList(value)
			if err != nil {
				return meta, q, fmt.Errorf(`query "order": %w`, err)
			}
			q.OrderBy = order
		case "limit", "offset":
			n, err := wholeNumber(value)
			if err != nil || n < 0 {
				return meta, q, fmt.Errorf("query %q must be a whole number of at least 0, got %v", key, value)
			}
			if key == "limit" {
				q.Limit = n
			} else {
				q.Offset = n
			}
		default:
			return meta, q, fmt.Errorf(`unknown query key %q: use where, order, limit or offset`, key)
		}
	}
	return meta, q, nil
}

func stringList(value any) ([]string, error) {
	switch list := value.(type) {
	case []string:
		return list, nil
	case []any:
		out := make([]string, len(list))
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must be field names, got %T", item)
			}
			out[i] = s
		}
		return out, nil
	}
	return nil, fmt.Errorf("must be a []string of field names, got %T", value)
}

func wholeNumber(value any) (int, error) {
	rv := reflect.ValueOf(value)
	switch {
	case rv.Kind() >= reflect.Int && rv.Kind() <= reflect.Int64:
		return int(rv.Int()), nil
	case rv.Kind() >= reflect.Uint && rv.Kind() <= reflect.Uint64:
		return int(rv.Uint()), nil
	}
	return 0, fmt.Errorf("not a whole number: %v", value)
}

func fieldOf(meta model.ModelMeta, name string) (model.FieldMeta, error) {
	names := make([]string, len(meta.Fields))
	for i, f := range meta.Fields {
		if f.Name == name {
			return f, nil
		}
		names[i] = f.Name
	}
	return model.FieldMeta{}, fmt.Errorf("%s has no field %q (fields: %s)", qualified(meta), name, strings.Join(names, ", "))
}

func primaryKeyValue(meta model.ModelMeta, pk any) (any, error) {
	for _, f := range meta.Fields {
		if f.PrimaryKey {
			val, err := coerce(pk, f.Type)
			if err != nil {
				return nil, fmt.Errorf("primary key %s of %s: %w", f.Name, qualified(meta), err)
			}
			return val.Interface(), nil
		}
	}
	return nil, fmt.Errorf("%s has no primary key", qualified(meta))
}

// rowOf reads a stored struct into a map keyed by Go field name; values keep
// their Go types.
func rowOf(meta model.ModelMeta, rv reflect.Value) map[string]any {
	row := make(map[string]any, len(meta.Fields))
	for _, f := range meta.Fields {
		row[f.Name] = rv.FieldByName(f.Name).Interface()
	}
	return row
}

// setFields copies a field-to-value map into the struct rv. A primary key
// may be given on create but never changed on update.
func setFields(meta model.ModelMeta, rv reflect.Value, values map[string]any, creating bool) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f, err := fieldOf(meta, key)
		if err != nil {
			return err
		}
		if f.PrimaryKey && !creating {
			return fmt.Errorf("the primary key %s cannot be changed", key)
		}
		val, err := coerce(values[key], f.Type)
		if err != nil {
			return fmt.Errorf("field %s: %w", key, err)
		}
		rv.FieldByName(key).Set(val)
	}
	return nil
}

// coerce converts value to target when that loses nothing: a number to another
// number type, or a value that already fits. A string never becomes a number
// or the other way round.
func coerce(value any, target reflect.Type) (reflect.Value, error) {
	if value == nil {
		switch target.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
			return reflect.Zero(target), nil
		}
		return reflect.Value{}, fmt.Errorf("%s cannot be nil", target)
	}
	rv := reflect.ValueOf(value)
	if rv.Type().AssignableTo(target) {
		out := reflect.New(target).Elem()
		out.Set(rv)
		return out, nil
	}
	if isNumber(rv.Kind()) && isNumber(target.Kind()) {
		out := reflect.New(target).Elem()
		if ok := setNumber(out, rv); ok {
			return out, nil
		}
		return reflect.Value{}, fmt.Errorf("%v does not fit %s", value, target)
	}
	if rv.Kind() == target.Kind() && rv.Type().ConvertibleTo(target) && (rv.Kind() == reflect.String || rv.Kind() == reflect.Bool) {
		return rv.Convert(target), nil
	}
	return reflect.Value{}, fmt.Errorf("want %s, got %T", target, value)
}

func isNumber(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Float64
}

// setNumber stores the number in dst when it is exactly representable.
func setNumber(dst, src reflect.Value) bool {
	switch {
	case src.Kind() >= reflect.Int && src.Kind() <= reflect.Int64:
		return setInt(dst, src.Int())
	case src.Kind() >= reflect.Uint && src.Kind() <= reflect.Uint64:
		u := src.Uint()
		if u > math.MaxInt64 {
			if dst.Kind() >= reflect.Uint && dst.Kind() <= reflect.Uint64 && !dst.OverflowUint(u) {
				dst.SetUint(u)
				return true
			}
			return false
		}
		return setInt(dst, int64(u))
	default:
		f := src.Float()
		switch {
		case dst.Kind() == reflect.Float32 || dst.Kind() == reflect.Float64:
			if dst.OverflowFloat(f) {
				return false
			}
			dst.SetFloat(f)
			return true
		case f == math.Trunc(f) && math.Abs(f) < 1<<53:
			return setInt(dst, int64(f))
		}
		return false
	}
}

func setInt(dst reflect.Value, n int64) bool {
	switch {
	case dst.Kind() >= reflect.Int && dst.Kind() <= reflect.Int64:
		if dst.OverflowInt(n) {
			return false
		}
		dst.SetInt(n)
	case dst.Kind() >= reflect.Uint && dst.Kind() <= reflect.Uint64:
		if n < 0 || dst.OverflowUint(uint64(n)) {
			return false
		}
		dst.SetUint(uint64(n))
	default:
		if dst.OverflowFloat(float64(n)) {
			return false
		}
		dst.SetFloat(float64(n))
	}
	return true
}
