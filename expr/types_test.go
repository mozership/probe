package expr

import (
	"errors"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/kind"
)

// typeNames are the shapes of what the expressions of TestCheckTypes read:
// a res whose fields are of the kinds of those of an http response, and
// more, and vars, which may hold anything.
var typeNames = map[string]Shape{
	"res": {Kind: kind.Map, Fields: map[string]Shape{
		"code":    {Kind: kind.Number},
		"status":  {Kind: kind.String},
		"ok":      {Kind: kind.Bool},
		"headers": {Kind: kind.Map},
		"rows":    {Kind: kind.List},
		"rt":      {Kind: kind.Map, Fields: map[string]Shape{"sec": {Kind: kind.Number}}},
	}},
	"status": {Kind: kind.Number},
	"vars":   {Kind: kind.Map},
}

// typeValues returns values of the shapes of typeNames, with code as the
// number of res, to evaluate the expressions against.
func typeValues(code any) map[string]any {
	return map[string]any{
		"res": map[string]any{
			"code":    code,
			"status":  "200 OK",
			"ok":      true,
			"headers": map[string]any{"a": "b"},
			"rows":    []any{int64(1), "a"},
			"rt":      map[string]any{"sec": 0.5},
			"body":    map[string]any{"id": int64(1)},
		},
		"status": 0,
		"vars":   map[string]any{"n": "1", "id": int64(1)},
	}
}

