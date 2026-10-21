package observability_test

import (
	"log/slog"
	"testing"

	"github.com/angvp/tango/observability"
)

type logRecorder struct {
	name string
	log  *[]string
}

func (r logRecorder) AddCounter(name string, delta int64, _ ...slog.Attr) {
	*r.log = append(*r.log, r.name+":counter:"+name)
}
func (r logRecorder) ObserveHistogram(name string, _ float64, _ ...slog.Attr) {
	*r.log = append(*r.log, r.name+":histogram:"+name)
}

type panicRecorder struct{}

func (panicRecorder) AddCounter(string, int64, ...slog.Attr)         { panic("exporter broke") }
func (panicRecorder) ObserveHistogram(string, float64, ...slog.Attr) { panic("exporter broke") }

func TestMultiCallsRecordersInOrderAndSkipsNil(t *testing.T) {
	var log []string
	multi := observability.Multi(logRecorder{"a", &log}, nil, logRecorder{"b", &log})
	multi.AddCounter("c", 1)
	multi.ObserveHistogram("h", 0.5)
	want := []string{"a:counter:c", "b:counter:c", "a:histogram:h", "b:histogram:h"}
	if len(log) != len(want) {
		t.Fatalf("calls = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("calls = %v, want %v", log, want)
		}
	}
}

func TestMultiOfNothingIsUsable(t *testing.T) {
	for _, multi := range []observability.Recorder{observability.Multi(), observability.Multi(nil, nil)} {
		multi.AddCounter("c", 1)
		multi.ObserveHistogram("h", 1)
	}
}

func TestMultiLetsARecorderPanicReachTheCaller(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a recorder panic was swallowed; it must reach the caller")
		}
	}()
	observability.Multi(panicRecorder{}).AddCounter("c", 1)
}

type fakeSource struct{}

func (fakeSource) Snapshot() (observability.Snapshot, error) { return observability.Snapshot{}, nil }

func TestSnapshotterIsASeparateReadSide(t *testing.T) {
	var _ observability.Snapshotter = fakeSource{}
	var _ observability.Recorder = observability.NopRecorder{}
	if _, ok := any(observability.NopRecorder{}).(observability.Snapshotter); ok {
		t.Fatal("NopRecorder must not become a Snapshotter")
	}
}
