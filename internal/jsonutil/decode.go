// Package jsonutil decodes and compares the JSON values that actions return.
package jsonutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Decode unmarshals a JSON string into either a map[string]any (object
// input) or []any (array input). On failure it returns a map[string]any
// with an error_message instead of an error, so callers can surface the
// parse error in place of the data.
//
// Example:
//
//	result := Decode(`{"name": "John"}`)
//	// result: map[string]any{"name": "John"}
//
//	result := Decode(`[{"id": 1}, {"id": 2}]`)
//	// result: []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2)}}
//
//	result := Decode(`invalid json`)
//	// result: map[string]any{"error_message": "mustMarshalJSON error: ..."}
func Decode(st string) any {
	// Pick the target type from the first non-space byte so callers
	// that handed us an array body don't get their data replaced by
	// the unmarshal error from the (object-only) default branch.
	trimmed := strings.TrimLeft(st, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []any
		if err := json.Unmarshal([]byte(st), &arr); err != nil {
			return map[string]any{
				"error_message": fmt.Sprintf("mustMarshalJSON error: %s", err),
			}
		}
		return arr
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(st), &obj); err != nil {
		return map[string]any{
			"error_message": fmt.Sprintf("mustMarshalJSON error: %s", err),
		}
	}
	return obj
}

// LooksLikeJSON checks if a string appears to be JSON by examining its first and last characters.
// This is a simple heuristic check and does not validate actual JSON syntax.
//
// Example:
//
//	LooksLikeJSON(`{"key": "value"}`)  // true
//	LooksLikeJSON(`["item1", "item2"]`) // true
//	LooksLikeJSON(`{key: value}`)       // true (note: this is actually invalid JSON but has JSON-like brackets)
//	LooksLikeJSON(`hello world`)        // false
func LooksLikeJSON(st string) bool {
	trimmed := strings.TrimSpace(st)
	if len(trimmed) < 2 {
		return false
	}

	fChar := rune(trimmed[0])
	lChar := rune(trimmed[len(trimmed)-1])

	return (fChar == '{' && lChar == '}') || (fChar == '[' && lChar == ']')
}
