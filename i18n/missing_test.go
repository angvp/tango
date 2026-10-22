package i18n

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type missingEvent struct{ locale, key string }

// recordMissing installs a hook that appends every report and returns the
// accessor. The hook is cleared when the test ends.
func recordMissing(t *testing.T) func() []missingEvent {
	t.Helper()
	var mu sync.Mutex
	var events []missingEvent
	OnMissing(func(locale, key string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, missingEvent{locale, key})
	})
	t.Cleanup(func() { OnMissing(nil) })
	return func() []missingEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]missingEvent(nil), events...)
	}
}

func TestOnMissingReportsEachCauseOncePerLocaleAndKey(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"empty": "", "bad": "Nuevo", "ok": "Hola"})
	events := recordMissing(t)
	ctx := WithLocale(context.Background(), "es")

	T(ctx, "absent", "Absent")
	T(ctx, "empty", "Empty")
	T(ctx, "bad", "New %s", "x")
	T(ctx, "ok", "Hello")
	for i := 0; i < 3; i++ { // repeats report nothing new
		T(ctx, "absent", "Absent")
		T(ctx, "empty", "Empty")
		T(ctx, "bad", "New %s", "x")
	}

	want := []missingEvent{{"es", "absent"}, {"es", "empty"}, {"es", "bad"}}
	got := events()
	if len(got) != len(want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("report %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestOnMissingIsPerResolvedLocale(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"other": "x"})
	RegisterCatalog("fr", map[string]string{"other": "x"})
	events := recordMissing(t)

	T(WithLocale(context.Background(), "es"), "k", "K")
	T(WithLocale(context.Background(), "fr"), "k", "K")
	T(WithLocale(context.Background(), "es"), "k", "K")

	if got := events(); len(got) != 2 || got[0].locale != "es" || got[1].locale != "fr" {
		t.Errorf("reports = %v, want one per locale", got)
	}
}

func TestOnMissingIgnoresWhatIsNotAMissingTranslation(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"greeting": "Hola"})
	events := recordMissing(t)

	T(context.Background(), "greeting", "Hello")                   // no locale
	T(WithLocale(context.Background(), "de"), "greeting", "Hello") // no catalog for the locale
	T(WithLocale(context.Background(), "es-MX"), "greeting", "Hi") // the base language answers
	T(WithLocale(context.Background(), "es"), "", "Hello")         // no key

	if got := events(); len(got) != 0 {
		t.Errorf("reports = %v, want none", got)
	}
}

func TestOnMissingNilClearsTheHook(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"x": "y"})
	var calls atomic.Int32
	OnMissing(func(string, string) { calls.Add(1) })
	OnMissing(nil)
	T(WithLocale(context.Background(), "es"), "k", "K")
	if calls.Load() != 0 {
		t.Error("a cleared hook was called")
	}
	OnMissing(nil) // clearing twice is fine
}

func TestOnMissingCallbackRunsOutsideInternalLocks(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"x": "y"})
	done := make(chan struct{})
	OnMissing(func(locale, key string) {
		// Both would deadlock if the hook ran under the catalog lock.
		RegisterCatalog("es", map[string]string{"added": "ok"})
		T(WithLocale(context.Background(), locale), "nested", "Nested")
	})
	t.Cleanup(func() { OnMissing(nil) })
	go func() {
		T(WithLocale(context.Background(), "es"), "k", "K")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: the hook ran while a lock was held")
	}
}

func TestOnMissingDoesNotRecoverAPanickingHook(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"x": "y"})
	OnMissing(func(string, string) { panic("boom") })
	t.Cleanup(func() { OnMissing(nil) })
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recovered %v, want the hook's own panic", r)
		}
	}()
	T(WithLocale(context.Background(), "es"), "k", "K")
	t.Error("T returned normally")
}

func TestOnMissingIsOncePerKeyUnderConcurrency(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"x": "y"})
	var calls atomic.Int32
	OnMissing(func(string, string) { calls.Add(1) })
	t.Cleanup(func() { OnMissing(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := WithLocale(context.Background(), "es")
			T(ctx, "shared", "Shared")
			if i%8 == 0 {
				OnMissing(func(string, string) { calls.Add(1) }) // swapping the hook is safe too
			}
		}(i)
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("hook calls = %d, want 1", got)
	}
}

func TestOnMissingReportsEveryDistinctPairWithoutALimit(t *testing.T) {
	resetCatalogsForTest()
	t.Cleanup(resetCatalogsForTest)
	RegisterCatalog("es", map[string]string{"other": "otro"})
	var n int
	OnMissing(func(string, string) { n++ })
	const keys = 20000
	for i := 0; i < keys; i++ {
		T(WithLocale(context.Background(), "es"), "key."+strconv.Itoa(i), "x")
	}
	if n != keys {
		t.Fatalf("reported %d of %d distinct missing keys", n, keys)
	}
}
