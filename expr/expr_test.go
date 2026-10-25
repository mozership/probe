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

func TestEvalOrEvalTemplate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		env      map[string]any
		expected string
		hasError bool
	}{
		{
			name:     "simple expression without template",
			input:    "1 + 1",
			env:      map[string]any{},
			expected: "2",
			hasError: false,
		},
		{
			name:  "template with variable",
			input: "Hello {{name}}!",
			env: map[string]any{
				"name": "World",
			},
			expected: "Hello World!",
			hasError: false,
		},
		{
			name:  "boolean expression",
			input: "status == 200",
			env: map[string]any{
				"status": 200,
			},
			expected: "true",
			hasError: false,
		},
		// The inputs below hold braces that are not a template, or a
		// template inside what reads as a string.
		{
			name:     "an opener that is never closed is an expression",
			input:    "a {{ b",
			hasError: true,
		},
		{
			name:     "a closer alone is an expression",
			input:    "a }}",
			hasError: true,
		},
		{
			name:     "an opener with no closer is an expression",
			input:    "{{ x",
			hasError: true,
		},
		{
			name:     "a closer before an opener is an expression",
			input:    "}} {{",
			hasError: true,
		},
		{
			name:     "braces in the string literals of an expression are an expression",
			input:    `"}}" + "{{"`,
			expected: "}}{{",
		},
		{
			name:     "a closer in an unclosed string literal is an expression",
			input:    `{{ "x }}`,
			hasError: true,
		},
		{
			name:     "an opener in a string literal is an expression",
			input:    `"{{"`,
			expected: "{{",
		},
		{
			name:     "a closer in the string literal of a template",
			input:    `{{ "}}" }}`,
			expected: "}}",
		},
		{
			name:     "a template between quotes is evaluated",
			input:    "'{{ 1 }}'",
			expected: "'1'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &Expr{}
			result, err := expr.EvalOrEvalTemplate(tt.input, tt.env)

			if tt.hasError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.hasError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.hasError && result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
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

func TestCustomFunctions(t *testing.T) {
	t.Run("match_json function", func(t *testing.T) {
		expr := &Expr{}
		input := "match_json(src, target)"
		env := map[string]any{
			"src": map[string]any{
				"name": "test",
				"age":  25,
			},
			"target": map[string]any{
				"name": "test",
				"age":  25,
			},
		}

		result, err := expr.Eval(input, env)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != true {
			t.Errorf("expected true, got %v", result)
		}
	})

	t.Run("diff_json function", func(t *testing.T) {
		expr := &Expr{}
		input := "diff_json(src, target)"
		env := map[string]any{
			"src": map[string]any{
				"name": "test",
				"age":  25,
			},
			"target": map[string]any{
				"name": "test",
				"age":  30,
			},
		}

		result, err := expr.Eval(input, env)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result == nil {
			t.Errorf("expected diff result, got nil")
		}
		// Should return difference as string
		if resultStr, ok := result.(string); !ok || resultStr == "No diff" {
			t.Errorf("expected diff string, got %v", result)
		}
	})

	t.Run("match_json and diff_json with an invalid regex", func(t *testing.T) {
		for _, fn := range []string{"match_json", "diff_json"} {
			t.Run(fn, func(t *testing.T) {
				expr := &Expr{}
				input := fn + `(src, {"x": "/[/"})`
				env := map[string]any{"src": map[string]any{"x": "abc"}}

				_, err := expr.Eval(input, env)
				if err == nil {
					t.Fatalf("expected error for invalid regex")
				}
				msg := err.Error()
				if strings.Contains(msg, "panicked") {
					t.Errorf("expected a regex error, got a panic: %v", msg)
				}
				want := fn + ": invalid regex at key 'x'"
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want containing %q", msg, want)
				}
			})
		}
	})

	t.Run("custom function with wrong parameters", func(t *testing.T) {
		expr := &Expr{}
		input := "match_json(src)"
		env := map[string]any{
			"src": map[string]any{"name": "test"},
		}

		_, err := expr.Eval(input, env)
		if err == nil {
			t.Errorf("expected error for wrong parameter count")
		}
	})

	t.Run("parse_int function", func(t *testing.T) {
		expr := &Expr{}
		tests := []struct {
			name     string
			input    string
			env      map[string]any
			expected int64
			wantErr  bool
		}{
			{
				name:     "valid integer string",
				input:    "parse_int(num_str)",
				env:      map[string]any{"num_str": "123"},
				expected: 123,
				wantErr:  false,
			},
			{
				name:     "negative integer",
				input:    "parse_int(num_str)",
				env:      map[string]any{"num_str": "-456"},
				expected: -456,
				wantErr:  false,
			},
			{
				name:    "invalid string",
				input:   "parse_int(num_str)",
				env:     map[string]any{"num_str": "not_a_number"},
				wantErr: true,
			},
			{
				name:     "integer parameter",
				input:    "parse_int(num)",
				env:      map[string]any{"num": 123},
				expected: 123,
				wantErr:  false,
			},
			{
				name:     "float64 parameter",
				input:    "parse_int(num)",
				env:      map[string]any{"num": 456.0},
				expected: 456,
				wantErr:  false,
			},
			{
				name:    "unsupported parameter type",
				input:   "parse_int(arr)",
				env:     map[string]any{"arr": []int{1, 2, 3}},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, tt.env)

				if tt.wantErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}

				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if result != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			})
		}
	})
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

