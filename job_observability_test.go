package tango

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/angvp/tango/observability"
)

func TestJobObservabilityOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func() (context.Context, func(context.Context) error)
		outcome   string
		wantLog   bool
		wantPanic bool
	}{
		{name: "success", prepare: func() (context.Context, func(context.Context) error) {
			return context.Background(), func(context.Context) error { return nil }
		}, outcome: "success"},
		{name: "error", prepare: func() (context.Context, func(context.Context) error) {
			return context.Background(), func(context.Context) error { return errors.New("boom") }
		}, outcome: "error", wantLog: true},
		{name: "panic", prepare: func() (context.Context, func(context.Context) error) {
			return context.Background(), func(context.Context) error { panic("boom") }
		}, outcome: "panic", wantLog: true, wantPanic: true},
		{name: "canceled", prepare: func() (context.Context, func(context.Context) error) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func(ctx context.Context) error { return ctx.Err() }
		}, outcome: "canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, run := tt.prepare()
			logger, logs := newCapturingLogger()
			recorder := &capturingRecorder{}
			done := make(chan struct{})
			runInvocation(ctx, Job{Name: "cleanup", Run: run}, done, newJobObserver(logger, recorder))
			<-done

			if len(recorder.counters) != 1 {
				t.Fatalf("metrics = %+v", recorder.counters)
			}
			metric := recorder.counters[0]
			if metric.name != observability.MetricSchedulerJobInvocations || metric.attrs["job"] != "cleanup" || metric.attrs["outcome"] != tt.outcome {
				t.Fatalf("metric = %+v", metric)
			}
			if (len(*logs) != 0) != tt.wantLog {
				t.Fatalf("logs = %+v, wantLog=%v", *logs, tt.wantLog)
			}
			if tt.wantLog {
				if (*logs)[0].message != EventJobFailed || (*logs)[0].attrs["panicked"] != tt.wantPanic {
					t.Fatalf("log = %+v", (*logs)[0])
				}
			}
		})
	}
}

func TestJobObservabilityPanicsDoNotChangeOutcome(t *testing.T) {
	records := []capturedLog{}
	logger := slog.New(&capturingHandler{mu: new(sync.Mutex), records: &records, panic: true})
	recorder := &capturingRecorder{panic: true}
	done := make(chan struct{})
	runInvocation(context.Background(), Job{Name: "broken", Run: func(context.Context) error { return errors.New("original") }}, done, newJobObserver(logger, recorder))
	<-done
	if len(records) != 1 || len(recorder.counters) != 1 {
		t.Fatalf("logs=%+v metrics=%+v", records, recorder.counters)
	}
}
