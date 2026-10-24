package dump

import (
	"strings"
	"testing"
)

func TestLines(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		opts Options
		want string
	}{
		{
			name: "keys are sorted",
			data: map[string]any{"url": "https://example.com/", "method": "GET", "code": 200},
			want: "code: 200\nmethod: GET\nurl: https://example.com/",
		},
		{
			name: "nested maps are indented",
			data: map[string]any{"headers": map[string]any{"b": "y", "a": "x"}, "x": map[string]string{"k": "v"}},
			want: "headers:\n  a: x\n  b: y\nx:\n  k: v",
		},
		{
			name: "lists, and maps in them",
			data: map[string]any{
				"rows":  []any{map[string]any{"id": 1, "name": "a"}, map[string]any{"id": 2}},
				"tags":  []string{"x", "y"},
				"grid":  []any{[]any{1, 2}},
				"empty": []any{},
			},
			want: "empty: []\ngrid:\n  -\n    - 1\n    - 2\nrows:\n  - id: 1\n    name: a\n  - id: 2\ntags:\n  - x\n  - y",
		},
		{
			name: "empty values are shown",
			data: map[string]any{"body": "", "cookies": map[string]any{}, "none": nil},
			want: "body: \"\"\ncookies: {}\nnone: null",
		},
		{
			name: "empty values are left out, zeros and falses are not",
			data: map[string]any{"body": "", "cookies": map[string]any{}, "none": nil, "code": 0, "ok": false, "list": []any{}},
			opts: Options{OmitEmpty: true},
			want: "code: 0\nok: false",
		},
		{
			name: "empty values nested in the map are kept",
			data: map[string]any{
				"body": map[string]any{"id": "1", "name": "", "items": []any{}, "err": nil},
				"rows": []any{map[string]any{"note": "", "deleted_at": nil}},
				"env":  map[string]any{},
			},
			opts: Options{OmitEmpty: true},
			want: "body:\n  err: null\n  id: \"1\"\n  items: []\n  name: \"\"\nrows:\n  - deleted_at: null\n    note: \"\"",
		},
		{
			name: "strings that read as other values are quoted",
			data: map[string]any{
				"num": "1", "float": "1.5", "bool": "true", "null": "null", "empty": "",
				"space": " x", "map": "{}", "quote": `"a"`, "plain": "abc", "int": 1, "yes": true,
			},
			want: "bool: \"true\"\nempty: \"\"\nfloat: \"1.5\"\nint: 1\nmap: \"{}\"\nnull: \"null\"\nnum: \"1\"\nplain: abc\nquote: \"\\\"a\\\"\"\nspace: \" x\"\nyes: true",
		},
		{
			name: "keys with control characters are quoted",
			data: map[string]any{"a\x1b[2Jb": 1, "c\nd": 2, "": 3},
			want: "\"\": 3\n\"a\\x1b[2Jb\": 1\n\"c\\nd\": 2",
		},
		{
			name: "numeric keys are sorted as numbers",
			data: map[string]any{"m": map[int]string{10: "x", 2: "y", 1: "z"}},
			want: "m:\n  1: z\n  2: y\n  10: x",
		},
		{
			name: "floats far from 1 have an exponent",
			data: map[string]any{"big": 1e300, "small": 1e-9, "zero": float64(0)},
			want: "big: 1e+300\nsmall: 1e-09\nzero: 0",
		},
		{
			name: "long scalars are cut",
			data: map[string]any{"n": int64(1234567890)},
			opts: Options{MaxBytes: 4},
			want: "n: 1234 … (6 more bytes)",
		},
		{
			name: "numbers decoded from JSON are written without an exponent",
			data: map[string]any{"big": float64(12345678), "frac": 1.5},
			want: "big: 12345678\nfrac: 1.5",
		},
		{
			name: "several lines are a block",
			data: map[string]any{"stdout": "line1\r\n\nline3\n"},
			want: "stdout: |\n  line1\n\n  line3",
		},
		{
			name: "a block without a newline at its end",
			data: map[string]any{"stdout": "line1\nline2"},
			want: "stdout: |-\n  line1\n  line2",
		},
		{
			name: "one line with a newline at its end",
			data: map[string]any{"stderr": "oops\n"},
			want: `stderr: "oops\n"`,
		},
		{
			name: "nothing but a newline",
			data: map[string]any{"out": "\n"},
			want: `out: "\n"`,
		},
		{
			name: "control characters are escaped",
			data: map[string]any{"s": "a\x1b[31mb\tc"},
			want: `s: "a\x1b[31mb\tc"`,
		},
		{
			name: "control characters in a block are escaped",
			data: map[string]any{"s": "a\x1b[31m\nb\n"},
			want: "s: |\n  a\\x1b[31m\n  b",
		},
		{
			name: "binary",
			data: map[string]any{"bytes": []byte{0, 1, 2}, "text": "\xff\xfe"},
			want: "bytes: (binary, 3 bytes)\ntext: (binary, 2 bytes)",
		},
		{
			name: "a long line is cut",
			data: map[string]any{"body": strings.Repeat("a", 20)},
			opts: Options{MaxBytes: 8},
			want: "body: aaaaaaaa … (12 more bytes)",
		},
		{
			name: "a line is not cut in a character",
			data: map[string]any{"body": "ああ"},
			opts: Options{MaxBytes: 4},
			want: "body: あ … (3 more bytes)",
		},
		{
			name: "a block is cut at its lines",
			data: map[string]any{"body": "1\n2\n3\n4\n"},
			opts: Options{MaxLines: 2},
			want: "body: |\n  1\n  2\n  … (2 more lines)",
		},
		{
			name: "a block is cut at its bytes",
			data: map[string]any{"body": "line1\nline2\n"},
			opts: Options{MaxBytes: 8},
			want: "body: |\n  line1\n  li\n  … (4 more bytes)",
		},
		{
			name: "one line of several is cut",
			data: map[string]any{"body": "line1\nline2"},
			opts: Options{MaxLines: 1},
			want: "body: |-\n  line1\n  … (1 more line)",
		},
		{
			name: "a long first line of several is cut",
			data: map[string]any{"body": "abcdefgh\nline2"},
			opts: Options{MaxBytes: 4},
			want: "body: abcd … (10 more bytes)",
		},
		{
			name: "lists and maps are cut",
			data: map[string]any{"list": []any{1, 2, 3}, "map": map[string]any{"a": 1, "b": "", "c": 3, "d": 4}},
			opts: Options{MaxItems: 2, OmitEmpty: true},
			want: "list:\n  - 1\n  - 2\n  … (1 more item)\nmap:\n  a: 1\n  b: \"\"\n  … (2 more keys)",
		},
		{
			name: "nil",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(Lines(tt.data, tt.opts), "\n")
			if got != tt.want {
				t.Errorf("Lines()\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestLinesEndInNoWhitespace(t *testing.T) {
	data := map[string]any{"a": map[string]any{"b": []any{"x\n\n\ny"}}}
	for _, l := range Lines(data, Options{}) {
		if strings.TrimRight(l, " \t") != l {
			t.Errorf("line %q ends in whitespace", l)
		}
	}
}
