package db

import (
	"reflect"
	"time"

	"github.com/angvp/tango/model"
)

// dateType is the Go type of model.Date.
var dateType = reflect.TypeFor[model.Date]()

// isCalendarType reports whether t is one of the two temporal field types
// whose zero value means "unset" and whose comparisons follow time order.
func isCalendarType(t reflect.Type) bool {
	return t == timeType || t == dateType
}

// zeroNoun names the zero value of a calendar field type in messages.
func zeroNoun(t reflect.Type) string {
	if t == dateType {
		return "date"
	}
	return "time"
}

// rejectZeroQueryValue fails a value-taking condition whose value is the zero
// time or zero date. A zero value is "no value", not something to compare: it
// is stored as NULL, so a comparison against it would match nothing, and
// binding NULL would do so silently. The caller finds unset rows with IsNull
// or IsNotNull.
func rejectZeroQueryValue(field model.FieldMeta, value any, label string) error {
	if !isCalendarType(field.Type) || !isZeroCalendarValue(value) {
		return nil
	}
	return conditionError(label, "value for field %q is the zero %s; use db.IsNull or db.IsNotNull to find unset rows", field.Name, zeroNoun(field.Type))
}

// isZeroCalendarValue reports whether value is the zero time or zero date. A
// time is judged by time.Time.IsZero, which ignores the location: a zero
// instant in a named zone is still no time. reflect.Value.IsZero would count
// that location as a value, and would disagree with how the write path stores
// the same time as NULL.
func isZeroCalendarValue(value any) bool {
	switch v := value.(type) {
	case time.Time:
		return v.IsZero()
	case model.Date:
		return v.IsZero()
	}
	return reflect.ValueOf(value).IsZero()
}
