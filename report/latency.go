package report

import (
	"fmt"
	"math"
	"slices"
)

// Latency is the distribution of the times the action of a repeated step
// took, over the runs in which it returned one. A run in which the action
// failed to run returns none, and one in which the step was skipped does not
// run it; neither is counted. A run whose test failed is, so a limit on the
// time does not take the slow runs out of what it is measured against.
//
// The percentiles are by nearest rank: the time that the given share of the
// runs took at most, which is always one of the times taken.
type Latency struct {
	Count int     `json:"count"`
	MinMs float64 `json:"min_ms"`
	// MeanMs is the arithmetic mean.
	MeanMs float64 `json:"mean_ms"`
	P50Ms  float64 `json:"p50_ms"`
	P90Ms  float64 `json:"p90_ms"`
	P95Ms  float64 `json:"p95_ms"`
	P99Ms  float64 `json:"p99_ms"`
	MaxMs  float64 `json:"max_ms"`
}

// NewLatency returns the distribution of secs, the times of the runs in
// seconds, or nil when there are none.
func NewLatency(secs []float64) *Latency {
	if len(secs) == 0 {
		return nil
	}
	ms := make([]float64, len(secs))
	var sum float64
	for i, s := range secs {
		ms[i] = s * 1000
		sum += ms[i]
	}
	slices.Sort(ms)
	return &Latency{
		Count:  len(ms),
		MinMs:  roundMs(ms[0]),
		MeanMs: roundMs(sum / float64(len(ms))),
		P50Ms:  roundMs(nearestRank(ms, 50)),
		P90Ms:  roundMs(nearestRank(ms, 90)),
		P95Ms:  roundMs(nearestRank(ms, 95)),
		P99Ms:  roundMs(nearestRank(ms, 99)),
		MaxMs:  roundMs(ms[len(ms)-1]),
	}
}

// nearestRank returns the p-th percentile of sorted: the smallest value that
// at least p percent of the values are less than or equal to.
func nearestRank(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}

// roundMs rounds a time in milliseconds to the microsecond, finer than the
// times are worth and short enough to read in JSON.
func roundMs(ms float64) float64 {
	return math.Round(ms*1000) / 1000
}

// FormatMs writes a time in milliseconds as a person reads it: in
// milliseconds below a second, with one decimal below 100ms, and in seconds
// from a second up.
func FormatMs(ms float64) string {
	switch {
	case ms >= 1000:
		return fmt.Sprintf("%.2fs", ms/1000)
	case ms >= 100:
		return fmt.Sprintf("%.0fms", ms)
	default:
		return fmt.Sprintf("%.1fms", ms)
	}
}

// Summary writes the distribution on one line, as the terminal and the
// Markdown report show it.
func (l *Latency) Summary() string {
	return fmt.Sprintf("min %s, p50 %s, p90 %s, p95 %s, p99 %s, max %s",
		FormatMs(l.MinMs), FormatMs(l.P50Ms), FormatMs(l.P90Ms),
		FormatMs(l.P95Ms), FormatMs(l.P99Ms), FormatMs(l.MaxMs))
}
