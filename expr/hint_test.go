package expr

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// evalHints returns the hints of input as it fails against hintEnv, or as
// it would if it failed somewhere other than where .length is read.
func evalHints(input string) []string {
	_, err := (&Expr{}).Eval(input, hintEnv())
	if err == nil {
		err = errors.New("failed")
	}
	return Hints(input, err)
}

func hintEnv() map[string]any {
	return map[string]any{
		"res": map[string]any{
			"code":  200,
			"body":  "Hello World",
			"items": []any{"a", "b"},
			"json":  map[string]any{"contains": "x", "length": 3},
		},
	}
}

// TestHints tells each expression written as another language would write
// it how expressions write it, and runs what it is told to write, which must
// evaluate to what the expression was meant to.
func TestHints(t *testing.T) {
	tests := []struct {
		input string
		hint  string
		// rewrite is the expression the hint tells to write, and want what
		// it evaluates to.
		rewrite string
		want    any
	}{
		{`res.body.contains("World")`, `expressions have no contains() method: write res.body contains "World" for a string, or "World" in res.body for a list`, `res.body contains "World"`, true},
		{`res.items.includes("a")`, `expressions have no includes() method: write res.items contains "a" for a string, or "a" in res.items for a list`, `"a" in res.items`, true},
		{`res.body.startsWith("Hello")`, `expressions have no startsWith() method: write res.body startsWith "Hello"`, `res.body startsWith "Hello"`, true},
		{`res.body.endsWith("World")`, `expressions have no endsWith() method: write res.body endsWith "World"`, `res.body endsWith "World"`, true},
		{`res.body.matches("^H.*d$")`, `expressions have no matches() method: write res.body matches "^H.*d$"`, `res.body matches "^H.*d$"`, true},
		{`res.body.length > 0`, `expressions take a length with len(): write len(res.body)`, `len(res.body)`, 11},
		{`res.items.size() == 2`, `expressions have no size() method: write len(res.items)`, `len(res.items)`, 2},
		{`res.items.len()`, `expressions have no len() method: write len(res.items)`, `len(res.items)`, 2},
		{`res.body.toLowerCase()`, `expressions have no toLowerCase() method: write lower(res.body)`, `lower(res.body)`, "hello world"},
		{`res.body.lower()`, `expressions have no lower() method: write lower(res.body)`, `lower(res.body)`, "hello world"},
		{`res.body.toUpperCase()`, `expressions have no toUpperCase() method: write upper(res.body)`, `upper(res.body)`, "HELLO WORLD"},
		{`res.body.upper()`, `expressions have no upper() method: write upper(res.body)`, `upper(res.body)`, "HELLO WORLD"},
		{`res.body.trim()`, `expressions have no trim() method: write trim(res.body)`, `trim(res.body)`, "Hello World"},
		{`res.body.strip()`, `expressions have no strip() method: write trim(res.body)`, `trim(res.body)`, "Hello World"},
		{`res.code.toString()`, `expressions have no toString() method: write string(res.code)`, `string(res.code)`, "200"},
		{`res.body.split(" ")`, `expressions have no split() method: write split(res.body, " ")`, `split(res.body, " ")`, []string{"Hello", "World"}},
		{`res.items.join(",")`, `expressions have no join() method: write join(res.items, ",")`, `join(res.items, ",")`, "a,b"},
		{`",".join(res.items)`, `expressions have no join() method: write join(res.items, ",")`, `join(res.items, ",")`, "a,b"},
		{`res.body.indexOf("o")`, `expressions have no indexOf() method: write indexOf(res.body, "o")`, `indexOf(res.body, "o")`, 4},
		{`res.body.lastIndexOf("o")`, `expressions have no lastIndexOf() method: write lastIndexOf(res.body, "o")`, `lastIndexOf(res.body, "o")`, 7},
		{`res.body.replace("o", "0")`, `expressions have no replace() method: write replace(res.body, "o", "0"), which replaces every match`, `replace(res.body, "o", "0")`, "Hell0 W0rld"},
		{`res.body.replaceAll("o", "0")`, `expressions have no replaceAll() method: write replace(res.body, "o", "0")`, `replace(res.body, "o", "0")`, "Hell0 W0rld"},
		{`res.json.keys()`, `expressions have no keys() method: write keys(res.json)`, `len(keys(res.json))`, 2},
		{`(res.body + "!").endsWith("!")`, `expressions have no endsWith() method: write (res.body + "!") endsWith "!"`, `(res.body + "!") endsWith "!"`, true},
		{`res?.body?.contains("World")`, `expressions have no contains() method: write res?.body contains "World" for a string`, `res?.body contains "World"`, true},
		{`all(res.items, #.trim() != "")`, `expressions have no trim() method: write trim(#)`, `all(res.items, trim(#) != "")`, true},
		{`res.code === 200`, `expressions have no ===: write ==`, `res.code == 200`, true},
		{`res.body.contains("World") === true`, `expressions have no ===: write ==`, `res.body contains "World" == true`, true},
		{`res?.body?.startsWith("Hello") === true`, `expressions have no ===: write ==`, `res?.body startsWith "Hello" == true`, true},
		{`res.body.trim().length > 0`, `expressions have no trim() method: write trim(res.body)`, `len(trim(res.body)) > 0`, true},
		{`res.code !== 404`, `expressions have no !==: write !=`, `res.code != 404`, true},
		{`startsWith(res.body, "Hello")`, `startsWith is an operator: write res.body startsWith "Hello", or hasPrefix(res.body, "Hello")`, `hasPrefix(res.body, "Hello")`, true},
		{`res.code > 0 && endsWith(res.body, "World")`, `endsWith is an operator: write res.body endsWith "World", or hasSuffix(res.body, "World")`, `hasSuffix(res.body, "World")`, true},
		{`contains(res.body + "!", "!")`, `contains is an operator: write (res.body + "!") contains "!"`, `(res.body + "!") contains "!"`, true},
		{`matches(res.body, "W")`, `matches is an operator: write res.body matches "W"`, `res.body matches "W"`, true},
		{`res.code == null`, `expressions have no null: write nil`, `res.nope == nil`, true},
		{`res.code == undefined`, `expressions have no undefined: write nil`, `res.nope == nil`, true},
		{`res.code != None`, `expressions have no None: write nil`, `res.code != nil`, true},
		{`True`, `expressions have no True: write true`, `true`, true},
		{`False`, `expressions have no False: write false`, `false`, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hints := evalHints(tt.input)
			if len(hints) != 1 || !strings.HasPrefix(hints[0], tt.hint) {
				t.Errorf("Hints(%q) = %q, want one starting %q", tt.input, hints, tt.hint)
			}
			got, err := (&Expr{}).Eval(tt.rewrite, hintEnv())
			if err != nil {
				t.Fatalf("Eval(%q) = %v", tt.rewrite, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tt.rewrite, got, tt.want)
			}
		})
	}
}

