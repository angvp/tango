package cache_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
)

type profile struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// failing wraps a Store and fails the operations it is told to.
type failing struct {
	cache.Store
	getErr, setErr error
}

func (f *failing) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	return f.Store.Get(ctx, key)
}

func (f *failing) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	return f.Store.Set(ctx, key, value, ttl)
}

func TestSetJSONThenGetJSONRoundTripsATypedValue(t *testing.T) {
	ctx, store := context.Background(), newLocal(t)
	want := profile{Name: "Ada", Score: 7}
	if err := cache.SetJSON(ctx, store, "p:1", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cache.GetJSON[profile](ctx, store, "p:1")
	if err != nil || !ok || got != want {
		t.Fatalf("GetJSON = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	raw, _, _ := store.Get(ctx, "p:1")
	if string(raw) != `{"name":"Ada","score":7}` {
		t.Fatalf("stored bytes = %s, want plain encoding/json output", raw)
	}
}

func TestGetJSONMissIsNotAnError(t *testing.T) {
	got, ok, err := cache.GetJSON[profile](context.Background(), newLocal(t), "absent")
	if err != nil || ok || got != (profile{}) {
		t.Fatalf("GetJSON of an absent key = %+v, %v, %v; want a zero miss", got, ok, err)
	}
}

func TestGetJSONOfBytesThatDoNotDecodeIsErrCorruptNeverAZeroHit(t *testing.T) {
	ctx, store := context.Background(), newLocal(t)
	for name, raw := range map[string][]byte{
		"not JSON":       []byte("not json"),
		"the wrong type": []byte(`{"name": 5}`),
		"empty":          {},
		"truncated":      []byte(`{"name":"Ada"`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.Set(ctx, "k", raw, time.Minute); err != nil {
				t.Fatal(err)
			}
			got, ok, err := cache.GetJSON[profile](ctx, store, "k")
			if ok || got != (profile{}) {
				t.Fatalf("GetJSON = %+v, ok=%v; a corrupt value must not be a hit", got, ok)
			}
			var corrupt *cache.CorruptError
			if !errors.Is(err, cache.ErrCorrupt) || !errors.As(err, &corrupt) {
				t.Fatalf("err = %v, want ErrCorrupt via both errors.Is and errors.As", err)
			}
			if corrupt.Key != "k" || corrupt.Err == nil || !errors.Is(err, corrupt.Err) {
				t.Fatalf("CorruptError = %+v; want the key and the underlying decode error", corrupt)
			}
		})
	}
}

func TestGetJSONAndSetJSONPassBackendAndRuleErrorsThroughAsSentinels(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("backend down")
	f := &failing{Store: newLocal(t), getErr: boom, setErr: boom}
	if _, _, err := cache.GetJSON[profile](ctx, f, "k"); !errors.Is(err, boom) || errors.Is(err, cache.ErrCorrupt) {
		t.Errorf("GetJSON backend failure: %v, want the backend's error and not ErrCorrupt", err)
	}
	if err := cache.SetJSON(ctx, f, "k", profile{}, time.Minute); !errors.Is(err, boom) {
		t.Errorf("SetJSON backend failure: %v", err)
	}
	store := newLocal(t)
	if _, _, err := cache.GetJSON[profile](ctx, store, "bad key"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("GetJSON invalid key: %v, want ErrInvalidKey", err)
	}
	if err := cache.SetJSON(ctx, store, "bad key", profile{}, time.Minute); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("SetJSON invalid key: %v, want ErrInvalidKey", err)
	}
	if err := cache.SetJSON(ctx, store, "k", profile{}, 0); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Errorf("SetJSON ttl 0: %v, want ErrInvalidTTL", err)
	}
}

func TestSetJSONOfAValueThatCannotBeEncodedStoresNothing(t *testing.T) {
	ctx, store := context.Background(), newLocal(t)
	if err := cache.SetJSON(ctx, store, "k", make(chan int), time.Minute); err == nil {
		t.Fatal("SetJSON of a channel succeeded")
	}
	if _, ok, _ := store.Get(ctx, "k"); ok {
		t.Fatal("a failed encode stored something")
	}
}

// recorder collects what an OnError hook is told.
type recorder struct {
	ops  []string
	errs []error
}

func (r *recorder) hook() cache.FetchOption {
	return cache.OnError(func(op string, err error) {
		r.ops = append(r.ops, op)
		r.errs = append(r.errs, err)
	})
}

