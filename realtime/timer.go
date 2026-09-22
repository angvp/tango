package realtime

import (
	"fmt"
	"time"
)

// roomTimer is one named timer's current generation and pending schedule.
// generation is incremented on every ResetTimer call for that name; an
// expiry's carried generation is compared against the current one before
// it is allowed to reach Logic.Handle — see handleTimerExpiry.
type roomTimer struct {
	generation uint64
	stop       func() bool
}

// ResetTimer (re)schedules a named timer for the room this RoomContext
// belongs to. Only valid while handling an Event or computing a Snapshot —
// see RoomContext's doc comment.
func (r *RoomContext) ResetTimer(name string, duration time.Duration) error {
	return r.room.resetTimer(name, duration)
}

func (r *room) resetTimer(name string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("tango realtime: timer duration must be positive")
	}

	t, ok := r.timers[name]
	if !ok {
		t = &roomTimer{}
		r.timers[name] = t
	} else if t.stop != nil {
		t.stop()
	}
	t.generation++
	gen := t.generation

	t.stop = r.hub.newTimer(duration, func() {
		r.submitInternal(roomOp{
			kind:     opTimerExpiry,
			timerGen: gen,
			event:    Event{Kind: EventTimer, RoomID: r.id, Timer: name},
		})
	})
	return nil
}

// handleTimerExpiry delivers a timer expiry to Logic.Handle, but only if
// it's still the current generation for that timer name — a stale expiry
// (superseded by a later ResetTimer call while this one was still
// enqueued, or scheduled for a room state that has since moved on) is
// silently discarded, never delivered.
func (r *room) handleTimerExpiry(op roomOp) {
	t, ok := r.timers[op.event.Timer]
	if !ok || op.timerGen != t.generation {
		return
	}
	rc := &RoomContext{room: r}
	_ = r.logic.Handle(rc, op.event)
}
