// Package truncate shortens long values before they are logged or printed,
// marking where they were cut.
package truncate

import (
	"fmt"
	"reflect"
	"unicode/utf8"

	"github.com/fatih/color"
)

// MaxLogLength is the maximum length for log output to prevent log bloat
const MaxLogLength = 200

// Message returns the colored marker appended to a truncated value.
func Message() string {
	return "... [" + color.New(color.FgYellow).Sprintf("⚠︎ probe truncated") + "]"
}

// String cuts s to at most maxLen bytes and appends Message when s is
// longer. The cut backs off to the start of a rune, so a multibyte
// character, such as one of Japanese text, is never split in two.
func String(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + Message()
}

// Value truncates the long strings in v, a value an action logs, at any
// depth, keeping its maps and lists as they are laid out, so that what logs
// it shows them as such. A map comes back as a map[string]any and a list as
// a []any, other than a []byte, which is left as it is, as are numbers and
// booleans. Any other value, such as a struct, is turned into a string and
// truncated.
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
