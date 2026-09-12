package admin

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

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

// buildFormFields builds the editable form field descriptors for a model,
// optionally pre-filled from an existing instance (zero Value if instance
// is the zero reflect.Value).
func buildFormFields(meta model.ModelMeta, instance reflect.Value) []formField {
	var fields []formField

	for _, field := range meta.Fields {
		if field.PrimaryKey {
			continue
		}

		f := formField{
			Name:      field.Name,
			Label:     humanizeFieldName(field.Name),
			InputType: inputTypeForKind(field.Type),
		}

		if instance.IsValid() {
			fieldValue := instance.FieldByName(field.Name)
			switch f.InputType {
			case "checkbox":
				f.Checked = fieldValue.Bool()
			default:
				f.Value = formatFieldValue(fieldValue)
			}
		}

		fields = append(fields, f)
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

// populateFromForm parses r.PostForm values into dest's editable fields
// (skipping the primary key), returning an error naming the first field
// that fails to parse.
func populateFromForm(dest reflect.Value, meta model.ModelMeta, form formValues) error {
	for _, field := range meta.Fields {
		if field.PrimaryKey {
			continue
		}

		fieldValue := dest.FieldByName(field.Name)
		raw := form.Get(field.Name)
		present := form.Has(field.Name)

		if err := setFieldFromString(fieldValue, field, raw, present); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}

	return nil
}

// formValues is the minimal surface admin needs from a parsed form.
type formValues interface {
	Get(key string) string
	Has(key string) bool
}

func setFieldFromString(fieldValue reflect.Value, field model.FieldMeta, raw string, present bool) error {
	switch field.Type.Kind() {
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
		return fmt.Errorf("unsupported field kind %s", field.Type.Kind())
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