// TestHints_None tells nothing of expressions written as expressions write
// them, a key named as a method is read without calling it, nor of a call
// of a method with arguments a hint cannot be written for.
func TestHints_None(t *testing.T) {
	for _, input := range []string{
		`res.body contains "World"`,
		`res.json.contains == "x"`,
		`hasPrefix(res.body, "Hello") && res.body startsWith "Hello"`,
		`len(res.items) > 0 && lower(res.body) != "" && trim(res.body) != ""`,
		`res.code == nil || res.code == 200`,
		`"null" in res.items`,
		`res.body.trim("x", "y")`,
		// It fails comparing 3 with "a", not reading .length.
		`res.json.length > "a"`,
		`res.code == 200 &&`,
		`res.code = 200`,
	} {
		if hints := evalHints(input); len(hints) > 0 {
			t.Errorf("Hints(%q) = %q, want none", input, hints)
		}
	}
}

// TestMethodCallHints tells only the calls of methods expressions do not
// have, which fail whatever they are called on, and not what fails only on
// some values.
func TestMethodCallHints(t *testing.T) {
	got := MethodCallHints(`res.body.contains("x") && res.body.length > 0 && res.code != null && res.json.contains == "x" && res.body.trim() != ""`)
	want := []string{
		`expressions have no contains() method: write res.body contains "x" for a string, or "x" in res.body for a list`,
		`expressions have no trim() method: write trim(res.body)`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MethodCallHints = %q, want %q", got, want)
	}
	if got := MethodCallHints(`res.code === 200`); got != nil {
		t.Errorf("MethodCallHints of an expression that does not parse = %q, want none", got)
	}
}

// TestEval_Hints keeps the error an expression fails with and adds the hint
// after it, when it is evaluated, when it is a template, and when it is
// parsed.
func TestEval_Hints(t *testing.T) {
	const hint = `hint: expressions have no contains() method: write res.body contains "x" for a string, or "x" in res.body for a list`

	_, err := (&Expr{}).Eval(`res.body.contains("x")`, hintEnv())
	if err == nil {
		t.Fatal("Eval = nil, want an error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "invalid operation: int(string) (1:10)\n | res.body.contains(\"x\")\n | .........^") || !strings.HasSuffix(msg, "\n"+hint) {
		t.Errorf("Eval error = %q, want the error of expr and then %q", msg, hint)
	}
	if h, ok := errors.AsType[*HintError](err); !ok || len(h.Hints) != 1 {
		t.Errorf("Eval error = %#v, want a *HintError with one hint", err)
	}

	_, err = (&Expr{}).EvalTemplate(`q={{ res.body.contains("x") }}`, hintEnv())
	var te *TemplateError
	if !errors.As(err, &te) || !strings.HasSuffix(err.Error(), "\n"+hint) {
		t.Errorf("EvalTemplate error = %v, want a *TemplateError ending %q", err, hint)
	}

	_, err = (&Expr{}).EvalTemplateWithTypePreservation(`{{ res.body.length > 0 }}`, hintEnv())
	if err == nil || !strings.HasSuffix(err.Error(), "\nhint: expressions take a length with len(): write len(res.body)") {
		t.Errorf("EvalTemplateWithTypePreservation error = %v, want the hint for .length", err)
	}

	err = Parse(`res.code === 200`)
	if err == nil || !strings.HasPrefix(err.Error(), `unexpected token Operator("=") (1:12)`) || !strings.HasSuffix(err.Error(), "\nhint: expressions have no ===: write ==") {
		t.Errorf("Parse error = %v, want the error of expr and then the hint for ===", err)
	}

	// An error of an expression written as expressions write it is left
	// as it is.
	_, err = (&Expr{}).Eval(`res.body contains 1`, hintEnv())
	if err == nil {
		t.Fatal("Eval = nil, want an error")
	}
	if _, ok := errors.AsType[*HintError](err); ok || strings.Contains(err.Error(), "hint:") {
		t.Errorf("Eval error = %v, want no hint", err)
	}

	// A key named as a method is read as it is: .length, which Hints
	// tells of, is told only of an expression that fails.
	for input, want := range map[string]any{`res.json.contains`: "x", `res.json.length`: 3} {
		got, err := (&Expr{}).Eval(input, hintEnv())
		if err != nil || got != want {
			t.Errorf("Eval(%s) = %v, %v, want %v", input, got, err, want)
		}
	}
}
