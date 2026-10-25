package expr

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/checker"
	"github.com/expr-lang/expr/checker/nature"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/types"
	"github.com/mozership/probe/internal/kind"
)

// Shape is what is known of a value before an expression reads it: its
// kind, when that is known, and for a map, the shapes of the keys that are
// known. A value of no kind, and a key of a map that is not among its
// fields, may hold anything.
type Shape struct {
	Kind   kind.Kind
	Fields map[string]Shape
}

// ErrNotBool is the error of an expression that is to come to true or
// false, and that is known to come to a value of another kind.
var ErrNotBool = errors.New("not true or false")

// CheckTypes returns what is sure to go wrong with input, an expression, by
// the kinds of the values it reads, which names gives the shapes of: an
// operator given values it does not take, as "id-" + 1 is, which fails, and
// a comparison of values of kinds that are never equal, as res.code ==
// "200 OK" is where res.code is a number, which is never true. With
// wantBool, an expression whose value is known not to be true or false, as
// a test's must be, is an error too, which wraps ErrNotBool.
//
// A value whose kind is not known, as what is read from a key no shape
// gives and what a function of Probe returns, is taken to be of the kind
// that would be right, so that nothing is told that may not go wrong. An
// expression that does not parse, which Parse tells of, has nothing told
// here.
//
// The error says what is wrong with the expression as written, and how it
// is likely meant, as a *HintError.
func CheckTypes(input string, names map[string]Shape, wantBool bool) error {
	if _, err := parser.Parse(input); err != nil {
		return nil
	}
	// The expression is checked with the functions and options it is
	// evaluated with, so that a function it calls is known.
	config := conf.CreateNew()
	for _, op := range (&Expr{}).Options(envTypes(names)) {
		op(config)
	}
	config.Check()
	tree, err := checker.ParseCheck(input, config)
	if err != nil {
		at, ok := errors.AsType[*file.Error](err)
		if !ok || tree == nil {
			return nil
		}
		return typeProblem(tree.Node, at)
	}
	if wantBool {
		if n := tree.Node.Nature(); known(n) && n.Kind != reflect.Bool {
			return &HintError{
				Err:   fmt.Errorf("%s is %s, %w", tree.Node, describe(n), ErrNotBool),
				Hints: []string{"compare it with what it should be, with == or another comparison, so that it comes to true or false"},
			}
		}
	}
	return nil
}

// envTypes returns the types of names, as the expression checker takes
// them. A name it does not give, and a key of a map that no shape gives, is
// of any type.
func envTypes(names map[string]Shape) types.Map {
	env := types.Map{types.Extra: types.Any}
	for name, s := range names {
		env[name] = shapeType(s)
	}
	return env
}

func shapeType(s Shape) types.Type {
	switch s.Kind {
	case kind.Number:
		// A number may be whole or not, but the checker is told it is an
		// integer: what it allows of an integer, such as %, it allows of
		// a number with a fraction too, and what it refuses of one, it
		// refuses of the other.
		return types.Int64
	case kind.String:
		return types.String
	case kind.Bool:
		return types.Bool
	case kind.List:
		return types.Array(types.Any)
	case kind.Map:
		m := types.Map{types.Extra: types.Any}
		for key, f := range s.Fields {
			m[key] = shapeType(f)
		}
		return m
	}
	if len(s.Fields) > 0 {
		return shapeType(Shape{Kind: kind.Map, Fields: s.Fields})
	}
	return types.Any
}

// typeProblem tells at, an error of the checker in the expression whose
// tree is root, as what goes wrong when it is evaluated, when it is one of
// those that are sure to: an operator given values it does not take, or a
// comparison of values that are never equal. It returns nil for any other
// error, of which some, such as indexing a string, the checker refuses but
// an evaluation does not.
func typeProblem(root ast.Node, at *file.Error) error {
	var problem error
	ast.Find(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryNode:
			if n.Location() == at.Location && (strings.HasPrefix(at.Message, "invalid operation:") || strings.HasSuffix(at.Message, " in array")) {
				problem = binaryProblem(n)
			}
		case *ast.UnaryNode:
			if n.Location() == at.Location && strings.HasPrefix(at.Message, "invalid operation:") {
				problem = unaryProblem(n)
			}
		case *ast.ConditionalNode:
			if n.Cond.Location() == at.Location && strings.HasPrefix(at.Message, "non-bool expression") {
				problem = &HintError{
					Err:   fmt.Errorf("%s fails: its condition, %s, is %s, where ? : takes true or false", n, n.Cond, describe(n.Cond.Nature())),
					Hints: []string{"compare the condition with what it should be, with == or another comparison"},
				}
			}
		}
		return problem != nil
	})
	return problem
}

