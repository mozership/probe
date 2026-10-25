package expr

import (
	"maps"
	"slices"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// Refs returns the names that the templates in v read from the object name,
// as name.key or name['key']. v is a string, or a map or array of them at any
// depth, as a value under vars is. dynamic is true when a template reads the
// object in a way whose key cannot be known before it runs, such as name[k]
// or the object as a whole, so that it may read any key. keys come in the
// order they are first read, a map's values in the order of its sorted keys.
//
// An expression that does not parse reads nothing here; evaluating it reports
// the error.
func Refs(v any, name string) (keys []string, dynamic bool) {
	f := &refFinder{name: name, seen: map[string]bool{}}
	f.value(v)
	return f.keys, f.dynamic
}

type refFinder struct {
	name    string
	keys    []string
	seen    map[string]bool
	dynamic bool
}

func (f *refFinder) value(v any) {
	switch v := v.(type) {
	case string:
		for _, span := range findTemplates(v) {
			f.expression(span.expr)
		}
	case map[string]any:
		// In the order of the sorted keys, so that the keys returned do not
		// depend on map iteration. A key may hold templates as well.
		for _, key := range slices.Sorted(maps.Keys(v)) {
			f.value(key)
			f.value(v[key])
		}
	case []any:
		for _, val := range v {
			f.value(val)
		}
	}
}

func (f *refFinder) expression(input string) {
	tree, err := parser.Parse(input)
	if err != nil {
		return
	}
	c := &refCollector{name: f.name}
	ast.Walk(&tree.Node, c)
	for _, k := range c.keys {
		if !f.seen[k] {
			f.seen[k] = true
			f.keys = append(f.keys, k)
		}
	}
	// Every identifier that is not the object of a member access with a
	// constant key reads the object some other way.
	if c.idents > len(c.keys) {
		f.dynamic = true
	}
}

// refCollector counts the identifiers that name the object and records the
// constant keys read from it, one for each such access.
type refCollector struct {
	name   string
	idents int
	keys   []string
}

func (c *refCollector) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.IdentifierNode:
		if n.Value == c.name {
			c.idents++
		}
	case *ast.MemberNode:
		id, ok := n.Node.(*ast.IdentifierNode)
		if !ok || id.Value != c.name {
			return
		}
		if key, ok := n.Property.(*ast.StringNode); ok {
			c.keys = append(c.keys, key.Value)
		}
	}
}

// CallsTemplate reports whether a template in v calls template, whose
// argument is evaluated as templates once it runs, so that what it reads
// cannot be known before then. v is as Refs takes it.
func CallsTemplate(v any) bool {
	switch v := v.(type) {
	case string:
		for _, span := range findTemplates(v) {
			tree, err := parser.Parse(span.expr)
			if err != nil {
				continue
			}
			c := &callFinder{name: "template"}
			ast.Walk(&tree.Node, c)
			if c.found {
				return true
			}
		}
	case map[string]any:
		for k, e := range v {
			if CallsTemplate(k) || CallsTemplate(e) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(v, CallsTemplate) {
			return true
		}
	}
	return false
}

// callFinder finds a call to the function name.
type callFinder struct {
	name  string
	found bool
}

func (c *callFinder) Visit(node *ast.Node) {
	if n, ok := (*node).(*ast.CallNode); ok {
		if id, ok := n.Callee.(*ast.IdentifierNode); ok && id.Value == c.name {
			c.found = true
		}
	}
}