// TestValidateExpressionAllowsEnvLikeText pins the fix for a guard that used to
// reject any expression whose text contained "env.path", "env.secret" and
// friends. The evaluation context has no `env` namespace, so the guard only
// ever rejected ordinary strings that happened to contain the pattern.
func TestValidateExpressionAllowsEnvLikeText(t *testing.T) {
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

// TestValidateExpressionRejectsOverlongInput keeps the one limit that
// validateExpression still enforces.
func TestValidateExpressionRejectsOverlongInput(t *testing.T) {
	expr := &Expr{}

	if err := expr.validateExpression(strings.Repeat("a", maxExpressionLength+1)); err == nil {
		t.Error("expected an error for an expression over the length limit")
	}
	if err := expr.validateExpression(strings.Repeat("a", 10)); err != nil {
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
		_, err := expr.Eval("invalid syntax $$", map[string]any{})

		if err == nil {
			t.Errorf("expected compilation error")
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

func TestTimeoutProtection(t *testing.T) {
	t.Run("prevents infinite loops", func(t *testing.T) {
		expr := &Expr{}

		// This test is tricky because we need an expression that would loop
		// but expr-lang is designed to be safe. Let's test the timeout mechanism
		// by using a mock timeout scenario
		start := time.Now()
		_, err := expr.Eval("true", map[string]any{})
		duration := time.Since(start)

		// The expression should complete quickly (not timeout)
		if err != nil {
			t.Errorf("simple expression should not error: %v", err)
		}
		if duration > time.Second {
			t.Errorf("simple expression took too long: %v", duration)
		}
	})
}

func TestNewCustomFunctions(t *testing.T) {
	expr := &Expr{}
	env := map[string]any{}

	t.Run("random_int", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			expectErr bool
		}{
			{
				name:      "valid random_int with positive integer",
				input:     "random_int(100)",
				expectErr: false,
			},
			{
				name:      "valid random_int with float64 (common in JSON)",
				input:     "random_int(100.0)",
				expectErr: false,
			},
			{
				name:      "invalid random_int with zero",
				input:     "random_int(0)",
				expectErr: true,
			},
			{
				name:      "invalid random_int with negative",
				input:     "random_int(-1)",
				expectErr: true,
			},
			{
				name:      "invalid random_int with string",
				input:     "random_int('100')",
				expectErr: true,
			},
			{
				name:      "invalid random_int with no params",
				input:     "random_int()",
				expectErr: true,
			},
			{
				name:      "invalid random_int with multiple params",
				input:     "random_int(100, 200)",
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, env)
				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				// Check result is integer and in valid range
				if val, ok := result.(int); ok {
					if tt.input == "random_int(100)" || tt.input == "random_int(100.0)" {
						if val < 0 || val >= 100 {
							t.Errorf("random_int(100) returned %d, expected 0 <= result < 100", val)
						}
					}
				} else {
					t.Errorf("expected int result, got %T", result)
				}
			})
		}
	})

	t.Run("random_str", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			expectErr bool
			length    int
		}{
			{
				name:      "valid random_str with positive length",
				input:     "random_str(10)",
				expectErr: false,
				length:    10,
			},
			{
				name:      "valid random_str with float64",
				input:     "random_str(5.0)",
				expectErr: false,
				length:    5,
			},
			{
				name:      "valid random_str with large length",
				input:     "random_str(10000)",
				expectErr: false,
				length:    10000,
			},
			{
				name:      "invalid random_str with zero",
				input:     "random_str(0)",
				expectErr: true,
			},
			{
				name:      "invalid random_str with negative",
				input:     "random_str(-1)",
				expectErr: true,
			},
			{
				name:      "invalid random_str too long",
				input:     "random_str(1000001)",
				expectErr: true,
			},
			{
				name:      "invalid random_str with string",
				input:     "random_str('10')",
				expectErr: true,
			},
			{
				name:      "invalid random_str with no params",
				input:     "random_str()",
				expectErr: true,
			},
			{
				name:      "invalid random_str with multiple params",
				input:     "random_str(10, 20)",
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, env)
				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				// Check result is string with correct length
				if str, ok := result.(string); ok {
					if len(str) != tt.length {
						t.Errorf("random_str(%d) returned string of length %d, expected %d", tt.length, len(str), tt.length)
					}

					// Check charset (alphanumeric)
					matched, err := regexp.MatchString("^[a-zA-Z0-9]+$", str)
					if err != nil {
						t.Errorf("regex error: %v", err)
					}
					if !matched {
						t.Errorf("random_str returned invalid characters: %s", str)
					}
				} else {
					t.Errorf("expected string result, got %T", result)
				}
			})
		}
	})

	t.Run("unixtime", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			expectErr bool
		}{
			{
				name:      "valid unixtime with no params",
				input:     "unixtime()",
				expectErr: false,
			},
			{
				name:      "invalid unixtime with params",
				input:     "unixtime(123)",
				expectErr: true,
			},
			{
				name:      "invalid unixtime with string param",
				input:     "unixtime('now')",
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				beforeCall := time.Now().Unix()
				result, err := expr.Eval(tt.input, env)
				afterCall := time.Now().Unix()

				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				// Check result is int64 and reasonable timestamp
				if val, ok := result.(int64); ok {
					if val < beforeCall || val > afterCall {
						t.Errorf("unixtime() returned %d, expected between %d and %d", val, beforeCall, afterCall)
					}
				} else {
					t.Errorf("expected int64 result, got %T", result)
				}
			})
		}
	})

	t.Run("template evaluation with custom functions", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
		}{
			{
				name:     "random_int in template",
				template: "User ID: {{random_int(9999)}}",
			},
			{
				name:     "random_str in template",
				template: "Session: {{random_str(16)}}",
			},
			{
				name:     "unixtime in template",
				template: "Timestamp: {{unixtime()}}",
			},
			{
				name:     "multiple functions in template",
				template: "ID: {{random_int(1000)}}, Token: {{random_str(8)}}, Time: {{unixtime()}}",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.EvalTemplate(tt.template, env)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				// Basic validation that template was processed
				if result == tt.template {
					t.Errorf("template was not processed: %s", result)
				}

				// Check that placeholders were replaced
				if regexp.MustCompile(`\{[^}]+\}`).MatchString(result) {
					t.Errorf("template still contains unreplaced placeholders: %s", result)
				}
			})
		}
	})
}

