package model

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// schemaAttrs are the exact-schema attributes a field declares through the
// int4, naive, notnull, default=, unique= and index= tag options. The zero
// value means none of them, which keeps a field's column exactly as before
// these options existed.
type schemaAttrs struct {
	Int4         bool
	Naive        bool
	NotNull      bool
	DefaultKind  string
	DefaultValue string
	UniqueName   string
	IndexName    string
}

// newSchemaOptions are the options whose presence switches a tag to strict
// parsing: every part must then be an option tanGO recognizes. A tag without
// any of them keeps the historical rule that unknown keys are ignored, so no
// existing tag changes meaning.
var newSchemaOptions = map[string]bool{
	"int4": true, "naive": true, "notnull": true,
	"default": true, "unique=": true, "index=": true,
}

var (
	bareOptions  = map[string]bool{"pk": true, "unique": true, "index": true, "text": true, "json": true, "int4": true, "naive": true, "notnull": true}
	keyedOptions = map[string]bool{"fk": true, "varchar": true, "default": true, "unique": true, "index": true}
	indexNameRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// maxIndexNameBytes is PostgreSQL's identifier limit; a longer name would be
// truncated silently there.
const maxIndexNameBytes = 63

type tagPart struct {
	name     string
	value    string
	hasValue bool
}

func splitTag(tag string) []tagPart {
	raw := strings.Split(tag, ",")
	parts := make([]tagPart, 0, len(raw))
	for _, p := range raw {
		name, value, hasValue := strings.Cut(strings.TrimSpace(p), "=")
		parts = append(parts, tagPart{name: strings.TrimSpace(name), value: strings.TrimSpace(value), hasValue: hasValue})
	}
	return parts
}

func usesNewSchemaOption(parts []tagPart) bool {
	for _, p := range parts {
		key := p.name
		if p.hasValue && (p.name == "unique" || p.name == "index") {
			key += "="
		}
		if newSchemaOptions[key] {
			return true
		}
	}
	return false
}

// schemaTags reads the exact-schema options from tag for a field of type t
// whose varchar bound is maxLength (0 when unbounded).
func schemaTags(tag string, t reflect.Type, maxLength int) (schemaAttrs, error) {
	var attrs schemaAttrs
	if strings.TrimSpace(tag) == "" {
		return attrs, nil
	}
	parts := splitTag(tag)
	if !usesNewSchemaOption(parts) {
		return attrs, nil
	}
	if err := checkPartsRecognized(parts); err != nil {
		return attrs, err
	}

	counts := map[string]int{}
	for _, p := range parts {
		key := p.name
		if p.hasValue && (p.name == "unique" || p.name == "index") {
			key += "="
		}
		counts[key]++
	}
	for _, opt := range []string{"int4", "naive", "notnull", "default", "unique=", "index="} {
		if counts[opt] > 1 {
			return attrs, fmt.Errorf("%s is declared more than once", strings.TrimSuffix(opt, "="))
		}
	}
	if counts["unique"] > 0 && counts["unique="] > 0 {
		return attrs, fmt.Errorf("unique and unique=<name> cannot be combined")
	}
	if counts["index"] > 0 && counts["index="] > 0 {
		return attrs, fmt.Errorf("index and index=<name> cannot be combined")
	}

	primaryKey := counts["pk"] > 0
	_, foreignKey := tagValue(tag, "fk")

	for _, p := range parts {
		switch {
		case p.name == "int4":
			if p.hasValue {
				return attrs, fmt.Errorf("int4 takes no value, got %q", p.name+"="+p.value)
			}
			if !isSignedInt(t) {
				return attrs, fmt.Errorf("int4 applies to signed integer fields, not %s", t)
			}
			attrs.Int4 = true
		case p.name == "naive":
			if p.hasValue {
				return attrs, fmt.Errorf("naive takes no value, got %q", p.name+"="+p.value)
			}
			if t != timeType {
				return attrs, fmt.Errorf("naive applies to time.Time fields, not %s", t)
			}
			attrs.Naive = true
		case p.name == "notnull":
			if p.hasValue {
				return attrs, fmt.Errorf("notnull takes no value, got %q", p.name+"="+p.value)
			}
			if primaryKey {
				return attrs, fmt.Errorf("notnull is redundant on a primary key: a primary key is always NOT NULL")
			}
			attrs.NotNull = true
		case p.name == "unique" && p.hasValue:
			if err := checkIndexName(p.value); err != nil {
				return attrs, err
			}
			attrs.UniqueName = p.value
		case p.name == "index" && p.hasValue:
			if err := checkIndexName(p.value); err != nil {
				return attrs, err
			}
			attrs.IndexName = p.value
		}
	}

	if value, ok := tagValue(tag, "default"); ok {
		switch {
		case value == "":
			return attrs, fmt.Errorf("default needs a value, as default=en")
		case primaryKey:
			return attrs, fmt.Errorf("a primary key cannot carry a default")
		case foreignKey:
			return attrs, fmt.Errorf("a foreign key cannot carry a default")
		}
		kind, err := typedDefault(value, t, maxLength, attrs.Int4)
		if err != nil {
			return attrs, err
		}
		attrs.DefaultKind = kind
		attrs.DefaultValue = value
	}
	return attrs, nil
}

func checkPartsRecognized(parts []tagPart) error {
	for _, p := range parts {
		if p.name == "" && !p.hasValue {
			return fmt.Errorf("empty part in tag: a comma ends an option, so a trailing or doubled comma is not allowed")
		}
		if p.hasValue && strings.ContainsAny(p.value, `\`+"\x00") && (p.name == "default" || p.name == "unique" || p.name == "index") {
			return fmt.Errorf("%s value %q contains a backslash or NUL, which is not allowed", p.name, p.value)
		}
		if p.hasValue && keyedOptions[p.name] {
			continue
		}
		if !p.hasValue && bareOptions[p.name] {
			continue
		}
		if p.hasValue && bareOptions[p.name] {
			return fmt.Errorf("%s takes no value, got %q", p.name, p.name+"="+p.value)
		}
		label := p.name
		if p.hasValue {
			label += "=" + p.value
		}
		return fmt.Errorf("unrecognized part %q: a comma ends a value, so text after it must be another tango option", label)
	}
	return nil
}

func checkIndexName(name string) error {
	if !indexNameRE.MatchString(name) || len(name) > maxIndexNameBytes {
		return fmt.Errorf("index name %q must match [A-Za-z_][A-Za-z0-9_]* and be at most %d bytes", name, maxIndexNameBytes)
	}
	return nil
}

func isSignedInt(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

// typedDefault validates value as a default for a field of type t and
// returns its kind: "string", "boolean" or "integer".
func typedDefault(value string, t reflect.Type, maxLength int, int4 bool) (string, error) {
	switch t.Kind() {
	case reflect.String:
		if maxLength > 0 && utf8.RuneCountInString(value) > maxLength {
			return "", fmt.Errorf("default %q is %d characters, longer than varchar=%d", value, utf8.RuneCountInString(value), maxLength)
		}
		return "string", nil
	case reflect.Bool:
		if value != "true" && value != "false" {
			return "", fmt.Errorf("a bool default must be true or false, got %q", value)
		}
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("an integer default must be a base-10 integer, got %q", value)
		}
		if reflect.New(t).Elem().OverflowInt(n) || (int4 && (n < math.MinInt32 || n > math.MaxInt32)) {
			return "", fmt.Errorf("default %s is out of range for %s", value, t)
		}
		return "integer", nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("an integer default must be a base-10 integer, got %q", value)
		}
		if reflect.New(t).Elem().OverflowUint(n) {
			return "", fmt.Errorf("default %s is out of range for %s", value, t)
		}
		return "integer", nil
	}
	return "", fmt.Errorf("default applies to string, bool and integer fields, not %s", t)
}
