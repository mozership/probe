package expr

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mozership/probe/internal/truncate"
)

func TestEvalTemplateMap(t *testing.T) {
	exprs := map[string]any{
		"url":           "{{env.URL}}",
		"authorization": "Bearer {{env.TOKEN}}",
	}
	env := map[string]any{
		"env": map[string]any{
			"URL":   "https://example.com",
			"TOKEN": "secrets",
		},
	}
	expected := map[string]any{
		"url":           "https://example.com",
		"authorization": "Bearer secrets",
	}
	expr := &Expr{}
	actual, err := expr.EvalTemplateMap(exprs, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("map are not equal: expected %+v, got %+v", expected, actual)
	}
}

func TestEvalTemplate(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		env      map[string]any
		expected string
	}{
		{
			name: "only variable",
			str:  "{{env.URL}}",
			env: map[string]any{
				"env": map[string]any{
					"URL": "https://example.com",
				},
			},
			expected: "https://example.com",
		},
		{
			name: "expr twice",
			str:  "Hi, {{ name }}. My name is {{ service }}.",
			env: map[string]any{
				"name":    "Bob",
				"service": "Alice",
			},
			expected: "Hi, Bob. My name is Alice.",
		},
		{
			name: "use nil coalescing operator",
			str:  "{{env.URL ?? 'http://localhost'}}",
			env: map[string]any{
				"env": map[string]any{},
			},
			expected: "http://localhost",
		},
		{
			name: "use ternary operator",
			str:  "{{env.URL == 'localhost' ? 'http://localhost:3000' : env.URL}}",
			env: map[string]any{
				"env": map[string]any{
					"URL": "localhost",
				},
			},
			expected: "http://localhost:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &Expr{}
			actual, err := expr.EvalTemplate(tt.str, tt.env)
			if err != nil {
				t.Errorf("EvalTemplate error %s", err)
			}
			if tt.expected != actual {
				t.Errorf("expected %+v, got %+v", tt.expected, actual)
			}
		})
	}
}