func loader(calls *int, v profile, err error) func(context.Context) (profile, error) {
	return func(context.Context) (profile, error) {
		*calls++
		return v, err
	}
}

func TestFetchJSONHitNeverCallsLoadAndMissLoadsAndStores(t *testing.T) {
	ctx, store := context.Background(), newLocal(t)
	calls := 0
	want := profile{Name: "Ada", Score: 1}
	for i := 0; i < 3; i++ {
		got, err := cache.FetchJSON(ctx, store, "k", time.Minute, loader(&calls, want, nil))
		if err != nil || got != want {
			t.Fatalf("FetchJSON #%d = %+v, %v", i+1, got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("load ran %d times, want once (the later calls are hits)", calls)
	}
	if v, ok, _ := cache.GetJSON[profile](ctx, store, "k"); !ok || v != want {
		t.Fatalf("the loaded value was not stored: %+v, %v", v, ok)
	}
}

func TestFetchJSONReadFailureReportsLoadsAndReturnsTheValue(t *testing.T) {
	ctx, boom := context.Background(), errors.New("redis down")
	f := &failing{Store: newLocal(t), getErr: boom}
	var rec recorder
	calls := 0
	got, err := cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{Name: "Ada"}, nil), rec.hook())
	if err != nil || got.Name != "Ada" || calls != 1 {
		t.Fatalf("FetchJSON = %+v, %v (load calls %d); want the loaded value and nil", got, err, calls)
	}
	if len(rec.ops) != 1 || rec.ops[0] != cache.OpGet || !errors.Is(rec.errs[0], boom) {
		t.Fatalf("hook saw %v %v, want one %q with the backend error", rec.ops, rec.errs, cache.OpGet)
	}
}

func TestFetchJSONDecodeFailureReportsAsCorruptThenLoadsAndRepairs(t *testing.T) {
	ctx, store := context.Background(), newLocal(t)
	_ = store.Set(ctx, "k", []byte("garbage"), time.Minute)
	var rec recorder
	calls := 0
	got, err := cache.FetchJSON(ctx, store, "k", time.Minute, loader(&calls, profile{Name: "Ada"}, nil), rec.hook())
	if err != nil || got.Name != "Ada" || calls != 1 {
		t.Fatalf("FetchJSON = %+v, %v (load calls %d)", got, err, calls)
	}
	if len(rec.ops) != 1 || rec.ops[0] != cache.OpDecode || !errors.Is(rec.errs[0], cache.ErrCorrupt) {
		t.Fatalf("hook saw %v %v, want one %q ErrCorrupt", rec.ops, rec.errs, cache.OpDecode)
	}
	if v, ok, err := cache.GetJSON[profile](ctx, store, "k"); err != nil || !ok || v.Name != "Ada" {
		t.Fatalf("the corrupt entry was not overwritten: %+v, %v, %v", v, ok, err)
	}
}

func TestFetchJSONWriteFailureReportsAndStillReturnsTheLoadedValue(t *testing.T) {
	ctx, boom := context.Background(), errors.New("set failed")
	f := &failing{Store: newLocal(t), setErr: boom}
	var rec recorder
	calls := 0
	got, err := cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{Name: "Ada"}, nil), rec.hook())
	if err != nil || got.Name != "Ada" {
		t.Fatalf("FetchJSON = %+v, %v; want the loaded value and nil", got, err)
	}
	if len(rec.ops) != 1 || rec.ops[0] != cache.OpSet || !errors.Is(rec.errs[0], boom) {
		t.Fatalf("hook saw %v %v, want one %q", rec.ops, rec.errs, cache.OpSet)
	}
}

func TestFetchJSONOfAValueThatCannotBeEncodedReportsEncodeAndReturnsIt(t *testing.T) {
	var rec recorder
	load := func(context.Context) (chan int, error) { return make(chan int), nil }
	got, err := cache.FetchJSON(context.Background(), newLocal(t), "k", time.Minute, load, rec.hook())
	if err != nil || got == nil {
		t.Fatalf("FetchJSON = %v, %v; want the loaded value and nil", got, err)
	}
	if len(rec.ops) != 1 || rec.ops[0] != cache.OpEncode {
		t.Fatalf("hook saw %v, want one %q", rec.ops, cache.OpEncode)
	}
}

