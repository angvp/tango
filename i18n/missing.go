package i18n

import (
	"sync"
	"sync/atomic"
)

// missingOnceLimit bounds the memory of the once-per-(locale, key) record.
// Keys are literals, so a program never gets near it; past the limit new
// pairs are no longer reported rather than growing without end.
const missingOnceLimit = 16384

type missingPair struct{ locale, key string }

var (
	missingHook atomic.Pointer[func(locale, key string)]
	missingSeen sync.Map // missingPair -> struct{}
	missingMu   sync.Mutex
	missingN    int
)

// OnMissing registers f to be told about a missing translation: a lookup
// whose resolved locale has a catalog (or a base-language one) that supplied
// no usable text for the key, either because the key is absent, its value is
// empty, or its format verbs do not match the fallback's. f receives the
// resolved locale and the key, at most once per pair for the life of the
// process. It is meant for development: run the application in a locale and
// see what is untranslated.
//
// OnMissing(nil) clears the hook, and the hook is nil by default. Setting,
// clearing and dispatching are safe for concurrent use. f runs on the
// goroutine that called T, outside any lock of this package, so it may call T
// or RegisterCatalog; a panic in f is not recovered.
func OnMissing(f func(locale, key string)) {
	if f == nil {
		missingHook.Store(nil)
		return
	}
	missingHook.Store(&f)
}

// reportMissing calls the hook the first time a (locale, key) is missing.
func reportMissing(locale, key string) {
	hook := missingHook.Load()
	if hook == nil {
		return
	}
	pair := missingPair{locale, key}
	if _, seen := missingSeen.Load(pair); seen {
		return
	}
	missingMu.Lock()
	if missingN >= missingOnceLimit {
		missingMu.Unlock()
		return
	}
	if _, loaded := missingSeen.LoadOrStore(pair, struct{}{}); loaded {
		missingMu.Unlock()
		return
	}
	missingN++
	missingMu.Unlock()
	(*hook)(locale, key)
}

func resetMissingForTest() {
	missingHook.Store(nil)
	missingMu.Lock()
	defer missingMu.Unlock()
	missingSeen.Clear()
	missingN = 0
}
