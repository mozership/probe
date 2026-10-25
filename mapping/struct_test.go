package mapping

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestMapToStructByTags_Int(t *testing.T) {
	type dest struct {
		N int `map:"n"`
	}
	tests := []struct {
		in      any
		want    int
		wantErr string
	}{
		{in: 3, want: 3},
		{in: int64(-3), want: -3},
		{in: float64(3), want: 3},
		{in: math.Copysign(0, -1), want: 0},
		{in: "42", want: 42},
		{in: float64(-1 << 63), want: math.MinInt},
		// A number that is not whole was cut down to one, so 2.5 was 2 and
		// -0.5 was 0.
		{in: 2.5, wantErr: "cannot convert 2.5 to int for field 'n': not a whole number"},
		{in: -0.5, wantErr: "not a whole number"},
		{in: math.NaN(), wantErr: "not a whole number"},
		{in: math.Inf(1), wantErr: "not a whole number"},
		// int64 of a float64 out of range is whatever the platform makes of
		// it, such as MaxInt64 on arm64.
		{in: 1e20, wantErr: "cannot convert 1e+20 to int for field 'n': out of range"},
		{in: float64(1 << 63), wantErr: "out of range"},
		{in: -1e20, wantErr: "out of range"},
		{in: "2.5", wantErr: "cannot convert '2.5' to int for field 'n'"},
		{in: "99999999999999999999", wantErr: "cannot convert '99999999999999999999' to int"},
		{in: true, wantErr: "cannot convert bool to int for field 'n'"},
	}
	for _, tt := range tests {
		var got dest
		err := MapToStructByTags(map[string]any{"n": tt.in}, &got)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%v (%T): error = %v, want %q", tt.in, tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got.N != tt.want {
			t.Errorf("%v (%T): got %d, %v, want %d", tt.in, tt.in, got.N, err, tt.want)
		}
	}
}

func TestMapToStructByTags_Bool(t *testing.T) {
	type dest struct {
		B bool `map:"b"`
	}
	tests := []struct {
		in      any
		want    bool
		wantErr string
	}{
		{in: true, want: true},
		{in: false, want: false},
		{in: "true", want: true},
		{in: "false", want: false},
		{in: "1", want: true},
		{in: "0", want: false},
		// A number reads as the string of it would, so 1 is true and 0 false.
		{in: 1, want: true},
		{in: int64(1), want: true},
		{in: int64(0), want: false},
		{in: float64(1), want: true},
		{in: float64(0), want: false},
		{in: math.Copysign(0, -1), want: false},
		{in: uint64(1), want: true},
		{in: int64(2), wantErr: "cannot convert 2 to bool for field 'b': use true or false"},
		{in: 0.5, wantErr: "cannot convert 0.5 to bool for field 'b'"},
		{in: "yes", wantErr: "cannot convert 'yes' to bool for field 'b'"},
		{in: []any{}, wantErr: "cannot convert []interface {} to bool for field 'b'"},
	}
	for _, tt := range tests {
		var got dest
		err := MapToStructByTags(map[string]any{"b": tt.in}, &got)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%v (%T): error = %v, want %q", tt.in, tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got.B != tt.want {
			t.Errorf("%v (%T): got %v, %v, want %v", tt.in, tt.in, got.B, err, tt.want)
		}
	}
}

