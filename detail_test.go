package probe

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
)

func TestEchoDetail(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name     string
		content  string
		err      error
		expected string
	}{
		{
			name:     "single line content",
			content:  "Hello World",
			err:      nil,
			expected: "       Hello World\n",
		},
		{
			name:     "multi-line content with explicit newlines",
			content:  "Line 1\nLine 2\nLine 3",
			err:      nil,
			expected: "       Line 1\n       Line 2\n       Line 3\n",
		},
		{
			name:     "complex multiline with indentation",
			content:  "Header\n  Indented\n    More indented\nBack to left",
			err:      nil,
			expected: "       Header\n         Indented\n           More indented\n       Back to left\n",
		},
		{
			name:     "empty line handling",
			content:  "Line 1\n\nLine 3",
			err:      nil,
			expected: "       Line 1\n       \n       Line 3\n",
		},
		{
			name:     "error case",
			content:  "",
			err:      fmt.Errorf("template error"),
			expected: "       error: template error\n",
		},
		{
			name:     "empty content",
			content:  "",
			err:      nil,
			expected: "       \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := echoDetail(tt.content, tt.err)
			if result != tt.expected {
				t.Errorf("echoDetail() = %q, want %q", result, tt.expected)
			}
		})
	}
}

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
