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
