package probe

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
)

func TestExchangeDetail(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := NewPrinter(false)

	tests := []struct {
		name     string
		testExpr string
		result   any
		req      map[string]any
		res      map[string]any
		expected string
	}{
		{
			name:     "simple test failure",
			testExpr: "res.status == 200",
			result:   false,
			req:      map[string]any{"method": "GET", "url": "http://example.com"},
			res:      map[string]any{"status": 404, "body": "Not Found"},
			expected: "       request:\n         method: GET\n         url: http://example.com\n       response:\n         body: Not Found\n         status: 404\n",
		},
		{
			name:     "empty maps",
			testExpr: "true",
			result:   false,
			req:      map[string]any{},
			res:      map[string]any{},
			expected: "       request: {}\n       response: {}\n",
		},
		{
			name:     "nil maps",
			testExpr: "false",
			result:   false,
			req:      nil,
			res:      nil,
			expected: "",
		},
		{
			name: "empty fields are left out",
			req:  map[string]any{"body": "", "headers": map[string]any{}, "method": "GET"},
			res:  map[string]any{"code": 0, "cookies": map[string]any{}, "filepath": "", "ok": false},
			expected: "       request:\n         method: GET\n" +
				"       response:\n         code: 0\n         ok: false\n",
		},
		{
			name: "a multi-line body is a block, cut",
			req:  map[string]any{"cmd": "seq 100"},
			res:  map[string]any{"stdout": strings.Repeat("line\n", 30)},
			expected: "       request:\n         cmd: seq 100\n" +
				"       response:\n         stdout: |\n" +
				strings.Repeat("           line\n", 20) +
				"           … (10 more lines)\n",
		},
		{
			name:     "a long body is cut",
			res:      map[string]any{"body": strings.Repeat("x", 600)},
			expected: "       response:\n         body: " + strings.Repeat("x", 512) + " … (88 more bytes)\n",
		},
		{
			name: "only the fields of the request and the response are left out when empty",
			res: map[string]any{
				"body":    map[string]any{"id": "1", "name": "", "items": []any{}, "err": nil},
				"cookies": map[string]any{},
			},
			expected: "       response:\n         body:\n           err: null\n           id: \"1\"\n" +
				"           items: []\n           name: \"\"\n",
		},
		{
			name:     "a decoded body is not shown again as rawbody",
			res:      map[string]any{"body": map[string]any{"id": 1}, "rawbody": `{"id":1}`},
			expected: "       response:\n         body:\n           id: 1\n",
		},
		{
			name:     "dump false",
			req:      map[string]any{"method": "GET"},
			res:      map[string]any{"dump": false, "body": "secret"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exchangeDetail(printer.Masker(), tt.req, tt.res)
			if result != tt.expected {
				t.Errorf("exchangeDetail() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTestErrorDetail(t *testing.T) {

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "simple error",
			err:      fmt.Errorf("compilation error"),
			expected: "       test error: compilation error\n",
		},
		{
			name:     "complex error message",
			err:      fmt.Errorf("invalid expression: res.status == \"200\""),
			expected: "       test error: invalid expression: res.status == \"200\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := testErrorDetail(tt.err)
			if result != tt.expected {
				t.Errorf("testErrorDetail() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTypeMismatchDetail(t *testing.T) {

	tests := []struct {
		name     string
		testExpr string
		result   any
		expected string
	}{
		{
			name:     "string result instead of bool",
			testExpr: "res.status",
			result:   "200",
			expected: "       Test: `res.status` = 200\n",
		},
		{
			name:     "number result instead of bool",
			testExpr: "res.code",
			result:   404,
			expected: "       Test: `res.code` = 404\n",
		},
		{
			name:     "map result instead of bool",
			testExpr: "res.body",
			result:   map[string]any{"error": "not found"},
			expected: "       Test: `res.body` = map[error:not found]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := typeMismatchDetail(tt.testExpr, tt.result)
			if result != tt.expected {
				t.Errorf("typeMismatchDetail() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFalseClauseDetail(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := NewPrinter(false)
	printer.SetMasker(mask.New(nil, nil))
	printer.Masker().Learn(map[string]any{"headers": map[string]any{"authorization": "Bearer s3cr3t-token"}})

	tests := []struct {
		name   string
		clause expr.Clause
		want   string
	}{
		{
			name: "a comparison and what it read",
			clause: expr.Clause{Expr: `res.code == 200`, Operands: []expr.Operand{
				{Expr: "res.code", Value: 500},
			}},
			want: "       failed: res.code == 200\n         res.code = 500\n",
		},
		{
			name:   "a condition with nothing to show",
			clause: expr.Clause{Expr: `res.code == 200 || res.code == 201`},
			want:   "       failed: res.code == 200 || res.code == 201\n",
		},
		{
			name: "a secret a value holds is hidden",
			clause: expr.Clause{Expr: `res.body.token == vars.token`, Operands: []expr.Operand{
				{Expr: "res.body.token", Value: "Bearer s3cr3t-token"},
			}},
			want: "       failed: res.body.token == vars.token\n         res.body.token = \"" + printer.Masker().String("Bearer s3cr3t-token") + "\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := falseClauseDetail(maskClause(printer.Masker(), tt.clause)); got != tt.want {
				t.Errorf("falseClauseDetail() = %q, want %q", got, tt.want)
			}
		})
	}
	if strings.Contains(tests[2].want, "s3cr3t-token") {
		t.Fatalf("the masker did not hide the secret: %q", tests[2].want)
	}
}

// A secret is hidden before its value is written out and cut: cut first, or
// with its quotes escaped, it is no longer the text the masker knows.
func TestMaskClauseBeforeCutting(t *testing.T) {
	long := strings.Repeat("k", maxOperandLength+50)
	quoted := `pa"ss\word`
	printer := NewPrinter(false)
	printer.SetMasker(mask.New(nil, nil))
	printer.Masker().Learn(map[string]any{"headers": map[string]any{"authorization": long}, "password": quoted})

	clause := maskClause(printer.Masker(), expr.Clause{Expr: `res.body.token == "` + long + `"`, Operands: []expr.Operand{
		{Expr: "res.body.token", Value: long},
		{Expr: "res.body", Value: map[string]any{"list": []any{quoted}, "nested": map[string]any{"token": long}}},
	}})

	for _, out := range []string{falseClauseDetail(clause), clauseSummary(clause)} {
		if strings.Contains(out, strings.Repeat("k", 20)) {
			t.Errorf("a cut secret is shown: %q", out)
		}
		if strings.Contains(out, "ss") {
			t.Errorf("a secret with a quote is shown: %q", out)
		}
	}
}

// A URL whose credentials stand where a long value is cut is hidden before
// the cut, which would otherwise leave a part of them that no scan finds.
func TestMaskClauseHidesURLCredentialsBeforeCutting(t *testing.T) {
	printer := NewPrinter(false)
	printer.SetMasker(mask.New(nil, nil))
	pad := strings.Repeat("x", maxOperandLength-30)
	body := map[string]any{"pad": pad, "next": "https://x/?user=a&password=hunter2-secret&page=2"}
	text := pad + " postgres://app:hunter2-secret@db/main"

	clause := maskClause(printer.Masker(), expr.Clause{Expr: "res.body == {}", Operands: []expr.Operand{
		{Expr: "res.body", Value: body},
		{Expr: "res.text", Value: text},
	}})
	for _, out := range []string{falseClauseDetail(clause), clauseSummary(clause)} {
		if strings.Contains(out, "hun") {
			t.Errorf("a part of a URL's password is shown: %q", out)
		}
	}
}

func TestIndentDetail(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"one line", "connection refused", "       connection refused\n"},
		{"several lines", "with.cmd: {{x.y}}: cannot fetch y\n | x.y\n | ..^", "       with.cmd: {{x.y}}: cannot fetch y\n        | x.y\n        | ..^\n"},
		{"surrounding newlines", "\nmessage\n", "       message\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indentDetail(tt.in); got != tt.want {
				t.Errorf("indentDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOperandText(t *testing.T) {
	tests := []struct {
		name    string
		operand expr.Operand
		want    string
	}{
		{"nil", expr.Operand{Expr: "res.body.id", Value: nil}, "res.body.id = nil"},
		{"a string is quoted", expr.Operand{Expr: "res.stdout", Value: "ok\n"}, `res.stdout = "ok\n"`},
		{"an empty string", expr.Operand{Expr: "vars.name", Value: ""}, `vars.name = ""`},
		{"a number", expr.Operand{Expr: "rt.sec", Value: 0.25}, "rt.sec = 0.25"},
		{"a bool", expr.Operand{Expr: "res.complete", Value: false}, "res.complete = false"},
		{"a list", expr.Operand{Expr: "res.values", Value: []any{"a", 1}}, `res.values = ["a",1]`},
		{"a map", expr.Operand{Expr: "res.body", Value: map[string]any{"ok": true}}, `res.body = {"ok":true}`},
		{"a long value is cut", expr.Operand{Expr: "res.body", Value: strings.Repeat("x", 300)}, `res.body = "` + strings.Repeat("x", maxOperandLength-1) + "..."},
		{"a value JSON cannot hold", expr.Operand{Expr: "f", Value: func() {}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := operandText(tt.operand)
			if tt.want == "" {
				if !strings.HasPrefix(got, "f = 0x") {
					t.Errorf("operandText() = %q, want the value as %%v writes it", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("operandText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClauseSummary(t *testing.T) {
	got := clauseSummary(expr.Clause{Expr: `res.body.name == vars.name`, Operands: []expr.Operand{
		{Expr: "res.body.name", Value: "alice"},
		{Expr: "vars.name", Value: "bob"},
	}})
	if want := `res.body.name == vars.name (res.body.name = "alice", vars.name = "bob")`; got != want {
		t.Errorf("clauseSummary() = %q, want %q", got, want)
	}
	if got := clauseSummary(expr.Clause{Expr: "1 == 2"}); got != "1 == 2" {
		t.Errorf("clauseSummary() = %q, want the condition alone", got)
	}
}
