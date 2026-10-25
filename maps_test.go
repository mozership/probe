package probe

import (
	"reflect"
	"testing"
)

func TestMergeMapsShared(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		over     map[string]any
		expected map[string]any
	}{
		{
			name:     "simple merge",
			base:     map[string]any{"a": 1, "b": 2},
			over:     map[string]any{"b": 3, "c": 4},
			expected: map[string]any{"a": 1, "b": 3, "c": 4},
		},
		{
			name: "recursive merge nested maps",
			base: map[string]any{
				"a":      1,
				"nested": map[string]any{"x": 1, "y": 2},
			},
			over: map[string]any{
				"nested": map[string]any{"y": 3, "z": 4},
				"c":      5,
			},
			expected: map[string]any{
				"a":      1,
				"nested": map[string]any{"x": 1, "y": 3, "z": 4},
				"c":      5,
			},
		},
		{
			name:     "overwrite with non-map value",
			base:     map[string]any{"a": map[string]any{"x": 1}},
			over:     map[string]any{"a": "string"},
			expected: map[string]any{"a": "string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeMapsShared(tt.base, tt.over)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("mergeMapsShared() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// A map only base has is shared with the result rather than copied, as
// mergeMapsShared says.
func TestMergeMapsShared_Shares(t *testing.T) {
	nested := map[string]any{"x": 1}
	merged := mergeMapsShared(map[string]any{"only": nested}, map[string]any{"c": 1})
	merged["only"].(map[string]any)["x"] = 2
	if nested["x"] != 2 {
		t.Errorf("base's map was copied: %v", nested)
	}
}
