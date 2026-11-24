// Package yamlnode reads the nodes of a parsed workflow and tells the
// lines of the files it was read from.
package yamlnode

import (
	"bytes"

	"github.com/goccy/go-yaml/ast"
)

// FileStart is a file of a workflow and the line of the source it starts on.
type FileStart struct {
	Name string
	Line int
}

// Files are the files of a workflow, each with the line of the source
// it starts on, in that order.
type Files []FileStart

// MapFiles returns the line each of files starts on in the text read, the
// files one after another, so that a line of the text can be told as a line
// of a file, whose contents are data. workflowfile.Read adds a newline after a
// file that does not end in one, unless it is the last.
func MapFiles(files []string, data [][]byte) Files {
	var out Files
	line := 1
	for k, f := range files {
		out = append(out, FileStart{Name: f, Line: line})
		line += bytes.Count(data[k], []byte("\n"))
		if k < len(files)-1 && len(data[k]) > 0 && data[k][len(data[k])-1] != '\n' {
			line++
		}
	}
	return out
}

// At tells line, a line of the source, as a file and a line of it.
func (fl Files) At(line int) (string, int) {
	if len(fl) == 0 {
		return "", 0
	}
	f := fl[0]
	for _, s := range fl {
		if line >= s.Line {
			f = s
		}
	}
	if line <= 0 {
		if len(fl) > 1 {
			return "", 0
		}
		return f.Name, 0
	}
	return f.Name, line - f.Line + 1
}

// Pairs returns the keys and values of n, a mapping, which a mapping of one
// key is parsed as on its own; nil when n is not a mapping.
func Pairs(n ast.Node) []*ast.MappingValueNode {
	switch v := n.(type) {
	case *ast.MappingNode:
		return v.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}
	case *ast.AnchorNode:
		return Pairs(v.Value)
	case *ast.TagNode:
		return Pairs(v.Value)
	}
	return nil
}

func Items(n ast.Node) []ast.Node {
	switch v := n.(type) {
	case *ast.SequenceNode:
		return v.Values
	case *ast.AnchorNode:
		return Items(v.Value)
	case *ast.TagNode:
		return Items(v.Value)
	}
	return nil
}

func LineOf(n ast.Node) int {
	if n == nil || n.GetToken() == nil {
		return 0
	}
	return n.GetToken().Position.Line
}

// HoldsAnchor reports whether n defines an anchor at any depth, as a key a
// workflow does not take may hold one for the files read after it.
func HoldsAnchor(n ast.Node) bool {
	found := false
	ast.Walk(VisitorFunc(func(n ast.Node) bool {
		if _, ok := n.(*ast.AnchorNode); ok {
			found = true
		}
		return !found
	}), n)
	return found
}

type VisitorFunc func(ast.Node) bool

func (f VisitorFunc) Visit(n ast.Node) ast.Visitor {
	if f(n) {
		return f
	}
	return nil
}
