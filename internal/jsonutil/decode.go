// Package jsonutil decodes and compares the JSON values that actions return.
package jsonutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Parse decodes a JSON object into a map[string]any or a JSON array into a
// []any, surrounding whitespace aside. It refuses an empty string, any other
// JSON value, such as a string, a number or null, and invalid JSON.
func Parse(s string) (any, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return nil, errors.New("empty string")
	}

	switch s[0] {
	case '{':
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	return nil, errors.New("input is not a JSON object or array")
}

// Decode decodes a JSON object or array as Parse does. On failure it returns
// a map[string]any with an error_message instead of an error, so callers can
// surface the parse error in place of the data.
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
//	// result: map[string]any{"error_message": "invalid JSON: ..."}
func Decode(s string) any {
	v, err := Parse(s)
	if err != nil {
		return map[string]any{
			"error_message": fmt.Sprintf("invalid JSON: %s", err),
		}
	}
	return v
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