// binaryProblem tells what goes wrong with n, a binary operation the
// checker refuses for the kinds of its operands.
func binaryProblem(n *ast.BinaryNode) error {
	l, r := n.Left.Nature(), n.Right.Nature()
	if !known(l) || !known(r) {
		return nil
	}
	// and and or are not told of: given a value that is not true or false,
	// they come to one of their operands, as res.code and true comes to
	// res.code, rather than failing.
	switch n.Operator {
	case "and", "&&", "or", "||":
		return nil
	}
	ls, rs := n.Left.String(), n.Right.String()
	kinds := fmt.Sprintf("%s is %s and %s %s", ls, describe(l), rs, describe(r))
	op := n.Operator
	switch op {
	case "==":
		return &HintError{
			Err:   fmt.Errorf("%s is never true: %s, which are never equal", n, kinds),
			Hints: []string{compareHint(n)},
		}
	case "!=":
		return &HintError{
			Err:   fmt.Errorf("%s is always true: %s, which are never equal", n, kinds),
			Hints: []string{compareHint(n)},
		}
	case "in":
		if !r.IsArray() {
			hint := fmt.Sprintf("in looks for an element of a list or a key of a map, and %s is neither", rs)
			if r.IsString() {
				hint += fmt.Sprintf(": to look for text in a string, write %s contains %s", operand(n.Right), operand(n.Left))
			}
			return &HintError{
				Err:   fmt.Errorf("%s fails: %s is %s", n, rs, describe(r)),
				Hints: []string{hint},
			}
		}
		return &HintError{
			Err:   fmt.Errorf("%s is never true: %s is %s and the elements of %s are %s, which are never equal", n, ls, describe(l), rs, elements(r)),
			Hints: []string{fmt.Sprintf("list values of the kind of %s, or convert it with string(), int() or float()", ls)},
		}
	}
	err := fmt.Errorf("%s fails: %s, which %s does not take together", n, kinds, op)
	var hint string
	switch op {
	case "+":
		switch {
		case l.IsString() && r.IsNumber():
			hint = fmt.Sprintf("to join them as text, convert the number with string(): %s + string(%s)", operand(n.Left), n.Right)
		case l.IsNumber() && r.IsString():
			hint = fmt.Sprintf("to join them as text, convert the number with string(): string(%s) + %s", n.Left, operand(n.Right))
		default:
			hint = "+ adds numbers and joins strings"
		}
	case "-", "*", "/", "%", "**", "^", "..":
		hint = fmt.Sprintf("%s takes numbers: convert a string that holds one with int() or float()", op)
	case "<", ">", "<=", ">=":
		hint = fmt.Sprintf("%s compares two numbers or two strings: convert one with string(), int() or float()", op)
	case "contains", "startsWith", "endsWith", "matches":
		hint = fmt.Sprintf("%s takes strings", op)
		if op == "contains" && l.IsArray() {
			hint += fmt.Sprintf(": to look for an element of a list, write %s in %s", operand(n.Right), operand(n.Left))
		} else {
			hint += ": convert a value with string()"
		}
	default:
		// An operator not known to fail for these kinds is not told of.
		return nil
	}
	return &HintError{Err: err, Hints: []string{hint}}
}

// compareHint tells how n, a comparison of two values that are never
// equal, is likely meant.
func compareHint(n *ast.BinaryNode) string {
	l, r := n.Left.Nature(), n.Right.Nature()
	switch {
	case l.IsNumber() && r.IsString(), l.IsString() && r.IsNumber():
		num, str := n.Left, n.Right
		if l.IsString() {
			num, str = n.Right, n.Left
		}
		hint := fmt.Sprintf("a number is never equal to a string: compare %s with a number", num)
		if s, ok := str.(*ast.StringNode); ok && isNumber(s.Value) {
			hint += fmt.Sprintf(", as %s %s %s", num, n.Operator, s.Value)
		}
		return hint + fmt.Sprintf(", or compare string(%s) with %s", num, str)
	case l.IsBool() || r.IsBool():
		return "true and false are equal only to true and false: compare a bool with true or false, or with another bool"
	}
	return fmt.Sprintf("compare %s with a value of its own kind", n.Left)
}

// unaryProblem tells what goes wrong with n, a unary operation the checker
// refuses for the kind of its operand.
func unaryProblem(n *ast.UnaryNode) error {
	nt := n.Node.Nature()
	// + given a value that is not a number comes to the value, rather
	// than failing.
	if !known(nt) || n.Operator == "+" {
		return nil
	}
	err := fmt.Errorf("%s fails: %s is %s", n, n.Node, describe(nt))
	switch n.Operator {
	case "!", "not":
		return &HintError{Err: err, Hints: []string{fmt.Sprintf("%s takes true or false: compare the value with what it should be, with == or another comparison", n.Operator)}}
	case "-":
		return &HintError{Err: err, Hints: []string{fmt.Sprintf("%s takes a number: convert a string that holds one with int() or float()", n.Operator)}}
	}
	return &HintError{Err: err}
}

// known reports whether the type of a value is known before it is read.
func known(n *nature.Nature) bool {
	return n != nil && n.Type != nil && n.Kind != reflect.Interface
}

// describe names the kind of a value of nature n, as "a number", or nil.
func describe(n *nature.Nature) string {
	if name := plainKind(n); name != "nil" {
		return "a " + name
	}
	return "nil"
}

// elements names the kind of the elements of a list of nature n, as
// "numbers".
func elements(n *nature.Nature) string {
	elem := nature.Nature{}
	switch {
	case n.Ref != nil:
		elem = *n.Ref
	case n.Type != nil && (n.Kind == reflect.Slice || n.Kind == reflect.Array):
		elem = nature.Nature{Type: n.Type.Elem(), Kind: n.Type.Elem().Kind()}
	}
	switch name := plainKind(&elem); name {
	case "nil", "value of any kind":
		return name
	default:
		return name + "s"
	}
}

var (
	durationType = reflect.TypeFor[time.Duration]()
	timeType     = reflect.TypeFor[time.Time]()
)

// plainKind names the kind of a value of nature n, as "number", or nil.
func plainKind(n *nature.Nature) string {
	if n == nil || n.Type == nil {
		if n != nil && n.Nil {
			return "nil"
		}
		return "value of any kind"
	}
	switch n.Type {
	case durationType:
		return "duration"
	case timeType:
		return "time"
	}
	switch n.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Slice, reflect.Array:
		return "list"
	case reflect.Map, reflect.Struct:
		return "map"
	}
	return n.Type.String()
}

// isNumber reports whether s is a number written in decimal, as "200" is.
func isNumber(s string) bool {
	if s == "" || strings.Trim(s, "0123456789.-") != "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
