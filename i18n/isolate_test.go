package i18n

import (
	"testing"
)

// isolate and isolateMissing mutate process-global state, so none of these
// tests may call t.Parallel.

func catalogEntry(locale, key string) (string, bool) {
	catalogsMu.RLock()
	defer catalogsMu.RUnlock()
	value, ok := catalogs[locale][key]
	return value, ok
}

func catalogSize() (catalogCount, canonicalCount int) {
	catalogsMu.RLock()
	defer catalogsMu.RUnlock()
	return len(catalogs), len(canonical)
}

func TestIsolateEmptiesCatalogsUntilRestore(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	RegisterCatalog("xx-iso", map[string]string{"greeting": "hola"})

	restore := isolate()
	if n, c := catalogSize(); n != 0 || c != 0 {
		t.Fatalf("isolated catalogs: got %d locales, %d canonical; want 0, 0", n, c)
	}
	if _, ok := catalogEntry("xx-iso", "greeting"); ok {
		t.Fatal("isolated state still holds xx-iso/greeting")
	}

	restore()
	if value, ok := catalogEntry("xx-iso", "greeting"); !ok || value != "hola" {
		t.Fatalf("after restore: got %q, %v; want %q, true", value, ok, "hola")
	}
}

func TestIsolateDiscardsWritesMadeWhileIsolated(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	RegisterCatalog("xx-keep", map[string]string{"a": "1"})

	restore := isolate()
	RegisterCatalog("xx-temp", map[string]string{"b": "2"})
	restore()

	if _, ok := catalogEntry("xx-temp", "b"); ok {
		t.Fatal("catalog registered during isolation survived restore")
	}
	if value, ok := catalogEntry("xx-keep", "a"); !ok || value != "1" {
		t.Fatalf("pre-isolation catalog after restore: got %q, %v; want %q, true", value, ok, "1")
	}
}

func TestIsolateNestedRestoresEachLevel(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	RegisterCatalog("xx-a", map[string]string{"k": "A"})

	restoreA := isolate()
	if _, ok := catalogEntry("xx-a", "k"); ok {
		t.Fatal("level A catalog visible inside isolation")
	}
	RegisterCatalog("xx-b", map[string]string{"k": "B"})

	restoreB := isolate()
	if _, ok := catalogEntry("xx-b", "k"); ok {
		t.Fatal("level B catalog visible inside nested isolation")
	}
	RegisterCatalog("xx-c", map[string]string{"k": "C"})
	restoreB()

	if _, ok := catalogEntry("xx-c", "k"); ok {
		t.Fatal("nested write survived the inner restore")
	}
	if value, ok := catalogEntry("xx-b", "k"); !ok || value != "B" {
		t.Fatalf("after inner restore: xx-b got %q, %v; want %q, true", value, ok, "B")
	}
	if _, ok := catalogEntry("xx-a", "k"); ok {
		t.Fatal("after inner restore the outer-level catalog leaked back in")
	}

	restoreA()
	if value, ok := catalogEntry("xx-a", "k"); !ok || value != "A" {
		t.Fatalf("after outer restore: got %q, %v; want %q, true", value, ok, "A")
	}
	if _, ok := catalogEntry("xx-b", "k"); ok {
		t.Fatal("inner-level catalog survived the outer restore")
	}
}

func TestIsolateObservedState(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	observedMu.Lock()
	observed["k"] = "fb"
	conflicts["k"] = fallbackConflict{}
	observedMu.Unlock()
	observedFull.Store(true)

	restore := isolate()
	observedMu.RLock()
	gotObserved, gotConflicts := len(observed), len(conflicts)
	observedMu.RUnlock()
	if gotObserved != 0 || gotConflicts != 0 || observedFull.Load() {
		t.Fatalf("isolated observed: %d keys, %d conflicts, full=%v; want 0, 0, false",
			gotObserved, gotConflicts, observedFull.Load())
	}

	restore()
	observedMu.RLock()
	value, ok := observed["k"]
	_, conflictOK := conflicts["k"]
	observedMu.RUnlock()
	if !ok || value != "fb" || !conflictOK || !observedFull.Load() {
		t.Fatalf("restored observed: key=%q,%v conflict=%v full=%v; want fb,true true true",
			value, ok, conflictOK, observedFull.Load())
	}
}

func TestIsolateMissingHookAndOnceStateRestore(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	var calls []string
	OnMissing(func(locale, key string) { calls = append(calls, locale+"/"+key) })
	reportMissing("en", "P")
	if len(calls) != 1 || calls[0] != "en/P" {
		t.Fatalf("before isolation: calls %v; want [en/P]", calls)
	}

	restore := isolate()
	reportMissing("en", "P")
	reportMissing("en", "Q")
	if len(calls) != 1 {
		t.Fatalf("isolated state invoked the hook: calls %v; want [en/P]", calls)
	}

	restore()
	reportMissing("en", "P")
	if len(calls) != 1 {
		t.Fatalf("already-reported pair re-fired after restore: calls %v", calls)
	}
	reportMissing("en", "R")
	if len(calls) != 2 || calls[1] != "en/R" {
		t.Fatalf("new pair after restore: calls %v; want [en/P en/R]", calls)
	}
}

func TestIsolateMissingWithoutHookReportsNothing(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	OnMissing(nil)
	reportMissing("en", "X")
	if _, seen := missingSeen.Load(missingPair{"en", "X"}); seen {
		t.Fatal("pair recorded as seen while no hook was registered")
	}
}

func TestCmpStrings(t *testing.T) {
	tests := []struct {
		name  string
		pairs []string
		want  int
	}{
		{"no pairs", nil, 0},
		{"single value ignored", []string{"a"}, 0},
		{"all equal pairs", []string{"a", "a", "b", "b"}, 0},
		{"first pair decides", []string{"a", "b", "z", "y"}, -1},
		{"later pair decides", []string{"a", "a", "b", "a"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cmpStrings(tt.pairs...); got != tt.want {
				t.Fatalf("cmpStrings(%q) = %d; want %d", tt.pairs, got, tt.want)
			}
		})
	}
}
