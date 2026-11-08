package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

var dateType = reflect.TypeOf(Date{})

// ErrInvalidDate is returned for a calendar date that does not exist, for a
// year outside 0001 through 9999, and for text that is not exactly YYYY-MM-DD.
var ErrInvalidDate = errors.New("tango: invalid date")

const (
	minDateYear = 1
	maxDateYear = 9999
)

// Date is a calendar date with no time of day and no zone. Its fields are
// unexported, so only the constructors build one and every Date is a real
// day. The zero value means no date and is distinct from 0001-01-01. Dates
// are comparable with ==.
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate returns the date year-month-day, or ErrInvalidDate if no such date
// exists or the year is outside 0001 through 9999.
func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < minDateYear || year > maxDateYear {
		return Date{}, fmt.Errorf("%w: year %d is outside %04d through %04d", ErrInvalidDate, year, minDateYear, maxDateYear)
	}
	if month < time.January || month > time.December {
		return Date{}, fmt.Errorf("%w: month %d", ErrInvalidDate, month)
	}
	if day < 1 || day > daysIn(year, month) {
		return Date{}, fmt.Errorf("%w: day %d in %04d-%02d", ErrInvalidDate, day, year, month)
	}
	return Date{year: year, month: month, day: day}, nil
}

// MustDate is NewDate for literals and tests. It panics on an invalid date.
func MustDate(year int, month time.Month, day int) Date {
	date, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return date
}

// ParseDate parses exactly YYYY-MM-DD. It rejects every other form, including
// a datetime, unpadded fields and an empty string.
func ParseDate(text string) (Date, error) {
	if len(text) != len("2006-01-02") || text[4] != '-' || text[7] != '-' {
		return Date{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidDate, text)
	}
	year, errYear := strconv.Atoi(text[0:4])
	month, errMonth := strconv.Atoi(text[5:7])
	day, errDay := strconv.Atoi(text[8:10])
	if errYear != nil || errMonth != nil || errDay != nil || !allDigits(text[0:4]) || !allDigits(text[5:7]) || !allDigits(text[8:10]) {
		return Date{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidDate, text)
	}
	return NewDate(year, time.Month(month), day)
}

// DateOf returns the calendar date that t shows in its own location, as
// t.Date() reports it. It never converts t to UTC. It returns ErrInvalidDate
// when that year is outside 0001 through 9999.
func DateOf(t time.Time) (Date, error) {
	return dateOfTime(t)
}

func dateOfTime(t time.Time) (Date, error) {
	year, month, day := t.Date()
	return NewDate(year, month, day)
}

// Year, Month and Day return the date's components; the zero Date has zeros.
func (d Date) Year() int         { return d.year }
func (d Date) Month() time.Month { return d.month }
func (d Date) Day() int          { return d.day }

// IsZero reports whether d is the zero value, which means no date.
func (d Date) IsZero() bool { return d == Date{} }

// Compare returns -1, 0 or 1 as d is before, the same as, or after other. The
// zero Date sorts before every real date.
func (d Date) Compare(other Date) int {
	switch {
	case d.year != other.year:
		return compareInts(d.year, other.year)
	case d.month != other.month:
		return compareInts(int(d.month), int(other.month))
	default:
		return compareInts(d.day, other.day)
	}
}

// Before, After and Equal report the order of d relative to other.
func (d Date) Before(other Date) bool { return d.Compare(other) < 0 }
func (d Date) After(other Date) bool  { return d.Compare(other) > 0 }
func (d Date) Equal(other Date) bool  { return d.Compare(other) == 0 }

// AddDays returns the date n days after d. The zero Date stays zero. It
// returns ErrInvalidDate when the result leaves years 0001 through 9999.
func (d Date) AddDays(n int) (Date, error) {
	if d.IsZero() {
		return d, nil
	}
	return DateOf(time.Date(d.year, d.month, d.day+n, 0, 0, 0, 0, time.UTC))
}

// In returns midnight at the start of d's calendar date in loc. The zero Date
// returns the zero time.Time. To take today's date in a zone, use
// DateOf(time.Now().In(loc)).
func (d Date) In(loc *time.Location) time.Time {
	if d.IsZero() {
		return time.Time{}
	}
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, loc)
}

// String returns YYYY-MM-DD, and the empty string for the zero Date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// Value implements driver.Valuer. A nonzero date is bound as the exact string
// YYYY-MM-DD, which every dialect stores in a DATE column. The zero Date is
// NULL.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.String(), nil
}

// Scan implements sql.Scanner. It accepts NULL (the zero Date), a time.Time
// (the calendar components in that value's own location, with no zone
// conversion), and an exact YYYY-MM-DD string or bytes. Anything else is an
// error, including a datetime string.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		date, err := dateOfTime(v)
		if err != nil {
			return err
		}
		*d = date
		return nil
	case string:
		return d.scanText(v)
	case []byte:
		return d.scanText(string(v))
	default:
		return fmt.Errorf("%w: cannot scan %T", ErrInvalidDate, src)
	}
}

func (d *Date) scanText(text string) error {
	date, err := ParseDate(text)
	if err != nil {
		return err
	}
	*d = date
	return nil
}

// MarshalJSON writes the zero Date as null and any other as "YYYY-MM-DD".
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON reads null as the zero Date and otherwise requires an exact
// "YYYY-MM-DD" string. The empty string is rejected.
func (d *Date) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = Date{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidDate, err)
	}
	return d.scanText(text)
}

// MarshalText writes the zero Date as the empty string and any other as
// YYYY-MM-DD.
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText reads the empty string as the zero Date and otherwise requires
// an exact YYYY-MM-DD.
func (d *Date) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*d = Date{}
		return nil
	}
	return d.scanText(string(text))
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func allDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
