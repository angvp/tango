package observability

import "time"

// Snapshotter is the read side of an activity source: a bounded, recent view
// of the numbers a Recorder was given. It is separate from Recorder on
// purpose, so a Recorder never has to store anything. A source that can fail
// (a shared store, say) reports it through the error.
type Snapshotter interface {
	Snapshot() (Snapshot, error)
}

// Snapshot is what a Snapshotter reports. Every number in it covers the
// window described by Scope, Since and Window and nothing wider.
type Snapshot struct {
	// Scope says what the numbers cover, for example "this instance only".
	// The source supplies it; a consumer displays it and never infers it.
	Scope string
	// Since is when the window of activity began. It is not the start time
	// of a process, which a status provider reports separately.
	Since time.Time
	// Window is how much recent activity the numbers span.
	Window time.Duration
	// Requests summarises HTTP requests across the whole window.
	Requests RequestSummary
	// Jobs counts scheduler job invocations by outcome.
	Jobs JobSummary
}

// RequestSummary counts HTTP requests by status class and reports latency
// percentiles in seconds. Total is the sample count behind the percentiles.
type RequestSummary struct {
	Total                              int64
	Class2xx, Class3xx                 int64
	Class4xx, Class5xx                 int64
	LatencyP50, LatencyP95, LatencyP99 float64
}

// JobSummary counts scheduler job invocations by outcome.
type JobSummary struct {
	Success, Error, Panic, Canceled int64
}
