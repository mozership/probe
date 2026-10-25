// Package duration reads the durations a workflow is written with: a step's
// timeout and wait, the interval of a repeat or a retry, and the timeout an
// action takes. A duration is a Go duration string such as "30s" or "1m30s",
// or a number of seconds, which may have a fraction, such as 1.5.
package duration

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// MaxSeconds is the most whole seconds a time.Duration can hold, about 292
// years.
const MaxSeconds = math.MaxInt64 / int64(time.Second)

var (
	// A duration string too long for a time.Duration is refused by
	// time.ParseDuration as any other it cannot read, so it is told this too.
	errNotDuration = errors.New("use a duration such as 30s, or a number of seconds")
	errNegative    = errors.New("a duration cannot be below zero")
	errTooLong     = errors.New("a duration cannot be longer than about 292 years")
	errTooShort    = errors.New("a duration above zero cannot be shorter than a nanosecond")
)

// Parse reads v as a duration: a time.Duration, a duration string, a number
// of seconds, or a string of one, with the spaces around it ignored. Zero is
// a duration; whether it means none, no limit or the default is for the
// caller to say. Parse refuses a negative value, one too long for a
// time.Duration rather than wrapping it around to another, and one above zero
// that would truncate to zero. The error says why, and not what v was, so
// that the caller can name the field.
func Parse(v any) (time.Duration, error) {
	switch t := v.(type) {
	case time.Duration:
		if t < 0 {
			return 0, errNegative
		}
		return t, nil
	case string:
		s := strings.TrimSpace(t)
		if d, err := time.ParseDuration(s); err == nil {
			if d < 0 {
				return 0, errNegative
			}
			return d, nil
		}
		secs, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, errNotDuration
		}
		return fromSeconds(secs)
	case float64:
		return fromSeconds(t)
	case float32:
		return fromSeconds(float64(t))
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fromWholeSeconds(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() > uint64(MaxSeconds) {
			return 0, errTooLong
		}
		return fromWholeSeconds(int64(rv.Uint()))
	default:
		return 0, errNotDuration
	}
}

// fromWholeSeconds converts after checking the range, since multiplying
// first would wrap a large value around to some other duration.
func fromWholeSeconds(secs int64) (time.Duration, error) {
	switch {
	case secs < 0:
		return 0, errNegative
	case secs > MaxSeconds:
		return 0, errTooLong
	}
	return time.Duration(secs) * time.Second, nil
}

// fromSeconds converts a number of seconds that may have a fraction, after
// checking the range as fromWholeSeconds does.
func fromSeconds(secs float64) (time.Duration, error) {
	switch {
	case math.IsNaN(secs):
		return 0, errNotDuration
	case secs < 0:
		return 0, errNegative
	case secs > float64(MaxSeconds):
		return 0, errTooLong
	case secs == 0:
		return 0, nil
	}
	d := time.Duration(secs * float64(time.Second))
	if d <= 0 {
		return 0, errTooShort
	}
	return d, nil
}
