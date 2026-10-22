package i18n

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// observedKeyLimit is how many distinct keys the registry records. Fallbacks
// are meant to be literals; a host that builds them from request data would
// otherwise grow the registry without bound. Once it is full no new key is
// recorded (at no cost) and Validate reports it.
const observedKeyLimit = 4096

var (
	observedMu   sync.RWMutex
	observed     = make(map[string]string)           // key -> first fallback seen
	conflicts    = make(map[string]fallbackConflict) // key -> the first disagreement
	observedFull atomic.Bool
)

type fallbackConflict struct{ first, other string }

// Key is a translation key together with the fallback text of an i18n.T call
// that has run in this process.
type Key struct {
	Key      string
	Fallback string
}

// observe records the (key, fallback) pair of a T call made with a resolved
// locale. The common case, a key already recorded with the same fallback, is
// a read-locked map lookup that allocates nothing.
func observe(key, fallback string) {
	observedMu.RLock()
	first, known := observed[key]
	observedMu.RUnlock()
	if known && first == fallback {
		return
	}
	if !known && observedFull.Load() {
		return
	}

	observedMu.Lock()
	defer observedMu.Unlock()
	first, known = observed[key]
	switch {
	case !known:
		if len(observed) >= observedKeyLimit {
			observedFull.Store(true)
			return
		}
		observed[key] = fallback
	case first != fallback:
		if _, seen := conflicts[key]; !seen {
			conflicts[key] = fallbackConflict{first: first, other: fallback}
		}
	}
}

// Keys returns the keys observed so far, sorted by key. Only text that has
// run with a resolved locale is known: a key no code path has executed is
// not listed. The slice is a copy.
func Keys() []Key {
	observedMu.RLock()
	keys := make([]Key, 0, len(observed))
	for key, fallback := range observed {
		keys = append(keys, Key{Key: key, Fallback: fallback})
	}
	observedMu.RUnlock()
	slices.SortFunc(keys, func(a, b Key) int { return strings.Compare(a.Key, b.Key) })
	return keys
}

// ProblemKind says what is wrong with a Problem.
type ProblemKind string

const (
	// ProblemMismatch: a catalog value's format verbs differ from the
	// fallback's, so T would ignore it.
	ProblemMismatch ProblemKind = "mismatch"
	// ProblemEmpty: a catalog value is empty, so T would ignore it.
	ProblemEmpty ProblemKind = "empty"
	// ProblemConflictingFallback: one key was observed with two different
	// fallback texts.
	ProblemConflictingFallback ProblemKind = "conflicting-fallback"
	// ProblemRegistryFull: more distinct keys ran than the registry records
	// (translation fallbacks must be stable literals, never request- or
	// user-derived text), so Validate has not seen every key.
	ProblemRegistryFull ProblemKind = "registry-full"
)

// Problem is one finding of Validate. Locale is empty for a problem that
// belongs to a key rather than a catalog.
type Problem struct {
	Kind   ProblemKind
	Locale string
	Key    string
	Detail string
}

// Unobserved is a catalog key that no observed call uses, such as a typo.
type Unobserved struct {
	Locale string
	Key    string
}

// Report is the result of Validate.
type Report struct {
	// Problems are real defects, sorted by locale, key and kind.
	Problems []Problem
	// Unobserved is advisory, sorted by locale then key: a page that did not
	// run in this process leaves its keys here. A host whose test exercised
	// every page may treat a non-empty list as a failure.
	Unobserved []Unobserved
}

// Validate checks every registered catalog against the keys observed so far
// (see Keys). It is a check of what has run in this process, not a startup or
// static guarantee: exercise the application's pages in the locale first, for
// instance in a test.
func Validate() Report {
	catalogsMu.RLock()
	defer catalogsMu.RUnlock()
	observedMu.RLock()
	defer observedMu.RUnlock()

	var report Report
	locales := make([]string, 0, len(catalogs))
	for locale := range catalogs {
		locales = append(locales, locale)
	}
	slices.Sort(locales)

	full := len(observed) >= observedKeyLimit && observedFull.Load()
	for _, locale := range locales {
		for key, value := range catalogs[locale] {
			fallback, known := observed[key]
			switch {
			case !known:
				if !full {
					report.Unobserved = append(report.Unobserved, Unobserved{Locale: locale, Key: key})
				}
			case value == "":
				report.Problems = append(report.Problems, Problem{Kind: ProblemEmpty, Locale: locale, Key: key,
					Detail: "the catalog value is empty, so the fallback " + fmt.Sprintf("%q", fallback) + " is shown"})
			case !compareVerbs(value, fallback):
				report.Problems = append(report.Problems, Problem{Kind: ProblemMismatch, Locale: locale, Key: key,
					Detail: fmt.Sprintf("the value %q does not use the same format verbs as the fallback %q", value, fallback)})
			}
		}
	}
	for key, c := range conflicts {
		report.Problems = append(report.Problems, Problem{Kind: ProblemConflictingFallback, Key: key,
			Detail: fmt.Sprintf("observed with the fallbacks %q and %q", c.first, c.other)})
	}
	if full {
		report.Problems = append(report.Problems, Problem{Kind: ProblemRegistryFull,
			Detail: fmt.Sprintf("more than %d distinct keys ran; fallbacks must be stable literals, not request- or user-derived text", observedKeyLimit)})
	}

	slices.SortFunc(report.Problems, func(a, b Problem) int {
		return cmpStrings(a.Locale, b.Locale, a.Key, b.Key, string(a.Kind), string(b.Kind))
	})
	slices.SortFunc(report.Unobserved, func(a, b Unobserved) int {
		return cmpStrings(a.Locale, b.Locale, a.Key, b.Key)
	})
	return report
}

// cmpStrings compares pairs of strings in order: (a1, b1), (a2, b2), ...
func cmpStrings(pairs ...string) int {
	for i := 0; i+1 < len(pairs); i += 2 {
		if c := strings.Compare(pairs[i], pairs[i+1]); c != 0 {
			return c
		}
	}
	return 0
}

func resetObservedForTest() {
	observedMu.Lock()
	defer observedMu.Unlock()
	observed = make(map[string]string)
	conflicts = make(map[string]fallbackConflict)
	observedFull.Store(false)
}
