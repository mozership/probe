package jsonutil

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/mozership/probe/internal/scalar"
)

// Match compares two `map[string]any` objects strictly.
// All fields in `src` and `target` must match, including structure and values.
// It returns an error when `target` holds a "/regex/" string that does not
// compile.
func Match(src, target map[string]any) (bool, error) {
	m, err := newMatcher(target)
	if err != nil {
		return false, err
	}
	var diffs []string
	return m.deepMatch(src, target, &diffs, ""), nil
}

// Diff compares two `map[string]any` objects strictly and describes the
// first difference it finds, walking keys in sorted order so the same input
// always reports the same difference. It returns an error when `target`
// holds a "/regex/" string that does not compile.
func Diff(src, target map[string]any) (string, error) {
	m, err := newMatcher(target)
	if err != nil {
		return "", err
	}
	var diffs []string
	if match := m.deepMatch(src, target, &diffs, ""); match {
		return "No diff", nil
	}
	return strings.Join(diffs, "\n"), nil
}

// matcher holds the compiled "/regex/" patterns found in a target.
type matcher struct {
	patterns map[string]*regexp.Regexp
}

// newMatcher compiles every "/regex/" pattern in target up front, so an
// invalid pattern is reported whatever src holds, not only when the walk
// happens to reach it.
func newMatcher(target any) (*matcher, error) {
	m := &matcher{patterns: map[string]*regexp.Regexp{}}
	if err := m.compile(target, ""); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *matcher) compile(target any, path string) error {
	switch v := target.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			if err := m.compile(v[key], extendPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for i, elem := range v {
			if err := m.compile(elem, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		pattern, ok := regexPattern(v)
		if !ok {
			return nil
		}
		if _, done := m.patterns[pattern]; done {
			return nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid regex at key '%s': %w", path, err)
		}
		m.patterns[pattern] = re
	}
	return nil
}

// regexPattern returns the pattern of a "/regex/" string.
func regexPattern(s string) (string, bool) {
	if len(s) > 2 && s[0] == '/' && s[len(s)-1] == '/' {
		return s[1 : len(s)-1], true
	}
	return "", false
}

// extendPath constructs a new path for nested keys.
func extendPath(path, key string) string {
	if path == "" {
		return key
	}
	return fmt.Sprintf("%s.%s", path, key)
}

// deepMatch recursively compares `src` and `target`.
func (m *matcher) deepMatch(src, target any, diffs *[]string, path string) bool {
	switch targetVal := target.(type) {
	case map[string]any:
		// Check if src is also a map
		srcMap, ok := src.(map[string]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected a map, got %T", path, src))
			return false
		}
		// Check for missing or mismatched keys
		for _, key := range slices.Sorted(maps.Keys(targetVal)) {
			targetValue := targetVal[key]
			newPath := extendPath(path, key)
			srcValue, exists := srcMap[key]
			if !exists {
				*diffs = append(*diffs, fmt.Sprintf("Key '%s': Missing in source", newPath))
				return false
			}
			if !m.deepMatch(srcValue, targetValue, diffs, newPath) {
				return false
			}
		}
		// Check for extra keys in src
		for _, key := range slices.Sorted(maps.Keys(srcMap)) {
			if _, exists := targetVal[key]; !exists {
				newPath := extendPath(path, key)
				*diffs = append(*diffs, fmt.Sprintf("Key '%s': Extra field in source", newPath))
				return false
			}
		}
		return true

	case []any:
		// Check if src is also a slice
		srcSlice, ok := src.([]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected a slice, got %T", path, src))
			return false
		}
		if len(srcSlice) != len(targetVal) {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Slice length mismatch (expected %d, got %d)", path, len(targetVal), len(srcSlice)))
			return false
		}
		// Recursively compare each element in the slice
		for i, targetElem := range targetVal {
			newPath := fmt.Sprintf("%s[%d]", path, i)
			if !m.deepMatch(srcSlice[i], targetElem, diffs, newPath) {
				return false
			}
		}
		return true

	case int, int64, uint, uint64, float64:
		targetStr, ok := scalar.String(target)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected number, got %T", path, target))
			return false
		}
		srcStr, ok := scalar.String(src)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected %#v, got %#v", path, target, src))
			return false
		}
		if srcStr != targetStr {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected %s, got %s", path, targetStr, srcStr))
			return false
		}
		return true

	case string:
		// If the target value is a regex (e.g., "/regex/")
		if pattern, ok := regexPattern(targetVal); ok {
			re := m.patterns[pattern]
			srcStr, ok := src.(string)
			if ok {
				if !re.MatchString(srcStr) {
					*diffs = append(*diffs, fmt.Sprintf("Key '%s': Regex mismatch (pattern: %s, value: %v)", path, pattern, srcStr))
					return false
				}
			} else {
				srcStr, ok := scalar.String(src)
				if !ok || !re.MatchString(srcStr) {
					*diffs = append(*diffs, fmt.Sprintf("Key '%s': Regex mismatch (pattern: %s, value: %v)", path, pattern, srcStr))
					return false
				}
			}
			return true
		}
		// Otherwise, compare as a regular string
		srcStr, ok := src.(string)
		if !ok || srcStr != targetVal {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected '%s', got '%v'", path, targetVal, src))
			return false
		}
		return true

	default:
		// Compare all other types directly
		if src != target {
			*diffs = append(*diffs, fmt.Sprintf("Key '%s': Expected '%v', got '%v'", path, target, src))
			return false
		}
		return true
	}
}

// Parse parses a JSON string into a map[string]any or []any.
func Parse(s string) (any, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return nil, fmt.Errorf("parse_json: empty string")
	}

	// Try parsing as object
	if s[0] == '{' {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, fmt.Errorf("parse_json: %w", err)
		}
		return obj, nil
	}

	// Try parsing as array
	if s[0] == '[' {
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, fmt.Errorf("parse_json: %w", err)
		}
		return arr, nil
	}

	return nil, fmt.Errorf("parse_json: input is not a JSON object or array")
}
