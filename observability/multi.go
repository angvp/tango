package observability

import "log/slog"

// Multi returns a Recorder that forwards every metric to each given recorder,
// in order, skipping nil ones. It does not recover panics: a broken exporter
// stays visible to the caller. It is safe for concurrent use when its
// recorders are.
func Multi(recorders ...Recorder) Recorder {
	kept := make([]Recorder, 0, len(recorders))
	for _, r := range recorders {
		if r != nil {
			kept = append(kept, r)
		}
	}
	return multiRecorder(kept)
}

type multiRecorder []Recorder

func (m multiRecorder) AddCounter(name string, delta int64, attrs ...slog.Attr) {
	for _, r := range m {
		r.AddCounter(name, delta, attrs...)
	}
}

func (m multiRecorder) ObserveHistogram(name string, value float64, attrs ...slog.Attr) {
	for _, r := range m {
		r.ObserveHistogram(name, value, attrs...)
	}
}
