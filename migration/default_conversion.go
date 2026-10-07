package migration

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	integerLiteral = regexp.MustCompile(`^-?[0-9]+$`)
	realLiteral    = regexp.MustCompile(`^-?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?$`)
)

// convertDefault converts def, a column's raw SQL default literal, by the
// same Widening type change as the column's values, so the converted
// default equals what a row holding the old default converts to. An empty
// def converts to an empty one. It fails for a default that is not a
// plain literal of type from, such as an expression.
func convertDefault(from, to, def string) (string, bool) {
	def = strings.TrimSpace(def)
	if def == "" {
		return "", true
	}
	switch (wideningKey{from, to}) {
	case wideningKey{"integer", "real"}:
		if integerLiteral.MatchString(def) {
			return def + ".0", true
		}
	case wideningKey{"integer", "text"}:
		if integerLiteral.MatchString(def) {
			return "'" + def + "'", true
		}
	case wideningKey{"real", "text"}:
		if realLiteral.MatchString(def) {
			if f, err := strconv.ParseFloat(def, 64); err == nil {
				return "'" + canonicalRealText(f) + "'", true
			}
		}
	case wideningKey{"boolean", "integer"}:
		if b, ok := booleanLiteral(def); ok {
			return map[bool]string{true: "1", false: "0"}[b], true
		}
	case wideningKey{"boolean", "text"}:
		if b, ok := booleanLiteral(def); ok {
			return map[bool]string{true: "'true'", false: "'false'"}[b], true
		}
	}
	return "", false
}

func booleanLiteral(def string) (bool, bool) {
	switch strings.ToLower(def) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}

// canonicalRealText writes f the way PostgreSQL's float8 text output does,
// which the SQLite real-to-text conversion also produces: the shortest
// digits that round-trip, without a trailing ".0", in exponent form when
// the decimal exponent is below -4 or at least 15.
func canonicalRealText(f float64) string {
	if f == 0 {
		return "0"
	}
	exponent := int(math.Floor(math.Log10(math.Abs(f))))
	if exponent < -4 || exponent >= 15 {
		return strconv.FormatFloat(f, 'e', -1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
