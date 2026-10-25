package gcs_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gcsstorage "cloud.google.com/go/storage"

	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/gcs"
)

type ctxKey struct{}

// gateBuild returns a build wrapper that waits for gate before the real build.
func gateBuild(gate <-chan struct{}) func(func(context.Context) (*gcsstorage.Client, error)) func(context.Context) (*gcsstorage.Client, error) {
	return func(real func(context.Context) (*gcsstorage.Client, error)) func(context.Context) (*gcsstorage.Client, error) {
		return func(ctx context.Context) (*gcsstorage.Client, error) {
			<-gate
			return real(ctx)
		}
	}
}

// fakeStore is a Store on the fake, with the adapter's own client built
// lazily.
func fakeStore(t *testing.T, endpoint string) *gcs.Store {
	t.Helper()
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: endpoint, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// runWithin runs fn and fails the test if it has not returned within d. The
// gate is released on cleanup so a blocked build cannot outlive the test.
func runWithin(t *testing.T, d time.Duration, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("the operation did not return within %v", d)
		return nil
	}
}

// The build of the adapter's client receives the operation's values, but not
// its cancellation: the token source keeps the build context for refreshes.
func TestTheBuildKeepsTheOperationsValuesButNotItsCancellation(t *testing.T) {
	server := newFake(t)
	s := fakeStore(t, server.URL())
	var built context.Context
	s.SetDial(func(real func(context.Context) (*gcsstorage.Client, error)) func(context.Context) (*gcsstorage.Client, error) {
		return func(ctx context.Context) (*gcsstorage.Client, error) {
			built = ctx
			return real(ctx)
		}
	})
	opCtx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "marker"))
	if _, err := s.Stat(opCtx, testKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat = %v, want ErrNotFound", err)
	}
	if built == nil || built.Value(ctxKey{}) != "marker" {
		t.Fatal("the build did not receive the operation's values")
	}
	cancel()
	if err := built.Err(); err != nil {
		t.Fatalf("the build context ended with the operation: %v", err)
	}
}

// A cancelled operation returns while its build is still blocked.
func TestACancelledOperationReturnsWhileTheBuildIsBlocked(t *testing.T) {
	server := newFake(t)
	s := fakeStore(t, server.URL())
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	s.SetDial(gateBuild(gate))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	err := runWithin(t, 3*time.Second, func() error {
		_, err := s.Stat(ctx, testKey)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stat = %v, want context.Canceled", err)
	}
}

// A cancelled first operation neither poisons the Store nor blocks a later one.
func TestALaterOperationSucceedsAfterACancelledFirstOne(t *testing.T) {
	server := newFake(t)
	s := fakeStore(t, server.URL())
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	// Registered after fakeStore's Close, so it runs first: a Close must not
	// wait on a build the test still holds.
	t.Cleanup(release)
	s.SetDial(gateBuild(gate))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_ = runWithin(t, 3*time.Second, func() error {
		_, err := s.Stat(ctx, testKey)
		return err
	})
	release()

	err := runWithin(t, 5*time.Second, func() error {
		_, err := s.Put(context.Background(), strings.NewReader("later"), storage.PutOptions{MaxSize: 1 << 20})
		return err
	})
	if err != nil {
		t.Fatalf("Put after a cancelled first operation = %v, want success", err)
	}
}

// Close does not wait for an in-flight build: the Store's lock is not held
// during credential or client work.
func TestCloseIsNotBlockedByABuildInFlight(t *testing.T) {
	server := newFake(t)
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, Endpoint: server.URL(), TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	s.SetDial(gateBuild(gate))

	go func() { _, _ = s.Stat(context.Background(), testKey) }()
	time.Sleep(50 * time.Millisecond)
	if err := runWithin(t, 3*time.Second, s.Close); err != nil {
		t.Fatalf("Close = %v", err)
	}
}

// Concurrent first uses publish one client; every caller succeeds.
func TestConcurrentFirstUsesPublishOneClient(t *testing.T) {
	server := newFake(t)
	s := fakeStore(t, server.URL())
	const callers = 16
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Stat(context.Background(), testKey)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("caller %d: Stat = %v, want ErrNotFound", i, err)
		}
	}
	if n := s.Publishes(); n != 1 {
		t.Fatalf("%d clients published, want exactly 1", n)
	}
}

// The credentials are copied at New: changing the caller's slice afterwards
// does not change the lazy initialization.
func TestMutatingTheCredentialsAfterNewDoesNotChangeTheBuild(t *testing.T) {
	isolateAmbientCredentials(t)
	tokens := newRecorder(t, 200, `{"access_token":"token","expires_in":3600,"token_type":"Bearer"}`)
	server := newFake(t)
	restore := gcs.SetGoogleEndpoint(server.URL() + "/storage/v1/")
	t.Cleanup(restore)

	creds := []byte(fmt.Sprintf(`{"type":"authorized_user","client_id":"id","client_secret":"sec","refresh_token":"rt","token_uri":%q}`, tokens.srv.URL))
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, CredentialsJSON: creds, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for i := range creds {
		creds[i] = 'x'
	}
	if _, err := s.Put(context.Background(), strings.NewReader("payload"), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put after mutating the caller's credentials = %v, want success", err)
	}
	if len(tokens.seen()) == 0 {
		t.Fatal("no token was requested from the original credentials")
	}
}

// A token refresh after the first operation has ended must succeed: the token
// source keeps the build context, so that context cannot be the operation's.
func TestATokenRefreshSurvivesTheEndOfTheFirstOperation(t *testing.T) {
	isolateAmbientCredentials(t)
	tokens := newRecorder(t, 200, `{"access_token":"token","expires_in":1,"token_type":"Bearer"}`)
	server := newFake(t)
	restore := gcs.SetGoogleEndpoint(server.URL() + "/storage/v1/")
	t.Cleanup(restore)

	creds := []byte(fmt.Sprintf(`{"type":"authorized_user","client_id":"id","client_secret":"sec","refresh_token":"rt","token_uri":%q}`, tokens.srv.URL))
	s, err := gcs.New(context.Background(), gcs.Config{Bucket: testBucket, CredentialsJSON: creds, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := s.Put(ctx, strings.NewReader("first"), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("first Put = %v", err)
	}
	cancel()
	if _, err := s.Put(context.Background(), strings.NewReader("second"), storage.PutOptions{MaxSize: 1 << 20}); err != nil {
		t.Fatalf("Put after the first operation's context ended = %v", err)
	}
	if n := len(tokens.seen()); n < 2 {
		t.Fatalf("token requests = %d, want a refresh before the second Put", n)
	}
}
