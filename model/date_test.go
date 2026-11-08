package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewDateRejectsDatesThatDoNotExist(t *testing.T) {
	for _, c := range []struct{ y, m, d int }{
		{2026, 2, 30}, {2026, 13, 1}, {2026, 1, 0}, {2026, 4, 31},
		{0, 1, 1}, {10000, 1, 1}, {2023, 2, 29},
	} {
		if _, err := NewDate(c.y, time.Month(c.m), c.d); err == nil {
			t.Errorf("NewDate(%d, %d, %d) accepted a date that does not exist", c.y, c.m, c.d)
		}
	}
	if _, err := NewDate(2024, 2, 29); err != nil {
		t.Errorf("NewDate(2024, 2, 29) = %v, want the leap day", err)
	}
}

func TestMustDatePanicsOnAnInvalidDate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustDate(2026, 2, 30) did not panic")
		}
	}()
	MustDate(2026, 2, 30)
}

func TestZeroDateIsDistinctAndComparable(t *testing.T) {
	var zero Date
	if !zero.IsZero() {
		t.Fatal("the zero Date is not zero")
	}
	first := MustDate(1, 1, 1)
	if first.IsZero() || zero == first {
		t.Fatal("the zero Date equals 0001-01-01; it must mean no date")
	}
	if MustDate(2026, 3, 1) != MustDate(2026, 3, 1) {
		t.Fatal("equal dates are not == in Go")
	}
}

func TestDateOfMatchesTheCalendarDateInTheTimesOwnLocation(t *testing.T) {
	for _, zone := range []*time.Location{time.FixedZone("UTC-11", -11*3600), time.UTC, time.FixedZone("UTC+13", 13*3600)} {
		for _, moment := range []time.Time{
			time.Date(2026, 3, 1, 23, 30, 0, 0, zone),
			time.Date(2026, 3, 1, 0, 30, 0, 0, zone),
		} {
			year, month, day := moment.Date()
			if got := DateOf(moment); got != MustDate(year, month, day) {
				t.Errorf("DateOf(%v) = %v, want the date %d-%02d-%02d the time shows", moment, got, year, month, day)
			}
		}
	}
}

func TestInReturnsMidnightOfTheSameCalendarDate(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*3600)
	got := MustDate(2026, 3, 1).In(zone)
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	if !got.Equal(want) || got.Location() != zone {
		t.Errorf("In = %v, want midnight %v", got, want)
	}
}

func TestScanAcceptsTheExactFormsAndNothingElse(t *testing.T) {
	want := MustDate(2026, 1, 2)
	accepted := []any{
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 23, 30, 0, 0, time.FixedZone("UTC-11", -11*3600)),
		"2026-01-02",
		[]byte("2026-01-02"),
	}
	for _, src := range accepted {
		var got Date
		if err := got.Scan(src); err != nil {
			t.Errorf("Scan(%T %v) = %v, want nil", src, src, err)
		}
		if got != want {
			t.Errorf("Scan(%T %v) = %v, want %v", src, src, got, want)
		}
	}

	var zero Date
	if err := zero.Scan(nil); err != nil || !zero.IsZero() {
		t.Errorf("Scan(nil) = %v, %v; want the zero Date and no error", zero, err)
	}

	for _, src := range []any{"2026-01-02 03:04:05", "", "2026-1-2", int64(20260102), 1.5} {
		var got Date
		if err := got.Scan(src); err == nil {
			t.Errorf("Scan(%T %v) accepted a value that is not an exact date", src, src)
		}
	}
}

func TestValueIsTheExactStringOrNULL(t *testing.T) {
	got, err := MustDate(2026, 1, 2).Value()
	if err != nil || got != "2026-01-02" {
		t.Errorf("Value() = %v, %v; want the string 2026-01-02", got, err)
	}
	var zero Date
	if v, err := zero.Value(); err != nil || v != nil {
		t.Errorf("zero Value() = %v, %v; want NULL", v, err)
	}
	var _ driver.Valuer = Date{}
}

