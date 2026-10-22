package db

import (
	"reflect"
	"strings"

	"github.com/angvp/tango/model"
)

const (
	// MaxInValues is the most values one IN list may hold.
	MaxInValues = 1000
	// MaxQueryArgs is the most bind values one Query may send across Where,
	// Any and Filter. It is one number for every dialect, well under each
	// driver's own ceiling, so a query that works on one dialect works on all.
	MaxQueryArgs = 10000
)

const (
	limitInValues   = "IN values"
	limitBindValues = "bind values"
)

// validateIn checks an OpIn condition's list against field: a slice or array
// (nil and empty are valid and match nothing), at most MaxInValues long, with
// every element of the field's kind. It never modifies the list.
func validateIn(field model.FieldMeta, condition Condition, label string) error {
	if condition.Value == nil {
		return nil
	}
	list := reflect.ValueOf(condition.Value)
	if list.Kind() != reflect.Slice && list.Kind() != reflect.Array {
		return conditionError(label, "operator %q needs a slice or array for field %q, got %s", condition.Op, field.Name, list.Type())
	}
	if list.Len() > MaxInValues {
		return &QueryLimitError{Limit: limitInValues, Field: field.Name, Max: MaxInValues, Got: list.Len()}
	}
	fieldKind := field.Type.Kind()
	for i := 0; i < list.Len(); i++ {
		element := list.Index(i)
		if element.Kind() == reflect.Interface {
			if element.IsNil() {
				return conditionError(label, "IN value %d for field %q is nil", i, field.Name)
			}
			element = element.Elem()
		}
		if element.Kind() != fieldKind || (fieldKind == reflect.Struct && element.Type() != field.Type) {
			return conditionError(label, "IN value %d for field %q has type %s, want %s", i, field.Name, element.Type(), field.Type)
		}
		if field.Type == timeType && element.IsZero() {
			return conditionError(label, "IN value %d for field %q is the zero time", i, field.Name)
		}
	}
	return nil
}

// inArguments returns the bind arguments of a validated IN list, in order.
func (s *Store) inArguments(value any) []any {
	if value == nil {
		return nil
	}
	list := reflect.ValueOf(value)
	arguments := make([]any, list.Len())
	for i := range arguments {
		arguments[i] = argumentValue(s.dialect, list.Index(i).Interface())
	}
	return arguments
}

// renderIn renders a validated IN condition. An empty list renders as the
// always-false predicate (1 = 0): valid on every dialect, with no arguments.
func (s *Store) renderIn(condition Condition, args *[]any) string {
	values := s.inArguments(condition.Value)
	if len(values) == 0 {
		return "(1 = 0)"
	}
	placeholders := make([]string, len(values))
	for i, value := range values {
		placeholders[i] = placeholder(s.dialect, len(*args)+1)
		*args = append(*args, value)
	}
	return s.ident(condition.Field) + " IN (" + strings.Join(placeholders, ", ") + ")"
}
