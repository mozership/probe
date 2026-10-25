package probe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/dump"
	"github.com/mozership/probe/mask"
)

// The details shown under a step, or under a test of a repeated job, say
// why it failed or what it echoed. They are text for the report, and hide
// the secrets they are given a masker for in the data, before it is laid
// out.

// indentDetail indents every line of s, including those after a newline in
// it, to the column of a step's name, where the details printed under a step
// start. The result ends with a newline.
func indentDetail(s string) string {
	indent := "       "
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n") + "\n"
}

// exchangeOptions are how the request and the response of a failed step are
// laid out: shortened, so that a large body cannot bury why the step failed,
// and without the empty fields every action fills in, such as an empty body
// or no cookies. -v shows them all, uncut.
var exchangeOptions = dump.Options{
	MaxBytes:  512,
	MaxLines:  20,
	MaxItems:  30,
	OmitEmpty: true,
}

// exchangeDetail formats the request and the response of a step that
// failed, unless the response has dump set to false:
//
//	request:
//	  method: GET
//	  url: https://example.com/
//	response:
//	  body: |
//	    <!doctype html>
//	    … (1234 more bytes)
//	  code: 200
func exchangeDetail(m *mask.Masker, req, res map[string]any) string {
	// Check if response has dump field set to false
	if res != nil {
		if d, exists := res["dump"]; exists {
			if dumpBool, ok := d.(bool); ok && !dumpBool {
				return "" // Don't dump request/response if dump is false
			}
		}
	}

	// Mask the data, not the text it is laid out as: a secret that is cut,
	// or whose control characters are escaped, is no longer the text the
	// masker knows.
	var b strings.Builder
	writeMapDetail(&b, "request:", m.Map(req))
	writeMapDetail(&b, "response:", m.Map(withoutRawBody(res)))
	return b.String()
}

// writeMapDetail writes data under its label, at the column of a step's
// name, as exchangeDetail lays it out. An action that told none is not
// written at all.
func writeMapDetail(b *strings.Builder, label string, data map[string]any) {
	if data == nil {
		return
	}
	lines := dump.Lines(data, exchangeOptions)
	if len(lines) == 0 {
		fmt.Fprintf(b, "       %s {}\n", colorInfo().Sprintf("%s", label))
		return
	}
	fmt.Fprintf(b, "       %s\n", colorInfo().Sprintf("%s", label))
	for _, l := range lines {
		fmt.Fprintf(b, "         %s\n", l)
	}
}

// withoutRawBody returns res without rawbody when its body was decoded from
// it: the decoded body says the same, laid out key by key.
func withoutRawBody(res map[string]any) map[string]any {
	if _, ok := res["rawbody"]; !ok {
		return res
	}
	if _, text := res["body"].(string); text {
		return res
	}
	out := make(map[string]any, len(res))
	for k, v := range res {
		if k != "rawbody" {
			out[k] = v
		}
	}
	return out
}

// falseClauseDetail formats the condition of a test that is false, and
// under it what each thing it compares evaluated to:
//
//	failed: res.code == 200
//	  res.code = 500
//
// c is a condition maskClause has hidden the secrets of.
func falseClauseDetail(c expr.Clause) string {
	var b strings.Builder
	fmt.Fprintf(&b, "       %s %s\n", colorInfo().Sprintf("failed:"), c.Expr)
	for _, o := range c.Operands {
		fmt.Fprintf(&b, "         %s\n", operandText(o))
	}
	return b.String()
}

// maskClause returns c with the secrets m knows hidden in the condition and
// in what its operands evaluated to. The values are masked as data, before
// they are written out and cut: a secret that is cut, or whose quotes are
// escaped, is no longer the text the masker knows.
func maskClause(m *mask.Masker, c expr.Clause) expr.Clause {
	out := expr.Clause{Expr: m.String(c.Expr)}
	for _, o := range c.Operands {
		out.Operands = append(out.Operands, expr.Operand{Expr: m.String(o.Expr), Value: m.Value(o.Value)})
	}
	return out
}

// maxOperandLength is how much of a value a false condition compared is
// shown, as a whole response body may be one.
const maxOperandLength = 200

// operandText is an operand and its value on one line, as res.code = 500.
// A string is quoted, so that an empty one and one that ends with a newline
// can be seen, and anything else is written as JSON.
func operandText(o expr.Operand) string {
	var value string
	switch v := o.Value.(type) {
	case nil:
		value = "nil"
	case string:
		value = strconv.Quote(v)
	default:
		if data, err := json.Marshal(v); err == nil {
			value = string(data)
		} else {
			value = fmt.Sprintf("%v", v)
		}
	}
	if r := []rune(value); len(r) > maxOperandLength {
		value = string(r[:maxOperandLength]) + "..."
	}
	return o.Expr + " = " + value
}

// clauseSummary is a false condition on one line, for the message of a
// failure: res.code == 200 (res.code = 500).
func clauseSummary(c expr.Clause) string {
	if len(c.Operands) == 0 {
		return c.Expr
	}
	parts := make([]string, len(c.Operands))
	for i, o := range c.Operands {
		parts[i] = operandText(o)
	}
	return fmt.Sprintf("%s (%s)", c.Expr, strings.Join(parts, ", "))
}

// contractDetail formats a response that breaks its contract: each
// violation, then the request and response as a failed test shows them.
func contractDetail(m *mask.Masker, f *StepFailure) string {
	var b strings.Builder
	for _, v := range f.Violations {
		b.WriteString(indentDetail(m.String(v.String())))
	}
	b.WriteString(exchangeDetail(m, f.Request, f.Response))
	return b.String()
}

// testErrorDetail formats the error a test could not be evaluated for, by
// its message.
func testErrorDetail(message string) string {
	return indentDetail("test error: " + message)
}

// typeMismatchText is what a test that did not evaluate to a boolean is
// told by.
func typeMismatchText(testExpr string, result any) string {
	return fmt.Sprintf("Test: `%s` = %v\n", testExpr, result)
}

// typeMismatchDetail formats a test that did not evaluate to a boolean.
func typeMismatchDetail(testExpr string, result any) string {
	return indentDetail(typeMismatchText(testExpr, result))
}
