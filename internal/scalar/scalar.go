// Package scalar writes a scalar value, such as a number or a bool, as the
// text a workflow compares or passes on.
package scalar

import (
	"fmt"
	"reflect"
	"strconv"
)

// String returns the text of a scalar value: a string as it is, a bool as
// true or false, a number in decimal without an exponent or trailing zeros,
// a []byte as its bytes and a fmt.Stringer as its String. It reports false
// for any other value, such as a slice or a map.
//
// nil, and the zero value of a type it does not write, such as a nil slice
// or map, is written as "nil", so a missing value reads as nil rather than
// as an empty string.
//
// Example:
//
//	s, ok := String(42)        // "42", true
//	s, ok := String(3.14)      // "3.14", true
//	s, ok := String("hello")   // "hello", true
//	s, ok := String(nil)       // "nil", true
//	s, ok := String([]int{1})  // "", false
func String(value any) (string, bool) {
	if value == nil {
		return "nil", true
	}

	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int, int8, int16, int32, int64:
		return strconv.FormatInt(reflect.ValueOf(v).Int(), 10), true
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflect.ValueOf(v).Uint(), 10), true
	case float32, float64:
		return strconv.FormatFloat(reflect.ValueOf(v).Float(), 'f', -1, 64), true
	case []byte:
		return string(v), true
	case fmt.Stringer:
		return v.String(), true
	default:
		if reflect.ValueOf(value).IsZero() {
			return "nil", true
		}
		return "", false
	}
}
