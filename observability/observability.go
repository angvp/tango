// Package observability defines tanGO's minimal backend-neutral metrics contract.
package observability

import "log/slog"

const (
	MetricHTTPRequestDuration     = "tango_http_request_duration_seconds"
	MetricSchedulerJobInvocations = "tango_scheduler_job_invocations_total"
	MetricRealtimeRoomEvents      = "tango_realtime_room_events_total"
)

// Recorder receives framework metrics. Implementations must be safe for
// concurrent use.
type Recorder interface {
	AddCounter(name string, delta int64, attrs ...slog.Attr)
	ObserveHistogram(name string, value float64, attrs ...slog.Attr)
}

// NopRecorder discards every metric. Its zero value is ready for direct use.
type NopRecorder struct{}

func (NopRecorder) AddCounter(string, int64, ...slog.Attr)         {}
func (NopRecorder) ObserveHistogram(string, float64, ...slog.Attr) {}

var _ Recorder = NopRecorder{}
