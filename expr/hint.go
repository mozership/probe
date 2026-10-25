package expr

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/parser/lexer"
)

// HintError is an error of an expression written as another language, such
// as JavaScript or Python, would write it, with how expressions write it.
type HintError struct {
	Err   error
	Hints []string
}

func (e *HintError) Error() string {
	var b strings.Builder
	b.WriteString(e.Err.Error())
	for _, h := range e.Hints {
		b.WriteString("\nhint: ")
		b.WriteString(h)
	}
	return b.String()
}

func (e *HintError) Unwrap() error {
	return e.Err
}

// withHints returns err, the error of input, with how input is likely meant
// to be written when it is written as another language would write it.
func withHints(input string, err error) error {
	if err == nil {
		return nil
	}
	if hints := Hints(input, err); len(hints) > 0 {
		return &HintError{Err: err, Hints: hints}
	}
	return err
}

// Hints returns how input, an expression that fails with err, is likely
// meant to be written where it is written as another language would write
// it: a method expressions do not have, such as res.body.contains("x") for
// res.body contains "x", .length for len(), === for ==, or null for nil.
// Some of these, such as .length or null, are not wrong in themselves, a
// map may have a key named length, so they are told only of an expression
// that fails, and .length only where err says it fails.
func Hints(input string, err error) []string {
	tree, perr := parser.Parse(input)
	if perr != nil {
		hints := operatorHints(input)
		// An expression written as a template, as {{ res.code == 200 }}, is
		// read as a map in a map.
		if inner := extractTemplateExpression(input); inner != "" {
			if _, err := parser.Parse(inner); err == nil {
				hints = append([]string{fmt.Sprintf("an expression is written without {{ }}: write %s", inner)}, hints...)
			}
		}
		return hints
	}
	f := &mistakeFinder{callees: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, f)
	hints := f.calls
	// The error of reading .length is where .length is read: one of
	// something else, such as res.json.length > "a" where res.json has a
	// key named length, or of the method .length is read from the result
	// of, is not told of len().
	at, located := errors.AsType[*file.Error](err)
	for _, m := range f.lengths {
		if located && at.From == m.Location().From && !f.callees[m] {
			hints = appendNew(hints, fmt.Sprintf("expressions take a length with len(): write len(%s)", m.Node))
		}
	}
	for _, name := range Names(input) {
		if to := NameFor(name); to != "" {
			hints = appendNew(hints, fmt.Sprintf("expressions have no %s: write %s", name, to))
		}
	}
	return hints
}

// MethodCallHints returns how input, an expression, is meant to be written
// where it calls a method expressions do not have, such as
// res.body.contains("x"), which fails whatever it is called on: no value an
// expression reads has methods of these names. It returns nothing for an
// expression that does not parse.
func MethodCallHints(input string) []string {
	tree, err := parser.Parse(input)
	if err != nil {
		return nil
	}
	f := &mistakeFinder{callees: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, f)
	return f.calls
}

// NameFor returns what name, which expressions do not have but another
// language does, is written as in expressions, such as nil for null, or an
// empty string.
func NameFor(name string) string {
	switch name {
	case "null", "undefined", "None":
		return "nil"
	case "True":
		return "true"
	case "False":
		return "false"
	}
	return ""
}

type mistakeFinder struct {
	callees map[ast.Node]bool
	calls   []string
	// lengths are the member accesses of .length, which are told only when
	// they are not called, which is known once the walk is done.
	lengths []*ast.MemberNode
}

func (f *mistakeFinder) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.CallNode:
		f.callees[n.Callee] = true
		m, ok := n.Callee.(*ast.MemberNode)
		if !ok {
			return
		}
		name, ok := m.Property.(*ast.StringNode)
		if !ok {
			return
		}
		if h := methodHint(name.Value, m.Node, n.Arguments); h != "" {
			f.calls = appendNew(f.calls, h)
		}
	case *ast.MemberNode:
		if name, ok := n.Property.(*ast.StringNode); ok && name.Value == "length" {
			f.lengths = append(f.lengths, n)
		}
	}
}

