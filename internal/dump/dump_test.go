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
			data: map[string]any{"headers": map[string]any{"b": "2", "a": "1"}, "x": map[string]string{"k": "v"}},
			want: "headers:\n  a: 1\n  b: 2\nx:\n  k: v",
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
			name: "a map of empty values is empty",
			data: map[string]any{"env": map[string]any{"A": ""}},
			opts: Options{OmitEmpty: true},
			want: "env: {}",
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
			name: "one line with a newline at its end",
			data: map[string]any{"stderr": "oops\n"},
			want: "stderr: oops",
		},
		{
			name: "nothing but a newline",
			data: map[string]any{"out": "\n"},
			want: `out: \n`,
		},
		{
			name: "control characters are escaped",
			data: map[string]any{"s": "a\x1b[31mb\tc"},
			want: "s: a\\x1b[31mb\tc",
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
			want: "body: aaaaaaaa… (12 more bytes)",
		},
		{
			name: "a line is not cut in a character",
			data: map[string]any{"body": "ああ"},
			opts: Options{MaxBytes: 4},
			want: "body: あ… (3 more bytes)",
		},
		{
			name: "a block is cut at its lines",
			data: map[string]any{"body": "1\n2\n3\n4\n"},
			opts: Options{MaxLines: 2},
			want: "body: |\n  1\n  2\n  … (4 more bytes)",
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
			want: "body: |\n  line1\n  … (5 more bytes)",
		},
		{
			name: "a long first line of several is cut",
			data: map[string]any{"body": "abcdefgh\nline2"},
			opts: Options{MaxBytes: 4},
			want: "body: abcd… (10 more bytes)",
		},
		{
			name: "lists and maps are cut",
			data: map[string]any{"list": []any{1, 2, 3}, "map": map[string]any{"a": 1, "b": "", "c": 3, "d": 4}},
			opts: Options{MaxItems: 2, OmitEmpty: true},
			want: "list:\n  - 1\n  - 2\n  … (1 more items)\nmap:\n  a: 1\n  c: 3\n  … (1 more keys)",
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
