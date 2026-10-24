package expr

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	for _, ok := range []string{`res.code == 200`, `all(res.body.items, #.active)`, `outputs['create-user'].id ?? ""`} {
		if err := Parse(ok); err != nil {
			t.Errorf("Parse(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{`res.code ==`, `res.code = 200`, `(res.code == 200`} {
		if err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) = nil, want an error", bad)
		}
	}
}

func TestTemplateExprs(t *testing.T) {
	got := TemplateExprs(`Bearer {{outputs.auth.token}} for {{ vars.name }}`)
	want := []string{"outputs.auth.token", " vars.name "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TemplateExprs = %q, want %q", got, want)
	}
	if got := TemplateExprs("no template"); got != nil {
		t.Errorf("TemplateExprs = %q, want none", got)
	}
}

func TestReads(t *testing.T) {
	tests := []struct {
		input string
		want  [][]string
	}{
		{`outputs.auth.token`, [][]string{{"auth"}, {"auth", "token"}}},
		{`outputs['create-user'].id`, [][]string{{"create-user"}, {"create-user", "id"}}},
		{`outputs.token`, [][]string{{"token"}}},
		{`outputs?.auth?.token ?? ""`, [][]string{{"auth"}, {"auth", "token"}, {"auth", "token"}}},
		{`outputs[vars.id].token`, nil},
		{`res.outputs.token`, nil},
		{`vars.outputs`, nil},
		{`outputs.auth.token ==`, nil},
	}
	for _, tt := range tests {
		if got := Reads(tt.input, "outputs"); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Reads(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadsByVariableKey(t *testing.T) {
	tests := []struct {
		input string
		want  [][]string
	}{
		{`outputs[vars.id].token`, [][]string{{}}},
		{`outputs.auth[vars.key]`, [][]string{{"auth"}}},
		{`outputs?.auth?.[vars.key] ?? ""`, [][]string{{"auth"}}},
		{`outputs.auth.token`, nil},
		{`outputs['create-user'].id`, nil},
		{`vars.outputs[k]`, nil},
		{`outputs[k] ==`, nil},
	}
	for _, tt := range tests {
		if got := ReadsByVariableKey(tt.input, "outputs"); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ReadsByVariableKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadsNothing(t *testing.T) {
	tests := map[string]bool{
		`true`:                          true,
		`1 == 1`:                        true,
		`len("abc") == 3`:               true,
		`res.code == 200`:               false,
		`status == 0`:                   false,
		`len(res.body) > 0`:             false,
		`all([1, 2], # > 0)`:            false,
		`res.code ==`:                   false,
		`upper("a") == lower("A") + ""`: true,
	}
	for input, want := range tests {
		if got := ReadsNothing(input); got != want {
			t.Errorf("ReadsNothing(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{`res.code == 200 && vars.id == outputs.login.id`, []string{"res", "vars", "outputs"}},
		{`steps.login.code == 1`, []string{"steps"}},
		{`len(res.body) > 0 && now() != nil`, []string{"res"}},
		{`all(res.body.items, #.active) && any(res.body.items, .id == vars.id)`, []string{"res", "vars"}},
		{`let n = len(res.body); n > 0 && n < limit`, []string{"res", "limit"}},
		{`{a: res.code}.a == 200`, []string{"res"}},
		{`$env.res.code == 200`, nil},
		{`1 == 1`, nil},
		{`res.code ==`, nil},
	}
	for _, tt := range tests {
		if got := Names(tt.input); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Names(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
