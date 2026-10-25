package duration

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	maxDuration := time.Duration(MaxSeconds) * time.Second
	tests := []struct {
		in      any
		want    time.Duration
		wantErr error
	}{
		// Duration strings.
		{"30s", 30 * time.Second, nil},
		{"1m30s", 90 * time.Second, nil},
		{"500ms", 500 * time.Millisecond, nil},
		{" 2s ", 2 * time.Second, nil},
		{"0s", 0, nil},
		{"-0s", 0, nil},
		{"1ns", time.Nanosecond, nil},
		{"-1ns", 0, errNegative},
		{"-1s", 0, errNegative},
		{"2562047h", 2562047 * time.Hour, nil},
		{"2562048h", 0, errNotDuration}, // too long for a duration
		{"soon", 0, errNotDuration},
		{"", 0, errNotDuration},
		{"1s soon", 0, errNotDuration},

		// Numeric strings, read as seconds.
		{"15", 15 * time.Second, nil},
		{" 2 ", 2 * time.Second, nil},
		{"1.5", 1500 * time.Millisecond, nil},
		{"0", 0, nil},
		{"0.0", 0, nil},
		{"-0", 0, nil},
		{"-1", 0, errNegative},
		{"-0.5", 0, errNegative},
		{"1e3", 1000 * time.Second, nil},
		{"1e20", 0, errTooLong},
		{"9223372036", maxDuration, nil},
		{"9223372037", 0, errTooLong},
		{"0.000000001", time.Nanosecond, nil},
		{"0.0000000001", 0, errTooShort},
		{"0.0000000009", 0, errTooShort},
		{"4.1", 4100 * time.Millisecond, nil},
		{"8.2", 8200 * time.Millisecond, nil},
		{"2.01", 2010 * time.Millisecond, nil},
		{"1.001", 1001 * time.Millisecond, nil},
		{"0.3", 300 * time.Millisecond, nil},
		{"1.000000001", time.Second + time.Nanosecond, nil},
		{"NaN", 0, errNotDuration},
		{"Inf", 0, errTooLong},
		{"-Inf", 0, errNegative},

		// Numbers, read as seconds.
		{0, 0, nil},
		{10, 10 * time.Second, nil},
		{-1, 0, errNegative},
		{int8(-1), 0, errNegative},
		{int64(3), 3 * time.Second, nil},
		{int64(MaxSeconds), maxDuration, nil},
		{int64(MaxSeconds) + 1, 0, errTooLong},
		{int64(math.MinInt64), 0, errNegative},
		{uint64(5), 5 * time.Second, nil},
		{uint64(MaxSeconds) + 1, 0, errTooLong},
		{uint64(math.MaxUint64), 0, errTooLong},
		{20_000_000_000, 0, errTooLong}, // would wrap to about 49 years
		{float64(3), 3 * time.Second, nil},
		{1.5, 1500 * time.Millisecond, nil},
		{0.25, 250 * time.Millisecond, nil},
		{float32(0.5), 500 * time.Millisecond, nil},
		{0.0, 0, nil},
		{math.Copysign(0, -1), 0, nil},
		{-0.5, 0, errNegative},
		{1e-9, time.Nanosecond, nil},
		{1e-10, 0, errTooShort},
		{9e-10, 0, errTooShort},
		{4.1, 4100 * time.Millisecond, nil},
		{8.2, 8200 * time.Millisecond, nil},
		{2.01, 2010 * time.Millisecond, nil},
		{1.001, 1001 * time.Millisecond, nil},
		{float32(0.1), 100 * time.Millisecond, nil},
		{float32(4.1), 4100 * time.Millisecond, nil},
		{1e20, 0, errTooLong},
		{math.NaN(), 0, errNotDuration},
		{math.Inf(1), 0, errTooLong},

		// Durations.
		{10 * time.Second, 10 * time.Second, nil},
		{time.Duration(0), 0, nil},
		{-time.Second, 0, errNegative},

		// Anything else.
		{nil, 0, errNotDuration},
		{true, 0, errNotDuration},
		{[]string{"1s"}, 0, errNotDuration},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("Parse(%#v) error = %v, want %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%#v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// A number of seconds written with a fraction is the duration it says, not
// a nanosecond less, as truncating the float nearest it would make it.
func TestParseFractionsExactly(t *testing.T) {
	for whole := range 100 {
		for hundredths := range 100 {
			want := time.Duration(whole)*time.Second + time.Duration(hundredths)*10*time.Millisecond
			s := fmt.Sprintf("%d.%02d", whole, hundredths)
			if got, err := Parse(s); err != nil || got != want {
				t.Errorf("Parse(%q) = %v, %v, want %v", s, got, err, want)
			}
			f, _ := strconv.ParseFloat(s, 64)
			if got, err := Parse(f); err != nil || got != want {
				t.Errorf("Parse(%v) = %v, %v, want %v", f, got, err, want)
			}
		}
	}
	for ms := range 10000 {
		want := time.Duration(ms) * time.Millisecond
		s := fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
		if got, err := Parse(s); err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v, want %v", s, got, err, want)
		}
	}
}
