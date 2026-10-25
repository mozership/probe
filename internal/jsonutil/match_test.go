package jsonutil

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name   string
		src    map[string]any
		target map[string]any
		want   bool
	}{
		{
			name: "Exact match with simple fields",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			want: true,
		},
		{
			name: "Extra field in src",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
				"age":  30,
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			want: false,
		},
		{
			name: "Missing field in src",
			src: map[string]any{
				"id": 123,
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			want: false,
		},
		{
			name: "Field value mismatch",
			src: map[string]any{
				"id":   123,
				"name": "Bob",
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			want: false,
		},
		{
			name: "Nested map exact match",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
				"meta": map[string]any{
					"role": "admin",
				},
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
				"meta": map[string]any{
					"role": "admin",
				},
			},
			want: true,
		},
		{
			name: "Nested map mismatch",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
				"meta": map[string]any{
					"role": "user",
				},
			},
			target: map[string]any{
				"id":   123,
				"name": "Alice",
				"meta": map[string]any{
					"role": "admin",
				},
			},
			want: false,
		},
		{
			name: "Regex in bool src matches",
			src: map[string]any{
				"bot": true,
			},
			target: map[string]any{
				"bot": "/^(true|false)$/",
			},
			want: true,
		},
		{
			name: "Different number type as float64 and uint64",
			src: map[string]any{
				"id": float64(123),
			},
			target: map[string]any{
				"id": uint64(123),
			},
			want: true,
		},
		{
			name: "Regex in float64 src matches",
			src: map[string]any{
				"id": float64(12345),
			},
			target: map[string]any{
				"id": "/^\\d{5}$/",
			},
			want: true,
		},
		{
			name: "Array exact match",
			src: map[string]any{
				"id":    123,
				"roles": []any{"admin", "editor"},
			},
			target: map[string]any{
				"id":    123,
				"roles": []any{"admin", "editor"},
			},
			want: true,
		},
		{
			name: "Array mismatch",
			src: map[string]any{
				"id":    123,
				"roles": []any{"admin", "viewer"},
			},
			target: map[string]any{
				"id":    123,
				"roles": []any{"admin", "editor"},
			},
			want: false,
		},
		{
			name: "Regex in target does not match",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			target: map[string]any{
				"id":   123,
				"name": "/^Bob$/", // Expecting "Bob", but "Alice" is provided
			},
			want: false,
		},
		{
			name: "Regex in target matches",
			src: map[string]any{
				"id":   123,
				"name": "Alice",
			},
			target: map[string]any{
				"id":   123,
				"name": "/^\\w{5}$/", // Matches any name match 5 ascii
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Match(tt.src, tt.target)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("deepMatch() = %v, want %v (src: %v, target: %v)", got, tt.want, tt.src, tt.target)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name   string
		src    map[string]any
		target map[string]any
		want   string
	}{
		{
			name:   "match",
			src:    map[string]any{"id": float64(1), "name": "Alice"},
			target: map[string]any{"id": 1, "name": "/^A/"},
			want:   "No diff",
		},
		{
			name:   "missing key",
			src:    map[string]any{"id": 1},
			target: map[string]any{"id": 1, "name": "Alice"},
			want:   "Key 'name': Missing in source",
		},
		{
			name:   "extra key",
			src:    map[string]any{"id": 1, "age": 30},
			target: map[string]any{"id": 1},
			want:   "Key 'age': Extra field in source",
		},
		{
			name:   "type mismatch map",
			src:    map[string]any{"meta": "admin"},
			target: map[string]any{"meta": map[string]any{"role": "admin"}},
			want:   "Key 'meta': Expected a map, got string",
		},
		{
			name:   "type mismatch slice",
			src:    map[string]any{"roles": "admin"},
			target: map[string]any{"roles": []any{"admin"}},
			want:   "Key 'roles': Expected a slice, got string",
		},
		{
			name:   "type mismatch number",
			src:    map[string]any{"id": []any{1}},
			target: map[string]any{"id": 1},
			want:   "Key 'id': Expected 1, got []interface {}{1}",
		},
		{
			name:   "nested object",
			src:    map[string]any{"meta": map[string]any{"role": "user"}},
			target: map[string]any{"meta": map[string]any{"role": "admin"}},
			want:   "Key 'meta.role': Expected 'admin', got 'user'",
		},
		{
			name:   "nested array",
			src:    map[string]any{"items": []any{map[string]any{"id": 1}, map[string]any{"id": 3}}},
			target: map[string]any{"items": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}},
			want:   "Key 'items[1].id': Expected 2, got 3",
		},
		{
			name:   "slice length",
			src:    map[string]any{"roles": []any{"admin"}},
			target: map[string]any{"roles": []any{"admin", "editor"}},
			want:   "Key 'roles': Slice length mismatch (expected 2, got 1)",
		},
		{
			name:   "regex mismatch",
			src:    map[string]any{"name": "Alice"},
			target: map[string]any{"name": "/^Bob$/"},
			want:   "Key 'name': Regex mismatch (pattern: ^Bob$, value: Alice)",
		},
		{
			name:   "first difference in sorted key order",
			src:    map[string]any{"a": 9, "b": 9, "c": 9, "d": 9, "e": 9},
			target: map[string]any{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5},
			want:   "Key 'a': Expected 1, got 9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Diff(tt.src, tt.target)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Diff() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiffIsDeterministic(t *testing.T) {
	src := map[string]any{
		"x": 0, "y": 0, "z": 0,
		"nested": map[string]any{"p": 0, "q": 0, "r": 0},
		"extra1": 0, "extra2": 0,
	}
	target := map[string]any{
		"z": 1, "y": 1, "x": 1,
		"nested":   map[string]any{"r": 1, "q": 1, "p": 1},
		"missing1": 1, "missing2": 1,
	}
	want := "Key 'missing1': Missing in source"
	for range 100 {
		got, err := Diff(src, target)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if got != want {
			t.Fatalf("Diff() = %q, want %q", got, want)
		}
	}

	// Without missing keys, the first mismatch in sorted order wins, ahead of
	// extra keys in src.
	delete(target, "missing1")
	delete(target, "missing2")
	want = "Key 'nested.p': Expected 1, got 0"
	for range 100 {
		got, err := Diff(src, target)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if got != want {
			t.Fatalf("Diff() = %q, want %q", got, want)
		}
	}
}

func TestInvalidRegex(t *testing.T) {
	tests := []struct {
		name   string
		src    map[string]any
		target map[string]any
		want   string
	}{
		{
			name:   "top level",
			src:    map[string]any{"x": "abc"},
			target: map[string]any{"x": "/[/"},
			want:   "invalid regex at key 'x'",
		},
		{
			name:   "nested in array",
			src:    map[string]any{"items": []any{map[string]any{"x": "abc"}}},
			target: map[string]any{"items": []any{map[string]any{"x": "/(/"}}},
			want:   "invalid regex at key 'items[0].x'",
		},
		{
			name: "reported even when an earlier key already differs",
			src:  map[string]any{"a": "no", "b": "abc"},
			target: map[string]any{
				"a": "yes",
				"b": "/[/",
			},
			want: "invalid regex at key 'b'",
		},
		{
			name:   "reported even when the key is missing in src",
			src:    map[string]any{},
			target: map[string]any{"x": "/[/"},
			want:   "invalid regex at key 'x'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Match(tt.src, tt.target)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Match() error = %v, want containing %q", err, tt.want)
			}
			if ok {
				t.Errorf("Match() = true, want false")
			}
			diff, err := Diff(tt.src, tt.target)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Diff() error = %v, want containing %q", err, tt.want)
			}
			if diff != "" {
				t.Errorf("Diff() = %q, want empty", diff)
			}
		})
	}
}
