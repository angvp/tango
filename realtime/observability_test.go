package realtime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/angvp/tango/observability"
)

type realtimeMetric struct {
	name  string
	attrs map[string]any
}

type realtimeRecorder struct {
	mu    sync.Mutex
	calls []realtimeMetric
	panic bool
}

func (r *realtimeRecorder) AddCounter(name string, _ int64, attrs ...slog.Attr) {
	values := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		values[attr.Key] = attr.Value.Any()
	}
	r.mu.Lock()
	r.calls = append(r.calls, realtimeMetric{name: name, attrs: values})
	r.mu.Unlock()
	if r.panic {
		panic("recorder panic")
	}
}
func (*realtimeRecorder) ObserveHistogram(string, float64, ...slog.Attr) {}

type observableLogic struct {
	handleErr error
	joinErr   error
}

func (l observableLogic) Handle(_ *RoomContext, event Event) error {
	if event.Kind == EventJoin {
		return l.joinErr
	}
	if event.Kind == EventAction {
		return l.handleErr
	}
	return nil
}
func (observableLogic) Snapshot(*RoomContext, Principal) ([]byte, error) { return nil, nil }

func TestHubRecordsOperationsExactlyOnceWithBoundedLabels(t *testing.T) {
	recorder := &realtimeRecorder{}
	hub, err := NewHub(func(roomID string) Logic {
		if roomID == "failing" {
			return observableLogic{handleErr: errors.New("domain failure")}
		}
		if roomID == "rejected" {
			return observableLogic{joinErr: errors.New("join rejected")}
		}
		return observableLogic{}
	}, Options{Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hub.Close(context.Background()) }()

	if err := hub.Join(context.Background(), "ok", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Join(context.Background(), "bad", Principal{UserID: "secret"}, nil); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("invalid peer error = %v", err)
	}
	if err := hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "missing", Principal: Principal{UserID: "private"}}); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("missing room error = %v", err)
	}
	if err := hub.Join(context.Background(), "failing", Principal{UserID: "bob"}, &fakePeer{}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "failing", Principal: Principal{UserID: "bob"}}); err == nil {
		t.Fatal("expected domain error")
	}
	if err := hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "ok", Principal: Principal{UserID: "alice"}}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Join(context.Background(), "rejected", Principal{UserID: "carol"}, &fakePeer{}); err == nil {
		t.Fatal("expected room-loop rejection")
	}

	if len(recorder.calls) != 7 {
		t.Fatalf("calls = %+v", recorder.calls)
	}
	want := [][2]string{{"join", "success"}, {"join", "error"}, {"dispatch", "error"}, {"join", "success"}, {"dispatch", "error"}, {"dispatch", "success"}, {"join", "error"}}
	for i, call := range recorder.calls {
		if call.name != observability.MetricRealtimeRoomEvents || call.attrs["event"] != want[i][0] || call.attrs["outcome"] != want[i][1] || len(call.attrs) != 2 {
			t.Fatalf("call[%d] = %+v", i, call)
		}
	}
}

func TestHubRecordsClosedRejections(t *testing.T) {
	recorder := &realtimeRecorder{}
	hub, err := NewHub(func(string) Logic { return observableLogic{} }, Options{Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hub.Join(context.Background(), "room", Principal{UserID: "alice"}, &fakePeer{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Join error = %v", err)
	}
	if err := hub.Dispatch(context.Background(), Event{Kind: EventAction, RoomID: "room", Principal: Principal{UserID: "alice"}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Dispatch error = %v", err)
	}
	if len(recorder.calls) != 2 || recorder.calls[0].attrs["outcome"] != "error" || recorder.calls[1].attrs["outcome"] != "error" {
		t.Fatalf("calls = %+v", recorder.calls)
	}
}

func TestHubRecordsDispatchPeerTrafficExactlyOnce(t *testing.T) {
	recorder := &realtimeRecorder{}
	hub, err := NewHub(func(roomID string) Logic {
		if roomID == "failing" {
			return observableLogic{handleErr: errors.New("domain failure")}
		}
		return observableLogic{}
	}, Options{Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}

	okPeer := &fakePeer{}
	if err := hub.Join(context.Background(), "ok", Principal{UserID: "alice"}, okPeer); err != nil {
		t.Fatal(err)
	}
	failingPeer := &fakePeer{}
	if err := hub.Join(context.Background(), "failing", Principal{UserID: "bob"}, failingPeer); err != nil {
		t.Fatal(err)
	}

	recorder.mu.Lock()
	recorder.calls = nil
	recorder.mu.Unlock()

	if err := hub.DispatchPeer(context.Background(), Event{Kind: EventAction, RoomID: "ok", Principal: Principal{UserID: "alice"}}, okPeer); err != nil {
		t.Fatalf("successful DispatchPeer: %v", err)
	}
	if err := hub.DispatchPeer(context.Background(), Event{Kind: EventAction, RoomID: "ok", Principal: Principal{UserID: "alice"}}, &fakePeer{}); !errors.Is(err, ErrStalePeer) {
		t.Fatalf("stale DispatchPeer error = %v", err)
	}
	if err := hub.DispatchPeer(context.Background(), Event{Kind: EventAction, RoomID: "failing", Principal: Principal{UserID: "bob"}}, failingPeer); err == nil {
		t.Fatal("expected Logic.Handle error")
	}
	if err := hub.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hub.DispatchPeer(context.Background(), Event{Kind: EventAction, RoomID: "ok", Principal: Principal{UserID: "alice"}}, okPeer); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Hub DispatchPeer error = %v", err)
	}

	if len(recorder.calls) != 4 {
		t.Fatalf("calls = %+v", recorder.calls)
	}
	wantOutcomes := []string{"success", "error", "error", "error"}
	for i, call := range recorder.calls {
		if call.name != observability.MetricRealtimeRoomEvents || call.attrs["event"] != "dispatch" || call.attrs["outcome"] != wantOutcomes[i] || len(call.attrs) != 2 {
			t.Fatalf("call[%d] = %+v", i, call)
		}
	}
}

func TestHubContainsRecorderPanic(t *testing.T) {
	recorder := &realtimeRecorder{panic: true}
	hub, err := NewHub(func(string) Logic { return observableLogic{} }, Options{Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hub.Close(context.Background()) }()
	if err := hub.Join(context.Background(), "room", Principal{UserID: "alice"}, &fakePeer{}); err != nil {
		t.Fatalf("Join changed by recorder panic: %v", err)
	}
}
