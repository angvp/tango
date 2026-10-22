package i18n

import (
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// A catalog value is usable for a fallback when it is non-empty and uses the
// same format verbs on the same arguments, so fmt.Sprintf with the caller's
// arguments cannot produce %! output. Flags, width and precision are free;
// only (argument, verb) pairs are compared, which lets a translation reorder
// arguments with %[n]s.

// verbUse is one argument consumed by a format verb (or by a * width or
// precision, recorded with the verb '*').
type verbUse struct {
	arg  int
	verb rune
}

const knownVerbs = "vTtbcdoOqxXUeEfFgGsp"

// verbSet returns the sorted, de-duplicated (argument, verb) pairs of format.
// ok is false when fmt would report a malformed directive.
func verbSet(format string) (uses []verbUse, ok bool) {
	arg := 0
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		var good bool
		if arg, i, good = bracketIndex(format, i, arg); !good {
			return nil, false
		}
		if i < len(format) && format[i] == '*' {
			uses = append(uses, verbUse{arg, '*'})
			arg++
			i++
		} else {
			i = skipDigits(format, i)
		}
		if i < len(format) && format[i] == '.' {
			i++
			if arg, i, good = bracketIndex(format, i, arg); !good {
				return nil, false
			}
			if i < len(format) && format[i] == '*' {
				uses = append(uses, verbUse{arg, '*'})
				arg++
				i++
			} else {
				i = skipDigits(format, i)
			}
		}
		if arg, i, good = bracketIndex(format, i, arg); !good {
			return nil, false
		}
		if i >= len(format) {
			return nil, false // %!(NOVERB)
		}
		verb, size := utf8.DecodeRuneInString(format[i:])
		i += size
		if verb == '%' {
			continue
		}
		if !strings.ContainsRune(knownVerbs, verb) {
			return nil, false
		}
		uses = append(uses, verbUse{arg, verb})
		arg++
	}
	slices.SortFunc(uses, func(a, b verbUse) int {
		if a.arg != b.arg {
			return a.arg - b.arg
		}
		return int(a.verb - b.verb)
	})
	return slices.Compact(uses), true
}

// bracketIndex reads an optional [n] argument index at format[i:]. It returns
// the argument to use next and the new position; ok is false for a malformed
// index.
func bracketIndex(format string, i, arg int) (next, pos int, ok bool) {
	if i >= len(format) || format[i] != '[' {
		return arg, i, true
	}
	end := strings.IndexByte(format[i:], ']')
	if end < 0 {
		return 0, i, false
	}
	n := 0
	digits := format[i+1 : i+end]
	if digits == "" {
		return 0, i, false
	}
	for _, c := range digits {
		if c < '0' || c > '9' || n > 1<<20 {
			return 0, i, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return 0, i, false
	}
	return n - 1, i + end + 1, true
}

func skipDigits(format string, i int) int {
	for i < len(format) && format[i] >= '0' && format[i] <= '9' {
		i++
	}
	return i
}

// usableCacheLimit bounds the cache of verb comparisons. Fallbacks are meant
// to be literals; a host that builds them from request data would otherwise
// grow it without limit. Past the limit comparisons are made without caching.
const usableCacheLimit = 4096

type valuePair struct{ value, fallback string }

var (
	usableCache sync.Map // valuePair -> bool
	usableCount int64
	usableMu    sync.Mutex
)

// usable reports whether a catalog value may stand in for fallback.
func usable(value, fallback string) bool {
	if value == "" {
		return false
	}
	// Most text has no verbs: nothing to compare and nothing to cache.
	if strings.IndexByte(value, '%') < 0 && strings.IndexByte(fallback, '%') < 0 {
		return true
	}
	pair := valuePair{value, fallback}
	if cached, ok := usableCache.Load(pair); ok {
		return cached.(bool)
	}
	result := compareVerbs(value, fallback)
	usableMu.Lock()
	if usableCount < usableCacheLimit {
		if _, loaded := usableCache.LoadOrStore(pair, result); !loaded {
			usableCount++
		}
	}
	usableMu.Unlock()
	return result
}

func compareVerbs(value, fallback string) bool {
	want, ok := verbSet(fallback)
	if !ok {
		return true // the host's own malformed fallback: trust the translation
	}
	got, ok := verbSet(value)
	return ok && slices.Equal(got, want)
}