func TestCheckTypes(t *testing.T) {
	tests := []struct {
		input    string
		wantBool bool
		// want is a part of the error, and empty for none.
		want string
		// hint is a part of a hint of the error.
		hint string
	}{
		{`res.code == "200 OK"`, true, `res.code == "200 OK" is never true: res.code is a number and "200 OK" a string, which are never equal`, `compare res.code with a number, or compare string(res.code) with "200 OK"`},
		{`res.code == "200"`, true, `is never true`, `compare res.code with a number, as res.code == 200, or`},
		{`"200" == res.code`, true, `"200" == res.code is never true: "200" is a string and res.code a number`, `as res.code == 200`},
		{`res.code != "200"`, true, `res.code != "200" is always true`, `as res.code != 200`},
		{`res.ok == "true"`, true, `res.ok == "true" is never true: res.ok is a bool and "true" a string`, `compare a bool with true or false`},
		{`status == "0"`, true, `status == "0" is never true`, ``},
		{`len(res.rows) == "2"`, true, `len(res.rows) is a number and "2" a string`, ``},
		{`upper(res.status) == 1`, true, `upper(res.status) is a string and 1 a number`, ``},
		{`res.headers == 1`, true, `res.headers == 1 is never true: res.headers is a map and 1 a number`, ``},
		{`res.code in ["200", "201"]`, true, `is never true: res.code is a number and the elements of ["200", "201"] are strings`, ``},
		{`"2" in res.status`, true, `"2" in res.status fails: res.status is a string`, `write res.status contains "2"`},
		{`"a" in res.code`, true, `"a" in res.code fails: res.code is a number`, `in looks for an element of a list or a key of a map`},
		{`res.code + "x" == "200x"`, true, `res.code + "x" fails: res.code is a number and "x" a string, which + does not take together`, `string(res.code) + "x"`},
		{`"id-" + 1`, false, `"id-" + 1 fails`, `to join them as text, convert the number with string(): "id-" + string(1)`},
		{`res.code > "199"`, true, `res.code > "199" fails`, `> compares two numbers or two strings`},
		{`res.status * 2`, false, `res.status * 2 fails`, `* takes numbers`},
		{`res.rows contains 1`, true, `res.rows contains 1 fails`, `to look for an element of a list, write 1 in res.rows`},
		{`res.status contains 2`, true, `res.status contains 2 fails`, `contains takes strings: convert a value with string()`},
		{`!res.code`, true, `!res.code fails: res.code is a number`, `! takes true or false`},
		{`-res.status`, false, `-res.status fails: res.status is a string`, `- takes a number`},
		{`res.code ? "a" : "b"`, false, `res.code ? "a" : "b" fails: its condition, res.code, is a number, where ? : takes true or false`, ``},
		{`let c = res.code; c + "x"`, false, `c + "x" fails`, ``},
		{`res.rt.sec + "s"`, false, `res.rt.sec + "s" fails`, ``},
		{`res.status .. 3`, false, `res.status..3 fails`, `.. takes numbers`},
		{`res.code matches "^2"`, true, `res.code matches "^2" fails`, `matches takes strings`},
		{`res.code startsWith "2"`, true, `res.code startsWith "2" fails`, ``},
		{`res.status % 2`, false, `res.status % 2 fails`, ``},
		{`res.status ** 2`, false, `res.status ** 2 fails`, ``},
		{`res.rows < 1`, true, `res.rows < 1 fails`, ``},
		{`res.rows == res.headers`, true, `is never true: res.rows is a list and res.headers a map`, ``},
		{`res.code == duration("200ns")`, true, `is never true: res.code is a number and duration("200ns") a duration`, ``},
		{`now() == res.code`, true, `is never true: now() is a time and res.code a number`, ``},
		{`now() - res.code`, false, `now() - res.code fails`, ``},
		{`res.code`, true, `res.code is a number, not true or false`, `compare it with what it should be`},
		{`res.code + 1`, true, `res.code + 1 is a number, not true or false`, ``},
		{`res.status`, true, `res.status is a string, not true or false`, ``},
		{`res.headers`, true, `res.headers is a map, not true or false`, ``},
		{`filter(res.rows, # == 1)`, true, `is a list, not true or false`, ``},
		{`now()`, true, `now() is a time, not true or false`, ``},

		// Nothing is told of a value whose kind is not known, nor of what
		// goes as it should.
		{`res.code == 200`, true, ``, ``},
		{`res.code == 200.0 || res.code in [200, 201]`, true, ``, ``},
		{`res.code == nil`, true, ``, ``},
		{`res.status == "200 OK" && res.ok`, true, ``, ``},
		{`res.code / 2 == 100 && res.code % 2 == 0 && res.code ** 2 > 1`, true, ``, ``},
		{`string(res.code) == "200"`, true, ``, ``},
		{`"code " + string(res.code)`, false, ``, ``},
		{`res.body.id == 1 && res.body.name == "a"`, true, ``, ``},
		{`res.body + 1`, false, ``, ``},
		{`res.anything == "x"`, true, ``, ``},
		{`vars.n == 1 && vars.n == "1"`, true, ``, ``},
		{`vars.n + 1`, true, ``, ``},
		{`outputs.x + 1 == 2`, true, ``, ``},
		{`parse_int(res.status) == 200`, true, ``, ``},
		{`match_json(res.body, {"id": 1})`, true, ``, ``},
		{`random_int(10) + "x"`, false, ``, ``},
		{`res.code ?? "none"`, false, ``, ``},
		// and and or given a value that is not true or false come to one
		// of their operands, rather than failing.
		{`res.code and true`, false, ``, ``},
		{`+res.status`, false, ``, ``},
		{`res.code == 200 || res.status`, false, ``, ``},
		{`res.code == 200 ? "ok" : 1`, false, ``, ``},
		{`res.headers["a"] == 1`, true, ``, ``},
		{`"a" in res.headers`, true, ``, ``},
		{`res.rows[0] == "x"`, true, ``, ``},
		{`len(res.rows) > 0 && all(res.rows, # != nil)`, true, ``, ``},
		// The checker refuses an index of a string, which an evaluation
		// takes.
		{`res.status[0:3] == "200"`, true, ``, ``},
		{`res.status[0] == "2"`, true, ``, ``},
		// A name that is not there is told by check of its own.
		{`nope + 1`, false, ``, ``},
		{`nope(1)`, true, ``, ``},
		// An expression that does not parse is told by Parse.
		{`res.code ==`, true, ``, ``},
		{`{{ res.code == 200 }}`, true, ``, ``},
		{``, true, ``, ``},
	}
	for _, tt := range tests {
		err := CheckTypes(tt.input, typeNames, tt.wantBool)
		if tt.want == "" {
			if err != nil {
				t.Errorf("CheckTypes(%q) = %v, want nil", tt.input, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("CheckTypes(%q) = %v, want it to say %q", tt.input, err, tt.want)
			continue
		}
		h, ok := errors.AsType[*HintError](err)
		if !ok || len(h.Hints) == 0 {
			t.Errorf("CheckTypes(%q) = %v, want a hint", tt.input, err)
			continue
		}
		if tt.hint != "" && !strings.Contains(strings.Join(h.Hints, "\n"), tt.hint) {
			t.Errorf("CheckTypes(%q) hints %q, want one saying %q", tt.input, h.Hints, tt.hint)
		}
		if notBool := strings.Contains(tt.want, "not true or false"); errors.Is(err, ErrNotBool) != notBool {
			t.Errorf("CheckTypes(%q) = %v, which wraps ErrNotBool: %v, want %v", tt.input, err, !notBool, notBool)
		}

		// What is told goes wrong does, whether the number of res is
		// whole or not: the expression fails, or comes to the value told.
		for _, code := range []any{int64(200), 200.5} {
			out, evalErr := (&Expr{}).Eval(tt.input, typeValues(code))
			switch {
			case evalErr != nil:
			case strings.Contains(tt.want, "is never true"):
				if out != false {
					t.Errorf("%q with res.code %v = %v, which it is told is never", tt.input, code, out)
				}
			case strings.Contains(tt.want, "is always true"):
				if out != true {
					t.Errorf("%q with res.code %v = %v, which it is told is always true", tt.input, code, out)
				}
			case errors.Is(err, ErrNotBool):
				if _, isBool := out.(bool); isBool {
					t.Errorf("%q with res.code %v = %v, which it is told is not true or false", tt.input, code, out)
				}
			default:
				t.Errorf("%q with res.code %v = %v, which it is told fails", tt.input, code, out)
			}
		}
	}
}

// A shape with fields and no kind is a map, and a name that no shape gives
// may hold anything.
func TestCheckTypesShapes(t *testing.T) {
	names := map[string]Shape{"rt": {Fields: map[string]Shape{"sec": {Kind: kind.Number}}}}
	if err := CheckTypes(`rt.sec + "s"`, names, false); err == nil {
		t.Error(`rt.sec + "s" = nil, want an error: rt.sec is a number`)
	}
	for _, input := range []string{`rt.duration + 1`, `res.code + "x"`} {
		if err := CheckTypes(input, names, true); err != nil {
			t.Errorf("CheckTypes(%q) = %v, want nil", input, err)
		}
	}
	if err := CheckTypes(`"a" + 1`, nil, false); err == nil {
		t.Error(`"a" + 1 with no names = nil, want an error`)
	}
}
