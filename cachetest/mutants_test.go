package cachetest

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
)

// faults switches deliberate bugs on in the store below. With none set it
// is a correct cache.Store, which is what proves a check passes for a good
// store and fails only because of the bug a case turns on.
type faults struct {
	missIsHit, missIsError                            bool
	getFails, setFails, deleteFails                   bool
	failOverwrite, overwriteIgnored                   bool
	deleteNoop, deleteMissingErrors                   bool
	emptyAsMiss                                       bool
	retainInput, shareOutput                          bool
	acceptBadKeys, rejectMaxKey                       bool
	acceptBadTTL, rejectMaxTTL                        bool
	expireEarly, neverExpire                          bool
	ignoreContext, storeOnCancel                      bool
	bigGeneric, bigTruncates, tornReads, flakyOnValue bool
}

type faultyEntry struct {
	value   []byte
	expires time.Time // zero: never
}

type faulty struct {
	f  faults
	mu sync.Mutex
	m  map[string]faultyEntry
}

func newFaulty(f faults) *faulty { return &faulty{f: f, m: map[string]faultyEntry{}} }

func (s *faulty) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if !s.f.ignoreContext {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
	}
	if s.f.getFails {
		return nil, false, errors.New("get failed")
	}
	if !s.f.acceptBadKeys && !cache.ValidKey(key) {
		return nil, false, cache.ErrInvalidKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if ok && !e.expires.IsZero() && time.Now().After(e.expires) {
		ok = false
	}
	if !ok {
		switch {
		case s.f.missIsHit:
			return []byte("ghost"), true, nil
		case s.f.missIsError:
			return nil, false, errors.New("a miss is not an error, but this store says it is")
		}
		return nil, false, nil
	}
	if s.f.emptyAsMiss && len(e.value) == 0 {
		return nil, false, nil
	}
	if s.f.tornReads && len(e.value) == 4096 {
		return append(append([]byte(nil), e.value[:2048]...), make([]byte, 2048)...), true, nil
	}
	if s.f.shareOutput {
		return e.value, true, nil
	}
	return append([]byte(nil), e.value...), true, nil
}

