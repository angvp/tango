package storagetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/storage"
)

// faults switches deliberate bugs on in the store below. With none set it is
// a correct storage.Store, which is what proves a check passes for a good
// store and fails only because of the bug a case turns on.
type faults struct {
	putFails, emptyFails, badObject, sameKey                 bool
	statMismatch, zeroModTime                                bool
	openCorrupts, openIgnoresRange, openAcceptsBadOffset     bool
	lastWins                                                 bool
	wrongInvalidKeyError, missingGeneric, missingDeleteFails bool
	acceptBadOptions, capOffByOne, capWrongError             bool
	ignoreContext                                            bool
	deleteFails, deleteNoop                                  bool
	leakTooLarge, leakDisallowed, leakReaderError            bool
	overwriteOnCollision, overwriteButReport                 bool
	wrongCollisionError, ignoreKeySource                     bool
}

type faultyStore struct {
	f       faults
	next    func() string
	inner   storage.Store
	mu      sync.Mutex
	first   string
	lastKey string
}

func newFaultyStore(f faults, next func() string) *faultyStore {
	if f.ignoreKeySource || next == nil {
		return &faultyStore{f: f, inner: NewMemory()}
	}
	return &faultyStore{f: f, next: next, inner: NewMemoryWithKeys(next)}
}

func (s *faultyStore) refuseKey(key string) error {
	if s.f.wrongInvalidKeyError && !storage.ValidKey(key) {
		return storage.ErrNotFound // the wrong sentinel for a malformed key
	}
	return nil
}

func (s *faultyStore) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if s.f.ignoreContext {
		ctx = context.Background()
	}
	if s.f.putFails {
		return storage.Object{}, errors.New("put failed")
	}
	if s.f.acceptBadOptions && opts.MaxSize <= 0 {
		opts.MaxSize = 1 << 20
	}
	if s.f.capOffByOne && opts.MaxSize > 0 {
		opts.MaxSize++
	}
	if err := s.leak(ctx, &r, opts); err != nil {
		return storage.Object{}, err
	}
	obj, err := s.inner.Put(ctx, r, opts)
	if errors.Is(err, storage.ErrExists) && (s.f.overwriteOnCollision || s.f.overwriteButReport) {
		if s.next != nil {
			_ = s.inner.Delete(ctx, s.next()) // remove the object the collision named
		}
		replaced, replaceErr := s.replaceColliding(ctx, r, opts)
		if s.f.overwriteButReport {
			return storage.Object{}, err
		}
		return replaced, replaceErr
	}
	if err != nil {
		if s.f.wrongCollisionError && errors.Is(err, storage.ErrExists) {
			return storage.Object{}, errors.New("collision")
		}
		if s.f.capWrongError && errors.Is(err, storage.ErrTooLarge) {
			return storage.Object{}, errors.New("too big")
		}
		return obj, err
	}
	if s.f.badObject {
		obj.ContentType = "application/octet-stream"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f.sameKey {
		if s.first == "" {
			s.first = obj.Key
		}
		obj.Key = s.first
	}
	s.lastKey = obj.Key
	if s.f.emptyFails && obj.Size == 0 {
		return storage.Object{}, errors.New("empty objects are not supported")
	}
	return obj, nil
}

// replaceColliding stores r over the key a collision named, which a
// create-only store must never do.
func (s *faultyStore) replaceColliding(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	return s.inner.Put(ctx, r, opts)
}

