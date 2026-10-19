// Package sqlbind writes one query with numbered placeholders ($1, $2, …)
// for every dialect, so tanGO's own raw SQL (migrations, tests) does not have
// to be duplicated per placeholder style.
package sqlbind

import (
	"fmt"
	"strconv"
	"strings"
)

// Rebind returns query ready for a dialect whose placeholders are numbered
// (numbered true: the query is returned unchanged) or positional ("?").
// Outside single-quoted string literals each $N becomes "?", which is only
// meaningful when the placeholders appear once each, in order 1, 2, 3, …:
// anything else panics, since silently rebinding would change which argument
// goes where.
func Rebind(query string, numbered bool) string {
	if numbered {
		return query
	}
	var out strings.Builder
	next := 1
	inLiteral := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'':
			inLiteral = !inLiteral // a doubled '' flips twice, staying in the literal
			out.WriteByte(c)
		case c == '$' && !inLiteral && i+1 < len(query) && isDigit(query[i+1]):
			j := i + 1
			for j < len(query) && isDigit(query[j]) {
				j++
			}
			n, _ := strconv.Atoi(query[i+1 : j])
			if n != next {
				panic(fmt.Sprintf("sqlbind: $%d where $%d was expected; positional placeholders cannot reuse or reorder arguments", n, next))
			}
			next++
			out.WriteByte('?')
			i = j - 1
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