func TestMapToStructByTags_NumberKinds(t *testing.T) {
	// Each numeric kind takes a number that fits it, and refuses one that
	// does not, rather than wrap it round or cut it down as a conversion
	// does.
	type dest struct {
		U   uint          `map:"u"`
		U8  uint8         `map:"u8"`
		I8  int8          `map:"i8"`
		I16 int16         `map:"i16"`
		I32 int32         `map:"i32"`
		I64 int64         `map:"i64"`
		F32 float32       `map:"f32"`
		F64 float64       `map:"f64"`
		D   time.Duration `map:"d"`
	}
	tests := []struct {
		key     string
		in      any
		want    any
		wantErr string
	}{
		{key: "u", in: int64(7), want: uint(7)},
		{key: "u", in: float64(7), want: uint(7)},
		{key: "u", in: "7", want: uint(7)},
		{key: "u", in: uint64(math.MaxUint64), want: uint(math.MaxUint)},
		{key: "u", in: int64(-1), wantErr: "cannot convert -1 to uint for field 'u': out of range"},
		{key: "u", in: -1.0, wantErr: "out of range"},
		{key: "u", in: "-1", wantErr: "cannot convert '-1' to uint for field 'u'"},
		{key: "u", in: 1.5, wantErr: "not a whole number"},
		{key: "u", in: float64(1 << 64), wantErr: "out of range"},
		{key: "u8", in: 255, want: uint8(255)},
		{key: "u8", in: 256, wantErr: "cannot convert 256 to uint8 for field 'u8': out of range"},
		{key: "i8", in: -128, want: int8(-128)},
		{key: "i8", in: 128, wantErr: "out of range"},
		{key: "i8", in: -129.0, wantErr: "out of range"},
		{key: "i16", in: "32767", want: int16(32767)},
		{key: "i16", in: "32768", wantErr: "cannot convert '32768' to int16"},
		{key: "i32", in: int64(1 << 40), wantErr: "cannot convert 1099511627776 to int32 for field 'i32': out of range"},
		{key: "i32", in: float64(math.MinInt32), want: int32(math.MinInt32)},
		{key: "i32", in: float64(1 << 31), wantErr: "out of range"},
		{key: "i64", in: uint64(math.MaxInt64), want: int64(math.MaxInt64)},
		{key: "i64", in: uint64(1 << 63), wantErr: "out of range"},
		{key: "i64", in: float64(1 << 63), wantErr: "out of range"},
		{key: "i64", in: float64(-1 << 63), want: int64(math.MinInt64)},
		{key: "f32", in: 1.5, want: float32(1.5)},
		{key: "f32", in: int64(3), want: float32(3)},
		{key: "f32", in: "2.25", want: float32(2.25)},
		{key: "f32", in: 1e39, wantErr: "cannot convert 1e+39 to float32 for field 'f32': out of range"},
		{key: "f32", in: math.Inf(1), want: float32(math.Inf(1))},
		{key: "f64", in: 1e300, want: 1e300},
		{key: "f64", in: uint64(math.MaxUint64), want: float64(math.MaxUint64)},
		{key: "f64", in: "x", wantErr: "cannot convert 'x' to float64 for field 'f64'"},
		{key: "f64", in: true, wantErr: "cannot convert bool to float64 for field 'f64'"},
		// A time.Duration field takes a time.Duration only: a plain
		// conversion made 30 thirty nanoseconds.
		{key: "d", in: 30 * time.Second, want: 30 * time.Second},
		{key: "d", in: int64(30), wantErr: "cannot convert int64 to time.Duration for field 'd': give a time.Duration"},
		{key: "d", in: 1.5, wantErr: "give a time.Duration"},
		{key: "d", in: "30s", wantErr: "give a time.Duration"},
	}
	for _, tt := range tests {
		var got dest
		err := MapToStructByTags(map[string]any{tt.key: tt.in}, &got)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s = %v (%T): error = %v, want %q", tt.key, tt.in, tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s = %v (%T): error = %v", tt.key, tt.in, tt.in, err)
			continue
		}
		field := reflect.ValueOf(got).FieldByNameFunc(func(name string) bool {
			f, _ := reflect.TypeOf(got).FieldByName(name)
			return f.Tag.Get("map") == tt.key
		})
		if v := field.Interface(); v != tt.want {
			t.Errorf("%s = %v (%T): got %v (%T), want %v (%T)", tt.key, tt.in, tt.in, v, v, tt.want, tt.want)
		}
	}
}
