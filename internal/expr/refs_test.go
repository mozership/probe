package expr

import (
	"reflect"
	"slices"
	"testing"
)

func TestRefs(t *testing.T) {
	tests := []struct {
		name        string
		input       any
		wantKeys    []string
		wantDynamic bool
	}{
		{"plain text", "localhost", nil, false},
		{"environment variable only", "{{PORT ?? '8080'}}", nil, false},
		{"dot access", "http://{{vars.host}}:{{vars.port}}", []string{"host", "port"}, false},
		{"bracket access with a constant key", "{{vars['host']}}", []string{"host"}, false},
		{"optional access", "{{vars?.host ?? 'x'}}", []string{"host"}, false},
		{"the same key twice", "{{vars.a}}{{vars.a + vars.b}}", []string{"a", "b"}, false},
		{"nested member", "{{vars.auth.user}}", []string{"auth"}, false},
		{"inside a function call", "{{encode_base64(vars.user + ':' + vars.password)}}", []string{"user", "password"}, false},
		{"inside a predicate", "{{filter(vars.items, {# > vars.min})}}", []string{"items", "min"}, false},
		{"key known only at run time", "{{vars[KEY]}}", nil, true},
		{"the object as a whole", "{{toJSON(vars)}}", nil, true},
		{"constant and dynamic", "{{vars.a + vars[KEY]}}", []string{"a"}, true},
		{"another object with the same key", "{{outputs.vars}}", nil, false},
		{"field named like the object", "{{other.vars.host}}", nil, false},
		{"does not parse", "{{vars.a +}}", nil, false},
		{"map", map[string]any{"user": "{{vars.user}}", "n": 1}, []string{"user"}, false},
		{"array", []any{"{{vars.a}}", map[string]any{"b": "{{vars.b}}"}}, []string{"a", "b"}, false},
		{"not a string", 42, nil, false},
		{"map values in the order of sorted keys", map[string]any{"z": "{{vars.c}}", "a": "{{vars.b}}", "m": "{{vars.a}}"}, []string{"b", "a", "c"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, dynamic := Refs(tt.input, "vars")
			if !reflect.DeepEqual(keys, tt.wantKeys) {
				t.Errorf("keys = %#v, want %#v", keys, tt.wantKeys)
			}
			if dynamic != tt.wantDynamic {
				t.Errorf("dynamic = %v, want %v", dynamic, tt.wantDynamic)
			}
		})
	}
}

func TestCallsTemplate(t *testing.T) {
	for _, tt := range []struct {
		v    any
		want bool
	}{
		{"{{ template(file('a.tmpl')) }}", true},
		{"x {{ vars.a }} {{ parse_json(template(vars.t)).k }}", true},
		{map[string]any{"a": []any{"plain", "{{ template('{{ vars.b }}') }}"}}, true},
		{"{{ file('a.json') }}", false},
		{"{{ vars.template }}", false},
		{"template(x) outside braces", false},
		{"{{ template( }}", false},
		{42, false},
	} {
		if got := CallsTemplate(tt.v); got != tt.want {
			t.Errorf("CallsTemplate(%#v) = %v, want %v", tt.v, got, tt.want)
		}
	}
}

func TestRefsAndCallsTemplateLookAtKeys(t *testing.T) {
	v := map[string]any{"{{ vars.tenant }}": map[string]any{"{{ template(vars.t) }}": 1}}
	keys, _ := Refs(v, "vars")
	if !slices.Contains(keys, "tenant") || !slices.Contains(keys, "t") {
		t.Errorf("Refs() = %v, want the vars the keys read", keys)
	}
	if !CallsTemplate(v) {
		t.Error("CallsTemplate() = false, want the call in a key found")
	}
}
