package jsonutil

import (
	"testing"
)

func TestDecode(t *testing.T) {
	t.Run("object input is unmarshaled into map[string]any", func(t *testing.T) {
		got := Decode(`{"name":"John","age":30}`)
		obj, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T (%+v)", got, got)
		}
		if obj["name"] != "John" {
			t.Errorf("expected name=John, got %v", obj["name"])
		}
	})

	t.Run("array input is unmarshaled into []any (regression: was clobbered with error_message)", func(t *testing.T) {
		// step.go feeds any body that LooksLikeJSON accepts (both `{...}` and
		// `[...]`) into Decode, so when an HTTP/DB action
		// returns a JSON array the body must round-trip as []any.
		// Previously the array path failed Unmarshal-into-map and the
		// caller's res.body got replaced by the error_message map.
		got := Decode(`[{"id":1},{"id":2}]`)
		arr, ok := got.([]any)
		if !ok {
			t.Fatalf("expected []any for array JSON input, got %T (%+v)", got, got)
		}
		if len(arr) != 2 {
			t.Fatalf("expected 2 items, got %d (%+v)", len(arr), arr)
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			t.Fatalf("expected first element to be map[string]any, got %T", arr[0])
		}
		if id, _ := first["id"].(float64); id != 1 {
			t.Errorf("expected first id=1, got %v", first["id"])
		}
	})

	t.Run("invalid JSON yields an error_message map", func(t *testing.T) {
		got := Decode(`not-json`)
		obj, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any with error_message, got %T (%+v)", got, got)
		}
		msg, ok := obj["error_message"].(string)
		if !ok || msg == "" {
			t.Errorf("expected non-empty error_message, got %v", obj)
		}
	})
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid object", `{"key": "value"}`, true},
		{"valid array", `["item1", "item2"]`, true},
		{"invalid json", `not json`, false},
		{"empty string", "", false},
		{"single char", "a", false},
		{"object with spaces", ` {"key": "value"} `, true},
		{"object-like string", `{key: value}`, true}, // LooksLikeJSON only checks { } brackets, not valid JSON syntax
		{"non-json string", `hello world`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LooksLikeJSON(tt.input)
			if result != tt.expected {
				t.Errorf("LooksLikeJSON() = %v, want %v", result, tt.expected)
			}
		})
	}
}
