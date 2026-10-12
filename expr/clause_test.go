package expr

import (
	"reflect"
	"testing"
)

func TestFalseClause(t *testing.T) {
	env := map[string]any{
		"res": map[string]any{
			"code": 500,
			"body": map[string]any{
				"name":  "alice",
				"items": []any{"a", "b"},
				"ok":    true,
			},
		},
		"vars": map[string]any{"name": "bob"},
	}

	tests := []struct {
		name  string
		input string
		want  Clause
		found bool
	}{
		{
			name:  "one condition",
			input: `res.code == 200`,
			want:  Clause{Expr: `res.code == 200`, Operands: []Operand{{Expr: "res.code", Value: 500}}},
			found: true,
		},
		{
			name:  "the first false condition of several",
			input: "res.body.ok &&\nres.body.name == vars.name &&\nres.code == 200",
			want: Clause{Expr: `res.body.name == vars.name`, Operands: []Operand{
				{Expr: "res.body.name", Value: "alice"},
				{Expr: "vars.name", Value: "bob"},
			}},
			found: true,
		},
		{
			name:  "and is &&",
			input: `res.body.ok and res.code < 400`,
			want:  Clause{Expr: `res.code < 400`, Operands: []Operand{{Expr: "res.code", Value: 500}}},
			found: true,
		},
		{
			name:  "conditions in parentheses are told apart",
			input: `res.body.ok && (len(res.body.items) == 2 && res.code == 200)`,
			want:  Clause{Expr: `res.code == 200`, Operands: []Operand{{Expr: "res.code", Value: 500}}},
			found: true,
		},
		{
			name:  "an or is one condition",
			input: `res.body.ok && (res.code == 200 || res.code == 201)`,
			want:  Clause{Expr: `res.code == 200 || res.code == 201`},
			found: true,
		},
		{
			name:  "what a not negates",
			input: `!res.body.ok`,
			want:  Clause{Expr: `!res.body.ok`, Operands: []Operand{{Expr: "res.body.ok", Value: true}}},
			found: true,
		},
		{
			name:  "a call that is compared",
			input: `len(res.body.items) == 3`,
			want:  Clause{Expr: `len(res.body.items) == 3`, Operands: []Operand{{Expr: "len(res.body.items)", Value: 2}}},
			found: true,
		},
		{
			name:  "an operator in words",
			input: `res.body.name startsWith "b"`,
			want:  Clause{Expr: `res.body.name startsWith "b"`, Operands: []Operand{{Expr: "res.body.name", Value: "alice"}}},
			found: true,
		},
		{
			name:  "literals on both sides",
			input: `1 == 2`,
			want:  Clause{Expr: `1 == 2`},
			found: true,
		},
		{
			name:  "a list of literals is a literal",
			input: `res.code in [200, -1]`,
			want:  Clause{Expr: `res.code in [200, -1]`, Operands: []Operand{{Expr: "res.code", Value: 500}}},
			found: true,
		},
		{
			name:  "a map of literals is a literal",
			input: `res.body == {"name": "alice", "tags": ["a"]}`,
			want: Clause{Expr: `res.body == {name: "alice", tags: ["a"]}`, Operands: []Operand{
				{Expr: "res.body", Value: map[string]any{"name": "alice", "items": []any{"a", "b"}, "ok": true}},
			}},
			found: true,
		},
		{
			name:  "a map that reads a value is not a literal",
			input: `{"name": vars.name} == {"name": "alice"}`,
			want: Clause{Expr: `{name: vars.name} == {name: "alice"}`, Operands: []Operand{
				{Expr: `{name: vars.name}`, Value: map[string]any{"name": "bob"}},
			}},
			found: true,
		},
		{
			name:  "a condition that is nil",
			input: `res.body.ok && res.body.missing`,
			want:  Clause{Expr: `res.body.missing`},
			found: true,
		},
		{
			name:  "every condition is true",
			input: `res.body.ok && res.code == 500`,
		},
		{
			name:  "an expression that does not parse",
			input: `res.code ==`,
		},
		{
			name:  "a condition that cannot be evaluated",
			input: `res.body.ok && nothing.here == 1`,
		},
		{
			name:  "a function that returns another value each time",
			input: `random_int(2) == 5 && res.code == 200`,
		},
		{
			name:  "such a function at any depth",
			input: `res.code == 200 && len(random_str(4)) == 5`,
		},
		{
			name:  "the time, which changes",
			input: `res.code == 200 && now().Year() > 3000`,
		},
		{
			name:  "the time in seconds",
			input: `unixtime() < res.code`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Expr{}
			got, found := e.FalseClause(tt.input, env)
			if found != tt.found {
				t.Fatalf("found = %v, want %v (clause %+v)", found, tt.found, got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clause = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A condition after the false one is not evaluated, as && does not evaluate
// it: it may read what the false one guards.
func TestFalseClauseStopsAtTheFirst(t *testing.T) {
	env := map[string]any{"res": map[string]any{"body": nil}}
	e := &Expr{}
	got, found := e.FalseClause(`res.body != nil && res.body.items[0] == "a"`, env)
	if !found || got.Expr != `res.body != nil` {
		t.Fatalf("clause = %+v, found = %v", got, found)
	}
	if want := []Operand{{Expr: "res.body", Value: nil}}; !reflect.DeepEqual(got.Operands, want) {
		t.Errorf("operands = %+v, want %+v", got.Operands, want)
	}
}