func (s *faulty) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctxErr := error(nil)
	if !s.f.ignoreContext {
		ctxErr = ctx.Err()
		if ctxErr != nil && !s.f.storeOnCancel {
			return ctxErr
		}
	}
	if s.f.setFails {
		return errors.New("set failed")
	}
	if !s.f.acceptBadKeys && !cache.ValidKey(key) {
		return cache.ErrInvalidKey
	}
	if s.f.rejectMaxKey && len(key) == cache.MaxKeyLength {
		return cache.ErrInvalidKey
	}
	if !s.f.acceptBadTTL {
		if err := cache.CheckTTL(ttl); err != nil {
			return err
		}
	}
	if s.f.rejectMaxTTL && ttl == cache.MaxTTL {
		return cache.ErrInvalidTTL
	}
	if s.f.flakyOnValue && len(value) > 0 && value[0] == 'b' {
		return errors.New("flaky set")
	}
	if len(value) > 1<<20 {
		if s.f.bigGeneric {
			return errors.New("the backend choked")
		}
		if s.f.bigTruncates {
			value = value[:len(value)/2]
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.m[key]; exists {
		if s.f.failOverwrite {
			return errors.New("set failed on overwrite")
		}
		if s.f.overwriteIgnored {
			return nil
		}
	}
	stored := value
	if !s.f.retainInput {
		stored = append([]byte(nil), value...)
	}
	entry := faultyEntry{value: stored, expires: time.Now().Add(ttl)}
	switch {
	case s.f.neverExpire || s.f.acceptBadTTL && ttl <= 0:
		entry.expires = time.Time{}
	case s.f.expireEarly:
		entry.expires = time.Now().Add(-time.Second)
	}
	s.m[key] = entry
	return ctxErr
}

func (s *faulty) Delete(ctx context.Context, key string) error {
	if !s.f.ignoreContext {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if s.f.deleteFails {
		return errors.New("delete failed")
	}
	if !s.f.acceptBadKeys && !cache.ValidKey(key) {
		return cache.ErrInvalidKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; !ok && s.f.deleteMissingErrors {
		return errors.New("delete of a missing key failed")
	}
	if !s.f.deleteNoop {
		delete(s.m, key)
	}
	return nil
}

// recorder is a reporter that remembers what a check reported. Fatal stops
// the check, as t.Fatal does, by ending its goroutine.
type recorder struct {
	mu       sync.Mutex
	failures []string
}

func (r *recorder) add(msg string) {
	r.mu.Lock()
	r.failures = append(r.failures, msg)
	r.mu.Unlock()
}
func (r *recorder) Helper()                      {}
func (r *recorder) Error(args ...any)            { r.add(fmt.Sprint(args...)) }
func (r *recorder) Errorf(f string, args ...any) { r.add(fmt.Sprintf(f, args...)) }
func (r *recorder) Fatal(args ...any)            { r.add(fmt.Sprint(args...)); runtime.Goexit() }
func (r *recorder) Fatalf(f string, args ...any) { r.add(fmt.Sprintf(f, args...)); runtime.Goexit() }

type check func(reporter, cache.Store)

// failuresOf runs one check against s and returns what it reported.
func failuresOf(c check, s cache.Store) []string {
	r := &recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c(r, s)
	}()
	<-done
	return r.failures
}

var allChecks = map[string]check{
	"setGetDelete": setGetDelete, "missIsNotAnError": missIsNotAnError, "emptyValue": emptyValue,
	"ownership": ownership, "invalidKeys": invalidKeys, "ttlRange": ttlRange,
	"ttlNotElapsedEarly": ttlNotElapsedEarly, "shortTTLExpires": shortTTLExpires,
	"cancelled": cancelled, "oversized": oversized, "concurrent": concurrent,
}

func shortWait(t *testing.T) {
	t.Helper()
	previous := expiryWait
	expiryWait = 300 * time.Millisecond
	t.Cleanup(func() { expiryWait = previous })
}

func TestEveryCheckPassesForACorrectStore(t *testing.T) {
	shortWait(t)
	for name, c := range allChecks {
		if failures := failuresOf(c, newFaulty(faults{})); len(failures) != 0 {
			t.Errorf("%s reported %q for a correct store", name, failures)
		}
	}
}

func TestEachCheckFailsForTheBugItExists(t *testing.T) {
	shortWait(t)
	tests := []struct {
		name  string
		bug   faults
		check string
		want  string // a fragment of what the check must report
	}{
		{"a miss reported as a hit", faults{missIsHit: true}, "setGetDelete", "after Delete"},
		{"a miss reported as a hit", faults{missIsHit: true}, "missIsNotAnError", "want a miss"},
		{"a miss reported as an error", faults{missIsError: true}, "missIsNotAnError", "want a miss"},
		{"Set failing", faults{setFails: true}, "setGetDelete", "set failed"},
		{"Set failing", faults{setFails: true}, "emptyValue", "set failed"},
		{"Set failing", faults{setFails: true}, "ownership", "set failed"},
		{"Set failing", faults{setFails: true}, "ttlNotElapsedEarly", "set failed"},
		{"Set failing", faults{setFails: true}, "shortTTLExpires", "set failed"},
		{"Get failing", faults{getFails: true}, "setGetDelete", "Get ="},
		{"Get failing", faults{getFails: true}, "shortTTLExpires", "get failed"},
		{"Get failing", faults{getFails: true}, "ttlNotElapsedEarly", "already gone"},
		{"Get failing while concurrent", faults{getFails: true}, "concurrent", "get failed"},
		{"an overwrite failing", faults{failOverwrite: true}, "setGetDelete", "set failed on overwrite"},
		{"an overwrite ignored", faults{overwriteIgnored: true}, "setGetDelete", "overwrite: Get"},
		{"Delete failing", faults{deleteFails: true}, "setGetDelete", "delete failed"},
		{"Delete doing nothing", faults{deleteNoop: true}, "setGetDelete", "after Delete"},
		{"Delete of a missing key failing", faults{deleteMissingErrors: true}, "setGetDelete", "Delete of a missing key"},
		{"an empty value lost", faults{emptyAsMiss: true}, "emptyValue", "an empty value"},
		{"Set keeping the caller's slice", faults{retainInput: true}, "ownership", "slice given to Set"},
		{"Get handing out its own slice", faults{shareOutput: true}, "ownership", "slice returned by Get"},
		{"invalid keys accepted", faults{acceptBadKeys: true}, "invalidKeys", "want ErrInvalidKey"},
		{"the longest valid key refused", faults{rejectMaxKey: true}, "invalidKeys", "200-byte key"},
		{"invalid TTLs accepted", faults{acceptBadTTL: true}, "ttlRange", "want ErrInvalidTTL"},
		{"an invalid TTL storing a value", faults{acceptBadTTL: true}, "ttlRange", "stored a value"},
		{"the longest valid TTL refused", faults{rejectMaxTTL: true}, "ttlRange", "exactly 30 days"},
		{"a TTL counted as elapsed already", faults{expireEarly: true}, "ttlNotElapsedEarly", "already gone"},
		{"a TTL that never ends", faults{neverExpire: true}, "shortTTLExpires", "still readable"},
		{"a cancelled context ignored", faults{ignoreContext: true}, "cancelled", "want context.Canceled"},
		{"a cancelled Set storing anyway", faults{storeOnCancel: true}, "cancelled", "stored a value"},
		{"a generic error for an oversized value", faults{bigGeneric: true}, "oversized", "success or ErrTooLarge"},
		{"an oversized value truncated", faults{bigTruncates: true}, "oversized", "came back wrong"},
		{"torn reads under concurrency", faults{tornReads: true}, "concurrent", "torn value"},
		{"a Set failing under concurrency", faults{flakyOnValue: true}, "concurrent", "flaky set"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" is caught by "+tt.check, func(t *testing.T) {
			failures := failuresOf(allChecks[tt.check], newFaulty(tt.bug))
			if len(failures) == 0 {
				t.Fatalf("%s passed for a store with this bug", tt.check)
			}
			for _, f := range failures {
				if strings.Contains(f, tt.want) {
					return
				}
			}
			t.Fatalf("%s reported %q, none containing %q", tt.check, failures, tt.want)
		})
	}
}