// leak stores the bytes of a write it is about to refuse, leaving an object
// behind that a refused write must not leave.
func (s *faultyStore) leak(ctx context.Context, r *io.Reader, opts storage.PutOptions) error {
	if !s.f.leakTooLarge && !s.f.leakDisallowed && !s.f.leakReaderError {
		return nil
	}
	data, readErr := io.ReadAll(*r)
	*r = bytes.NewReader(data)
	big := storage.PutOptions{MaxSize: 1 << 24}
	switch {
	case s.f.leakReaderError && readErr != nil:
		_, _ = s.inner.Put(ctx, bytes.NewReader(data), big)
		return readErr
	case s.f.leakTooLarge && int64(len(data)) > opts.MaxSize:
		_, _ = s.inner.Put(ctx, bytes.NewReader(data), big)
		return &storage.TooLargeError{Max: opts.MaxSize}
	case s.f.leakDisallowed && len(opts.AllowedTypes) > 0 && !bytes.HasPrefix(data, []byte("\x89PNG")):
		_, _ = s.inner.Put(ctx, bytes.NewReader(data), big)
		return &storage.TypeNotAllowedError{Type: "text/html"}
	}
	if readErr != nil {
		*r = io.MultiReader(bytes.NewReader(data), errReader{readErr})
	}
	return nil
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func (s *faultyStore) Stat(ctx context.Context, key string) (storage.Info, error) {
	if s.f.ignoreContext {
		ctx = context.Background()
	}
	if err := s.refuseKey(key); err != nil {
		return storage.Info{}, err
	}
	info, err := s.inner.Stat(ctx, key)
	if err != nil {
		if s.f.missingGeneric && errors.Is(err, storage.ErrNotFound) {
			return storage.Info{}, errors.New("no such thing")
		}
		return info, err
	}
	if s.f.statMismatch {
		info.ETag = `"bad"`
	}
	if s.f.zeroModTime {
		info.ModTime = time.Time{}
	}
	return info, nil
}

func (s *faultyStore) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if s.f.ignoreContext {
		ctx = context.Background()
	}
	if err := s.refuseKey(key); err != nil {
		return nil, err
	}
	if s.f.lastWins {
		s.mu.Lock()
		key = s.lastKey
		s.mu.Unlock()
	}
	if s.f.openIgnoresRange {
		return s.inner.Open(ctx, key, 0, -1)
	}
	rc, err := s.inner.Open(ctx, key, offset, length)
	if errors.Is(err, storage.ErrInvalidRange) && s.f.openAcceptsBadOffset {
		return io.NopCloser(strings.NewReader("")), nil
	}
	if err != nil {
		if s.f.missingGeneric && errors.Is(err, storage.ErrNotFound) {
			return nil, errors.New("no such thing")
		}
		return nil, err
	}
	if s.f.openCorrupts {
		data, _ := io.ReadAll(rc)
		rc.Close()
		if len(data) > 0 {
			data[0] ^= 0xff
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return rc, nil
}

func (s *faultyStore) Delete(ctx context.Context, key string) error {
	if s.f.ignoreContext {
		ctx = context.Background()
	}
	if err := s.refuseKey(key); err != nil {
		return err
	}
	if s.f.deleteFails {
		return errors.New("delete failed")
	}
	if s.f.missingDeleteFails {
		if _, err := s.inner.Stat(ctx, key); errors.Is(err, storage.ErrNotFound) {
			return errors.New("delete of a missing object failed")
		}
	}
	if s.f.deleteNoop {
		return nil
	}
	return s.inner.Delete(ctx, key)
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

// Run runs a subtest the way a failing one behaves: its failures are the
// parent's, and its Fatal ends only its own goroutine.
func (r *recorder) Run(_ string, f func(reporter)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
}

// scenario runs one check against a store with the given faults and returns
// what it reported.
type scenario func(r reporter, f faults)

func plain(check func(reporter, storage.Store)) scenario {
	return func(r reporter, f faults) { check(r, newFaultyStore(f, nil)) }
}

func keyed(check func(reporter, func(reporter, func() string) storage.Store)) scenario {
	return func(r reporter, f faults) {
		check(r, func(_ reporter, next func() string) storage.Store { return newFaultyStore(f, next) })
	}
}

var allScenarios = map[string]scenario{
	"putStatOpen": plain(putStatOpen), "openRanges": plain(openRanges), "missing": plain(missing),
	"invalidKeys": plain(invalidKeys), "invalidOptions": plain(invalidOptions), "sizeCap": plain(sizeCap),
	"cancelled": plain(cancelled), "concurrent": plain(concurrent), "deleteTwice": plain(deleteTwice),
	"refusedStoreNothing": keyed(refusedStoreNothing), "createOnly": keyed(createOnly),
}

func failuresOf(s scenario, f faults) []string {
	r := &recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s(r, f)
	}()
	<-done
	return r.failures
}

func TestEveryCheckPassesForACorrectStore(t *testing.T) {
	for name, s := range allScenarios {
		if failures := failuresOf(s, faults{}); len(failures) != 0 {
			t.Errorf("%s reported %q for a correct store", name, failures)
		}
	}
}

func TestEachCheckFailsForTheBugItExists(t *testing.T) {
	tests := []struct {
		name  string
		bug   faults
		check string
		want  string // a fragment of what the check must report
	}{
		{"Put failing", faults{putFails: true}, "putStatOpen", "Put: put failed"},
		{"Put failing", faults{putFails: true}, "concurrent", "put failed"},
		{"Put failing", faults{putFails: true}, "deleteTwice", "Put: put failed"},
		{"a wrong object returned by Put", faults{badObject: true}, "putStatOpen", "Put returned"},
		{"a wrong ETag from Stat", faults{statMismatch: true}, "putStatOpen", "Stat ="},
		{"no modification time from Stat", faults{zeroModTime: true}, "putStatOpen", "Stat ="},
		{"corrupted bytes from Open", faults{openCorrupts: true}, "putStatOpen", "Open read"},
		{"an empty object refused", faults{emptyFails: true}, "putStatOpen", "Put: empty objects"},
		{"Open ignoring its range", faults{openIgnoresRange: true}, "openRanges", "Open("},
		{"Open accepting a bad offset", faults{openAcceptsBadOffset: true}, "openRanges", "want ErrInvalidRange"},
		{"a missing object reported generically", faults{missingGeneric: true}, "missing", "want ErrNotFound"},
		{"Delete of a missing object failing", faults{missingDeleteFails: true}, "missing", "Delete of a missing object"},
		{"the wrong error for a malformed key", faults{wrongInvalidKeyError: true}, "invalidKeys", "want ErrInvalidKey"},
		{"invalid options accepted", faults{acceptBadOptions: true}, "invalidOptions", "want ErrInvalidOptions"},
		{"a cap that lets one more byte in", faults{capOffByOne: true}, "sizeCap", "one over"},
		{"the wrong error for a too-large write", faults{capWrongError: true}, "sizeCap", "one over"},
		{"a cancelled context ignored", faults{ignoreContext: true}, "cancelled", "want context.Canceled"},
		{"the same key issued twice", faults{sameKey: true}, "concurrent", "issued twice"},
		{"objects read back as another's", faults{lastWins: true}, "concurrent", "first byte"},
		{"Delete failing", faults{deleteFails: true}, "deleteTwice", "Delete #1"},
		{"Delete doing nothing", faults{deleteNoop: true}, "deleteTwice", "Stat after Delete"},
		{"a too-large write leaving its bytes", faults{leakTooLarge: true}, "refusedStoreNothing", "a too-large write"},
		{"a disallowed type leaving its bytes", faults{leakDisallowed: true}, "refusedStoreNothing", "a disallowed type"},
		{"a failing reader leaving its bytes", faults{leakReaderError: true}, "refusedStoreNothing", "a failing reader"},
		{"a collision overwriting the first object", faults{overwriteOnCollision: true}, "createOnly", "colliding Put"},
		{"a collision overwriting and then reporting", faults{overwriteButReport: true}, "createOnly", "first object changed"},
		{"the wrong error for a collision", faults{wrongCollisionError: true}, "createOnly", "colliding Put"},
		{"a store ignoring the key it was given", faults{ignoreKeySource: true}, "createOnly", "Put used key"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" is caught by "+tt.check, func(t *testing.T) {
			failures := failuresOf(allScenarios[tt.check], tt.bug)
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
