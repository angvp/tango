package migration

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	integerLiteral = regexp.MustCompile(`^-?[0-9]+$`)
	realLiteral    = regexp.MustCompile(`^-?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?$`)
)

// convertDefault converts def, a column's raw SQL default literal, by the
// Widening type change from to to, so the converted default equals what a
// row holding the old default converts to. An empty def converts to an
// empty one. It fails for a pair that is not a Widening type change, and
// for a default that is not a plain literal of type from, such as an
// expression.
func convertDefault(from, to, def string) (string, bool) {
	conversion, ok := wideningConversions[wideningKey{from, to}]
	if !ok {
		return "", false
	}
	def = strings.TrimSpace(def)
	if def == "" {
		return "", true
	}
	return conversion.convertDefault(def)
}

func integerDefault(convert func(string) string) func(string) (string, bool) {
	return func(def string) (string, bool) {
		if !integerLiteral.MatchString(def) {
			return "", false
		}
		return convert(def), true
	}
}

func realToTextDefault(def string) (string, bool) {
	if !realLiteral.MatchString(def) {
		return "", false
	}
	f, err := strconv.ParseFloat(def, 64)
	if err != nil {
		return "", false
	}
	return "'" + canonicalRealText(f) + "'", true
}

func booleanDefault(whenTrue, whenFalse string) func(string) (string, bool) {
	return func(def string) (string, bool) {
		switch strings.ToLower(def) {
		case "true", "1":
			return whenTrue, true
		case "false", "0":
			return whenFalse, true
		}
		return "", false
	}
}

// canonicalRealText writes f the way PostgreSQL's float8 text output does,
// which the SQLite real-to-text conversion also produces: the shortest
// digits that round-trip, without a trailing ".0", in exponent form when
// the decimal exponent is below -4 or at least 15.
func canonicalRealText(f float64) string {
	scientific := strconv.FormatFloat(f, 'e', -1, 64)
	// The exponent is read from the shortest digits themselves: computing
	// it with Log10 is off by one at exact powers of ten such as 1e15.
	exponent, err := strconv.Atoi(scientific[strings.LastIndexByte(scientific, 'e')+1:])
	if err != nil || exponent < -4 || exponent >= 15 {
		return scientific
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
