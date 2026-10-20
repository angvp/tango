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
// Outside quoted string literals and identifiers each $N becomes "?", which
// is only meaningful when the placeholders appear once each, in order
// 1, 2, 3, …: anything else panics, since silently rebinding would change
// which argument goes where. RebindArgs handles a query that reuses or
// reorders them.
func Rebind(query string, numbered bool) string {
	if numbered {
		return query
	}
	rewritten, order := scan(query)
	for i, n := range order {
		if n != i+1 {
			panic(fmt.Sprintf("sqlbind: $%d where $%d was expected; positional placeholders cannot reuse or reorder arguments (use RebindArgs)", n, i+1))
		}
	}
	return rewritten
}

// RebindArgs rewrites query for a positional dialect like Rebind and also
// returns which numbered argument each "?" takes, 1-indexed, in order: a query
// that uses $1 twice returns order [1 …, 1]. A query with no numbered
// placeholders returns a nil order, meaning the arguments are used as given.
func RebindArgs(query string) (rewritten string, order []int) {
	return scan(query)
}

// scan rewrites each $N outside a quoted literal or identifier to "?" and
// lists the N's in the order they appear.
func scan(query string) (string, []int) {
	var out strings.Builder
	var order []int
	var quote byte // the quote character of the literal or identifier being read, or 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			// A doubled quote closes and reopens, staying inside.
			switch quote {
			case 0:
				quote = c
			case c:
				quote = 0
			}
			out.WriteByte(c)
		case c == '$' && quote == 0 && i+1 < len(query) && isDigit(query[i+1]):
			j := i + 1
			for j < len(query) && isDigit(query[j]) {
				j++
			}
			n, _ := strconv.Atoi(query[i+1 : j])
			order = append(order, n)
			out.WriteByte('?')
			i = j - 1
		default:
			out.WriteByte(c)
		}
	}
	return out.String(), order
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