func TestNewCustomFunctionsEdgeCases(t *testing.T) {
	expr := &Expr{}
	env := map[string]any{}

	t.Run("random_int boundary values", func(t *testing.T) {
		// Test with 1 (smallest valid value)
		result, err := expr.Eval("random_int(1)", env)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if val, ok := result.(int); ok {
			if val != 0 {
				t.Errorf("random_int(1) should always return 0, got %d", val)
			}
		}

		// Test with 2 (returns 0 or 1)
		result, err = expr.Eval("random_int(2)", env)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if val, ok := result.(int); ok {
			if val < 0 || val >= 2 {
				t.Errorf("random_int(2) returned %d, expected 0 or 1", val)
			}
		}
	})

	t.Run("random_str boundary values", func(t *testing.T) {
		// Test with 1 (smallest valid length)
		result, err := expr.Eval("random_str(1)", env)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if str, ok := result.(string); ok {
			if len(str) != 1 {
				t.Errorf("random_str(1) returned string of length %d, expected 1", len(str))
			}
		}
	})

	t.Run("unixtime consistency", func(t *testing.T) {
		// Call unixtime multiple times in quick succession
		results := make([]int64, 3)
		for i := range 3 {
			result, err := expr.Eval("unixtime()", env)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if val, ok := result.(int64); ok {
				results[i] = val
			}
		}

		// All results should be within a reasonable time range (same second or consecutive seconds)
		for i := 1; i < len(results); i++ {
			diff := results[i] - results[i-1]
			if diff < 0 || diff > 1 {
				t.Errorf("unixtime() calls returned inconsistent results: %v", results)
			}
		}
	})

	t.Run("encode_base64", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			expectErr bool
			expected  string
		}{
			{
				name:      "valid encode_base64 with simple string",
				input:     "encode_base64('hello')",
				expectErr: false,
				expected:  "aGVsbG8=",
			},
			{
				name:      "valid encode_base64 with empty string",
				input:     "encode_base64('')",
				expectErr: false,
				expected:  "",
			},
			{
				name:      "valid encode_base64 with special characters",
				input:     "encode_base64('Hello, World!')",
				expectErr: false,
				expected:  "SGVsbG8sIFdvcmxkIQ==",
			},
			{
				name:      "invalid encode_base64 with no params",
				input:     "encode_base64()",
				expectErr: true,
			},
			{
				name:      "invalid encode_base64 with multiple params",
				input:     "encode_base64('hello', 'world')",
				expectErr: true,
			},
			{
				name:      "invalid encode_base64 with non-string param",
				input:     "encode_base64(123)",
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, env)
				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if str, ok := result.(string); ok {
					if str != tt.expected {
						t.Errorf("encode_base64 returned %s, expected %s", str, tt.expected)
					}
				} else {
					t.Errorf("expected string result, got %T", result)
				}
			})
		}
	})

	t.Run("decode_base64", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			expectErr bool
			expected  string
		}{
			{
				name:      "valid decode_base64 with simple string",
				input:     "decode_base64('aGVsbG8=')",
				expectErr: false,
				expected:  "hello",
			},
			{
				name:      "valid decode_base64 with empty string",
				input:     "decode_base64('')",
				expectErr: false,
				expected:  "",
			},
			{
				name:      "valid decode_base64 with special characters",
				input:     "decode_base64('SGVsbG8sIFdvcmxkIQ==')",
				expectErr: false,
				expected:  "Hello, World!",
			},
			{
				name:      "invalid decode_base64 with invalid base64",
				input:     "decode_base64('invalid!')",
				expectErr: true,
			},
			{
				name:      "invalid decode_base64 with no params",
				input:     "decode_base64()",
				expectErr: true,
			},
			{
				name:      "invalid decode_base64 with multiple params",
				input:     "decode_base64('aGVsbG8=', 'extra')",
				expectErr: true,
			},
			{
				name:      "invalid decode_base64 with non-string param",
				input:     "decode_base64(123)",
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, env)
				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if str, ok := result.(string); ok {
					if str != tt.expected {
						t.Errorf("decode_base64 returned %s, expected %s", str, tt.expected)
					}
				} else {
					t.Errorf("expected string result, got %T", result)
				}
			})
		}
	})

	t.Run("base64 encode-decode round trip", func(t *testing.T) {
		testStrings := []string{
			"hello world",
			"Hello, World!",
			"user:password",
			"Special chars: !@#$%^&*()",
			"Unicode: こんにちは",
			"",
		}

		for _, original := range testStrings {
			t.Run("round trip: "+original, func(t *testing.T) {
				// First encode
				encodeResult, err := expr.Eval("encode_base64('"+original+"')", env)
				if err != nil {
					t.Errorf("encode error: %v", err)
					return
				}

				encoded, ok := encodeResult.(string)
				if !ok {
					t.Errorf("encode result is not string: %T", encodeResult)
					return
				}

				// Then decode
				decodeResult, err := expr.Eval("decode_base64('"+encoded+"')", env)
				if err != nil {
					t.Errorf("decode error: %v", err)
					return
				}

				decoded, ok := decodeResult.(string)
				if !ok {
					t.Errorf("decode result is not string: %T", decodeResult)
					return
				}

				if decoded != original {
					t.Errorf("round trip failed: original=%s, decoded=%s", original, decoded)
				}
			})
		}
	})

	t.Run("parse_json", func(t *testing.T) {
		tests := []struct {
			name      string
			input     string
			env       map[string]any
			expected  any
			expectErr bool
		}{
			{
				name:     "parse object and access field",
				input:    "parse_json(data).name",
				env:      map[string]any{"data": `{"name": "test", "id": 123}`},
				expected: "test",
			},
			{
				name:     "parse object and access nested field",
				input:    "parse_json(data).data.id",
				env:      map[string]any{"data": `{"data": {"id": 456}}`},
				expected: float64(456),
			},
			{
				name:     "parse and use with match_json",
				input:    "match_json(parse_json(data), expected)",
				env:      map[string]any{"data": `{"name": "test"}`, "expected": map[string]any{"name": "test"}},
				expected: true,
			},
			{
				name:      "invalid json",
				input:     "parse_json(data)",
				env:       map[string]any{"data": "not json"},
				expectErr: true,
			},
			{
				name:      "non-string parameter",
				input:     "parse_json(data)",
				env:       map[string]any{"data": 123},
				expectErr: true,
			},
			{
				name:      "no parameters",
				input:     "parse_json()",
				env:       map[string]any{},
				expectErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.Eval(tt.input, tt.env)
				if tt.expectErr {
					if err == nil {
						t.Errorf("expected error but got none")
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if !reflect.DeepEqual(result, tt.expected) {
					t.Errorf("expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
				}
			})
		}
	})

	t.Run("parse_json in template", func(t *testing.T) {
		result, err := expr.EvalTemplate("ID: {{parse_json(data).id}}", map[string]any{
			"data": `{"id": 42, "name": "test"}`,
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if result != "ID: 42" {
			t.Errorf("expected 'ID: 42', got %s", result)
		}
	})

	t.Run("base64 functions in templates", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			env      map[string]any
			expected string
		}{
			{
				name:     "encode_base64 in template",
				template: "Authorization: Basic {{encode_base64('user:pass')}}",
				env:      map[string]any{},
				expected: "Authorization: Basic dXNlcjpwYXNz",
			},
			{
				name:     "decode_base64 in template",
				template: "Decoded: {{decode_base64('aGVsbG8=')}}",
				env:      map[string]any{},
				expected: "Decoded: hello",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := expr.EvalTemplate(tt.template, tt.env)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}

				if result != tt.expected {
					t.Errorf("template result: got %s, expected %s", result, tt.expected)
				}
			})
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