func TestFetchJSONLoaderFailureIsReturnedAloneAndNeverReachesTheHook(t *testing.T) {
	ctx, boom := context.Background(), errors.New("database down")
	var rec recorder
	calls := 0
	_, err := cache.FetchJSON(ctx, newLocal(t), "k", time.Minute, loader(&calls, profile{}, boom), rec.hook())
	if err != boom {
		t.Fatalf("err = %v, want exactly the loader's error", err)
	}
	if len(rec.ops) != 0 {
		t.Fatalf("the hook was told about a loader failure: %v", rec.ops)
	}
	// A cache failure plus a loader failure, with a hook: the loader's error
	// alone is returned and the cache failure goes to the hook.
	cacheDown := errors.New("cache down")
	f := &failing{Store: newLocal(t), getErr: cacheDown}
	rec = recorder{}
	_, err = cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{}, boom), rec.hook())
	if err != boom || len(rec.ops) != 1 || !errors.Is(rec.errs[0], cacheDown) {
		t.Fatalf("err = %v, hook %v %v; want the loader error alone and the cache error in the hook", err, rec.ops, rec.errs)
	}
}

func TestFetchJSONWithoutAHookFallsBackSilentlyButJoinsWhenBothFail(t *testing.T) {
	ctx := context.Background()
	cacheDown, loadDown := errors.New("cache down"), errors.New("database down")
	f := &failing{Store: newLocal(t), getErr: cacheDown, setErr: cacheDown}
	calls := 0
	got, err := cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{Name: "Ada"}, nil))
	if err != nil || got.Name != "Ada" {
		t.Fatalf("a successful fallback load = %+v, %v; want the value and nil", got, err)
	}
	_, err = cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{}, loadDown))
	if !errors.Is(err, cacheDown) || !errors.Is(err, loadDown) {
		t.Fatalf("err = %v, want the cache failure joined with the loader error", err)
	}
	// A loader failure with a healthy cache is the loader's error alone.
	_, err = cache.FetchJSON(ctx, newLocal(t), "k2", time.Minute, loader(&calls, profile{}, loadDown))
	if err != loadDown {
		t.Fatalf("err = %v, want exactly the loader's error", err)
	}
}

func TestFetchJSONHookIsSynchronousAndOncePerFailedOperation(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("down")
	f := &failing{Store: newLocal(t), getErr: boom, setErr: boom}
	var order []string
	hook := cache.OnError(func(op string, _ error) { order = append(order, "hook:"+op) })
	load := func(context.Context) (profile, error) {
		order = append(order, "load")
		return profile{Name: "Ada"}, nil
	}
	if _, err := cache.FetchJSON(ctx, f, "k", time.Minute, load, hook); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprint([]string{"hook:get", "load", "hook:set"})
	if fmt.Sprint(order) != want {
		t.Fatalf("order = %v, want %v (the hook runs synchronously, once per failed operation)", order, want)
	}
}

func TestFetchJSONHookCannotChangeCacheBehaviorAndAPanicPropagates(t *testing.T) {
	ctx := context.Background()
	f := &failing{Store: newLocal(t), getErr: errors.New("down")}
	calls := 0
	_, _ = cache.FetchJSON(ctx, f, "k", time.Minute, loader(&calls, profile{Name: "Ada"}, nil),
		cache.OnError(func(string, error) {}))
	if calls != 1 {
		t.Fatalf("load ran %d times, want 1 regardless of the hook", calls)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a panicking hook was swallowed; it is host code and may panic")
		}
	}()
	_, _ = cache.FetchJSON(ctx, f, "k2", time.Minute, loader(&calls, profile{}, nil),
		cache.OnError(func(string, error) { panic("host bug") }))
}

func TestFetchJSONPassesItsContextToLoadAndRefusesABadKeyOrTTLUpFront(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	var rec recorder
	var seen any
	_, err := cache.FetchJSON(ctx, newLocal(t), "k", time.Minute, func(c context.Context) (profile, error) {
		seen = c.Value(ctxKey{})
		return profile{}, nil
	}, rec.hook())
	if err != nil || seen != "marker" {
		t.Fatalf("load got context value %v (err %v), want the caller's context", seen, err)
	}
	calls := 0
	if _, err := cache.FetchJSON(ctx, newLocal(t), "bad key", time.Minute, loader(&calls, profile{}, nil), rec.hook()); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
	if _, err := cache.FetchJSON(ctx, newLocal(t), "k", 0, loader(&calls, profile{}, nil), rec.hook()); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Fatalf("err = %v, want ErrInvalidTTL", err)
	}
	if calls != 0 {
		t.Fatalf("loader ran %d times for a call that was refused up front", calls)
	}
}
