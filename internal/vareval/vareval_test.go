package vareval

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/expr"
)

func TestEval(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]any
		env      map[string]string
		expected map[string]any
	}{
		{
			name: "use expr",
			vars: map[string]any{
				"host":  "{{HOST ?? 'http://localhost:3000'}}",
				"token": "{{TOKEN}}",
			},
			env: map[string]string{"TOKEN": "secrets"},
			expected: map[string]any{
				"host":  "http://localhost:3000",
				"token": "secrets",
			},
		},
		{
			name: "not exists environment",
			vars: map[string]any{
				"host":  "{{HOST}}",
				"token": "{{TOKEN}}",
			},
			env: map[string]string{"TOKEN": "secrets"},
			expected: map[string]any{
				"host":  "<nil>",
				"token": "secrets",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := Eval(tt.vars, nil, tt.env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Errorf("expected %#v, got %#v", tt.expected, actual)
			}
		})
	}
}

func TestEvalReadsOtherVars(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]any
		env      map[string]string
		expected map[string]any
	}{
		{
			name: "a var defined after the one that reads it",
			vars: map[string]any{
				"base": "http://localhost:{{vars.port}}",
				"port": "{{PORT ?? '18080'}}",
			},
			env: map[string]string{"PORT": "9000"},
			expected: map[string]any{
				"base": "http://localhost:9000",
				"port": "9000",
			},
		},
		{
			name: "a chain of vars",
			vars: map[string]any{
				"a": "{{vars.b}}-a",
				"b": "{{vars.c}}-b",
				"c": "c",
			},
			expected: map[string]any{
				"a": "c-b-a",
				"b": "c-b",
				"c": "c",
			},
		},
		{
			name: "a map var reading a string var",
			vars: map[string]any{
				"domain": "example.test",
				"alice": map[string]any{
					"user": "alice@{{vars.domain}}",
					"tags": []any{"{{vars.domain}}"},
				},
			},
			expected: map[string]any{
				"domain": "example.test",
				"alice": map[string]any{
					"user": "alice@example.test",
					"tags": []any{"example.test"},
				},
			},
		},
		{
			name: "a string var reading a field of a map var",
			vars: map[string]any{
				"auth": map[string]any{"user": "admin", "password": "{{PASSWORD}}"},
				"pair": "{{vars.auth.user + ':' + vars.auth.password}}",
			},
			env: map[string]string{"PASSWORD": "secret"},
			expected: map[string]any{
				"auth": map[string]any{"user": "admin", "password": "secret"},
				"pair": "admin:secret",
			},
		},
		{
			name: "a var that is not defined falls back",
			vars: map[string]any{
				"port": "{{vars.missing ?? '8080'}}",
			},
			expected: map[string]any{
				"port": "8080",
			},
		},
		{
			name: "a var read by a key known only at run time",
			vars: map[string]any{
				"picked": "{{vars[WHICH]}}",
				"x":      "{{vars.y}}!",
				"y":      "why",
			},
			env: map[string]string{"WHICH": "x"},
			expected: map[string]any{
				"picked": "why!",
				"x":      "why!",
				"y":      "why",
			},
		},
		{
			name: "an array at the top level",
			vars: map[string]any{
				"port":  "8080",
				"list":  []any{"{{vars.port}}", map[string]any{"p": "{{vars.port}}"}},
				"label": "{{vars.list[0]}}",
			},
			expected: map[string]any{
				"port":  "8080",
				"list":  []any{"8080", map[string]any{"p": "8080"}},
				"label": "8080",
			},
		},
		{
			name: "a value that is not a string",
			vars: map[string]any{
				"retries": 3,
				"label":   "{{vars.retries * 2}}",
			},
			expected: map[string]any{
				"retries": 3,
				"label":   "6",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := Eval(tt.vars, nil, tt.env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Errorf("expected %#v, got %#v", tt.expected, actual)
			}
		})
	}
}

