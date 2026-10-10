package adminregistry

import (
	"fmt"
	"reflect"

	"github.com/angvp/tango/model"
)

// validateFilters checks Options.Filters: every name must be a field of
// meta, listed at most once, and be either a boolean or a foreign key.
func validateFilters(meta model.ModelMeta, names []string) error {
	if err := validateFields(meta, names, "Filters"); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, dup := seen[name]; dup {
			return fmt.Errorf("tango admin: Filters lists %q more than once on %s", name, meta.Name)
		}
		seen[name] = struct{}{}

		field, _ := findField(meta, name)
		if !filterable(field) {
			return fmt.Errorf("tango admin: Filters field %q on %s must be a boolean or foreign key", name, meta.Name)
		}
	}

	return nil
}

// filterable reports whether a changelist can filter by field: a boolean, or
// a foreign key whose Go kind is a string or an integer, which is how the
// admin reads the related primary key back from a query value.
func filterable(field model.FieldMeta) bool {
	if field.ForeignKey == "" {
		return field.Type.Kind() == reflect.Bool
	}
	switch field.Type.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

// findField returns the metadata of the named field of meta, and whether it
// exists.
func findField(meta model.ModelMeta, name string) (model.FieldMeta, bool) {
	for _, field := range meta.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return model.FieldMeta{}, false
}
