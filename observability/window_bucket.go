package observability

import (
	"math"
	"time"
)

// latencyBounds are the fixed upper bounds, in seconds, of the latency
// histogram. A value equal to a bound falls in that bound's bucket. Values
// above the last bound fall in one overflow bucket, which reports +Inf.
var latencyBounds = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

const (
	// overflowLatency is the index of the bucket for values above every bound.
	overflowLatency = len(latencyBounds)
	// statusClassCount is the number of tracked status classes, 2xx to 5xx.
	statusClassCount = 4
	// jobOutcomeCount is the number of tracked job outcomes.
	jobOutcomeCount = 4
)

// Indexes into minuteBucket.jobs, in the order JobSummary reports them.
const (
	jobSuccess = iota
	jobError
	jobPanic
	jobCanceled
)

// minuteBucket holds the aggregates for one minute. Its fields are only
// meaningful while live is true and minute matches the slot's minute.
type minuteBucket struct {
	live    bool
	minute  int64
	classes [statusClassCount]int64
	latency [overflowLatency + 1]int64
	jobs    [jobOutcomeCount]int64
}

// newMinuteBucket returns an empty bucket for the given unix minute.
func newMinuteBucket(minute int64) minuteBucket {
	return minuteBucket{live: true, minute: minute}
}

// windowTotals is the sum of every live bucket in a window.
type windowTotals struct {
	classes [statusClassCount]int64
	latency [overflowLatency + 1]int64
	jobs    [jobOutcomeCount]int64
}

// with returns the totals plus bucket, when bucket is live and falls inside
// the 60-minute window that ends at now. Otherwise it returns t unchanged.
func (t windowTotals) with(bucket minuteBucket, now time.Time) windowTotals {
	current := minuteIndex(now)
	if !bucket.live || bucket.minute <= current-windowMinutes || bucket.minute > current {
		return t
	}
	for i, n := range bucket.classes {
		t.classes[i] += n
	}
	for i, n := range bucket.latency {
		t.latency[i] += n
	}
	for i, n := range bucket.jobs {
		t.jobs[i] += n
	}
	return t
}

// requestSummary derives the request counts and latency percentiles.
// It returns the zero value when no requests were recorded.
func (t windowTotals) requestSummary() RequestSummary {
	total := sumCounts(t.classes[:])
	if total == 0 {
		return RequestSummary{}
	}
	return RequestSummary{
		Total:      total,
		Class2xx:   t.classes[0],
		Class3xx:   t.classes[1],
		Class4xx:   t.classes[2],
		Class5xx:   t.classes[3],
		LatencyP50: quantile(t.latency[:], total, 50),
		LatencyP95: quantile(t.latency[:], total, 95),
		LatencyP99: quantile(t.latency[:], total, 99),
	}
}

// jobSummary reports the job outcome counts.
func (t windowTotals) jobSummary() JobSummary {
	return JobSummary{
		Success:  t.jobs[jobSuccess],
		Error:    t.jobs[jobError],
		Panic:    t.jobs[jobPanic],
		Canceled: t.jobs[jobCanceled],
	}
}

// quantile returns the upper bound of the histogram bucket holding the
// percent-th percentile. It uses integer ranks so that no float rounding can
// shift a sample into the next bucket.
func quantile(counts []int64, total, percent int64) float64 {
	rank := (total*percent + 99) / 100
	var cumulative int64
	for i, n := range counts {
		cumulative += n
		if cumulative >= rank {
			return bucketBound(i)
		}
	}
	return math.Inf(1)
}

// bucketBound returns the upper bound, in seconds, of latency bucket i.
func bucketBound(i int) float64 {
	if i >= len(latencyBounds) {
		return math.Inf(1)
	}
	return latencyBounds[i]
}

// latencyIndex returns the latency bucket that holds seconds.
func latencyIndex(seconds float64) int {
	for i, bound := range latencyBounds {
		if seconds <= bound {
			return i
		}
	}
	return overflowLatency
}

// statusClassIndex maps an HTTP status code to 0 for 2xx through 3 for 5xx.
// Codes outside 200 to 599 report false.
func statusClassIndex(status int) (int, bool) {
	class := status / 100
	if class < 2 || class > 5 {
		return 0, false
	}
	return class - 2, true
}

// jobOutcomeIndex maps a scheduler outcome to its index in minuteBucket.jobs.
// Unknown outcomes report false.
func jobOutcomeIndex(outcome string) (int, bool) {
	switch outcome {
	case "success":
		return jobSuccess, true
	case "error":
		return jobError, true
	case "panic":
		return jobPanic, true
	case "canceled":
		return jobCanceled, true
	default:
		return 0, false
	}
}

// minuteIndex returns the number of whole minutes from the unix epoch to t.
func minuteIndex(t time.Time) int64 {
	return t.Unix() / 60
}

// slotIndex returns the ring slot that holds the given minute.
func slotIndex(minute int64) int {
	return int(((minute % windowMinutes) + windowMinutes) % windowMinutes)
}

// sumCounts adds up a slice of counters.
func sumCounts(counts []int64) int64 {
	var total int64
	for _, n := range counts {
		total += n
	}
	return total
}
