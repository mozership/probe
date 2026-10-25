package expr

import (
	"slices"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// Parse reports whether input is an expression that parses, without
// evaluating it, so that a workflow can be checked before it runs. Its
// error tells how input is likely meant to be written where it is written
// as another language would write it, as a *HintError.
func Parse(input string) error {
	_, err := parser.Parse(input)
	return withHints(input, err)
}

// Reads returns the constant keys that input, an expression, reads from the
// object name, as chains: outputs.auth.token is read as [auth token], and
// as [auth] too. A key read in a way that cannot be known before it runs,
// such as name[k], is not returned. An expression that does not parse reads
// nothing here.
func Reads(input, name string) [][]string {
	tree, err := parser.Parse(input)
	if err != nil {
		return nil
	}
	c := &chainCollector{name: name}
	ast.Walk(&tree.Node, c)
	return c.chains
}

type chainCollector struct {
	name   string
	chains [][]string
}

func (c *chainCollector) Visit(node *ast.Node) {
	if chain, ok := memberChain(*node, c.name); ok && len(chain) > 0 {
		c.chains = append(c.chains, chain)
	}
}

// ReadsByVariableKey returns where input, an expression, reads the object
// name by a key that cannot be known before it runs, as the chains of the
// constant keys read before that key: outputs[vars.id].token is read as [],
// and outputs.auth[k] as [auth]. An expression that does not parse reads
// nothing here.
func ReadsByVariableKey(input, name string) [][]string {
	tree, err := parser.Parse(input)
	if err != nil {
		return nil
	}
	c := &variableKeyCollector{name: name}
	ast.Walk(&tree.Node, c)
	return c.chains
}

type variableKeyCollector struct {
	name   string
	chains [][]string
}

func (c *variableKeyCollector) Visit(node *ast.Node) {
	m, ok := (*node).(*ast.MemberNode)
	if !ok {
		return
	}
	if _, constant := m.Property.(*ast.StringNode); constant {
		return
	}
	if chain, ok := memberChain(m.Node, c.name); ok {
		c.chains = append(c.chains, append([]string{}, chain...))
	}
}

// memberChain returns the constant keys read from the identifier name by n,
// a chain of member accesses such as name.a.b, or false when n is not one.
func memberChain(n ast.Node, name string) ([]string, bool) {
	switch v := n.(type) {
	case *ast.IdentifierNode:
		return nil, v.Value == name
	case *ast.MemberNode:
		key, ok := v.Property.(*ast.StringNode)
		if !ok {
			return nil, false
		}
		chain, ok := memberChain(v.Node, name)
		if !ok {
			return nil, false
		}
		return append(chain, key.Value), true
	case *ast.ChainNode:
		return memberChain(v.Node, name)
	}
	return nil, false
}

// ReadsNothing reports whether input, an expression that parses, reads no
// variable, so that it evaluates the same whatever it is evaluated against,
// as true or 1 == 1 does. A function it calls, such as now(), is not a
// variable.
func ReadsNothing(input string) bool {
	tree, err := parser.Parse(input)
	if err != nil {
		return false
	}
	f := &identFinder{callees: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, f)
	// Walk visits the callee of a call before the call, so which
	// identifiers are callees is known only once it is done.
	for _, id := range f.idents {
		if !f.callees[id] {
			return false
		}
	}
	return !f.pointer
}

type identFinder struct {
	callees map[ast.Node]bool
	idents  []ast.Node
	pointer bool
	// declared are the names let declares.
	declared map[string]bool
}

func (f *identFinder) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.CallNode:
		f.callees[n.Callee] = true
	case *ast.IdentifierNode:
		f.idents = append(f.idents, n)
	case *ast.PointerNode:
		// # in a predicate reads the element the predicate is given.
		f.pointer = true
	case *ast.VariableDeclaratorNode:
		if f.declared == nil {
			f.declared = map[string]bool{}
		}
		f.declared[n.Name] = true
	}
}

// Names returns the names that input, an expression, reads from what it is
// evaluated against, in the order they first come: vars and outputs in
// vars.id == outputs.login.id. A function it calls, a name let declares and
// $env, which is what it is evaluated against as a whole, are not among
// them. An expression that does not parse reads nothing here.
func Names(input string) []string {
	tree, err := parser.Parse(input)
	if err != nil {
		return nil
	}
	f := &identFinder{callees: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, f)
	// Walk visits the callee of a call before the call, so which
	// identifiers are callees is known only once it is done.
	var names []string
	for _, n := range f.idents {
		id := n.(*ast.IdentifierNode)
		if f.callees[n] || f.declared[id.Value] || id.Value == "$env" || slices.Contains(names, id.Value) {
			continue
		}
		names = append(names, id.Value)
	}
	return names
}
