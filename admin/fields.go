package admin

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

const dateTimeLayout = "2006-01-02T15:04"

// humanizeFieldName splits a Go field name's words apart for display, e.g.
// "CreatedAt" -> "Created At", "UserID" -> "User ID". Consecutive uppercase
// letters (acronyms) are kept together.
func humanizeFieldName(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			switch {
			case unicode.IsLower(prev) || unicode.IsDigit(prev):
				b.WriteByte(' ')
			case unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// inputTypeForKind returns the HTML input type for a supported Go kind.
func inputTypeForKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "checkbox"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Struct:
		return "datetime-local"
	default:
		return "text"
	}
}

// orderedEditableFields returns meta's non-primary-key fields, ordered per
// opts.FieldOrder (fields not listed there keep their default order,
// appended after the ordered ones).
func orderedEditableFields(meta model.ModelMeta, fieldOrder []string) []model.FieldMeta {
	byName := make(map[string]model.FieldMeta, len(meta.Fields))
	var editable []model.FieldMeta
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			continue
		}
		byName[field.Name] = field
		editable = append(editable, field)
	}

	if len(fieldOrder) == 0 {
		return editable
	}

	placed := make(map[string]bool, len(fieldOrder))
	ordered := make([]model.FieldMeta, 0, len(editable))
	for _, name := range fieldOrder {
		if field, ok := byName[name]; ok {
			ordered = append(ordered, field)
			placed[name] = true
		}
	}
	for _, field := range editable {
		if !placed[field.Name] {
			ordered = append(ordered, field)
		}
	}
	return ordered
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// buildFieldContext builds the FieldContext for one field — label, help
// text, read-only flag, current value/checked state, and (for a foreign
// key field) the related model's select options — from field metadata,
// admin.Options, and instance (the value to pre-fill from: the existing
// stored row on edit, or the zero reflect.Value on create). This is the
// single source of FieldContext for both rendering (buildFormFields) and
// parsing (populateFromForm), so a Widget's Render and Parse always see
// the same shape, per the Widget contract (ADR 0013).
//
// The second return value reports whether a foreign key field's related
// model resolved to a registered model at all (always true for a
// non-foreign-key field) — used only to decide whether to fall back to a
// plain numeric input when it didn't; it does not distinguish that from a
// registered-but-empty related table, which still gets a real (empty)
// select.
func buildFieldContext(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, field model.FieldMeta, opts adminregistry.Options, instance reflect.Value) (FieldContext, bool) {
	fc := FieldContext{
		Name:     field.Name,
		Label:    humanizeFieldName(field.Name),
		ReadOnly: contains(opts.ReadOnly, field.Name),
	}
	if label, ok := opts.Labels[field.Name]; ok {
		fc.Label = label
	}
	if helpText, ok := opts.HelpText[field.Name]; ok {
		fc.HelpText = helpText
	}

	if instance.IsValid() {
		fieldValue := instance.FieldByName(field.Name)
		fc.Value = formatFieldValue(fieldValue)
		if field.Type.Kind() == reflect.Bool {
			fc.Checked = fieldValue.Bool()
		}
	}

	if field.ForeignKey == "" {
		return fc, true
	}

	if opts.Labels[field.Name] == "" {
		fc.Label = humanizeFieldName(field.ForeignKey)
	}
	options, ok := relatedSelectOptions(ctx, store, models, adminReg, field.ForeignKey, fc.Value)
	fc.SelectOptions = options
	return fc, ok
}

// widgetForField picks field's effective Widget: the generic read-only
// presentation if it's read-only (regardless of any Options.Widgets
// override), else that override if set, else a plain numeric input if
// it's a foreign key field whose related model didn't resolve
// (fkResolved false), else tanGO's built-in default for its Go kind.
func widgetForField(field model.FieldMeta, opts adminregistry.Options, fc FieldContext, fkResolved bool) Widget {
	switch {
	case fc.ReadOnly:
		return readOnlyWidget{}
	case opts.Widgets[field.Name] != nil:
		return opts.Widgets[field.Name]
	case field.ForeignKey != "" && !fkResolved:
		return inputWidget{InputType: inputTypeForKind(field.Type)}
	default:
		return defaultWidgetForField(field)
	}
}