func TestJSONZeroIsNullAndExactDatesRoundTrip(t *testing.T) {
	data, err := json.Marshal(Date{})
	if err != nil || string(data) != "null" {
		t.Errorf("zero marshals as %s, %v; want null", data, err)
	}
	data, err = json.Marshal(MustDate(2026, 1, 2))
	if err != nil || string(data) != `"2026-01-02"` {
		t.Errorf("date marshals as %s, %v; want the quoted date", data, err)
	}
	for _, input := range []string{"null", `"2026-01-02"`} {
		var got Date
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Errorf("unmarshal %s: %v", input, err)
		}
	}
	var got Date
	if err := json.Unmarshal([]byte(`""`), &got); err == nil {
		t.Error(`JSON "" was accepted; it must be rejected`)
	}
	if err := json.Unmarshal([]byte(`"2026-1-2"`), &got); err == nil {
		t.Error("JSON with a non-exact date was accepted")
	}
}

func TestTextZeroIsEmptyAndEmptyTextIsZero(t *testing.T) {
	text, err := Date{}.MarshalText()
	if err != nil || string(text) != "" {
		t.Errorf("zero MarshalText = %q, %v; want empty", text, err)
	}
	var got Date
	if err := got.UnmarshalText([]byte("")); err != nil || !got.IsZero() {
		t.Errorf(`UnmarshalText("") = %v, %v; want zero and no error`, got, err)
	}
	if err := got.UnmarshalText([]byte("2026-01-02")); err != nil || got != MustDate(2026, 1, 2) {
		t.Errorf("UnmarshalText of an exact date = %v, %v", got, err)
	}
	if err := got.UnmarshalText([]byte("2026-01-02T10:00")); err == nil {
		t.Error("UnmarshalText accepted a datetime")
	}
}

func TestAddDaysCrossesMonthAndYearBoundaries(t *testing.T) {
	if got := MustDate(2026, 12, 31).AddDays(1); got != MustDate(2027, 1, 1) {
		t.Errorf("AddDays across the year end = %v", got)
	}
	if got := MustDate(2024, 2, 28).AddDays(1); got != MustDate(2024, 2, 29) {
		t.Errorf("AddDays into a leap day = %v", got)
	}
}

func TestCompareOrdersChronologically(t *testing.T) {
	a, b := MustDate(2026, 1, 31), MustDate(2026, 2, 1)
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare does not order dates chronologically")
	}
	if !a.Before(b) || !b.After(a) || !a.Equal(a) {
		t.Error("Before, After or Equal disagree with Compare")
	}
}

func TestStringIsYYYYMMDDAndEmptyForZero(t *testing.T) {
	if got := MustDate(2026, 1, 2).String(); got != "2026-01-02" {
		t.Errorf("String() = %q", got)
	}
	if got := (Date{}).String(); got != "" {
		t.Errorf("zero String() = %q, want empty", got)
	}
}

func TestRegistrationAcceptsADateWithUniqueAndIndexAndRejectsPK(t *testing.T) {
	type dated struct {
		ID   int64 `tango:"pk"`
		Day  Date  `tango:"unique,index"`
		Note string
	}
	registry := NewRegistry()
	if err := registry.Register(dated{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	meta, _ := registry.Get("dated")
	for _, field := range meta.Fields {
		if field.Name == "Day" && (!field.Unique || !field.Indexed) {
			t.Errorf("Day unique=%v index=%v, want both", field.Unique, field.Indexed)
		}
	}

	type datedKey struct {
		Day Date `tango:"pk"`
	}
	err := NewRegistry().Register(datedKey{})
	if !errors.Is(err, ErrInvalidFieldTag) || !strings.Contains(err.Error(), "Day") {
		t.Errorf("pk on a Date: err = %v, want ErrInvalidFieldTag naming the field", err)
	}
}
