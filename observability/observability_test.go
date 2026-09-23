package observability

import (
	"log/slog"
	"testing"
)

func TestNopRecorderZeroValue(t *testing.T) {
	var recorder Recorder = NopRecorder{}
	recorder.AddCounter(MetricSchedulerJobInvocations, 1, slog.String("outcome", "success"))
	recorder.ObserveHistogram(MetricHTTPRequestDuration, 0.1, slog.Int("status", 200))
}
