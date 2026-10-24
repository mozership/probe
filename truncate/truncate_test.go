package truncate

import (
	"reflect"
	"testing"

	"github.com/fatih/color"
)

func TestMessage(t *testing.T) {
	// Disable color output for consistent testing
	color.NoColor = true
	defer func() { color.NoColor = false }()

	result := Message()
	expected := "... [⚠︎ probe truncated]"

	if result != expected {
		t.Errorf("Message() = %q, want %q", result, expected)
	}
}

func TestString(t *testing.T) {
	// Disable color output for consistent testing
	color.NoColor = true
	defer func() { color.NoColor = false }()

	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long string",
			input:    "this is a very long string that exceeds the limit",
			maxLen:   10,
			expected: "this is a ... [⚠︎ probe truncated]",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "zero max length",
			input:    "hello",
			maxLen:   0,
			expected: "... [⚠︎ probe truncated]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := String(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("String(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestMapString(t *testing.T) {
	// Disable color output for consistent testing
	color.NoColor = true
	defer func() { color.NoColor = false }()

	tests := []struct {
		name     string
		input    map[string]string
		maxLen   int
		expected map[string]string
	}{
		{
			name: "short values",
			input: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			maxLen: 10,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "mixed length values",
			input: map[string]string{
				"short": "abc",
				"long":  "this is a very long string that will be truncated",
			},
			maxLen: 10,
			expected: map[string]string{
				"short": "abc",
				"long":  "this is a ... [⚠︎ probe truncated]",
			},
		},
		{
			name: "all long values",
			input: map[string]string{
				"url":  "https://example.com/very/long/path/that/exceeds/the/limit",
				"body": "this is a very long request body that contains lots of data",
			},
			maxLen: 15,
			expected: map[string]string{
				"url":  "https://example... [⚠︎ probe truncated]",
				"body": "this is a very ... [⚠︎ probe truncated]",
			},
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			maxLen:   10,
			expected: map[string]string{},
		},
		{
			name: "zero max length",
			input: map[string]string{
				"key": "value",
			},
			maxLen: 0,
			expected: map[string]string{
				"key": "... [⚠︎ probe truncated]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapString(tt.input, tt.maxLen)

			if len(result) != len(tt.expected) {
				t.Errorf("MapString() returned map with %d keys, expected %d", len(result), len(tt.expected))
			}

			for key, expectedValue := range tt.expected {
				actualValue, exists := result[key]
				if !exists {
					t.Errorf("MapString() missing key %q", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("MapString() key %q = %q, want %q", key, actualValue, expectedValue)
				}
			}
		})
	}
}

func TestMaxLogLength(t *testing.T) {
	// Test that the constant is properly defined
	if MaxLogLength <= 0 {
		t.Errorf("MaxLogLength should be positive, got %d", MaxLogLength)
	}

	// Test that it has a reasonable value (expected to be 200)
	expectedValue := 200
	if MaxLogLength != expectedValue {
		t.Errorf("MaxLogLength = %d, expected %d", MaxLogLength, expectedValue)
	}
}

func TestMaxLength(t *testing.T) {
	// Test that the constant is properly defined
	if MaxLength <= 0 {
		t.Errorf("MaxLength should be positive, got %d", MaxLength)
	}

	// Test that it has a reasonable value (expected to be 1000000)
	expectedValue := 1000000
	if MaxLength != expectedValue {
		t.Errorf("MaxLength = %d, expected %d", MaxLength, expectedValue)
	}
}

func TestMap(t *testing.T) {
	// Disable color output for consistent testing
	color.NoColor = true
	defer func() { color.NoColor = false }()

	input := map[string]any{
		"short":  "abc",
		"long":   "abcdefghij",
		"number": 1234567,
		"nested": map[string]any{"k": "vvvvvvvv"},
		"none":   nil,
	}
	got := Map(input, 5)

	want := map[string]any{
		"short":  "abc",
		"long":   "abcde" + Message(),
		"number": "12345" + Message(),
		// A value that is not a string is printed first, so a nested map is
		// cut as one string rather than value by value.
		"nested": "map[k" + Message(),
		"none":   "<nil>",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("Map()[%q] = %#v, want %#v", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Map() has %d keys, want %d", len(got), len(want))
	}
	if input["long"] != "abcdefghij" {
		t.Error("Map() changed its input")
	}
}

func TestValue(t *testing.T) {
	color.NoColor = true
	defer func() { color.NoColor = false }()

	type point struct{ X, Y int }
	input := map[string]any{
		"short":   "abc",
		"long":    "abcdefghij",
		"number":  1234567,
		"float":   1.5,
		"flag":    true,
		"none":    nil,
		"nested":  map[string]any{"k": "vvvvvvvv", "n": 2},
		"headers": map[string]string{"Accept": "text/plain"},
		"list":    []any{"wwwwwwww", 3},
		"tags":    []string{"a"},
		"bytes":   []byte("xyz"),
		"struct":  point{X: 123456, Y: 2},
	}
	got := Value(input, 5)

	// Maps and lists are kept, so that what logs them lays them out, and
	// the strings in them are cut one by one.
	want := map[string]any{
		"short":   "abc",
		"long":    "abcde" + Message(),
		"number":  1234567,
		"float":   1.5,
		"flag":    true,
		"none":    nil,
		"nested":  map[string]any{"k": "vvvvv" + Message(), "n": 2},
		"headers": map[string]any{"Accept": "text/" + Message()},
		"list":    []any{"wwwww" + Message(), 3},
		"tags":    []any{"a"},
		"bytes":   []byte("xyz"),
		"struct":  "{1234" + Message(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Value() = %#v, want %#v", got, want)
	}
	if input["long"] != "abcdefghij" || input["nested"].(map[string]any)["k"] != "vvvvvvvv" {
		t.Error("Value() changed its input")
	}
}
