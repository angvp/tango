package admin

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/angvp/tango/i18n"
)

// FieldError is a validation failure a Widget's Parse reports for its own
// field. Admin shows Message beside that field, marks the field's control
// aria-invalid and re-renders the form with a 422, instead of the generic
// form-level error any other Parse error produces.
//
// Parse may return a FieldError (or a *FieldError) directly, wrapped with
// fmt.Errorf("%w"), or inside errors.Join; every one found is shown.
type FieldError struct {
	Message string

	// text, when set, is the translatable form of Message that the built-in
	// widgets use; Message holds its English rendering for Error.
	text *localizedText
}

// localizedText is a message's translation key, English fallback and format
// arguments.
type localizedText struct {
	key      string
	fallback string
	args     []any
}

func (e FieldError) Error() string { return e.Message }

// fieldErrorMessages returns the Message of every FieldError in err's tree,
// in order, falling back to a generic message for an empty one.
func fieldErrorMessages(ctx context.Context, err error) []string {
	var out []string
	var walk func(error)
	walk = func(err error) {
		switch e := err.(type) {
		case nil:
			return
		case FieldError:
			out = append(out, fieldErrorText(ctx, e))
			return
		case *FieldError:
			if e != nil {
				out = append(out, fieldErrorText(ctx, *e))
			}
			return
		}
		switch e := err.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range e.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(e.Unwrap())
		}
	}
	walk(err)
	return out
}

func fieldErrorText(ctx context.Context, e FieldError) string {
	if e.text != nil {
		return i18n.T(ctx, e.text.key, e.text.fallback, e.text.args...)
	}
	if e.Message == "" {
		return i18n.T(ctx, "admin.error.invalid_value", "Invalid value")
	}
	return e.Message
}

// formFieldErrors carries the FieldErrors populateFromForm collected, keyed
// by the field's Go name.
type formFieldErrors struct {
	messages map[string]string
}

func (e *formFieldErrors) Error() string {
	parts := make([]string, 0, len(e.messages))
	for name, message := range e.messages {
		parts = append(parts, name+": "+message)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// valueParseError is what a built-in parser returns when a submitted value
// does not read as its field's Go type. kind names the type, so the message
// beside the field says what the field needs.
type valueParseError struct {
	kind reflect.Kind
	err  error
}

func (e valueParseError) Error() string { return e.err.Error() }
func (e valueParseError) Unwrap() error { return e.err }

// parseErrorMessage is the message beside a field whose submitted value a
// widget refused. A FieldError keeps its own wording, a value of the wrong
// type gets the plain form for that type, and any other error gets a generic
// message. The error text itself is never shown.
func parseErrorMessage(ctx context.Context, f FieldContext, err error) string {
	if messages := fieldErrorMessages(ctx, err); len(messages) > 0 {
		return strings.Join(messages, " ")
	}
	var parseErr valueParseError
	if errors.As(err, &parseErr) {
		switch parseErr.kind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return i18n.T(ctx, "admin.error.not_whole_number", "%s must be a whole number.", f.Label)
		case reflect.Float32, reflect.Float64:
			return i18n.T(ctx, "admin.error.not_number", "%s must be a number.", f.Label)
		case reflect.Struct:
			return i18n.T(ctx, "admin.error.not_date_time", "%s must be a date and time.", f.Label)
		}
	}
	return i18n.T(ctx, "admin.error.invalid_value", "Invalid value")
}
