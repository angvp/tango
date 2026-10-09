// Package shellcore is the evaluator, formatter and built-in helpers behind
// the public shell package. Nothing here is covered API.
package shellcore

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// bytesPrefix is how many bytes of a []byte value are shown.
const bytesPrefix = 16

// Format renders v the way the shell prints results: deterministic, one
// line per value, one line per row for a list of maps. A zero v prints
// nothing. limit is the most runes a single line may hold before it is cut
// with an ellipsis; 0 never truncates.
func Format(v any, limit int) string {
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	if rows, ok := asRows(rv); ok {
		lines := make([]string, len(rows))
		for i, row := range rows {
			lines[i] = truncate(formatValue(row), limit)
		}
		return strings.Join(lines, "\n")
	}
	return truncate(formatValue(rv), limit)
}

func truncate(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}

// asRows reports whether rv is a non-empty list whose elements are all maps.
func asRows(rv reflect.Value) ([]reflect.Value, bool) {
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	if rv.Len() == 0 || rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	rows := make([]reflect.Value, rv.Len())
	for i := range rows {
		elem := unwrap(rv.Index(i))
		if elem.Kind() != reflect.Map {
			return nil, false
		}
		rows[i] = elem
	}
	return rows, true
}

// unwrap follows interfaces so a value held as any formats as what it holds.
func unwrap(rv reflect.Value) reflect.Value {
	for rv.Kind() == reflect.Interface && !rv.IsNil() {
		rv = rv.Elem()
	}
	return rv
}

var (
	timeType  = reflect.TypeOf(time.Time{})
	errorType = reflect.TypeOf((*error)(nil)).Elem()
)

func formatValue(rv reflect.Value) string {
	rv = unwrap(rv)
	if !rv.IsValid() || (rv.Kind() == reflect.Interface && rv.IsNil()) {
		return "nil"
	}
	if rv.Type() == timeType {
		return rv.Interface().(time.Time).Format(time.RFC3339Nano)
	}
	if rv.Type().Implements(errorType) && rv.CanInterface() && !(rv.Kind() == reflect.Pointer && rv.IsNil()) {
		return fmt.Sprintf("error(%q)", rv.Interface().(error).Error())
	}
	switch rv.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", rv.String())
	case reflect.Pointer:
		if rv.IsNil() {
			return "nil"
		}
		if rv.Elem().Kind() == reflect.Struct {
			return "&" + formatValue(rv.Elem())
		}
		return formatValue(rv.Elem())
	case reflect.Map:
		return formatMap(rv)
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return formatBytes(rv)
		}
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = formatValue(rv.Index(i))
		}
		return "[" + strings.Join(parts, " ") + "]"
	case reflect.Struct:
		var parts []string
		for i := 0; i < rv.NumField(); i++ {
			if !rv.Type().Field(i).IsExported() {
				continue
			}
			parts = append(parts, rv.Type().Field(i).Name+":"+formatValue(rv.Field(i)))
		}
		return "{" + strings.Join(parts, " ") + "}"
	default:
		if rv.CanInterface() {
			return fmt.Sprint(rv.Interface())
		}
		return fmt.Sprint(rv)
	}
}

func formatMap(rv reflect.Value) string {
	type entry struct{ key, text string }
	entries := make([]entry, 0, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key := fmt.Sprint(unwrap(iter.Key()).Interface())
		entries = append(entries, entry{key, key + ":" + formatValue(iter.Value())})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.text
	}
	return "map[" + strings.Join(parts, " ") + "]"
}

func formatBytes(rv reflect.Value) string {
	n := rv.Len()
	shown := min(n, bytesPrefix)
	prefix := make([]byte, shown)
	for i := range prefix {
		prefix[i] = byte(rv.Index(i).Uint())
	}
	text := fmt.Sprintf("%q", string(prefix))
	if n > shown {
		text = text[:len(text)-1] + `…"`
	}
	return fmt.Sprintf("[]byte(%d) %s", n, text)
}
