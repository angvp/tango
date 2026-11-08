package shellcore

import (
	"reflect"
	"testing"

	"github.com/angvp/tango/model"
)

func TestDateFormatsAsYYYYMMDD(t *testing.T) {
	if got := formatValue(reflect.ValueOf(model.MustDate(2026, 3, 1))); got != "2026-03-01" {
		t.Errorf("formatValue(date) = %q, want 2026-03-01", got)
	}
}

func TestZeroDateFormatsAsNil(t *testing.T) {
	if got := formatValue(reflect.ValueOf(model.Date{})); got != "nil" {
		t.Errorf("formatValue(zero date) = %q, want nil, the form of a NULL", got)
	}
}

func TestCoerceParsesAStrictDateStringForADateField(t *testing.T) {
	got, err := coerce("2026-03-01", reflect.TypeOf(model.Date{}))
	if err != nil {
		t.Fatalf("coerce exact date: %v", err)
	}
	if got.Interface() != model.MustDate(2026, 3, 1) {
		t.Errorf("coerce exact date = %v", got.Interface())
	}
	for _, bad := range []string{"2026-1-2", "2026-01-02 10:00:00", "2026-02-30"} {
		if _, err := coerce(bad, reflect.TypeOf(model.Date{})); err == nil {
			t.Errorf("coerce(%q) accepted a value that is not a date", bad)
		}
	}
}