func TestEval(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		env      map[string]any
		expected any
		hasError bool
	}{
		{
			name:     "simple boolean expression",
			input:    "true && false",
			env:      map[string]any{},
			expected: false,
			hasError: false,
		},
		{
			name:  "variable access",
			input: "name == 'test'",
			env: map[string]any{
				"name": "test",
			},
			expected: true,
			hasError: false,
		},
		{
			name:  "numeric comparison",
			input: "status >= 200 && status < 300",
			env: map[string]any{
				"status": 200,
			},
			expected: true,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &Expr{}
			result, err := expr.Eval(tt.input, tt.env)

			if tt.hasError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.hasError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.hasError && result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSecurityValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "expression too long",
			input:       strings.Repeat("a", 1000001),
			shouldError: true,
			errorMsg:    "expression exceeds maximum length",
		},
		{
			// There is no `env` namespace in the evaluation context, so this
			// fails on the missing name rather than on a pattern in the text.
			name:        "env namespace does not exist",
			input:       "env.SECRET_KEY",
			shouldError: true,
			errorMsg:    "cannot fetch SECRET_KEY",
		},
		{
			name:        "safe expression",
			input:       "status == 200",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &Expr{}
			_, err := expr.Eval(tt.input, map[string]any{"status": 200})

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestFunctions evaluates the functions probe adds to expr. An input that
// holds a template is evaluated as one, and any other as an expression.
func TestFunctions(t *testing.T) {
	over := strings.Repeat("a", maxStringLength+1)
	pair := func(a, b any) map[string]any { return map[string]any{"src": a, "target": b} }
	tests := []struct {
		name    string
		input   string
		env     any
		want    any
		wantErr string
		// notErr, when it is set, is text the error must not hold.
		notErr string
		// check, when it is set, checks a value that cannot be known in
		// advance, such as a random one, in place of want.
		check func(got any) error
	}{
		// match_json
		{name: "match_json of equal objects", input: "match_json(src, target)", env: pair(map[string]any{"name": "test", "age": 25}, map[string]any{"name": "test", "age": 25}), want: true},
		{name: "match_json of different objects", input: "match_json(src, target)", env: pair(map[string]any{"name": "test"}, map[string]any{"name": "other"}), want: false},
		{name: "match_json with an invalid regex", input: `match_json(src, {"x": "/[/"})`, env: map[string]any{"src": map[string]any{"x": "abc"}}, wantErr: "match_json: invalid regex at key 'x'", notErr: "panicked"},
		{name: "match_json with one parameter", input: "match_json(src)", env: map[string]any{"src": map[string]any{"name": "test"}}, wantErr: "match_json requires exactly 2 parameters"},
		{name: "match_json of a number", input: "match_json(1, {})", wantErr: "match_json parameters must be objects"},
		{name: "match_json of a string target", input: "match_json({}, 'a')", wantErr: "match_json parameters must be objects"},

		// diff_json
		{name: "diff_json of different objects", input: "diff_json(src, target)", env: pair(map[string]any{"name": "test", "age": 25}, map[string]any{"name": "test", "age": 30}), check: func(got any) error {
			if s, ok := got.(string); !ok || s == "" || s == "No diff" {
				return fmt.Errorf("want a diff, got %#v", got)
			}
			return nil
		}},
		{name: "diff_json of equal objects", input: "diff_json({'a': 1}, {'a': 1})", want: "No diff"},
		{name: "diff_json with an invalid regex", input: `diff_json(src, {"x": "/[/"})`, env: map[string]any{"src": map[string]any{"x": "abc"}}, wantErr: "diff_json: invalid regex at key 'x'", notErr: "panicked"},
		{name: "diff_json with one parameter", input: "diff_json({})", wantErr: "diff_json requires exactly 2 parameters"},
		{name: "diff_json of a number", input: "diff_json({}, 1)", wantErr: "diff_json parameters must be objects"},

		// random_int
		{name: "random_int with a positive integer", input: "random_int(100)", check: intBelow(100)},
		{name: "random_int with a float", input: "random_int(100.0)", check: intBelow(100)},
		{name: "random_int of 1", input: "random_int(1)", want: 0},
		{name: "random_int of 2", input: "random_int(2)", check: intBelow(2)},
		{name: "random_int with zero", input: "random_int(0)", wantErr: "random_int parameter must be positive"},
		{name: "random_int with a negative", input: "random_int(-1)", wantErr: "random_int parameter must be positive"},
		{name: "random_int with a string", input: "random_int('100')", wantErr: "random_int parameter must be an integer"},
		{name: "random_int with no parameters", input: "random_int()", wantErr: "random_int requires exactly 1 parameter"},
		{name: "random_int with two parameters", input: "random_int(100, 200)", wantErr: "random_int requires exactly 1 parameter"},

		// random_str
		{name: "random_str with a positive length", input: "random_str(10)", check: randomStr(10)},
		{name: "random_str with a float", input: "random_str(5.0)", check: randomStr(5)},
		{name: "random_str with a large length", input: "random_str(10000)", check: randomStr(10000)},
		{name: "random_str of 1", input: "random_str(1)", check: randomStr(1)},
		{name: "random_str with zero", input: "random_str(0)", wantErr: "random_str parameter must be positive"},
		{name: "random_str with a negative", input: "random_str(-1)", wantErr: "random_str parameter must be positive"},
		{name: "random_str too long", input: "random_str(1000001)", wantErr: "random_str parameter must be <= 1000000"},
		{name: "random_str with a string", input: "random_str('10')", wantErr: "random_str parameter must be an integer"},
		{name: "random_str with no parameters", input: "random_str()", wantErr: "random_str requires exactly 1 parameter"},
		{name: "random_str with two parameters", input: "random_str(10, 20)", wantErr: "random_str requires exactly 1 parameter"},

		// unixtime
		{name: "unixtime", input: "unixtime()", check: func(got any) error {
			now := time.Now().Unix()
			if v, ok := got.(int64); !ok || v > now || v < now-5 {
				return fmt.Errorf("want the time now, %d, got %#v", now, got)
			}
			return nil
		}},
		{name: "unixtime called in succession", input: "[unixtime(), unixtime(), unixtime()]", check: func(got any) error {
			times, ok := got.([]any)
			if !ok || len(times) != 3 {
				return fmt.Errorf("want three times, got %#v", got)
			}
			for i := 1; i < len(times); i++ {
				if d := times[i].(int64) - times[i-1].(int64); d < 0 || d > 1 {
					return fmt.Errorf("want each within a second of the one before, got %v", times)
				}
			}
			return nil
		}},
		{name: "unixtime with a number", input: "unixtime(123)", wantErr: "unixtime takes no parameters"},
		{name: "unixtime with a string", input: "unixtime('now')", wantErr: "unixtime takes no parameters"},

		// parse_float
		{name: "parse_float of a decimal", input: "parse_float('1.5')", want: 1.5},
		{name: "parse_float of an exponent", input: "parse_float(s)", env: map[string]any{"s": "-2e3"}, want: -2000.0},
		{name: "parse_float of text", input: "parse_float('abc')", wantErr: `parse_float error: strconv.ParseFloat: parsing "abc": invalid syntax`},
		{name: "parse_float of a number", input: "parse_float(1)", wantErr: "parse_float parameter must be a string"},
		{name: "parse_float with no parameters", input: "parse_float()", wantErr: "parse_float requires exactly 1 parameter"},
		{name: "parse_float with two parameters", input: "parse_float('1', '2')", wantErr: "parse_float requires exactly 1 parameter"},

		// parse_int
		{name: "parse_int of an integer string", input: "parse_int(num_str)", env: map[string]any{"num_str": "123"}, want: int64(123)},
		{name: "parse_int of a negative integer", input: "parse_int(num_str)", env: map[string]any{"num_str": "-456"}, want: int64(-456)},
		{name: "parse_int of text", input: "parse_int(num_str)", env: map[string]any{"num_str": "not_a_number"}, wantErr: "parse_int error"},
		{name: "parse_int of an int", input: "parse_int(num)", env: map[string]any{"num": 123}, want: int64(123)},
		{name: "parse_int of an int64", input: "parse_int(num)", env: map[string]any{"num": int64(-7)}, want: int64(-7)},
		{name: "parse_int of a uint64", input: "parse_int(num)", env: map[string]any{"num": uint64(8)}, want: int64(8)},
		{name: "parse_int of a float", input: "parse_int(num)", env: map[string]any{"num": 456.0}, want: int64(456)},
		{name: "parse_int of a list", input: "parse_int(arr)", env: map[string]any{"arr": []int{1, 2, 3}}, wantErr: "parse_int parameter must be a string or number, got []int"},
		{name: "parse_int with no parameters", input: "parse_int()", wantErr: "parse_int requires exactly 1 parameter"},

		// encode_base64
		{name: "encode_base64 of a string", input: "encode_base64('hello')", want: "aGVsbG8="},
		{name: "encode_base64 of an empty string", input: "encode_base64('')", want: ""},
		{name: "encode_base64 of punctuation", input: "encode_base64('Hello, World!')", want: "SGVsbG8sIFdvcmxkIQ=="},
		{name: "encode_base64 with no parameters", input: "encode_base64()", wantErr: "encode_base64 requires exactly 1 parameter"},
		{name: "encode_base64 with two parameters", input: "encode_base64('hello', 'world')", wantErr: "encode_base64 requires exactly 1 parameter"},
		{name: "encode_base64 of a number", input: "encode_base64(123)", wantErr: "encode_base64 parameter must be a string"},
		{name: "encode_base64 over the length limit", input: "encode_base64(s)", env: map[string]any{"s": over}, wantErr: "encode_base64 parameter exceeds maximum length (1000000 chars)"},

		// decode_base64
		{name: "decode_base64 of a string", input: "decode_base64('aGVsbG8=')", want: "hello"},
		{name: "decode_base64 of an empty string", input: "decode_base64('')", want: ""},
		{name: "decode_base64 of punctuation", input: "decode_base64('SGVsbG8sIFdvcmxkIQ==')", want: "Hello, World!"},
		{name: "decode_base64 of what is not base64", input: "decode_base64('invalid!')", wantErr: "decode_base64 error"},
		{name: "decode_base64 with no parameters", input: "decode_base64()", wantErr: "decode_base64 requires exactly 1 parameter"},
		{name: "decode_base64 with two parameters", input: "decode_base64('aGVsbG8=', 'extra')", wantErr: "decode_base64 requires exactly 1 parameter"},
		{name: "decode_base64 of a number", input: "decode_base64(123)", wantErr: "decode_base64 parameter must be a string"},
		{name: "decode_base64 over the length limit", input: "decode_base64(s)", env: map[string]any{"s": over}, wantErr: "decode_base64 parameter exceeds maximum length (1000000 chars)"},

		// encode_base64 and decode_base64 round trip
		{name: "base64 round trip of words", input: "decode_base64(encode_base64('hello world'))", want: "hello world"},
		{name: "base64 round trip of punctuation", input: "decode_base64(encode_base64('Hello, World!'))", want: "Hello, World!"},
		{name: "base64 round trip of credentials", input: "decode_base64(encode_base64('user:password'))", want: "user:password"},
		{name: "base64 round trip of symbols", input: "decode_base64(encode_base64('Special chars: !@#$%^&*()'))", want: "Special chars: !@#$%^&*()"},
		{name: "base64 round trip of Unicode", input: "decode_base64(encode_base64('Unicode: こんにちは'))", want: "Unicode: こんにちは"},
		{name: "base64 round trip of an empty string", input: "decode_base64(encode_base64(''))", want: ""},

		// parse_json
		{name: "parse_json and a field", input: "parse_json(data).name", env: map[string]any{"data": `{"name": "test", "id": 123}`}, want: "test"},
		{name: "parse_json and a nested field", input: "parse_json(data).data.id", env: map[string]any{"data": `{"data": {"id": 456}}`}, want: float64(456)},
		{name: "parse_json with match_json", input: "match_json(parse_json(data), expected)", env: map[string]any{"data": `{"name": "test"}`, "expected": map[string]any{"name": "test"}}, want: true},
		{name: "parse_json of what is not JSON", input: "parse_json(data)", env: map[string]any{"data": "not json"}, wantErr: "parse_json"},
		{name: "parse_json of an empty string", input: "parse_json('')", wantErr: "parse_json: empty string"},
		{name: "parse_json of a number", input: "parse_json(data)", env: map[string]any{"data": 123}, wantErr: "parse_json parameter must be a string"},
		{name: "parse_json with no parameters", input: "parse_json()", wantErr: "parse_json requires exactly 1 parameter"},
		{name: "parse_json over the length limit", input: "parse_json(s)", env: map[string]any{"s": over}, wantErr: "parse_json parameter exceeds maximum length (1000000 chars)"},

		// template
		{name: "template with no parameters", input: "template()", wantErr: "template requires exactly 1 parameter"},
		{name: "template with two parameters", input: "template('a', 'b')", wantErr: "template requires exactly 1 parameter"},
		{name: "template over the length limit", input: "template(s)", env: map[string]any{"s": over}, wantErr: "template parameter exceeds maximum length (1000000 chars)"},

		// In templates
		{name: "random_int in a template", input: "User ID: {{random_int(9999)}}", check: expanded("User ID: ")},
		{name: "random_str in a template", input: "Session: {{random_str(16)}}", check: expanded("Session: ")},
		{name: "unixtime in a template", input: "Timestamp: {{unixtime()}}", check: expanded("Timestamp: ")},
		{name: "functions in a template", input: "ID: {{random_int(1000)}}, Token: {{random_str(8)}}, Time: {{unixtime()}}", check: expanded("ID: ")},
		{name: "parse_json in a template", input: "ID: {{parse_json(data).id}}", env: map[string]any{"data": `{"id": 42, "name": "test"}`}, want: "ID: 42"},
		{name: "encode_base64 in a template", input: "Authorization: Basic {{encode_base64('user:pass')}}", want: "Authorization: Basic dXNlcjpwYXNz"},
		{name: "decode_base64 in a template", input: "Decoded: {{decode_base64('aGVsbG8=')}}", want: "Decoded: hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.env
			if env == nil {
				env = map[string]any{}
			}
			e := &Expr{}
			var got any
			var err error
			if HasTemplate(tt.input) {
				got, err = e.EvalTemplate(tt.input, env)
			} else {
				got, err = e.Eval(tt.input, env)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %#v, %v, want an error containing %q", got, err, tt.wantErr)
				}
				if tt.notErr != "" && strings.Contains(err.Error(), tt.notErr) {
					t.Errorf("error = %v, want one without %q", err, tt.notErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				if err := tt.check(got); err != nil {
					t.Error(err)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// intBelow checks that a value is an int from 0 up to n.
func intBelow(n int) func(any) error {
	return func(got any) error {
		if v, ok := got.(int); !ok || v < 0 || v >= n {
			return fmt.Errorf("want an int from 0 up to %d, got %#v", n, got)
		}
		return nil
	}
}

// randomStr checks that a value is a string of n letters and digits.
func randomStr(n int) func(any) error {
	return func(got any) error {
		s, ok := got.(string)
		if !ok || len(s) != n || !regexp.MustCompile("^[a-zA-Z0-9]+$").MatchString(s) {
			return fmt.Errorf("want %d letters and digits, got %#v", n, got)
		}
		return nil
	}
}

// expanded checks that a value is text that starts with prefix and has no
// braces left of a template.
func expanded(prefix string) func(any) error {
	return func(got any) error {
		s, ok := got.(string)
		if !ok || !strings.HasPrefix(s, prefix) || strings.ContainsAny(s, "{}") {
			return fmt.Errorf("want the templates after %q evaluated, got %#v", prefix, got)
		}
		return nil
	}
}

func TestWholeStringTemplateTypePreservation(t *testing.T) {
	expr := &Expr{}

	t.Run("integer type preservation", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("{{ vars.count }}", map[string]any{
			"vars": map[string]any{"count": 123},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != 123 {
			t.Errorf("expected 123, got %v (type: %T)", result, result)
		}
	})

	t.Run("float64 type preservation", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("{{ vars.price }}", map[string]any{
			"vars": map[string]any{"price": 19.99},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != 19.99 {
			t.Errorf("expected 19.99, got %v (type: %T)", result, result)
		}
	})

	t.Run("boolean type preservation", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("{{ vars.enabled }}", map[string]any{
			"vars": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != true {
			t.Errorf("expected true, got %v (type: %T)", result, result)
		}
	})

	t.Run("string type preservation", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("{{ vars.message }}", map[string]any{
			"vars": map[string]any{"message": "hello"},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "hello" {
			t.Errorf("expected \"hello\", got %v (type: %T)", result, result)
		}
	})

	t.Run("partial template remains string", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("Count: {{ vars.count }}", map[string]any{
			"vars": map[string]any{"count": 123},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		expected := "Count: 123"
		if result != expected {
			t.Errorf("expected %q, got %v (type: %T)", expected, result, result)
		}
	})

	t.Run("multiple templates remain string", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("{{ vars.name }} is {{ vars.age }}", map[string]any{
			"vars": map[string]any{"name": "John", "age": 30},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		expected := "John is 30"
		if result != expected {
			t.Errorf("expected %q, got %v (type: %T)", expected, result, result)
		}
	})

	t.Run("whitespace handling", func(t *testing.T) {
		result, err := expr.EvalTemplateWithTypePreservation("  {{ vars.number }}  ", map[string]any{
			"vars": map[string]any{"number": 42},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != 42 {
			t.Errorf("expected 42, got %v (type: %T)", result, result)
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("isWholeStringTemplate", func(t *testing.T) {
		tests := []struct {
			input    string
			expected bool
		}{
			{"{{ vars.count }}", true},
			{"  {{ vars.count }}  ", true},
			{"Count: {{ vars.count }}", false},
			{"{{ vars.a }} and {{ vars.b }}", false},
			{"{{ vars.count }} extra", false},
			{"prefix {{ vars.count }}", false},
			{"{vars.count}", false},
			{"vars.count", false},
			{"", false},
		}

		for _, tt := range tests {
			result := isWholeStringTemplate(tt.input)
			if result != tt.expected {
				t.Errorf("isWholeStringTemplate(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		}
	})

	t.Run("extractTemplateExpression", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"{{ vars.count }}", "vars.count"},
			{"  {{ vars.count }}  ", "vars.count"},
			{"{{ vars.price * 1.1 }}", "vars.price * 1.1"},
			{"{vars.count}", ""},
			{"vars.count", ""},
			{"", ""},
		}

		for _, tt := range tests {
			result := extractTemplateExpression(tt.input)
			if result != tt.expected {
				t.Errorf("extractTemplateExpression(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	})
}

func TestEvalTemplateMapTypePreservation(t *testing.T) {
	expr := &Expr{}

	input := map[string]any{
		"number":    "{{ vars.count }}",
		"price":     "{{ vars.price }}",
		"enabled":   "{{ vars.enabled }}",
		"message":   "{{ vars.message }}",
		"partial":   "Count: {{ vars.count }}",
		"multiple":  "{{ vars.name }} is {{ vars.age }}",
		"unchanged": "static value",
		"nested": map[string]any{
			"inner_number": "{{ vars.inner }}",
		},
	}

	env := map[string]any{
		"vars": map[string]any{
			"count":   123,
			"price":   19.99,
			"enabled": true,
			"message": "hello",
			"name":    "John",
			"age":     30,
			"inner":   456,
		},
	}

	result, err := expr.EvalTemplateMap(input, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check type preservation
	if result["number"] != 123 {
		t.Errorf("number: expected 123 (int), got %v (%T)", result["number"], result["number"])
	}
	if result["price"] != 19.99 {
		t.Errorf("price: expected 19.99 (float64), got %v (%T)", result["price"], result["price"])
	}
	if result["enabled"] != true {
		t.Errorf("enabled: expected true (bool), got %v (%T)", result["enabled"], result["enabled"])
	}
	if result["message"] != "hello" {
		t.Errorf("message: expected \"hello\" (string), got %v (%T)", result["message"], result["message"])
	}

	// Check string templates remain string
	if result["partial"] != "Count: 123" {
		t.Errorf("partial: expected \"Count: 123\", got %v", result["partial"])
	}
	if result["multiple"] != "John is 30" {
		t.Errorf("multiple: expected \"John is 30\", got %v", result["multiple"])
	}

	// Check unchanged values
	if result["unchanged"] != "static value" {
		t.Errorf("unchanged: expected \"static value\", got %v", result["unchanged"])
	}

	// Check nested maps
	nested, ok := result["nested"].(map[string]any)
	if !ok {
		t.Errorf("nested: expected map[string]any, got %T", result["nested"])
	} else if nested["inner_number"] != 456 {
		t.Errorf("nested.inner_number: expected 456 (int), got %v (%T)", nested["inner_number"], nested["inner_number"])
	}
}

// TestExpressionContextIsNotFiltered pins the contract that replaced the
// environment blocklist: every name the caller puts in the environment reaches
// the expression. probe's own examples read credentials that way
// ("{{TOKEN}}", "{{SSH_PASS}}"), so a name-based filter here would break the
// documented way of getting a secret into `vars`.
func TestExpressionContextIsNotFiltered(t *testing.T) {
	expr := &Expr{}
	env := map[string]any{
		"API_TOKEN":    "tok",
		"DB_PASSWORD":  "pw",
		"SSH_KEY_FILE": "/home/probe/id_rsa",
		"BASE_URL":     "http://example.com",
	}

	for key, want := range map[string]string{
		"API_TOKEN":    "tok",
		"DB_PASSWORD":  "pw",
		"SSH_KEY_FILE": "/home/probe/id_rsa",
		"BASE_URL":     "http://example.com",
	} {
		t.Run(key, func(t *testing.T) {
			got, err := expr.Eval(key, env)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got != want {
				t.Errorf("expected %q, got %v", want, got)
			}
		})
	}
}

// TestCheckLengthAllowsEnvLikeText pins the fix for a guard that used to
// reject, with the length check, any expression whose text contained
// "env.path", "env.secret" and friends. The evaluation context has no `env`
// namespace, so the guard only ever rejected ordinary strings that happened
// to contain the pattern.
func TestCheckLengthAllowsEnvLikeText(t *testing.T) {
	expr := &Expr{}

	inputs := []string{
		`"https://env.pathfinder.example.com"`,
		`"/opt/env.home/bin"`,
		`"dev.env.secret-manager.internal"`,
		`"env.password-rotation.example.com"`,
	}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			got, err := expr.Eval(in, map[string]any{})
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			want := strings.Trim(in, `"`)
			if got != want {
				t.Errorf("expected %q, got %v", want, got)
			}
		})
	}
}

func TestCheckLength(t *testing.T) {
	if err := checkLength(strings.Repeat("a", maxExpressionLength)); err != nil {
		t.Errorf("unexpected error for an expression of the length limit: %s", err)
	}
	if err := checkLength(strings.Repeat("a", maxExpressionLength+1)); err == nil {
		t.Error("expected an error for an expression over the length limit")
	}
	if err := checkLength(strings.Repeat("a", 10)); err != nil {
		t.Errorf("unexpected error for a short expression: %s", err)
	}
}

// TestPredicateBuiltinsAreAvailable pins that expr's predicate builtins work.
// probe used to call ex.DisableBuiltin on six of them, which expr's parser
// ignores, so they have always been reachable and the docs describe them.
func TestPredicateBuiltinsAreAvailable(t *testing.T) {
	expr := &Expr{}
	env := map[string]any{"arr": []any{1, 2, 3}}

	tests := map[string]any{
		`all(arr, # > 0)`:      true,
		`any(arr, # > 2)`:      true,
		`one(arr, # == 2)`:     true,
		`count(arr, # > 1)`:    2,
		`filter(arr, # > 1)`:   []any{2, 3},
		`len(map(arr, # * 2))`: 3,
	}

	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			got, err := expr.Eval(in, env)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("expected %v, got %v", want, got)
			}
		})
	}
}

func TestErrorHandling(t *testing.T) {
	t.Run("handles compilation errors", func(t *testing.T) {
		expr := &Expr{}
		result, err := expr.Eval("invalid syntax $$", map[string]any{})

		if err == nil {
			t.Errorf("expected compilation error")
		}
		if result != nil {
			t.Errorf("expected no value with the error, got %#v", result)
		}
	})

	t.Run("an expression over the length limit has no value", func(t *testing.T) {
		expr := &Expr{}
		result, err := expr.Eval(strings.Repeat("1", maxExpressionLength+1), map[string]any{})

		if err == nil {
			t.Errorf("expected an error for an expression over the length limit")
		}
		if result != nil {
			t.Errorf("expected no value with the error, got %#v", result)
		}
	})

	t.Run("handles template evaluation errors", func(t *testing.T) {
		expr := &Expr{}
		result, err := expr.EvalTemplate("a {{invalid syntax $$}} b", map[string]any{})

		var tErr *TemplateError
		if !errors.As(err, &tErr) || tErr.Template != "{{invalid syntax $$}}" {
			t.Fatalf("expected a TemplateError naming the template, got %v", err)
		}
		if result != "" {
			t.Errorf("expected no string with the error, got %q", result)
		}
	})

	t.Run("handles template runtime errors", func(t *testing.T) {
		expr := &Expr{}
		env := map[string]any{"outputs": map[string]any{}}
		result, err := expr.EvalTemplate("Bearer {{outputs.login.token}}", env)

		var tErr *TemplateError
		if !errors.As(err, &tErr) || tErr.Template != "{{outputs.login.token}}" {
			t.Fatalf("expected a TemplateError naming the template, got %v", err)
		}
		if !strings.Contains(err.Error(), "{{outputs.login.token}}: ") {
			t.Errorf("expected the error to start with the template, got %q", err.Error())
		}
		if result != "" {
			t.Errorf("expected no string with the error, got %q", result)
		}
	})

	t.Run("an undefined name is not an error", func(t *testing.T) {
		expr := &Expr{}
		result, err := expr.EvalTemplate("{{MISSING ?? 'x'}}-{{vars.none ?? 'y'}}", map[string]any{"vars": map[string]any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "x-y" {
			t.Errorf("expected %q, got %q", "x-y", result)
		}
	})

	t.Run("handles map evaluation with errors", func(t *testing.T) {
		expr := &Expr{}
		inputMap := map[string]any{
			"valid":   "simple string",
			"invalid": "{{invalid syntax $$}}",
		}

		result, err := expr.EvalTemplateMap(inputMap, map[string]any{})

		var fErr *FieldError
		if !errors.As(err, &fErr) || fErr.Path != "invalid" {
			t.Fatalf("expected a FieldError for invalid, got %v", err)
		}
		want := map[string]any{"valid": "simple string", "invalid": nil}
		if !reflect.DeepEqual(result, want) {
			t.Errorf("expected the values that could be evaluated, got %#v", result)
		}
	})

	t.Run("names every value that fails, nested", func(t *testing.T) {
		expr := &Expr{}
		inputMap := map[string]any{
			"url": "{{base.host}}/x",
			"headers": map[string]any{
				"authorization": "Bearer {{login.token}}",
				"accept":        "application/json",
			},
			"items": []any{"ok", map[string]any{"name": "{{1 +}}"}},
		}

		_, err := expr.EvalTemplateMap(inputMap, map[string]any{"base": nil, "login": nil})
		if err == nil {
			t.Fatal("expected an error")
		}
		var paths []string
		for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
			var fErr *FieldError
			if !errors.As(e, &fErr) {
				t.Fatalf("expected a FieldError, got %T: %v", e, e)
			}
			paths = append(paths, fErr.Path)
		}
		want := []string{"headers.authorization", "items[1].name", "url"}
		if !reflect.DeepEqual(paths, want) {
			t.Errorf("paths = %v, want %v", paths, want)
		}
	})
}

// shortenTimeout sets evaluationTimeout to d for the rest of the test. The
// tests that call it do not run in parallel, as evaluationTimeout is shared.
func shortenTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	saved := evaluationTimeout
	evaluationTimeout = d
	t.Cleanup(func() { evaluationTimeout = saved })
}

// blockingEnv returns an environment whose function wait returns only once
// the test is over, so that an expression that calls it outlasts any timeout.
func blockingEnv(t *testing.T) map[string]any {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return map[string]any{
		"wait": func() bool {
			<-release
			return true
		},
	}
}

func TestTimeoutProtection(t *testing.T) {
	t.Run("an expression that finishes in time has its value", func(t *testing.T) {
		got, err := (&Expr{}).Eval("1 + 1", map[string]any{})
		if err != nil || got != 2 {
			t.Errorf("Eval = %#v, %v, want 2", got, err)
		}
	})

	t.Run("an expression that runs too long times out", func(t *testing.T) {
		shortenTimeout(t, 20*time.Millisecond)
		got, err := (&Expr{}).Eval("wait()", blockingEnv(t))
		if err == nil || !strings.Contains(err.Error(), "SECURITY: expression evaluation timed out after 20ms") {
			t.Fatalf("Eval = %#v, %v, want a timeout", got, err)
		}
		if got != nil {
			t.Errorf("expected no value with the timeout, got %#v", got)
		}
	})

	t.Run("a template that runs too long times out", func(t *testing.T) {
		shortenTimeout(t, 20*time.Millisecond)
		got, err := (&Expr{}).EvalTemplate("a {{ wait() }} b", blockingEnv(t))
		var tErr *TemplateError
		if !errors.As(err, &tErr) || tErr.Template != "{{ wait() }}" {
			t.Fatalf("EvalTemplate = %q, %v, want a TemplateError naming the template", got, err)
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("expected a timeout, got %v", err)
		}
		if got != "" {
			t.Errorf("expected no string with the timeout, got %q", got)
		}
	})

	t.Run("a function that panics is an error, which expr recovers", func(t *testing.T) {
		env := map[string]any{"boom": func() bool { panic("boom") }}
		got, err := (&Expr{}).Eval("boom()", env)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("Eval = %#v, %v, want the panic as an error", got, err)
		}
		// The error is expr's, not the one withTimeout makes of a panic
		// that gets past expr.
		if strings.Contains(err.Error(), "expression execution panicked") {
			t.Errorf("Eval error = %v, want the panic recovered by expr", err)
		}
	})
}

func TestWithTimeout(t *testing.T) {
	t.Run("returns what run returns", func(t *testing.T) {
		want := errors.New("failed")
		got, err := withTimeout(func() (any, error) { return 1, want })
		if got != 1 || !errors.Is(err, want) {
			t.Errorf("withTimeout = %#v, %v, want 1, %v", got, err, want)
		}
	})

	t.Run("a panic is an error", func(t *testing.T) {
		got, err := withTimeout(func() (any, error) { panic("lost") })
		if err == nil || err.Error() != "expression execution panicked: lost" {
			t.Errorf("withTimeout = %#v, %v, want the panic as an error", got, err)
		}
		if got != nil {
			t.Errorf("expected no value with the panic, got %#v", got)
		}
	})

	t.Run("a run that outlasts the timeout", func(t *testing.T) {
		shortenTimeout(t, time.Millisecond)
		release := make(chan struct{})
		defer close(release)
		got, err := withTimeout(func() (any, error) {
			<-release
			return 1, nil
		})
		if err == nil || err.Error() != "SECURITY: expression evaluation timed out after 1ms" {
			t.Errorf("withTimeout = %#v, %v, want a timeout", got, err)
		}
	})
}

func TestFileFunction(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body.json")
	if err := os.WriteFile(body, []byte(`{"name": "alice", "tags": ["a"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Expr{}
	env := map[string]any{"path": body}

	got, err := e.Eval("file(path)", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"name": "alice", "tags": ["a"]}` {
		t.Errorf("file() = %#v", got)
	}

	// The content combines with parse_json, keeping its type in a template.
	v, err := e.EvalTemplateWithTypePreservation("{{ parse_json(file(path)) }}", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"name": "alice", "tags": []any{"a"}}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("parse_json(file()) = %#v, want %#v", v, want)
	}

	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, maxStringLength+1), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.json")
	for _, tt := range []struct {
		input   string
		wantErr string
	}{
		{"file()", "file requires exactly 1 parameter"},
		{"file(1)", "file parameter must be a path"},
		{"file('')", "file parameter must be a path"},
		{"file('" + missing + "')", "file: stat " + missing + ": no such file or directory"},
		{"file('" + dir + "')", "file: " + dir + " is a directory"},
		{"file('" + big + "')", "file: " + big + " exceeds maximum length (1000000 bytes)"},
		// A device reports no size and would be read without end.
		{"file('/dev/zero')", "file: /dev/zero is not a regular file"},
	} {
		if _, err := e.Eval(tt.input, env); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("Eval(%q) error = %v, want %q", tt.input, err, tt.wantErr)
		}
	}
}

func TestFileFunctionReadsUpToTheLimit(t *testing.T) {
	full := filepath.Join(t.TempDir(), "full.txt")
	if err := os.WriteFile(full, make([]byte, maxStringLength), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readFile(full)
	if err != nil || len(got) != maxStringLength {
		t.Errorf("readFile = %d bytes, %v, want %d bytes", len(got), err, maxStringLength)
	}
}

// TestReadOpened gives readOpened what readFile found at a path before it
// opened it, as if the path had come to hold something else in between.
func TestReadOpened(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := readOpened(first, info); err != nil || got != first {
		t.Errorf("readOpened of the file found = %q, %v", got, err)
	}
	for _, tt := range []struct {
		name, path, wantErr string
	}{
		{"another file", second, "file: " + second + " changed while it was opened"},
		{"a directory", dir, "file: " + dir + " is a directory"},
		{"nothing", filepath.Join(dir, "gone.txt"), "no such file or directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readOpened(tt.path, info); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("readOpened(%q) error = %v, want %q", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestTemplateFunction(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "user.json.tmpl")
	if err := os.WriteFile(tmpl, []byte(`{"name": "{{ vars.name }}", "count": {{ vars.count + 1 }}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Expr{}
	env := map[string]any{"vars": map[string]any{"name": "alice", "count": 2, "tmpl": tmpl}}

	got, err := e.Eval("template(file(vars.tmpl))", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"name": "alice", "count": 3}` {
		t.Errorf("template() = %#v", got)
	}

	// The file is read as it is when it is not given to template.
	raw, err := e.Eval("file(vars.tmpl)", env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(raw.(string), "{{ vars.name }}") {
		t.Errorf("file() should not expand templates, got %q", raw)
	}

	// toJSON writes a string value quoted, so one holding a quote stays JSON.
	quoted := map[string]any{"vars": map[string]any{"name": `a"b`}}
	v, err := e.EvalTemplateWithTypePreservation(`{{ parse_json(template('{"name": {{ toJSON(vars.name) }}}')) }}`, quoted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(v, map[string]any{"name": `a"b`}) {
		t.Errorf("template() with toJSON = %#v", v)
	}

	if got, err := e.Eval("template('no templates')", env); err != nil || got != "no templates" {
		t.Errorf("template() of plain text = %#v, %v", got, err)
	}

	// A template that cannot be evaluated is an error naming it.
	if _, err := e.Eval("template('{{ nosuch() }}')", env); err == nil || !strings.Contains(err.Error(), "{{ nosuch() }}") {
		t.Errorf("error = %v, want one naming the template", err)
	}
	if _, err := e.Eval("template(1)", env); err == nil || !strings.Contains(err.Error(), "template parameter must be a string") {
		t.Errorf("error = %v", err)
	}
}

func TestTemplateFunctionNestsBoundedly(t *testing.T) {
	dir := t.TempDir()
	loop := filepath.Join(dir, "loop.tmpl")
	if err := os.WriteFile(loop, []byte("{{ template(file(vars.loop)) }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Expr{}
	env := map[string]any{"vars": map[string]any{"loop": loop}}

	start := time.Now()
	_, err := e.Eval("template(file(vars.loop))", env)
	if err == nil || !strings.Contains(err.Error(), "template calls nest deeper than 10") {
		t.Errorf("error = %v, want one about the nesting", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("a template that reads itself should fail at once, took %v", time.Since(start))
	}
}

func TestTemplateFunctionBoundsWhatItExpands(t *testing.T) {
	dir := t.TempDir()
	leaf := filepath.Join(dir, "leaf.txt")
	if err := os.WriteFile(leaf, []byte(strings.Repeat("x", 300000)), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Expr{}
	env := map[string]any{"vars": map[string]any{"leaf": leaf}}

	// A result larger than a string may be is an error, not cut short.
	big := strings.Repeat("{{ template(file(vars.leaf)) }}", 4)
	if _, err := e.Eval("template('"+big+"')", env); err == nil || !strings.Contains(err.Error(), "template result exceeds maximum length (1000000 chars)") {
		t.Errorf("error = %v, want one about the result", err)
	}

	// Results that are not kept still count against what may be expanded
	// in all.
	wide := strings.Repeat("{{ len(template(file(vars.leaf))) }} ", 40)
	if _, err := e.Eval("template('"+wide+"')", env); err == nil || !strings.Contains(err.Error(), "template calls expand more than 10000000 chars in all") {
		t.Errorf("error = %v, want one about the total", err)
	}

	// Each outermost call has a budget of its own.
	few := strings.Repeat("{{ len(template(file(vars.leaf))) }} ", 3)
	for i := range 20 {
		if _, err := e.Eval("template('"+few+"')", env); err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
	}
}

func TestBeforeTemplate(t *testing.T) {
	var seen []string
	stop := errors.New("stop")
	e := &Expr{BeforeTemplate: func(text string) error {
		seen = append(seen, text)
		if strings.Contains(text, "deny") {
			return stop
		}
		return nil
	}}
	env := map[string]any{"inner": "{{ 1 + 1 }}"}

	got, err := e.EvalTemplate("{{ template('a {{ template(inner) }}') }}", env)
	if err != nil || got != "a 2" {
		t.Fatalf("EvalTemplate() = %q, %v", got, err)
	}
	if want := []string{"a {{ template(inner) }}", "{{ 1 + 1 }}"}; !reflect.DeepEqual(seen, want) {
		t.Errorf("BeforeTemplate saw %q, want %q at every depth", seen, want)
	}

	if _, err := e.EvalTemplate("{{ template('deny') }}", env); !errors.Is(err, stop) {
		t.Errorf("error = %v, want the error BeforeTemplate returned", err)
	}
}

// TestTemplateFunctionStopsExpandingAtTheLimit checks that a template is
// expanded no further than the limit of its result, rather than in full
// before the limit is checked.
func TestTemplateFunctionStopsExpandingAtTheLimit(t *testing.T) {
	calls := 0
	env := map[string]any{"chunk": func() string {
		calls++
		return strings.Repeat("x", 300000)
	}}
	e := &Expr{}
	_, err := e.Eval("template('"+strings.Repeat("{{ chunk() }}", 40)+"')", env)
	if err == nil || !strings.Contains(err.Error(), "template result exceeds maximum length (1000000 chars)") {
		t.Errorf("error = %v, want one about the result", err)
	}
	if calls > 4 {
		t.Errorf("chunk was called %d times, want the expansion stopped once it was over the limit", calls)
	}
}

func TestFileFunctionDoesNotBlockOnAFIFO(t *testing.T) {
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo is not available")
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if out, err := exec.Command(mkfifo, fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}

	start := time.Now()
	_, err = (&Expr{}).Eval("file(path)", map[string]any{"path": fifo})
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Errorf("error = %v, want one about the file", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("a FIFO should be refused at once, took %v", time.Since(start))
	}
}

// TestNilExprEvaluates checks that a nil Expr evaluates as the zero one
// does, as it did before Expr had fields.
func TestNilExprEvaluates(t *testing.T) {
	var e *Expr
	got, err := e.EvalTemplate("{{ template('{{ 1 + 1 }}') }} {{ len('ab') }}", map[string]any{})
	if err != nil || got != "2 2" {
		t.Errorf("EvalTemplate() = %q, %v", got, err)
	}
}

func TestEvalTemplateMapEvaluatesKeys(t *testing.T) {
	e := &Expr{}
	env := map[string]any{"vars": map[string]any{"tenant": `t"1`, "n": 2, "hdr": "x-tenant"}}

	got, err := e.EvalTemplateMap(map[string]any{
		"body": map[string]any{
			"type":              "status",
			"{{ vars.tenant }}": map[string]any{"active": true},
			"item-{{ vars.n }}": "{{ vars.n }}",
			"plain {not} templ": 1,
		},
		"headers": map[string]any{"{{ vars.hdr }}": "{{ vars.tenant }}"},
	}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{
		"body": map[string]any{
			"type":              "status",
			`t"1`:               map[string]any{"active": true},
			"item-2":            2,
			"plain {not} templ": 1,
		},
		"headers": map[string]any{"x-tenant": `t"1`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EvalTemplateMap() = %#v, want %#v", got, want)
	}
}

func TestEvalTemplateMapKeyErrors(t *testing.T) {
	e := &Expr{}
	env := map[string]any{"vars": map[string]any{"a": "type"}}

	// A key that becomes another key is an error, named by the key written.
	_, err := e.EvalTemplateMap(map[string]any{
		"body": map[string]any{"type": 1, "{{ vars.a }}": 2},
	}, env)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Path != "body.{{ vars.a }}" || !strings.Contains(err.Error(), `comes to the same key as "type"`) {
		t.Errorf("error = %v, want one naming the keys as written", err)
	}

	// The key a template comes to may be a credential, such as a token, so
	// the error names the keys as written alone.
	secret := map[string]any{"vars": map[string]any{"a": "runtime-token", "b": "runtime-token"}}
	_, err = e.EvalTemplateMap(map[string]any{"{{ vars.a }}": 1, "{{ vars.b }}": 2}, secret)
	if err == nil || strings.Contains(err.Error(), "runtime-token") {
		t.Errorf("error = %v, want one that does not show the key it came to", err)
	}

	// A key that cannot be evaluated is an error, and the others are kept.
	got, err := e.EvalTemplateMap(map[string]any{"{{ nosuch() }}": 1, "ok": "{{ vars.a }}"}, env)
	if !errors.As(err, &fe) || fe.Path != "{{ nosuch() }}" {
		t.Errorf("error = %v, want one naming the key", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"ok": "type"}) {
		t.Errorf("EvalTemplateMap() = %#v, want the other keys kept", got)
	}
}

// TestEvalTemplateMapLimitCountsEveryKey checks that keys that fail or
// collide count against the number of keys a map is evaluated for.
func TestEvalTemplateMapLimitCountsEveryKey(t *testing.T) {
	input := make(map[string]any)
	for i := range 1500 {
		input[fmt.Sprintf("{{ nosuch() }}-%04d", i)] = i
	}
	_, err := (&Expr{}).EvalTemplateMap(input, map[string]any{})
	if n := len(unwrapAll(err)); n > 1001 {
		t.Errorf("%d keys were evaluated, want no more than the limit", n)
	}
}

// unwrapAll returns the errors joined into err.
func unwrapAll(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	if err == nil {
		return nil
	}
	return []error{err}
}

// TestEvalTemplateMapEvaluatedPath checks that an error under a key holding
// templates also says the path as the keys were evaluated.
func TestEvalTemplateMapEvaluatedPath(t *testing.T) {
	_, err := (&Expr{}).EvalTemplateMap(map[string]any{
		"headers": map[string]any{"{{ vars.h }}": "{{ nosuch() }}"},
		"plain":   "{{ nosuch() }}",
	}, map[string]any{"vars": map[string]any{"h": "authorization"}})
	got := map[string]string{}
	for _, e := range unwrapAll(err) {
		var fe *FieldError
		if errors.As(e, &fe) {
			got[fe.Path] = fe.EvaluatedPath
		}
	}
	want := map[string]string{"headers.{{ vars.h }}": "headers.authorization", "plain": "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

// TestEvalTemplateTruncatesAtARune checks the output of a template longer
// than maxStringLength is cut at the start of a rune, so a long value of
// Japanese text does not end in a broken byte.
func TestEvalTemplateTruncatesAtARune(t *testing.T) {
	// Three bytes a rune, so maxStringLength falls inside one.
	s := strings.Repeat("日", maxStringLength/3+1)
	got, err := (&Expr{}).EvalTemplate("{{ s }}", map[string]any{"s": s})
	if err != nil {
		t.Fatal(err)
	}
	kept, ok := strings.CutSuffix(got, truncate.Message())
	if !ok {
		t.Fatalf("the output should end with the truncation marker, got ...%q", got[max(0, len(got)-40):])
	}
	if !utf8.ValidString(kept) || len(kept) > maxStringLength || !strings.HasPrefix(s, kept) {
		t.Errorf("kept %d bytes, valid UTF-8 %v; want a valid prefix of at most %d bytes", len(kept), utf8.ValidString(kept), maxStringLength)
	}
}