// methodHint returns how a call of the method name on recv with args is
// written in expressions, when name is that of a method another language
// has and expressions do not, or an empty string.
func methodHint(name string, recv ast.Node, args []ast.Node) string {
	r := recv.String()
	a := make([]string, len(args))
	for i, arg := range args {
		a[i] = arg.String()
	}
	write := func(format string, operands ...any) string {
		return fmt.Sprintf("expressions have no %s() method: write "+format, append([]any{name}, operands...)...)
	}
	switch {
	case (name == "contains" || name == "includes") && len(a) == 1:
		return write("%s contains %s for a string, or %s in %s for a list", operand(recv), operand(args[0]), operand(args[0]), operand(recv))
	case (name == "startsWith" || name == "endsWith" || name == "matches") && len(a) == 1:
		return write("%s %s %s", operand(recv), name, operand(args[0]))
	case (name == "length" || name == "size" || name == "len" || name == "count") && len(a) == 0:
		return write("len(%s)", r)
	case (name == "toLowerCase" || name == "toLower" || name == "lower") && len(a) == 0:
		return write("lower(%s)", r)
	case (name == "toUpperCase" || name == "toUpper" || name == "upper") && len(a) == 0:
		return write("upper(%s)", r)
	case (name == "trim" || name == "strip") && len(a) == 0:
		return write("trim(%s)", r)
	case name == "toString" && len(a) == 0:
		return write("string(%s)", r)
	case name == "join" && len(a) == 1:
		// Python joins with the string it calls join on: ",".join(items).
		if _, sep := recv.(*ast.StringNode); sep {
			return write("join(%s, %s)", a[0], r)
		}
		return write("join(%s, %s)", r, a[0])
	case (name == "split" || name == "indexOf" || name == "lastIndexOf") && len(a) == 1:
		return write("%s(%s, %s)", name, r, a[0])
	case name == "replace" && len(a) == 2:
		return write("replace(%s, %s, %s), which replaces every match", r, a[0], a[1])
	case name == "replaceAll" && len(a) == 2:
		return write("replace(%s, %s, %s)", r, a[0], a[1])
	case (name == "keys" || name == "values") && len(a) == 0:
		return write("%s(%s)", name, r)
	}
	return ""
}

// operand returns n as written as an operand of a binary operator, in
// parentheses where it is not a single term.
func operand(n ast.Node) string {
	switch n.(type) {
	case *ast.BinaryNode, *ast.ConditionalNode, *ast.UnaryNode:
		return "(" + n.String() + ")"
	}
	return n.String()
}

// operatorHints returns how the operators of input, an expression that
// does not parse, are written in expressions where they are written as
// another language would write them, as === is, or are called as functions,
// as startsWith(a, b) is.
func operatorHints(input string) []string {
	tokens, err := lexer.Lex(file.NewSource(input))
	if err != nil {
		return nil
	}
	var hints []string
	for i, t := range tokens {
		if t.Kind != lexer.Operator {
			continue
		}
		if i > 0 && t.Value == "=" && tokens[i-1].Kind == lexer.Operator && tokens[i-1].To == t.From {
			switch tokens[i-1].Value {
			case "==":
				hints = appendNew(hints, "expressions have no ===: write ==")
			case "!=":
				hints = appendNew(hints, "expressions have no !==: write !=")
			}
			continue
		}
		if h := operatorCallHint(input, tokens, i); h != "" {
			hints = appendNew(hints, h)
		}
	}
	return hints
}

// operatorCallHint returns how the operator at tokens[i] of input is
// written when it is called as a function, as startsWith(a, b) is, or an
// empty string.
func operatorCallHint(input string, tokens []lexer.Token, i int) string {
	op := tokens[i].Value
	function := map[string]string{"startsWith": "hasPrefix", "endsWith": "hasSuffix", "contains": "", "matches": ""}
	fn, ok := function[op]
	if !ok || i+1 >= len(tokens) || !tokens[i+1].Is(lexer.Bracket, "(") {
		return ""
	}
	// Written as an operator, it follows its left operand. Called as a
	// function, it follows an operator, but not . or ?., which make it a
	// method called as res.body.contains(x) is.
	if i > 0 {
		prev := tokens[i-1]
		if prev.Is(lexer.Operator, ".") || prev.Is(lexer.Operator, "?.") {
			return ""
		}
		if prev.Kind != lexer.Operator && !prev.Is(lexer.Bracket, "(") && !prev.Is(lexer.Bracket, "[") {
			return ""
		}
	}
	a, b, fa, fb := "a", "b", "a", "b"
	if args := callArguments(input, tokens, i+1); len(args) == 2 {
		a, b = operand(args[0]), operand(args[1])
		fa, fb = args[0].String(), args[1].String()
	}
	h := fmt.Sprintf("%s is an operator: write %s %s %s", op, a, op, b)
	if fn != "" {
		h += fmt.Sprintf(", or %s(%s, %s)", fn, fa, fb)
	}
	return h
}

// callArguments returns the arguments in the parentheses that open at
// tokens[open] of input, or nil when they do not parse.
func callArguments(input string, tokens []lexer.Token, open int) []ast.Node {
	depth := 0
	for j := open; j < len(tokens); j++ {
		switch {
		case tokens[j].Is(lexer.Bracket, "(") || tokens[j].Is(lexer.Bracket, "[") || tokens[j].Is(lexer.Bracket, "{"):
			depth++
		case tokens[j].Is(lexer.Bracket, ")") || tokens[j].Is(lexer.Bracket, "]") || tokens[j].Is(lexer.Bracket, "}"):
			depth--
		}
		if depth > 0 {
			continue
		}
		runes := []rune(input)
		tree, err := parser.Parse("[" + string(runes[tokens[open].To:tokens[j].From]) + "]")
		if err != nil {
			return nil
		}
		if list, ok := tree.Node.(*ast.ArrayNode); ok {
			return list.Nodes
		}
		return nil
	}
	return nil
}

// appendNew returns list with s at its end, unless it has s already.
func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
