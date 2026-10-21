package observability

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// windowScope is the scope every Window reports.
	windowScope = "this instance only"
	// windowMinutes is the number of one-minute buckets a Window keeps.
	windowMinutes = 60
	// windowLength is how much recent activity a Window's snapshot spans.
	windowLength = windowMinutes * time.Minute
)

// Window is an in-process, in-memory activity source. It is both a Recorder
// and a Snapshotter: the framework records into it, and a dashboard reads a
// recent view of it. It keeps only aggregates for the last 60 minutes: HTTP
// requests by status class with latency, and scheduler job invocations by
// outcome. It retains no route, method, raw status code, job name, user id
// or room id. Counters reset when the process restarts.
//
// A Window is safe for concurrent use. Create one with NewWindow.
type Window struct {
	now     func() time.Time
	started time.Time

	mu      sync.Mutex
	buckets [windowMinutes]minuteBucket
}

var (
	_ Recorder    = (*Window)(nil)
	_ Snapshotter = (*Window)(nil)
)

// WindowOption configures a Window created by NewWindow.
type WindowOption func(*windowConfig)

type windowConfig struct {
	now func() time.Time
}

// WithClock sets the clock a Window reads when it records or snapshots.
// A nil clock is ignored. The default is time.Now.
func WithClock(now func() time.Time) WindowOption {
	return func(cfg *windowConfig) {
		if now != nil {
			cfg.now = now
		}
	}
}

// NewWindow returns an empty Window that starts now, according to its clock.
func NewWindow(opts ...WindowOption) *Window {
	cfg := windowConfig{now: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Window{now: cfg.now, started: cfg.now()}
}

// AddCounter records one scheduler job invocation when name is
// MetricSchedulerJobInvocations and the outcome attribute is one of success,
// error, panic or canceled. Every other name, outcome and attribute is ignored.
// A non-positive delta is ignored.
func (w *Window) AddCounter(name string, delta int64, attrs ...slog.Attr) {
	if name != MetricSchedulerJobInvocations || delta <= 0 {
		return
	}
	outcome, ok := stringAttr(attrs, "outcome")
	if !ok {
		return
	}
	index, ok := jobOutcomeIndex(outcome)
	if !ok {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bucketLocked(w.now()).jobs[index] += delta
}

// ObserveHistogram records one HTTP request when name is
// MetricHTTPRequestDuration and the status attribute is an integer in the
// 2xx to 5xx range. value is the duration in seconds. Only the status class and
// the latency bucket are kept. Every other name is ignored.
func (w *Window) ObserveHistogram(name string, value float64, attrs ...slog.Attr) {
	if name != MetricHTTPRequestDuration {
		return
	}
	status, ok := intAttr(attrs, "status")
	if !ok {
		return
	}
	class, ok := statusClassIndex(status)
	if !ok {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	bucket := w.bucketLocked(w.now())
	bucket.classes[class]++
	bucket.latency[latencyIndex(value)]++
}

// Snapshot reports the activity in the last 60 minutes. It never returns an
// error. An empty window reports zero counts and zero percentiles.
func (w *Window) Snapshot() (Snapshot, error) {
	now := w.now()
	w.mu.Lock()
	var totals windowTotals
	for i := range w.buckets {
		totals = totals.with(w.buckets[i], now)
	}
	w.mu.Unlock()

	return Snapshot{
		Scope:    windowScope,
		Since:    w.started,
		Window:   windowLength,
		Requests: totals.requestSummary(),
		Jobs:     totals.jobSummary(),
	}, nil
}

// bucketLocked returns the bucket for the minute of now, clearing it first if it
// still holds an earlier minute. The caller must hold w.mu.
func (w *Window) bucketLocked(now time.Time) *minuteBucket {
	minute := minuteIndex(now)
	slot := &w.buckets[slotIndex(minute)]
	if !slot.live || slot.minute != minute {
		*slot = newMinuteBucket(minute)
	}
	return slot
}

// stringAttr returns the first string attribute named key.
func stringAttr(attrs []slog.Attr, key string) (string, bool) {
	for _, attr := range attrs {
		if attr.Key == key && attr.Value.Kind() == slog.KindString {
			return attr.Value.String(), true
		}
	}
	return "", false
}

// intAttr returns the first integer attribute named key, as an int.
func intAttr(attrs []slog.Attr, key string) (int, bool) {
	for _, attr := range attrs {
		if attr.Key == key && attr.Value.Kind() == slog.KindInt64 {
			return int(attr.Value.Int64()), true
		}
	}
	return 0, false
}
