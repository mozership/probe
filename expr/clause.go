package expr

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// Clause is one of the conditions an expression joins with &&, as each line
// of a test usually is.
type Clause struct {
	// Expr is the condition, as the parser writes it back: res.code == 200.
	Expr string
	// Operands are what the condition compares, with what each evaluated
	// to. A literal, such as the 200 above, is left out, as it says what it
	// is.
	Operands []Operand
}

// Operand is a part of a condition and what it evaluated to.
type Operand struct {
	Expr  string
	Value any
}

// comparisons are the operators whose operands tell why a condition is
// false.
var comparisons = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"in": true, "not in": true,
	"contains": true, "startsWith": true, "endsWith": true, "matches": true,
}

// FalseClause returns the first of the conditions input joins with && that
// is not true against env, so that a test of several conditions that failed
// can say which one did. An expression that joins none is one condition.
//
// It reports false when no condition can be told: input does not parse, a
// condition cannot be evaluated on its own, or each of them is true now, as
// may be of one that calls random_int.
func (e *Expr) FalseClause(input string, env any) (Clause, bool) {
	tree, err := parser.Parse(input)
	if err != nil {
		return Clause{}, false
	}
	for _, node := range conjuncts(tree.Node) {
		src := node.String()
		out, err := e.Eval(src, env)
		if err != nil {
			return Clause{}, false
		}
		if out == true {
			continue
		}
		return Clause{Expr: src, Operands: e.operands(node, env)}, true
	}
	return Clause{}, false
}

// conjuncts returns the conditions n joins with &&, in the order they are
// evaluated. An || is one condition, whatever it holds.
func conjuncts(n ast.Node) []ast.Node {
	if b, ok := n.(*ast.BinaryNode); ok && (b.Operator == "&&" || b.Operator == "and") {
		return append(conjuncts(b.Left), conjuncts(b.Right)...)
	}
	return []ast.Node{n}
}

// operands returns what the condition n compares, with what each evaluates
// to against env: both sides of a comparison, and what a ! negates. An
// operand that cannot be evaluated on its own is left out.
func (e *Expr) operands(n ast.Node, env any) []Operand {
	var parts []ast.Node
	switch v := n.(type) {
	case *ast.BinaryNode:
		if comparisons[v.Operator] {
			parts = []ast.Node{v.Left, v.Right}
		}
	case *ast.UnaryNode:
		if v.Operator == "!" || v.Operator == "not" {
			parts = []ast.Node{v.Node}
		}
	}

	var out []Operand
	for _, p := range parts {
		if isLiteral(p) {
			continue
		}
		src := p.String()
		value, err := e.Eval(src, env)
		if err != nil {
			continue
		}
		out = append(out, Operand{Expr: src, Value: value})
	}
	return out
}

// isLiteral reports whether n is a value written in the expression, at any
// depth, as 200, "ok" and [1, 2] are.
func isLiteral(n ast.Node) bool {
	switch v := n.(type) {
	case *ast.NilNode, *ast.BoolNode, *ast.IntegerNode, *ast.FloatNode, *ast.StringNode, *ast.BytesNode, *ast.ConstantNode:
		return true
	case *ast.UnaryNode:
		// A negative number is a - and a number.
		return (v.Operator == "-" || v.Operator == "+") && isLiteral(v.Node)
	case *ast.ArrayNode:
		for _, item := range v.Nodes {
			if !isLiteral(item) {
				return false
			}
		}
		return true
	}
	return false
}
