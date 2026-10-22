package i18n

import (
	"maps"

	"github.com/angvp/tango/i18n/internal/testhook"
)

func init() { testhook.Isolate = isolate }

// isolate empties every piece of package state and returns a function that
// restores the previous values. The state is process-global, so it must not be
// used while other goroutines call into the package.
func isolate() (restore func()) {
	catalogsMu.Lock()
	savedCatalogs := make(map[string]map[string]string, len(catalogs))
	for locale, catalog := range catalogs {
		savedCatalogs[locale] = maps.Clone(catalog)
	}
	savedCanonical := maps.Clone(canonical)
	catalogs = make(map[string]map[string]string)
	canonical = make(map[string]string)
	catalogsMu.Unlock()

	savedHook := missingHook.Load()
	missingMu.Lock()
	savedSeen := make(map[missingPair]struct{})
	missingSeen.Range(func(k, _ any) bool {
		savedSeen[k.(missingPair)] = struct{}{}
		return true
	})
	savedSeenN := missingN
	missingMu.Unlock()
	resetMissingForTest()

	observedMu.Lock()
	savedObserved := maps.Clone(observed)
	savedConflicts := maps.Clone(conflicts)
	savedFull := observedFull.Load()
	observed = make(map[string]string)
	conflicts = make(map[string]fallbackConflict)
	observedFull.Store(false)
	observedMu.Unlock()

	return func() {
		catalogsMu.Lock()
		catalogs = savedCatalogs
		canonical = savedCanonical
		catalogsMu.Unlock()

		missingMu.Lock()
		missingSeen.Clear()
		for pair := range savedSeen {
			missingSeen.Store(pair, struct{}{})
		}
		missingN = savedSeenN
		missingMu.Unlock()
		missingHook.Store(savedHook)

		observedMu.Lock()
		observed = savedObserved
		conflicts = savedConflicts
		observedFull.Store(savedFull)
		observedMu.Unlock()
	}
}
