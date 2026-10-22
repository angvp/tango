package i18n

import (
	"maps"

	"github.com/angvp/tango/i18n/internal/testhook"
)

// init hands i18ntest the one function it needs. It registers nothing about
// the application: it only lets the test-support package reach this package's
// state without exporting a reset.
func init() { testhook.Isolate = isolate }

// isolate empties every piece of package state and returns a function that
// restores the previous values. The state is process-global, so it must not be
// used while other goroutines call into the package.
func isolate() (restore func()) {
	restores := []func(){isolateCatalogs(), isolateMissing(), isolateObserved()}
	return func() {
		for _, restore := range restores {
			restore()
		}
	}
}

func isolateCatalogs() (restore func()) {
	catalogsMu.Lock()
	defer catalogsMu.Unlock()
	savedCatalogs := make(map[string]map[string]string, len(catalogs))
	for locale, catalog := range catalogs {
		savedCatalogs[locale] = maps.Clone(catalog)
	}
	savedCanonical := maps.Clone(canonical)
	catalogs = make(map[string]map[string]string)
	canonical = make(map[string]string)
	return func() {
		catalogsMu.Lock()
		defer catalogsMu.Unlock()
		catalogs = savedCatalogs
		canonical = savedCanonical
	}
}

func isolateObserved() (restore func()) {
	observedMu.Lock()
	defer observedMu.Unlock()
	savedObserved := maps.Clone(observed)
	savedConflicts := maps.Clone(conflicts)
	savedFull := observedFull.Load()
	observed = make(map[string]string)
	conflicts = make(map[string]fallbackConflict)
	observedFull.Store(false)
	return func() {
		observedMu.Lock()
		defer observedMu.Unlock()
		observed = savedObserved
		conflicts = savedConflicts
		observedFull.Store(savedFull)
	}
}
