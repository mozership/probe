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
// The conditions are evaluated again, each on its own, after the expression
// was found false as a whole. One that calls a function that returns another
// value each time, such as random_int, could then be told as true, or with a
// value it did not have, so nothing is told of an expression that calls one.
//
// It reports false when no condition can be told: input does not parse,
// calls such a function, or a condition cannot be evaluated on its own.
func (e *Expr) FalseClause(input string, env any) (Clause, bool) {
	tree, err := parser.Parse(input)
	if err != nil {
		return Clause{}, false
	}
	if callsAny(tree.Node, changing) {
		return Clause{}, false
	}
	// What the expression declares with let is declared again before each
	// condition, which is evaluated on its own, and left out of what is
	// told: the condition is the part that is false.
	lets, body := declarations(tree.Node)
	for _, node := range conjuncts(body) {
		src := node.String()
		out, err := e.Eval(lets+src, env)
		if err != nil {
			return Clause{}, false
		}
		if out == true {
			continue
		}
		return Clause{Expr: src, Operands: e.operands(node, lets, env)}, true
	}
	return Clause{}, false
}

// declarations returns what n declares with let before its expression, as
// source that declares it again, and the expression.
func declarations(n ast.Node) (lets string, body ast.Node) {
	for {
		d, ok := n.(*ast.VariableDeclaratorNode)
		if !ok {
			return lets, n
		}
		lets += "let " + d.Name + " = " + d.Value.String() + "; "
		n = d.Expr
	}
}

// changing are the functions that return another value each time they are
// called.
var changing = map[string]bool{
	"random_int": true, "random_str": true, "unixtime": true, "now": true,
}

// callsAny reports whether n calls one of names, at any depth.
func callsAny(n ast.Node, names map[string]bool) bool {
	f := &callNames{names: names}
	ast.Walk(&n, f)
	return f.found
}

type callNames struct {
	names map[string]bool
	found bool
}

func (f *callNames) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.CallNode:
		if id, ok := n.Callee.(*ast.IdentifierNode); ok && f.names[id.Value] {
			f.found = true
		}
	case *ast.BuiltinNode:
		if f.names[n.Name] {
			f.found = true
		}
	}
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
// to against env: both sides of a comparison, and what a ! negates. lets
// declares what the expression n is a condition of declares. An operand
// that cannot be evaluated on its own is left out.
func (e *Expr) operands(n ast.Node, lets string, env any) []Operand {
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
		value, err := e.Eval(lets+src, env)
		if err != nil {
			continue
		}
		out = append(out, Operand{Expr: src, Value: value})
	}
	return out
}

// isLiteral reports whether n is a value written in the expression, at any
// depth, as 200, "ok", [1, 2] and {"ok": true} are.
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
	case *ast.MapNode:
		for _, pair := range v.Pairs {
			p, ok := pair.(*ast.PairNode)
			if !ok || !isLiteral(p.Key) || !isLiteral(p.Value) {
				return false
			}
		}
		return true
	}
	return false
}
