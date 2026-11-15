package migration

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/angvp/tango/db"
)

// int4Width is Column.Width for a 4-byte integer.
const int4Width = 32

// maxIndexNameBytes is PostgreSQL's identifier limit; a longer name would be
// truncated silently there.
const maxIndexNameBytes = 63

var indexNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// defaultKindForType is the only DefaultKind each column type accepts.
var defaultKindForType = map[string]string{
	"text":    "string",
	"varchar": "string",
	"boolean": "boolean",
	"integer": "integer",
}

// columnUniqueName is the name of c's unique index: UniqueName, or the
// derived uniq_<table>_<col>.
func columnUniqueName(table string, c Column) string {
	if c.UniqueName != "" {
		return c.UniqueName
	}
	return uniqueIndexName(table, c.Name)
}

// columnIndexName is the name of c's plain index: IndexName, or the derived
// idx_<table>_<col>.
func columnIndexName(table string, c Column) string {
	if c.IndexName != "" {
		return c.IndexName
	}
	return indexName(table, c.Name)
}

// exactClausesSQL is the " NOT NULL" and " DEFAULT <literal>" suffix of a
// column definition of SQL type columnType, or "" for a column with neither.
func exactClausesSQL(dialect db.Dialect, columnType string, c Column) string {
	var b strings.Builder
	if c.NotNull {
		b.WriteString(" NOT NULL")
	}
	if c.DefaultKind != "" {
		b.WriteString(" DEFAULT ")
		b.WriteString(defaultSQL(dialect, columnType, c.DefaultKind, c.DefaultValue))
	}
	return b.String()
}

// defaultSQL renders a typed default as a literal for dialect. A string is
// single-quoted with quotes doubled; validateExact has already refused the
// backslash and NUL that would need dialect-specific escaping. MySQL accepts
// a TEXT default only as a parenthesized expression.
func defaultSQL(dialect db.Dialect, columnType, kind, value string) string {
	switch kind {
	case "string":
		return defaultLiteralSQL(dialect, columnType, "'"+strings.ReplaceAll(value, "'", "''")+"'")
	case "boolean":
		return strings.ToUpper(value)
	}
	return value
}

// validateExact checks the exact-schema fields of c.
func validateExact(table string, c Column) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("tango migration: %s.%s: "+format, append([]any{table, c.Name}, args...)...)
	}
	switch {
	case c.Width != 0 && c.Width != int4Width:
		return fail("width must be 0 or %d, got %d", int4Width, c.Width)
	case c.Width != 0 && c.Type != "integer":
		return fail("width applies to integer columns, not %s", c.Type)
	case c.Naive && c.Type != "timestamp":
		return fail("naive applies to timestamp columns, not %s", c.Type)
	case c.NotNull && c.PrimaryKey:
		return fail("NotNull is redundant on a primary key")
	case c.Default != "" && (c.NotNull || c.DefaultKind != "" || c.DefaultValue != ""):
		return fail("the legacy Default cannot be combined with NotNull or a typed default")
	case c.UniqueName != "" && !c.Unique:
		return fail("UniqueName needs Unique")
	case c.IndexName != "" && !c.Indexed:
		return fail("IndexName needs Indexed")
	}
	for _, name := range []string{c.UniqueName, c.IndexName} {
		if name != "" && (!indexNameRE.MatchString(name) || len(name) > maxIndexNameBytes) {
			return fail("index name %q must match [A-Za-z_][A-Za-z0-9_]* and be at most %d bytes", name, maxIndexNameBytes)
		}
	}
	if err := validateDefault(c); err != nil {
		return fail("%v", err)
	}
	return nil
}

func validateDefault(c Column) error {
	if c.DefaultKind == "" {
		if c.DefaultValue != "" {
			return fmt.Errorf("DefaultValue needs a DefaultKind")
		}
		return nil
	}
	want, ok := defaultKindForType[c.Type]
	switch {
	case c.DefaultKind != "string" && c.DefaultKind != "boolean" && c.DefaultKind != "integer":
		return fmt.Errorf("unknown default kind %q", c.DefaultKind)
	case !ok || want != c.DefaultKind:
		return fmt.Errorf("a %s default does not fit a %s column", c.DefaultKind, c.Type)
	case c.PrimaryKey:
		return fmt.Errorf("a primary key cannot carry a default")
	}
	switch c.DefaultKind {
	case "string":
		if strings.ContainsAny(c.DefaultValue, "\\\x00") {
			return fmt.Errorf("default %q contains a backslash or NUL", c.DefaultValue)
		}
		if c.Type == "varchar" && utf8.RuneCountInString(c.DefaultValue) > c.Length {
			return fmt.Errorf("default %q is longer than varchar(%d)", c.DefaultValue, c.Length)
		}
	case "boolean":
		if c.DefaultValue != "true" && c.DefaultValue != "false" {
			return fmt.Errorf("a boolean default must be true or false, got %q", c.DefaultValue)
		}
	case "integer":
		n, err := strconv.ParseInt(c.DefaultValue, 10, 64)
		if err != nil {
			return fmt.Errorf("an integer default must be a base-10 integer, got %q", c.DefaultValue)
		}
		if c.Width == int4Width && (n < math.MinInt32 || n > math.MaxInt32) {
			return fmt.Errorf("default %d is out of range for a 4-byte integer", n)
		}
	}
	return nil
}

