package cache

import (
	"context"
	"errors"
	"time"
)

// The operations a FetchJSON cache failure is reported under.
const (
	// OpGet is a Store.Get failure.
	OpGet = "get"
	// OpDecode is a stored value that does not decode (ErrCorrupt).
	OpDecode = "decode"
	// OpEncode is a loaded value that cannot be encoded as JSON.
	OpEncode = "encode"
	// OpSet is a Store.Set failure.
	OpSet = "set"
)

// FetchOption configures FetchJSON.
type FetchOption func(*fetchConfig)

type fetchConfig struct {
	onError func(op string, err error)
}

// OnError reports each cache failure FetchJSON works around. fn is called
// synchronously, once per failed cache operation, with the operation (OpGet,
// OpDecode, OpEncode or OpSet) and the error. It cannot change what
// FetchJSON does, and a loader failure is never reported to it. A panic in
// fn is the host's and propagates.
func OnError(fn func(op string, err error)) FetchOption {
	return func(c *fetchConfig) { c.onError = fn }
}

// FetchJSON returns the value cached under key, or calls load, caches what it
// returns for ttl and returns that. It is deliberately fail-open: the cache is
// an optimization, so
//
//   - if reading or decoding fails, FetchJSON reports it, calls load, and
//     returns the loaded value if loading succeeds;
//   - if writing the loaded value fails, FetchJSON reports it and still
//     returns the value.
//
// A key or ttl that is not valid (ErrInvalidKey, ErrInvalidTTL) is a
// programming error, not a cache failure: FetchJSON returns it before calling
// load, and never works around it. That includes a key the store itself
// refuses, such as one a Prefix made too long.
//
// A load failure is returned as is (it is not a cache error). Cache failures
// go to the OnError hook when one is supplied. Without a hook they are not
// returned when loading succeeds, since observability is the hook's job, but
// when both the cache and load fail the errors are returned joined.
//
// FetchJSON does not coalesce concurrent calls for the same key: when a hot
// key expires, every caller loads. Wrap load with singleflight to avoid it.
func FetchJSON[T any](ctx context.Context, store Store, key string, ttl time.Duration, load func(context.Context) (T, error), opts ...FetchOption) (T, error) {
	if !ValidKey(key) {
		var zero T
		return zero, ErrInvalidKey
	}
	if err := CheckTTL(ttl); err != nil {
		var zero T
		return zero, err
	}
	var cfg fetchConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	var unreported []error
	report := func(op string, err error) {
		if cfg.onError != nil {
			cfg.onError(op, err)
			return
		}
		unreported = append(unreported, err)
	}

	value, ok, err := GetJSON[T](ctx, store, key)
	if errors.Is(err, ErrInvalidKey) {
		return value, err // a key the store refuses (say, behind a long Prefix) is a bug, not an outage
	}
	if err != nil {
		report(readOp(err), err)
	} else if ok {
		return value, nil
	}

	loaded, err := load(ctx)
	if err != nil {
		var zero T
		if len(unreported) == 0 {
			return zero, err // the loader's error, as is
		}
		return zero, errors.Join(append(unreported, err)...)
	}
	if err := SetJSON(ctx, store, key, loaded, ttl); err != nil {
		report(writeOp(err), err)
	}
	return loaded, nil
}

func readOp(err error) string {
	if errors.Is(err, ErrCorrupt) {
		return OpDecode
	}
	return OpGet
}

func writeOp(err error) string {
	var encode *encodeError
	if errors.As(err, &encode) {
		return OpEncode
	}
	return OpSet
}