// buildFormFields builds the rendered form field descriptors for a model,
// optionally pre-filled from an existing instance (zero Value if instance
// is the zero reflect.Value). Each field is rendered by its Widget, backed
// by store/models/adminReg for foreign-key fields (Milestone 14) — pass
// nil for all three from a caller that never registers foreign keys.
func buildFormFields(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, meta model.ModelMeta, opts adminregistry.Options, instance reflect.Value) []formField {
	var fields []formField

	for _, field := range orderedEditableFields(meta, opts.FieldOrder) {
		fc, fkResolved := buildFieldContext(ctx, store, models, adminReg, field, opts, instance)
		widget := widgetForField(field, opts, fc, fkResolved)
		fields = append(fields, formField{Name: field.Name, HTML: widget.Render(fc)})
	}

	return fields
}

func formatFieldValue(v reflect.Value) string {
	if t, ok := v.Interface().(time.Time); ok {
		if t.IsZero() {
			return ""
		}
		return t.Format(dateTimeLayout)
	}
	return fmt.Sprint(v.Interface())
}

// populateFromForm parses form values into dest's editable fields (skipping
// the primary key) via each field's Widget, returning an error naming the
// first field that fails to parse. A field listed in opts.ReadOnly is never
// parsed from form data: existing holds the row's current values (a valid
// reflect.Value on edit, or the zero reflect.Value on create), and a
// read-only field is copied from there instead — on create, that leaves it
// at its Go zero value, since existing is invalid. Every other field's
// Widget.Parse receives the same FieldContext buildFormFields would have
// rendered it with (built from existing, so an edit's Parse sees the row's
// current values, matching what the form was actually rendered from) —
// not just a bare field name — per the Widget contract (ADR 0013).
func populateFromForm(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, dest reflect.Value, meta model.ModelMeta, opts adminregistry.Options, form FieldValues, existing reflect.Value) error {
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			continue
		}

		fieldValue := dest.FieldByName(field.Name)

		if contains(opts.ReadOnly, field.Name) {
			if existing.IsValid() {
				fieldValue.Set(existing.FieldByName(field.Name))
			}
			continue
		}

		fc, fkResolved := buildFieldContext(ctx, store, models, adminReg, field, opts, existing)
		widget := widgetForField(field, opts, fc, fkResolved)

		if err := widget.Parse(fc, form, fieldValue); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}

	return nil
}

func setFieldFromString(fieldValue reflect.Value, fieldType reflect.Type, raw string, present bool) error {
	switch fieldType.Kind() {
	case reflect.String:
		fieldValue.SetString(raw)
	case reflect.Bool:
		fieldValue.SetBool(present)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if raw == "" {
			fieldValue.SetInt(0)
			return nil
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fieldValue.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if raw == "" {
			fieldValue.SetUint(0)
			return nil
		}
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		fieldValue.SetUint(v)
	case reflect.Float32, reflect.Float64:
		if raw == "" {
			fieldValue.SetFloat(0)
			return nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fieldValue.SetFloat(v)
	case reflect.Struct:
		if raw == "" {
			fieldValue.Set(reflect.ValueOf(time.Time{}))
			return nil
		}
		v, err := time.Parse(dateTimeLayout, raw)
		if err != nil {
			return err
		}
		fieldValue.Set(reflect.ValueOf(v))
	default:
		return fmt.Errorf("unsupported field kind %s", fieldType.Kind())
	}

	return nil
}

// parsePKValue parses raw (from a URL path segment) into a value typed to
// match the primary key field's Go kind.
func parsePKValue(field model.FieldMeta, raw string) (any, error) {
	switch field.Type.Kind() {
	case reflect.String:
		return raw, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported primary key kind %s", field.Type.Kind())
	}
}

// setPKField sets fieldValue (the destination struct's primary key field)
// from raw, matching its Go kind.
func setPKField(fieldValue reflect.Value, field model.FieldMeta, raw string) error {
	value, err := parsePKValue(field, raw)
	if err != nil {
		return err
	}

	switch v := value.(type) {
	case string:
		fieldValue.SetString(v)
	case int64:
		fieldValue.SetInt(v)
	case uint64:
		fieldValue.SetUint(v)
	}

	return nil
}

func primaryKeyField(meta model.ModelMeta) (model.FieldMeta, error) {
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			return field, nil
		}
	}
	return model.FieldMeta{}, fmt.Errorf("model %s has no primary key field", meta.Name)
}
