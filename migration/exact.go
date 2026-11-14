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

// exactChanges describes the changes to the exact-schema attributes of an
// existing column that no step can express yet. A column carrying any of
// them also cannot change type, uniqueness or indexing: those steps would
// drop its explicit index names, default or NOT NULL.
func exactChanges(name string, current ColumnState, column Column) []string {
	var changes []string
	add := func(what string, from, to any) {
		changes = append(changes, fmt.Sprintf("%s changes %s from %v to %v, which no migration step supports yet", name, what, from, to))
	}
	if current.Width != column.Width {
		add("integer width", widthLabel(current.Width), widthLabel(column.Width))
	}
	if current.Naive != column.Naive {
		add("naive (timestamp without time zone)", current.Naive, column.Naive)
	}
	if current.NotNull != column.NotNull {
		add("NOT NULL", current.NotNull, column.NotNull)
	}
	if current.DefaultKind != column.DefaultKind || current.DefaultValue != column.DefaultValue {
		add("its default", defaultLabel(current.DefaultKind, current.DefaultValue), defaultLabel(column.DefaultKind, column.DefaultValue))
	}
	if current.UniqueName != column.UniqueName {
		add("its unique index name", nameLabel(current.UniqueName), nameLabel(column.UniqueName))
	}
	if current.IndexName != column.IndexName {
		add("its index name", nameLabel(current.IndexName), nameLabel(column.IndexName))
	}
	if len(changes) > 0 || !hasExactAttributes(column) {
		return changes
	}
	if current.Type != column.Type || current.Length != column.Length || current.Unique != column.Unique || current.Indexed != column.Indexed {
		changes = append(changes, fmt.Sprintf("%s changes type, uniqueness or indexing while it declares int4, naive, notnull, default or an index name, which no migration step supports yet", name))
	}
	return changes
}

func hasExactAttributes(c Column) bool {
	return c.Width != 0 || c.Naive || c.NotNull || c.DefaultKind != "" || c.UniqueName != "" || c.IndexName != ""
}

func widthLabel(width int) string {
	if width == int4Width {
		return "int4"
	}
	return "int8"
}

func defaultLabel(kind, value string) string {
	if kind == "" {
		return "none"
	}
	return fmt.Sprintf("%s %q", kind, value)
}

func nameLabel(name string) string {
	if name == "" {
		return "the derived name"
	}
	return name
}