// typeToken names a column's type for the widening rules: an int4 and a
// timestamp without time zone are distinct types there.
func typeToken(columnType string, width int, naive bool) string {
	switch {
	case columnType == "integer" && width == int4Width:
		return "int4"
	case columnType == "timestamp" && naive:
		return "timestamp_naive"
	}
	return columnType
}

func tokenLabel(token string, length int) string {
	if token == "timestamp_naive" {
		return "naive timestamp"
	}
	return typeLabel(token, length)
}

func columnToken(c Column) string     { return typeToken(c.Type, c.Width, c.Naive) }
func stateToken(c ColumnState) string { return typeToken(c.Type, c.Width, c.Naive) }

// typeChanged reports whether a column's type, length, integer width or time
// zone differs from its replayed state.
func typeChanged(current ColumnState, column Column) bool {
	return current.Type != column.Type || current.Length != column.Length ||
		current.Width != column.Width || current.Naive != column.Naive
}

func widthLabel(width int) string {
	if width == int4Width {
		return "int4"
	}
	return "int8"
}

// validateAlterExact checks the width and time-zone fields at both ends of an
// AlterColumnType, and its carried typed default.
func validateAlterExact(s AlterColumnType) error {
	ends := []struct {
		label string
		typ   string
		width int
		naive bool
	}{
		{"from", s.From, s.FromWidth, s.FromNaive},
		{"to", s.To, s.ToWidth, s.ToNaive},
	}
	for _, end := range ends {
		switch {
		case end.width != 0 && end.width != int4Width:
			return fmt.Errorf("tango migration: %s.%s: %s width must be 0 or %d, got %d", s.Table, s.Column, end.label, int4Width, end.width)
		case end.width != 0 && end.typ != "integer":
			return fmt.Errorf("tango migration: %s.%s: %s width applies to integer, not %s", s.Table, s.Column, end.label, end.typ)
		case end.naive && end.typ != "timestamp":
			return fmt.Errorf("tango migration: %s.%s: %s naive applies to timestamp, not %s", s.Table, s.Column, end.label, end.typ)
		}
	}
	return validateTypedDefault(s.Table, s.Column, s.DefaultKind, s.DefaultValue)
}

// validateTypedDefault checks a typed default carried by a step: a known kind
// with a value of that kind. The column type is not known to the step, so the
// kind-to-type fit is checked where the column is.
func validateTypedDefault(table, column, kind, value string) error {
	switch kind {
	case "":
		if value != "" {
			return fmt.Errorf("tango migration: %s.%s: DefaultValue needs a DefaultKind", table, column)
		}
	case "string":
		if strings.ContainsAny(value, "\\\x00") {
			return fmt.Errorf("tango migration: %s.%s: default %q contains a backslash or NUL", table, column, value)
		}
	case "boolean":
		if value != "true" && value != "false" {
			return fmt.Errorf("tango migration: %s.%s: a boolean default must be true or false, got %q", table, column, value)
		}
	case "integer":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("tango migration: %s.%s: an integer default must be a base-10 integer, got %q", table, column, value)
		}
	default:
		return fmt.Errorf("tango migration: %s.%s: unknown default kind %q", table, column, kind)
	}
	return nil
}

// validateIndexName checks an optional explicit index name on a step.
func validateIndexName(table, column, name string) error {
	if name == "" {
		return nil
	}
	if !indexNameRE.MatchString(name) || len(name) > maxIndexNameBytes {
		return fmt.Errorf("tango migration: %s.%s: index name %q must match [A-Za-z_][A-Za-z0-9_]* and be at most %d bytes", table, column, name, maxIndexNameBytes)
	}
	return nil
}
