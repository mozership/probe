package jsonutil

import (
	"reflect"
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

	t.Run("an error_message says why Parse refused the input", func(t *testing.T) {
		tests := map[string]string{
			``:         "invalid JSON: empty string",
			`null`:     "invalid JSON: input is not a JSON object or array",
			`"hello"`:  "invalid JSON: input is not a JSON object or array",
			`{"a": 1`:  "invalid JSON: unexpected end of JSON input",
			`[1, 2,]`:  "invalid JSON: invalid character ']' looking for beginning of value",
			`  {"a":}`: "invalid JSON: invalid character '}' looking for beginning of value",
		}
		for in, want := range tests {
			got := Decode(in)
			if !reflect.DeepEqual(got, map[string]any{"error_message": want}) {
				t.Errorf("Decode(%q) = %#v, want error_message %q", in, got, want)
			}
		}
	})

	t.Run("surrounding whitespace is ignored as Parse ignores it", func(t *testing.T) {
		got := Decode("\n  [1]\n")
		if !reflect.DeepEqual(got, []any{float64(1)}) {
			t.Errorf("Decode() = %#v, want []any{1}", got)
		}
	})
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected any
		wantErr  bool
	}{
		{
			name:  "simple object",
			input: `{"id": 123, "name": "test"}`,
			expected: map[string]any{
				"id":   float64(123),
				"name": "test",
			},
			wantErr: false,
		},
		{
			name:  "nested object",
			input: `{"data": {"id": 456, "tags": ["a", "b"]}}`,
			expected: map[string]any{
				"data": map[string]any{
					"id":   float64(456),
					"tags": []any{"a", "b"},
				},
			},
			wantErr: false,
		},
		{
			name:     "array",
			input:    `[1, 2, 3]`,
			expected: []any{float64(1), float64(2), float64(3)},
			wantErr:  false,
		},
		{
			name:     "array of objects",
			input:    `[{"id": 1}, {"id": 2}]`,
			expected: []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2)}},
			wantErr:  false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   "not json",
			wantErr: true,
		},
		{
			name:    "plain string",
			input:   `"hello"`,
			wantErr: true,
		},
		{
			name:    "plain number",
			input:   "123",
			wantErr: true,
		},
		{
			name:  "object with whitespace",
			input: "  \n  {\"key\": \"value\"}  \n  ",
			expected: map[string]any{
				"key": "value",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error but got none, result: %v", got)
				}
				return
			}

			if err != nil {
				t.Errorf("Parse() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Parse() = %v, want %v", got, tt.expected)
			}
		})
	}
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
