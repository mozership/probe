package truncate

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

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
		{
			name:     "a cut at a rune boundary",
			input:    "日本語のテキスト",
			maxLen:   6,
			expected: "日本... [⚠︎ probe truncated]",
		},
		{
			name:     "a cut inside a rune backs off to its start",
			input:    "日本語のテキスト",
			maxLen:   8,
			expected: "日本... [⚠︎ probe truncated]",
		},
		{
			name:     "a cut inside the first rune leaves nothing of it",
			input:    "日本語",
			maxLen:   2,
			expected: "... [⚠︎ probe truncated]",
		},
		{
			name:     "a cut inside an emoji",
			input:    "ok🙂🙂",
			maxLen:   5,
			expected: "ok... [⚠︎ probe truncated]",
		},
		{
			name:     "invalid UTF-8 is cut by its bytes",
			input:    "\xff\xfe\xfd",
			maxLen:   1,
			expected: "\xff... [⚠︎ probe truncated]",
		},
		{
			name:     "a run of continuation bytes is cut where the limit falls",
			input:    strings.Repeat("\x80", 10),
			maxLen:   6,
			expected: strings.Repeat("\x80", 6) + "... [⚠︎ probe truncated]",
		},
		{
			name:     "stray continuation bytes before a rune are kept",
			input:    "ab\x80\x80日本",
			maxLen:   6,
			expected: "ab\x80\x80... [⚠︎ probe truncated]",
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

// TestStringKeepsValidUTF8 cuts multibyte text at every length and checks
// the result is still valid UTF-8 of at most that many bytes before the
// marker.
func TestStringKeepsValidUTF8(t *testing.T) {
	color.NoColor = true
	defer func() { color.NoColor = false }()

	s := "aé日🙂b日本"
	for maxLen := 0; maxLen < len(s); maxLen++ {
		got := String(s, maxLen)
		kept, ok := strings.CutSuffix(got, Message())
		if !ok {
			t.Fatalf("String(%q, %d) = %q, want the marker at the end", s, maxLen, got)
		}
		if !utf8.ValidString(kept) || len(kept) > maxLen || !strings.HasPrefix(s, kept) {
			t.Errorf("String(%q, %d) kept %q, want a valid prefix of at most %d bytes", s, maxLen, kept, maxLen)
		}
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
