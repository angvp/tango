package realtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// benchPayloadSize is the fixed broadcast payload size for every benchmark
// case, so raw and framed runs carry identical bytes.
const benchPayloadSize = 128

// benchPeerQueue sizes each member's outbound queue well above the producer's
// per-iteration output, so a writer goroutine lagging the room loop does not
// overflow a queue. The run-time guard in runBroadcastBench fails the benchmark
// if any member is closed anyway.
const benchPeerQueue = 4096

var benchMemberCounts = []int{1, 16, 128}

// benchSink is a Peer that counts Send and Close calls without retaining
// payloads. Send never blocks and never allocates.
type benchSink struct {
	sends  atomic.Int64
	closes atomic.Int64
}

func (s *benchSink) Send(_ context.Context, _ []byte) error {
	s.sends.Add(1)
	return nil
}

func (s *benchSink) Close() error {
	s.closes.Add(1)
	return nil
}

// benchLogic broadcasts Event.Payload to every member on each action event.
// Its join snapshot is one byte, which keeps a framed join from failing on an
// empty frame.
type benchLogic struct{}

func (benchLogic) Handle(rc *RoomContext, ev Event) error {
	if ev.Kind != EventAction {
		return nil
	}
	return rc.Broadcast(ev.Payload)
}

func (benchLogic) Snapshot(_ *RoomContext, _ Principal) ([]byte, error) {
	return []byte{'s'}, nil
}

// benchFramedLogic is benchLogic plus the cheapest legal Framer: it returns
// its input unchanged, so it adds no allocation of its own beyond the
// per-member copy the room already makes.
type benchFramedLogic struct {
	benchLogic
}

func (benchFramedLogic) Frame(_ Principal, _ uint64, payload []byte) ([]byte, error) {
	return payload, nil
}

// benchDrainEvery bounds how far the producer may run ahead of the writer
// goroutines: every benchDrainEvery dispatches the timer stops while the sinks
// catch up. It must stay well below benchPeerQueue.
const benchDrainEvery = 1024

// awaitDelivery waits, outside the timed region, until every sink has received
// perMember sends (the join snapshot plus one per completed iteration). A
// sink that was closed fails the benchmark, as does a delivery count that never
// reaches the expected value.
func awaitDelivery(b *testing.B, sinks []*benchSink, perMember int64) {
	b.Helper()
	want := int64(len(sinks)) * perMember
	deadline := time.Now().Add(60 * time.Second)
	for {
		var got int64
		for i, s := range sinks {
			if n := s.closes.Load(); n != 0 {
				b.Fatalf("member %d was closed %d time(s) during the run", i, n)
			}
			got += s.sends.Load()
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			b.Fatalf("sinks received %d sends, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// runBroadcastBench joins members sinks to a fresh hub, then times b.N
// Dispatch calls, each of which broadcasts the fixed payload to every member.
// Dispatch returns after Logic.Handle, so each broadcast has been admitted to
// every member queue when it returns; the writer goroutines drain those queues
// asynchronously. The sinks are checked for exact delivery counts and for
// closes at every drain point and at the end, so a run in which any member
// overflowed fails instead of reporting numbers for a smaller broadcast.
func runBroadcastBench(b *testing.B, logic Logic, members int) {
	b.Helper()
	b.ReportAllocs()

	hub, err := NewHub(func(string) Logic { return logic }, Options{PeerQueue: benchPeerQueue})
	if err != nil {
		b.Fatalf("NewHub: %v", err)
	}
	ctx := context.Background()
	sinks := make([]*benchSink, members)
	for i := range sinks {
		sinks[i] = &benchSink{}
		if err := hub.Join(ctx, "bench", Principal{UserID: fmt.Sprintf("m%d", i)}, sinks[i]); err != nil {
			b.Fatalf("Join member %d: %v", i, err)
		}
	}

	ev := Event{Kind: EventAction, RoomID: "bench", Principal: Principal{UserID: "sender"}, Payload: make([]byte, benchPayloadSize)}

	b.ResetTimer()
	for i := range b.N {
		if err := hub.Dispatch(ctx, ev); err != nil {
			b.Fatalf("Dispatch: %v", err)
		}
		if (i+1)%benchDrainEvery == 0 {
			b.StopTimer()
			awaitDelivery(b, sinks, int64(i+2))
			b.StartTimer()
		}
	}
	b.StopTimer()
	awaitDelivery(b, sinks, int64(b.N+1))
	if err := hub.Close(ctx); err != nil {
		b.Fatalf("Close: %v", err)
	}
}

func BenchmarkBroadcastRaw(b *testing.B) {
	for _, members := range benchMemberCounts {
		b.Run(fmt.Sprintf("members=%d", members), func(b *testing.B) {
			runBroadcastBench(b, benchLogic{}, members)
		})
	}
}

func BenchmarkBroadcastNoopFramer(b *testing.B) {
	for _, members := range benchMemberCounts {
		b.Run(fmt.Sprintf("members=%d", members), func(b *testing.B) {
			runBroadcastBench(b, benchFramedLogic{}, members)
		})
	}
}
