package mapping

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type TestStruct struct {
	String         string            `map:"string"`
	Number         int               `map:"number"`
	SliceString    []string          `map:"slice_string"`
	SliceAnyString []string          `map:"slice_any_string"`
	SliceStruct    []TestEmbedStruct `map:"slice_struct"`
	Bool           bool              `map:"bool"`
	Bytes          []byte            `map:"bytes"`
	Required       string            `map:"required" validate:"required"`
	MapStrStr      map[string]string `map:"map_str_str"`
	EmbedStruct    TestEmbedStruct   `map:"embed_struct"`
	Nested1        TestNested1       `map:"nested1"`
}

type TestEmbedStruct struct {
	Name string `map:"name"`
}

type TestNested1 struct {
	Nested2 TestNested2 `map:"nested2"`
}

type TestNested2 struct {
	Nested3 TestNested3 `map:"nested3"`
}

type TestNested3 struct {
	Nested4 TestNested4 `map:"nested4"`
}

type TestNested4 struct {
	Nested5 []TestEmbedStruct `map:"nested5"`
}

func TestMapToStructByTags_Types(t *testing.T) {
	got := TestStruct{
		String: "hello, world!",
		MapStrStr: map[string]string{
			"foo":   "bar",
			"hello": "world",
		},
	}

	params := map[string]any{
		"string": "s-t-r-i-n-g",
		"number": 123,
		"slice_string": []string{
			"a-a-a",
			"b-b-b",
			"c-c-c",
		},
		"slice_any_string": []any{
			"a-a-a",
			"b-b-b",
			"c-c-c",
		},
		"slice_struct": []any{
			map[string]any{"name": "foo"},
			map[string]any{"name": "bar"},
		},
		"bool":     false,
		"bytes":    "b-y-t-e-s",
		"required": "required!",
		"map_str_str": map[string]any{
			"foo": "f-o-o",
			"bar": "b-a-r",
			"baz": "b-a-z",
		},
		"embed_struct": map[string]any{
			"name": "probe",
		},
	}

	expects := TestStruct{
		String: "s-t-r-i-n-g",
		Number: 123,
		SliceString: []string{
			"a-a-a",
			"b-b-b",
			"c-c-c",
		},
		SliceAnyString: []string{
			"a-a-a",
			"b-b-b",
			"c-c-c",
		},
		SliceStruct: []TestEmbedStruct{
			{Name: "foo"},
			{Name: "bar"},
		},
		Bool:     false,
		Bytes:    []byte("b-y-t-e-s"),
		Required: "required!",
		MapStrStr: map[string]string{
			"foo":   "f-o-o",
			"bar":   "b-a-r",
			"baz":   "b-a-z",
			"hello": "world",
		},
		EmbedStruct: TestEmbedStruct{
			Name: "probe",
		},
	}

	if err := MapToStructByTags(params, &got); err != nil {
		t.Errorf("MapToStructByTags error %s", err)
	}

	if !reflect.DeepEqual(got, expects) {
		t.Errorf("\nExpected:\n%#v\nGot:\n%#v", expects, got)
	}
}

func TestMapToStructByTags_Required(t *testing.T) {
	got := TestStruct{}
	params := map[string]any{"string": "yo"}
	err := MapToStructByTags(params, &got)

	if err.Error() != "required field 'required' is missing" {
		t.Errorf("MapToStructByTags error is wrong: %s", err)
	}
}

