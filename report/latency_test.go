package report

import (
	"testing"
)

func TestNewLatency(t *testing.T) {
	t.Run("no times", func(t *testing.T) {
		if l := NewLatency(nil); l != nil {
			t.Errorf("NewLatency(nil) = %+v, want nil", l)
		}
	})

	t.Run("one time is every percentile", func(t *testing.T) {
		got := *NewLatency([]float64{0.012})
		want := Latency{Count: 1, MinMs: 12, MeanMs: 12, P50Ms: 12, P90Ms: 12, P95Ms: 12, P99Ms: 12, MaxMs: 12}
		if got != want {
			t.Errorf("NewLatency = %+v, want %+v", got, want)
		}
	})

	t.Run("nearest rank of a hundred times", func(t *testing.T) {
		// 100ms down to 1ms, out of order, so that the result does not
		// depend on the order the runs ended in.
		secs := make([]float64, 100)
		for i := range secs {
			secs[i] = float64(100-i) / 1000
		}
		got := *NewLatency(secs)
		want := Latency{Count: 100, MinMs: 1, MeanMs: 50.5, P50Ms: 50, P90Ms: 90, P95Ms: 95, P99Ms: 99, MaxMs: 100}
		if got != want {
			t.Errorf("NewLatency = %+v, want %+v", got, want)
		}
	})

	t.Run("percentiles of a few times are times taken", func(t *testing.T) {
		got := *NewLatency([]float64{0.03, 0.01, 0.02, 0.5})
		// Ranks of 4 runs: p50 is the 2nd, p90, p95 and p99 are the 4th.
		want := Latency{Count: 4, MinMs: 10, MeanMs: 140, P50Ms: 20, P90Ms: 500, P95Ms: 500, P99Ms: 500, MaxMs: 500}
		if got != want {
			t.Errorf("NewLatency = %+v, want %+v", got, want)
		}
	})

	t.Run("does not sort the times it is given", func(t *testing.T) {
		secs := []float64{0.03, 0.01, 0.02}
		NewLatency(secs)
		if secs[0] != 0.03 || secs[1] != 0.01 || secs[2] != 0.02 {
			t.Errorf("times = %v, want them left in order", secs)
		}
	})

	t.Run("rounds to the microsecond", func(t *testing.T) {
		got := NewLatency([]float64{0.0123456789})
		if got.MinMs != 12.346 {
			t.Errorf("MinMs = %v, want 12.346", got.MinMs)
		}
	})
}

func TestFormatMs(t *testing.T) {
	tests := []struct {
		ms   float64
		want string
	}{
		{0.25, "0.2ms"},
		{12.34, "12.3ms"},
		{99.94, "99.9ms"},
		{100, "100ms"},
		{999.4, "999ms"},
		{1000, "1.00s"},
		{12345, "12.35s"},
	}
	for _, tt := range tests {
		if got := FormatMs(tt.ms); got != tt.want {
			t.Errorf("FormatMs(%v) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestLatency_Summary(t *testing.T) {
	l := Latency{Count: 4, MinMs: 10, MeanMs: 140, P50Ms: 20, P90Ms: 500, P95Ms: 500, P99Ms: 500, MaxMs: 1500}
	want := "min 10.0ms, p50 20.0ms, p90 500ms, p95 500ms, p99 500ms, max 1.50s"
	if got := l.Summary(); got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}
