package i18n

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func resolved(locale string) context.Context { return WithLocale(context.Background(), locale) }

func TestKeysListsWhatRanWithAResolvedLocale(t *testing.T) {
	resetCatalogsForTest()
	T(context.Background(), "unwired.key", "Never recorded") // no locale: not observed
	T(resolved("es"), "b.key", "B")                          // no catalog for es: still observed
	T(resolved("es"), "a.key", "A %s", "x")
	T(resolved("fr"), "a.key", "A %s", "y") // same pair again

	want := []Key{{"a.key", "A %s"}, {"b.key", "B"}}
	if got := Keys(); !reflect.DeepEqual(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
}

func TestKeysReturnsACopy(t *testing.T) {
	resetCatalogsForTest()
	T(resolved("es"), "k", "K")
	keys := Keys()
	keys[0].Fallback = "tampered"
	if got := Keys()[0].Fallback; got != "K" {
		t.Errorf("caller mutated the registry: %q", got)
	}
}

func TestValidateChecksCatalogsAgainstObservedKeys(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("fr", map[string]string{"greet": "Bonjour %s %s", "empty": "", "fine": "Bien", "typo.key": "x"})
	RegisterCatalog("es", map[string]string{"greet": "Hola %d", "fine": "Bien", "another.typo": "x"})
	ctx := resolved("es")
	T(ctx, "greet", "Hello %s", "x")
	T(ctx, "empty", "Empty")
	T(ctx, "fine", "Fine")

	report := Validate()
	// Sorted by locale, then key.
	wantProblems := []Problem{
		{Kind: ProblemMismatch, Locale: "es", Key: "greet"},
		{Kind: ProblemEmpty, Locale: "fr", Key: "empty"},
		{Kind: ProblemMismatch, Locale: "fr", Key: "greet"},
	}
	if len(report.Problems) != len(wantProblems) {
		t.Fatalf("Problems = %+v, want %d entries", report.Problems, len(wantProblems))
	}
	for i, want := range wantProblems {
		got := report.Problems[i]
		if got.Kind != want.Kind || got.Locale != want.Locale || got.Key != want.Key {
			t.Errorf("Problems[%d] = %+v, want %+v", i, got, want)
		}
		if got.Detail == "" {
			t.Errorf("Problems[%d] has no detail", i)
		}
	}

	wantUnobserved := []Unobserved{{"es", "another.typo"}, {"fr", "typo.key"}}
	if !reflect.DeepEqual(report.Unobserved, wantUnobserved) {
		t.Errorf("Unobserved = %v, want %v (sorted, advisory)", report.Unobserved, wantUnobserved)
	}
}

func TestValidateIsDeterministicAndHandsOutCopies(t *testing.T) {
	resetCatalogsForTest()
	for _, locale := range []string{"pt", "es", "fr", "de"} {
		RegisterCatalog(locale, map[string]string{"b": "", "a": "", "c": ""})
	}
	T(resolved("es"), "a", "A")
	T(resolved("es"), "b", "B")
	T(resolved("es"), "c", "C")
	first, second := Validate(), Validate()
	if !reflect.DeepEqual(first, second) {
		t.Error("two Validate calls disagree")
	}
	first.Problems[0].Key = "tampered"
	if Validate().Problems[0].Key == "tampered" {
		t.Error("caller mutated internal state")
	}
	for i := 1; i < len(second.Problems); i++ {
		a, b := second.Problems[i-1], second.Problems[i]
		if a.Locale > b.Locale || a.Locale == b.Locale && a.Key > b.Key {
			t.Errorf("Problems not sorted at %d: %+v then %+v", i, a, b)
		}
	}
}

func TestValidateReportsAKeyObservedWithTwoFallbacks(t *testing.T) {
	resetCatalogsForTest()
	T(resolved("es"), "k", "First")
	T(resolved("es"), "k", "Second")
	report := Validate()
	if len(report.Problems) != 1 || report.Problems[0].Kind != ProblemConflictingFallback || report.Problems[0].Key != "k" {
		t.Fatalf("Problems = %+v, want one conflicting fallback for k", report.Problems)
	}
	if got := Keys(); len(got) != 1 || got[0].Fallback != "First" {
		t.Errorf("Keys() = %v, want the first fallback kept", got)
	}
}

func TestValidateTrustsAValueWhenTheObservedFallbackIsMalformed(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"k": "Hola"})
	T(resolved("es"), "k", "Hello %")
	if report := Validate(); len(report.Problems) != 0 {
		t.Errorf("Problems = %+v, want none", report.Problems)
	}
}

func TestObservedRegistryStopsAtItsCap(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"typo": "x"})
	ctx := resolved("es")
	for i := 0; i < observedKeyLimit+50; i++ {
		T(ctx, fmt.Sprintf("key.%d", i), "Text")
	}
	if got := len(Keys()); got != observedKeyLimit {
		t.Errorf("recorded %d keys, want the cap %d", got, observedKeyLimit)
	}
	report := Validate()
	var full bool
	for _, p := range report.Problems {
		if p.Kind == ProblemRegistryFull {
			full = true
		}
	}
	if !full {
		t.Errorf("Problems = %+v, want a registry-full problem", report.Problems)
	}
	if len(report.Unobserved) != 0 {
		t.Errorf("Unobserved = %v, want none once the registry is full (it would be unreliable)", report.Unobserved)
	}
	// Past the cap a key that is already recorded still works and a new key costs nothing.
	if allocs := testing.AllocsPerRun(100, func() { T(ctx, "overflow.key", "Text") }); allocs != 0 {
		t.Errorf("a key past the cap allocated %v times, want 0", allocs)
	}
}

func TestTAllocationGates(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"plain": "Guardar", "verb": "Nuevo %s"})
	wired := resolved("es")
	T(wired, "plain", "Save")
	T(wired, "verb", "New %s")
	T(wired, "absent", "Absent")

	gates := []struct {
		name string
		ctx  context.Context
		key  string
		fb   string
	}{
		{"no locale", context.Background(), "plain", "Save"},
		{"resolved, recorded, translated", wired, "plain", "Save"},
		{"resolved, recorded, translated with verbs", wired, "verb", "New %s"},
		{"resolved, recorded, falling back", wired, "absent", "Absent"},
	}
	for _, g := range gates {
		if allocs := testing.AllocsPerRun(200, func() { T(g.ctx, g.key, g.fb) }); allocs != 0 {
			t.Errorf("%s: %v allocations, want 0", g.name, allocs)
		}
	}
}

func TestObservedRegistryIsSafeUnderConcurrency(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"k0": "x"})
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				T(resolved("es"), fmt.Sprintf("k%d", i%25), "Text")
				if i%50 == 0 {
					Validate()
					Keys()
				}
			}
		}(g)
	}
	wg.Wait()
	if got := len(Keys()); got != 25 {
		t.Errorf("Keys() has %d entries, want 25", got)
	}
}
