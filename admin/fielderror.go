package admin

import (
	"context"
	"errors"
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

// parseFailure splits a populateFromForm error into per-field messages and,
// for any other error, the form-level message.
func parseFailure(err error) (fieldErrors map[string]string, formError string) {
	var collected *formFieldErrors
	if errors.As(err, &collected) {
		return collected.messages, ""
	}
	return nil, err.Error()
}
