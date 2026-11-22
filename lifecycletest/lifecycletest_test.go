package lifecycletest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/lifecycletest"
)

// fakeT records what Start asks of the test, and stops like t.Fatalf does.
type fakeT struct {
	cleanups []func()
	failure  string
}

type stopped struct{}

func (f *fakeT) Helper()                   {}
func (f *fakeT) Cleanup(fn func())         { f.cleanups = append(f.cleanups, fn) }
func (f *fakeT) Fatalf(s string, a ...any) { f.failure = fmt.Sprintf(s, a...); panic(stopped{}) }

// run calls Start and reports whether it stopped the test.
func (f *fakeT) run(registry *tango.Registry) (failed bool) {
	defer func() {
		if recover() != nil {
			failed = true
		}
	}()
	lifecycletest.Start(f, registry)
	return false
}

func (f *fakeT) finish() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func recorded(events *[]string, name string, startErr error) tango.Lifecycle {
	return tango.Lifecycle{
		Name:  name,
		Start: func(context.Context) error { *events = append(*events, "start "+name); return startErr },
		Stop:  func(context.Context) error { *events = append(*events, "stop "+name); return nil },
	}
}

func registryWith(t *testing.T, lifecycles ...tango.Lifecycle) *tango.Registry {
	t.Helper()
	registry := tango.NewRegistry()
	for _, lc := range lifecycles {
		if err := registry.RegisterLifecycle(lc); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func TestStartRunsEachStartInRegistrationOrderAndStopsThemInReverseOnCleanup(t *testing.T) {
	var events []string
	registry := registryWith(t, recorded(&events, "a", nil), recorded(&events, "b", nil), recorded(&events, "c", nil))
	ft := &fakeT{}

	if ft.run(registry) {
		t.Fatalf("Start failed the test: %s", ft.failure)
	}
	if got := strings.Join(events, ","); got != "start a,start b,start c" {
		t.Fatalf("events after Start = %q, want the starts in registration order", got)
	}
	ft.finish()

	if got := strings.Join(events, ","); got != "start a,start b,start c,stop c,stop b,stop a" {
		t.Fatalf("events after cleanup = %q, want the stops in reverse", got)
	}
}

func TestStartStopsAtTheFirstErrorAndStopsOnlyWhatStarted(t *testing.T) {
	var events []string
	boom := errors.New("seed failed")
	registry := registryWith(t, recorded(&events, "a", nil), recorded(&events, "b", boom), recorded(&events, "c", nil))
	ft := &fakeT{}

	failed := ft.run(registry)
	ft.finish()

	if !failed || !strings.Contains(ft.failure, "seed failed") || !strings.Contains(ft.failure, `"b"`) {
		t.Fatalf("failed = %v, failure = %q, want the test failed naming lifecycle b and its error", failed, ft.failure)
	}
	if got := strings.Join(events, ","); got != "start a,start b,stop a" {
		t.Fatalf("events = %q, want c never started and only a (which started) stopped", got)
	}
}

func TestStartSkipsALifecycleWithNoStartAndStillRunsItsStop(t *testing.T) {
	var events []string
	registry := registryWith(t,
		tango.Lifecycle{Name: "only-stop", Stop: func(context.Context) error { events = append(events, "stop only-stop"); return nil }},
		recorded(&events, "b", nil),
	)
	ft := &fakeT{}

	ft.run(registry)
	ft.finish()

	if got := strings.Join(events, ","); got != "start b,stop b,stop only-stop" {
		t.Fatalf("events = %q", got)
	}
}

func TestStartGivesStartAContextThatIsCancelledOnCleanup(t *testing.T) {
	var seen context.Context
	registry := registryWith(t, tango.Lifecycle{Name: "a", Start: func(ctx context.Context) error { seen = ctx; return nil }})
	ft := &fakeT{}

	ft.run(registry)
	if seen.Err() != nil {
		t.Fatal("the context was cancelled before cleanup")
	}
	ft.finish()

	if seen.Err() == nil {
		t.Fatal("the context was not cancelled on cleanup")
	}
}

func TestStartNeverRunsTheInternalSchedulerLifecycle(t *testing.T) {
	var events []string
	registry := registryWith(t, recorded(&events, "a", nil))
	ft := &fakeT{}

	ft.run(registry)
	ft.finish()

	for _, e := range events {
		if strings.Contains(e, "scheduler") {
			t.Fatalf("events = %v, want no scheduler lifecycle", events)
		}
	}
}