func TestMergeStringMaps(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]string
		over     map[string]any
		expected map[string]string
	}{
		{
			name:     "merge string values",
			base:     map[string]string{"a": "1", "b": "2"},
			over:     map[string]any{"b": "overridden", "c": "3"},
			expected: map[string]string{"a": "1", "b": "overridden", "c": "3"},
		},
		{
			name:     "ignore non-string values",
			base:     map[string]string{"a": "1"},
			over:     map[string]any{"a": "overridden", "b": 123, "c": true},
			expected: map[string]string{"a": "overridden"},
		},
		{
			name:     "empty base map",
			base:     map[string]string{},
			over:     map[string]any{"a": "1", "b": "2"},
			expected: map[string]string{"a": "1", "b": "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeStringMaps(tt.base, tt.over)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("mergeStringMaps() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStructToMapByTags(t *testing.T) {
	input := TestStruct{
		String:         "test",
		Number:         42,
		SliceString:    []string{"a", "b", "c"},
		SliceAnyString: []string{},
		SliceStruct:    []TestEmbedStruct{{Name: "foo"}, {Name: "bar"}},
		Bool:           true,
		Bytes:          []byte("bytes"),
		Required:       "required",
		MapStrStr:      map[string]string{"key": "value"},
		EmbedStruct:    TestEmbedStruct{Name: "embedded"},
		Nested1: TestNested1{
			Nested2: TestNested2{
				Nested3: TestNested3{
					Nested4: TestNested4{
						Nested5: []TestEmbedStruct{{Name: "aaa"}, {Name: "bbb"}},
					},
				},
			},
		},
	}

	expected := map[string]any{
		"string":           "test",
		"number":           42,
		"slice_string":     []string{"a", "b", "c"},
		"slice_any_string": []string{},
		"slice_struct":     []any{map[string]any{"name": "foo"}, map[string]any{"name": "bar"}},
		"bool":             true,
		"bytes":            "bytes",
		"required":         "required",
		"map_str_str":      map[string]string{"key": "value"},
		"embed_struct":     map[string]any{"name": "embedded"},
		"nested1": map[string]any{
			"nested2": map[string]any{
				"nested3": map[string]any{
					"nested4": map[string]any{
						"nested5": []any{
							map[string]any{"name": "aaa"},
							map[string]any{"name": "bbb"},
						},
					},
				},
			},
		},
	}

	result, err := StructToMapByTags(input)
	if err != nil {
		t.Errorf("StructToMapByTags() error = %v", err)
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("StructToMapByTags() = %v, want %v", result, expected)
	}
}

type assignTarget struct {
	Name    string `map:"name" validate:"required"`
	Port    int    `map:"port" validate:"required"`
	Label   string `map:"label"`
	Timeout int    `map:"timeout"`
	Enabled bool   `map:"enabled"`
	Skipped string
}

func TestAssignStruct(t *testing.T) {
	var got assignTarget
	err := AssignStruct(map[string]any{
		"name":    "bulk",
		"port":    "25",
		"label":   42,
		"timeout": "30",
		"Skipped": "ignored",
	}, &got)
	if err != nil {
		t.Fatalf("AssignStruct() error = %v", err)
	}
	want := assignTarget{Name: "bulk", Port: 25, Label: "42", Timeout: 30}
	if got != want {
		t.Errorf("AssignStruct() = %+v, want %+v", got, want)
	}

	// An int that arrives as a number, as one written without quotes does.
	var n assignTarget
	if err := AssignStruct(map[string]any{"name": "n", "port": 0, "timeout": 5}, &n); err != nil || n.Timeout != 5 {
		t.Errorf("AssignStruct() with a numeric int = %+v, %v", n, err)
	}
}

func TestAssignStructErrors(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{name: "a required field missing", params: map[string]any{"port": 25, "label": "x"}, want: []string{"params 'name' is required"}},
		{name: "a required string empty", params: map[string]any{"name": "", "port": 25}, want: []string{"params 'name' is required"}},
		{name: "a required int missing", params: map[string]any{"name": "n"}, want: []string{"params 'port' is required"}},
		{name: "an int that is not a number", params: map[string]any{"name": "n", "port": 25, "timeout": "soon"}, want: []string{"params 'timeout' can't convert to int"}},
		{name: "a field of a type it does not assign", params: map[string]any{"name": "n", "port": 25, "enabled": true}, want: []string{"params 'enabled'"}},
		{
			name:   "every problem at once",
			params: map[string]any{"timeout": "soon"},
			want:   []string{"params 'name' is required", "params 'port' is required", "params 'timeout' can't convert to int"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssignStruct(tt.params, &assignTarget{})
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("AssignStruct() error = %v, want a *ValidationError", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestFieldTags(t *testing.T) {
	type req struct {
		URL      string            `map:"url"`
		Headers  map[string]string `map:"headers"`
		Option   string            `map:"value,omitempty"`
		Untagged string
		hidden   string `map:"hidden"`
	}
	// A tag is taken as written, as MapToStructByTags takes it.
	want := []string{"url", "headers", "value,omitempty"}
	for _, v := range []any{req{}, &req{}} {
		if got := FieldTags(v); !reflect.DeepEqual(got, want) {
			t.Errorf("FieldTags(%T) = %v, want %v", v, got, want)
		}
	}
	type untagged struct{ Name string }
	if got := FieldTags(untagged{}); got == nil || len(got) != 0 {
		t.Errorf("FieldTags(untagged) = %#v, want empty and not nil", got)
	}
	if got := FieldTags("not a struct"); got != nil {
		t.Errorf("FieldTags(string) = %v, want nil", got)
	}
	_ = req{}.hidden
}