func TestEvalReadsOneRandomValue(t *testing.T) {
	vars := map[string]any{
		"password": "{{random_str(24)}}",
		"auth":     "alice:{{vars.password}}",
		"header":   "Basic {{encode_base64(vars.auth)}}",
	}
	for range 20 {
		actual, err := Eval(vars, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		password := actual["password"].(string)
		if actual["auth"] != "alice:"+password {
			t.Fatalf("auth = %q, want it built from password %q", actual["auth"], password)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+password))
		if actual["header"] != want {
			t.Fatalf("header = %q, want %q", actual["header"], want)
		}
	}
}

func TestEvalReadsVarsAsAWhole(t *testing.T) {
	vars := map[string]any{
		"name":     "probe",
		"snapshot": map[string]any{"all": "{{vars}}"},
	}
	actual, err := Eval(vars, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]any{
		"name":     "probe",
		"snapshot": map[string]any{"all": map[string]any{"name": "probe"}},
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected %#v, got %#v", expected, actual)
	}
	// A var holding the map being filled in would make it cyclic, which JSON
	// cannot encode.
	if _, err := json.Marshal(actual); err != nil {
		t.Errorf("vars cannot be encoded as JSON: %v", err)
	}
}

// writeTemplate writes content to a file named name in dir and returns its
// path.
func writeTemplate(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEvalWithTemplatesFromFiles checks that a var that expands a template
// comes after the vars that do not, since what the template reads is known
// only once it runs, and that two such vars do not make a cycle.
func TestEvalWithTemplatesFromFiles(t *testing.T) {
	dir := t.TempDir()
	body := writeTemplate(t, dir, "body.tmpl", "{{ vars.zone }}-{{ vars.yes }}")
	other := writeTemplate(t, dir, "other.tmpl", "other {{ vars.zone }}")

	vars := map[string]any{
		"a":    "{{ template(file('" + body + "')) }}",
		"b":    "{{ template(file('" + other + "')) }} and {{ vars.a }}",
		"yes":  true,
		"zone": "{{ ZONE }}",
	}
	actual, err := Eval(vars, nil, map[string]string{"ZONE": "tokyo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]any{
		"a":    "tokyo-true",
		"b":    "other tokyo and tokyo-true",
		"yes":  true,
		"zone": "tokyo",
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected %#v, got %#v", expected, actual)
	}
}

// TestEvalTemplateReadsALaterTemplateVar checks that a var whose template
// reads another var that expands a template, and would come first by name,
// is evaluated after it, and that a cycle between them is reported.
func TestEvalTemplateReadsALaterTemplateVar(t *testing.T) {
	dir := t.TempDir()
	first := writeTemplate(t, dir, "a.tmpl", "a sees {{ vars.z }}")
	last := writeTemplate(t, dir, "z.tmpl", "z is {{ vars.n }}-{{ random_str(8) }}")

	vars := map[string]any{
		"a": "{{ template(file('" + first + "')) }}",
		"n": 1,
		"z": "{{ template(file('" + last + "')) }}",
	}
	actual, err := Eval(vars, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	z, _ := actual["z"].(string)
	if !strings.HasPrefix(z, "z is 1-") || actual["a"] != "a sees "+z {
		t.Errorf("a should see the value z holds, got a = %q, z = %q", actual["a"], z)
	}

	cyclic := map[string]any{
		"a": "{{ template(file('" + writeTemplate(t, dir, "ca.tmpl", "{{ vars.b }}") + "')) }}",
		"b": "{{ template(file('" + writeTemplate(t, dir, "cb.tmpl", "{{ vars.a }}") + "')) }}",
	}
	if _, err := Eval(cyclic, nil, nil); err == nil || !strings.Contains(err.Error(), "vars: circular reference: a -> b -> a") {
		t.Errorf("error = %v, want the cycle", err)
	}
}

// TestEvalTemplateReadsVarsByAKeyKnownWhenItRuns checks that a template
// reading vars by a key known only when it runs waits for every other var.
func TestEvalTemplateReadsVarsByAKeyKnownWhenItRuns(t *testing.T) {
	dir := t.TempDir()
	vars := map[string]any{
		"a": "{{ template(file('" + writeTemplate(t, dir, "a.tmpl", "a sees {{ vars[WHICH] }}") + "')) }}",
		"z": "{{ template(file('" + writeTemplate(t, dir, "z.tmpl", "z") + "')) }}",
	}
	actual, err := Eval(vars, nil, map[string]string{"WHICH": "z"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual["a"] != "a sees z" {
		t.Errorf("a = %q, want it to see z", actual["a"])
	}
}

// TestEvalKeyReadsAVar checks that a var whose key reads another var is
// evaluated after it, as one whose value reads it is.
func TestEvalKeyReadsAVar(t *testing.T) {
	vars := map[string]any{
		"body":   map[string]any{"{{ vars.tenant }}": map[string]any{"active": true}},
		"tenant": "{{ TENANT }}",
	}
	actual, err := Eval(vars, nil, map[string]string{"TENANT": "acme"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"acme": map[string]any{"active": true}}
	if !reflect.DeepEqual(actual["body"], want) {
		t.Errorf("body = %#v, want %#v", actual["body"], want)
	}
}

// TestEvalListNamedLikeATemplate checks that a list var is evaluated
// whatever its name holds, now that the keys of a map are templates too.
func TestEvalListNamedLikeATemplate(t *testing.T) {
	vars := map[string]any{
		"odd{{name}}": []any{"{{ 1 + 1 }}"},
		"bad":         []any{"ok", map[string]any{"x": "{{ nosuch() }}"}},
	}
	_, err := Eval(vars, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "vars.bad[1].x: ") {
		t.Errorf("error = %v, want one naming vars.bad[1].x", err)
	}

	delete(vars, "bad")
	actual, err := Eval(vars, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(actual["odd{{name}}"], []any{2}) {
		t.Errorf("odd{{name}} = %#v, want the list evaluated", actual["odd{{name}}"])
	}
}

func TestEvalCycleIsReproducible(t *testing.T) {
	// a reads b and c through the values of a map, and both read a back, so
	// the cycle reported depends on which of them is visited first.
	vars := map[string]any{
		"a": map[string]any{"x": "{{vars.c}}", "y": "{{vars.b}}", "z": "{{vars.c}}"},
		"b": "{{vars.a.y}}",
		"c": "{{vars.a.x}}",
	}
	for i := range 50 {
		_, err := Eval(vars, nil, nil)
		if err == nil || err.Error() != "vars: circular reference: a -> c -> a" {
			t.Fatalf("run %d: error = %v, want the cycle through c", i, err)
		}
	}
}

func TestEvalCircularReference(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]any
		want string
	}{
		{
			name: "itself",
			vars: map[string]any{"a": "{{vars.a}}"},
			want: "vars: circular reference: a -> a",
		},
		{
			name: "two vars",
			vars: map[string]any{"a": "{{vars.b}}", "b": "{{vars.a}}"},
			want: "vars: circular reference: a -> b -> a",
		},
		{
			name: "through a map var",
			vars: map[string]any{
				"a": map[string]any{"x": "{{vars.c}}"},
				"b": "{{vars.a.x}}",
				"c": "{{vars.b}}",
			},
			want: "vars: circular reference: a -> c -> b -> a",
		},
		{
			name: "itself by name, along with a read of vars as a whole",
			vars: map[string]any{
				"a": "{{vars.a ?? 'fallback'}} {{toJSON(vars)}}",
				"b": "b",
			},
			want: "vars: circular reference: a -> a",
		},
		{
			name: "through an array var",
			vars: map[string]any{"a": []any{"{{vars.b}}"}, "b": "{{vars.a[0]}}"},
			want: "vars: circular reference: a -> b -> a",
		},
		{
			name: "two vars read by keys known only at run time",
			vars: map[string]any{"a": "{{vars[K]}}", "b": "{{toJSON(vars)}}"},
			want: "vars: circular reference: a -> b -> a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Eval(tt.vars, nil, nil)
			if err == nil || err.Error() != tt.want {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

// A var the person running the workflow gave is the text given, in place of
// what the workflow writes for it, and the other vars read that text.
func TestEvalGiven(t *testing.T) {
	written := map[string]any{
		"host":    "{{HOST ?? 'localhost'}}",
		"url":     "http://{{vars.host}}:{{vars.port}}",
		"port":    8080,
		"nested":  map[string]any{"a": 1},
		"literal": "kept",
	}
	given := map[string]string{
		"host":     "given.example.com",
		"nested":   "flat",
		"template": "{{ 1 + 1 }}",
		"new":      "",
	}
	got, err := Eval(written, given, map[string]string{"HOST": "from-env"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"host":    "given.example.com",
		"url":     "http://given.example.com:8080",
		"port":    8080,
		"nested":  "flat",
		"literal": "kept",
		// A template in what is given is text: it is not evaluated.
		"template": "{{ 1 + 1 }}",
		// A var the workflow does not write is defined by being given.
		"new": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval() = %#v, want %#v", got, want)
	}
	if _, ok := written["new"]; ok || written["host"] != "{{HOST ?? 'localhost'}}" {
		t.Errorf("the vars written were changed: %#v", written)
	}

	// A workflow that writes no vars takes those given.
	if got, err := Eval(nil, map[string]string{"a": "1"}, nil); err != nil || !reflect.DeepEqual(got, map[string]any{"a": "1"}) {
		t.Errorf("Eval() = %#v, %v, want the var given", got, err)
	}

	// A var that read itself no longer does once it is given.
	cyclic := map[string]any{"a": "{{vars.b}}", "b": "{{vars.a}}"}
	if got, err := Eval(cyclic, map[string]string{"a": "x"}, nil); err != nil || got["b"] != "x" {
		t.Errorf("Eval() = %#v, %v, want b to read the var given", got, err)
	}
}

func TestEvalTemplateError(t *testing.T) {
	vars := map[string]any{
		"ok":   "fine",
		"bad":  "{{1 +}}",
		"auth": map[string]any{"user": "{{nothing.here}}"},
	}
	got, err := Eval(vars, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got != nil {
		t.Errorf("expected no vars, got %#v", got)
	}
	for _, m := range []string{"vars.auth.user: {{nothing.here}}: ", "vars.bad: {{1 +}}: "} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("error %q does not contain %q", err.Error(), m)
		}
	}
	if strings.Index(err.Error(), "vars.auth") > strings.Index(err.Error(), "vars.bad") {
		t.Errorf("errors are not in name order: %q", err.Error())
	}
}

func TestEvalSkipsVarsReadingAFailedOne(t *testing.T) {
	vars := map[string]any{
		"base":  "{{1 +}}",
		"url":   "{{vars.base}}/x",
		"all":   "{{toJSON(vars)}}",
		"list":  []any{"ok", "{{nothing.here}}"},
		"other": "fine",
	}
	_, err := Eval(vars, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	errs := unjoin(err)
	if len(errs) != 2 {
		t.Fatalf("expected the errors of base and list alone, got %d: %v", len(errs), err)
	}
	for _, m := range []string{"vars.base: {{1 +}}: ", "vars.list[1]: {{nothing.here}}: "} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("error %q does not contain %q", err.Error(), m)
		}
	}
}

// TestEvalDoesNotChangeEnv checks that the vars evaluated are not left in
// the environment variables given, and that an environment variable named
// vars does not hide them.
func TestEvalDoesNotChangeEnv(t *testing.T) {
	env := map[string]string{"vars": "shadow", "HOST": "h"}
	got, err := Eval(map[string]any{"a": "{{HOST}}", "b": "{{vars.a}}"}, nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"a": "h", "b": "h"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Eval() = %#v, want %#v", got, want)
	}
	if want := map[string]string{"vars": "shadow", "HOST": "h"}; !reflect.DeepEqual(env, want) {
		t.Errorf("env = %#v, want it unchanged", env)
	}
}

func TestEvalNoVars(t *testing.T) {
	got, err := Eval(nil, nil, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("Eval() = %#v, %v, want no vars", got, err)
	}
}

// TestEvalTemplateReadsAFailedVar checks that a var whose template, expanded
// only when it runs, reads a var that could not be evaluated is left out
// along with it, and only that var's error is reported.
func TestEvalTemplateReadsAFailedVar(t *testing.T) {
	dir := t.TempDir()
	// b's template reads z, which comes after b by what b reads outside its
	// template, so b waits for z, and z fails.
	b := writeTemplate(t, dir, "b.tmpl", "{{ vars.z }}")
	z := writeTemplate(t, dir, "z.tmpl", "{{ 1 + }}")
	vars := map[string]any{
		"b": "{{ template(file('" + b + "')) }}",
		"z": "{{ template(file('" + z + "')) }}",
	}
	got, err := Eval(vars, nil, nil)
	if err == nil {
		t.Fatalf("Eval() = %#v, want an error", got)
	}
	errs := unjoin(err)
	if len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), "vars.z: ") {
		t.Errorf("errors = %v, want the error of z alone", errs)
	}
}

// TestEvalTemplateCycleThroughAThirdVar checks that a cycle found while a
// var waits for another, which itself waits for a third, stops the
// evaluation of all of them.
func TestEvalTemplateCycleThroughAThirdVar(t *testing.T) {
	dir := t.TempDir()
	vars := map[string]any{
		"a": "{{ template(file('" + writeTemplate(t, dir, "a.tmpl", "{{ vars.b }}") + "')) }}",
		"b": "{{ template(file('" + writeTemplate(t, dir, "b.tmpl", "{{ vars.c }}") + "')) }}",
		"c": "{{ template(file('" + writeTemplate(t, dir, "c.tmpl", "{{ vars.a }}") + "')) }}",
	}
	_, err := Eval(vars, nil, nil)
	if err == nil || err.Error() != "vars: circular reference: a -> b -> c -> a" {
		t.Errorf("error = %v, want the cycle through c", err)
	}
}

func TestEvalValue(t *testing.T) {
	ev := &expr.Expr{}
	env := map[string]any{"n": 2}
	tests := []struct {
		name string
		v    any
		want any
		err  string
	}{
		{name: "string", v: "{{ n + 1 }}", want: "3"},
		{name: "map", v: map[string]any{"x": "{{ n }}"}, want: map[string]any{"x": 2}},
		{name: "list", v: []any{"{{ n }}", "a"}, want: []any{2, "a"}},
		{name: "other", v: 7, want: 7},
		{name: "string error", v: "{{ 1 + }}", err: "vars.v: {{ 1 + }}: "},
		{name: "map error", v: map[string]any{"x": "{{ 1 + }}"}, err: "vars.v.x: {{ 1 + }}: "},
		{name: "list error", v: []any{"a", "{{ 1 + }}"}, err: "vars.v[1]: {{ 1 + }}: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalValue(ev, "v", tt.v, env)
			if tt.err != "" {
				var fe *expr.FieldError
				if err == nil || !strings.HasPrefix(err.Error(), tt.err) || !errors.As(err, &fe) {
					t.Errorf("error = %v, want a field error starting %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvalValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEvalOrder(t *testing.T) {
	order, err := evalOrder(map[string]any{
		"c": "{{ vars.b }}",
		"b": "{{ vars.a }}",
		"a": "a",
		"d": "{{ vars.missing }}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c", "d"}; !reflect.DeepEqual(order, want) {
		t.Errorf("evalOrder() = %q, want %q", order, want)
	}
}

// unjoin returns the errors joined into err, or err alone.
func unjoin(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}
