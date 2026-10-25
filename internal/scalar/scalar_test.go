package scalar

import (
	"testing"
	"time"
)

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
		ok       bool
	}{
		{"string", "hello", "hello", true},
		{"bool true", true, "true", true},
		{"bool false", false, "false", true},
		{"int", 42, "42", true},
		{"int64", int64(42), "42", true},
		{"negative int8", int8(-3), "-3", true},
		{"uint", uint(7), "7", true},
		{"float64", 3.14, "3.14", true},
		{"float64 without a fraction", float64(1e6), "1000000", true},
		{"float32", float32(0.5), "0.5", true},
		{"[]byte", []byte("bytes"), "bytes", true},
		{"Stringer", 2 * time.Second, "2s", true},
		{"nil", nil, "nil", true},
		{"nil slice", []int(nil), "nil", true},
		{"nil map", map[string]any(nil), "nil", true},
		{"unsupported", []int{1, 2, 3}, "", false},
		{"empty slice", []int{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := String(tt.input)
			if ok != tt.ok {
				t.Errorf("String() ok = %v, want %v", ok, tt.ok)
			}
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}
