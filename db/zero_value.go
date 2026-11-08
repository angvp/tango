package db

import (
	"reflect"

	"github.com/angvp/tango/model"
)

// rejectZeroQueryValue fails a value-taking condition whose value is the zero
// time. A zero time is "no time", not a value to compare: it is stored as
// NULL, so a comparison against it would match nothing, and binding NULL would
// do so silently. The caller finds unset rows with IsNull or IsNotNull.
func rejectZeroQueryValue(field model.FieldMeta, value any, label string) error {
	if field.Type != timeType || !reflect.ValueOf(value).IsZero() {
		return nil
	}
	return conditionError(label, "value for field %q is the zero time; use db.IsNull or db.IsNotNull to find unset rows", field.Name)
}
