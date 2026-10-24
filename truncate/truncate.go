// Package truncate shortens long values before they are logged or printed,
// marking where they were cut.
package truncate

import (
	"fmt"
	"reflect"

	"github.com/fatih/color"
)

const (
	// MaxLogLength is the maximum length for log output to prevent log bloat
	MaxLogLength = 200
	// MaxLength is the maximum length for general string processing
	MaxLength = 1000000
)

// Message returns the colored marker appended to a truncated value.
func Message() string {
	return "... [" + color.New(color.FgYellow).Sprintf("⚠︎ probe truncated") + "]"
}

// String truncates a string if it exceeds the maximum length
func String(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + Message()
}

// MapString truncates long values in map[string]string for logging
func MapString(params map[string]string, maxLen int) map[string]string {
	truncated := make(map[string]string)
	for key, value := range params {
		truncated[key] = String(value, maxLen)
	}
	return truncated
}

// Map truncates long values in map[string]any for logging
//
// Deprecated: use Value, which keeps the maps and lists in the values.
func Map(params map[string]any, maxLen int) map[string]any {
	truncated := make(map[string]any)
	for key, value := range params {
		switch v := value.(type) {
		case string:
			truncated[key] = String(v, maxLen)
		default:
			// For non-string values, convert to string first, then truncate
			str := fmt.Sprintf("%v", v)
			truncated[key] = String(str, maxLen)
		}
	}
	return truncated
}

// Value truncates the long strings in v, a value an action logs, at any
// depth, keeping its maps and lists as they are laid out, so that what logs
// it shows them as such. A map comes back as a map[string]any and a list as
// a []any, other than a []byte, which is left as it is, as are numbers and
// booleans. Any other value, such as a struct, is turned into a string and
// truncated, as Map does.
func Value(v any, maxLen int) any {
	switch val := v.(type) {
	case nil, bool, []byte,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v
	case string:
		return String(val, maxLen)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[fmt.Sprint(iter.Key().Interface())] = Value(iter.Value().Interface(), maxLen)
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = Value(rv.Index(i).Interface(), maxLen)
		}
		return out
	}
	return String(fmt.Sprintf("%v", v), maxLen)
}
