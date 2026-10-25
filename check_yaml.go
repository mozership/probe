package probe

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/mozership/probe/internal/suggest"
)

type fileStart struct {
	name string
	line int
}

// mapFiles notes the line each file starts on in the text read, the files
// one after another, so that a line of the text can be told as a line of a
// file, whose contents are data. readYamlFiles adds a newline after a file
// that does not end in one, unless it is the last.
func (c *checker) mapFiles(files []string, data [][]byte) {
	line := 1
	for k, f := range files {
		c.files = append(c.files, fileStart{name: f, line: line})
		line += bytes.Count(data[k], []byte("\n"))
		if k < len(files)-1 && len(data[k]) > 0 && data[k][len(data[k])-1] != '\n' {
			line++
		}
	}
}

// position tells line, a line of the text read, as a file and a line of it.
func (c *checker) position(line int) (string, int) {
	if len(c.files) == 0 {
		return "", 0
	}
	f := c.files[0]
	for _, s := range c.files {
		if line >= s.line {
			f = s
		}
	}
	if line <= 0 {
		if len(c.files) > 1 {
			return "", 0
		}
		return f.name, 0
	}
	return f.name, line - f.line + 1
}

// Keys each part of a workflow takes, from the fields Probe decodes them into.
var (
	workflowKeys = yamlKeys(reflect.TypeFor[Workflow]())
	jobKeys      = yamlKeys(reflect.TypeFor[Job]())
	stepKeys     = yamlKeys(reflect.TypeFor[Step]())
	retryKeys    = yamlKeys(reflect.TypeFor[StepRetry]())
	repeatKeys   = yamlKeys(reflect.TypeFor[Repeat]())
)

func yamlKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// pairs returns the keys and values of n, a mapping, which a mapping of one
// key is parsed as on its own; nil when n is not a mapping.
func pairs(n ast.Node) []*ast.MappingValueNode {
	switch v := n.(type) {
	case *ast.MappingNode:
		return v.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}
	case *ast.AnchorNode:
		return pairs(v.Value)
	case *ast.TagNode:
		return pairs(v.Value)
	}
	return nil
}

func items(n ast.Node) []ast.Node {
	switch v := n.(type) {
	case *ast.SequenceNode:
		return v.Values
	case *ast.AnchorNode:
		return items(v.Value)
	case *ast.TagNode:
		return items(v.Value)
	}
	return nil
}

func lineOf(n ast.Node) int {
	if n == nil || n.GetToken() == nil {
		return 0
	}
	return n.GetToken().Position.Line
}

// holdsAnchor reports whether n defines an anchor at any depth, as a key a
// workflow does not take may hold one for the files read after it.
func holdsAnchor(n ast.Node) bool {
	found := false
	ast.Walk(visitorFunc(func(n ast.Node) bool {
		if _, ok := n.(*ast.AnchorNode); ok {
			found = true
		}
		return !found
	}), n)
	return found
}

type visitorFunc func(ast.Node) bool

func (f visitorFunc) Visit(n ast.Node) ast.Visitor {
	if f(n) {
		return f
	}
	return nil
}

// keys checks the keys of the mapping n, at path, against those the part of
// a workflow it is takes, and calls each with each key it takes.
func (c *checker) keys(n ast.Node, path, where string, allowed []string, each func(key string, value ast.Node)) {
	for _, kv := range pairs(n) {
		if kv.Key == nil || kv.Key.GetToken() == nil {
			continue
		}
		key := kv.Key.GetToken().Value
		if kv.Key.IsMergeKey() {
			continue
		}
		line := lineOf(kv.Key)
		c.lines[join(path, key)] = line
		if !slices.Contains(allowed, key) {
			if holdsAnchor(kv.Value) {
				continue
			}
			msg := fmt.Sprintf("unknown key %q", key)
			msg += suggest.Hint(key, allowed)
			c.add(SeverityError, line, where, "%s", msg)
			continue
		}
		if each != nil {
			each(key, kv.Value)
		}
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (c *checker) walkWorkflow(body ast.Node) {
	c.keys(body, "", "", workflowKeys, func(key string, value ast.Node) {
		if key == "vars" || key == "actions" || key == "defaults" {
			c.noteLines(key, value)
		}
		if key != "jobs" {
			return
		}
		for i, job := range items(value) {
			jp := fmt.Sprintf("jobs[%d]", i)
			c.lines[jp] = lineOf(job)
			jobLabel := named(fmt.Sprintf("job %d", i), job)
			c.keys(job, jp, jobLabel, jobKeys, func(key string, value ast.Node) {
				switch key {
				case "defaults":
					c.noteLines(jp+".defaults", value)
				case "repeat":
					c.keys(value, jp+".repeat", jobLabel, repeatKeys, nil)
				case "steps":
					for j, step := range items(value) {
						sp := fmt.Sprintf("%s.steps[%d]", jp, j)
						c.lines[sp] = lineOf(step)
						where := named(fmt.Sprintf("%s, step %d", jobLabel, j), step)
						c.keys(step, sp, where, stepKeys, func(key string, value ast.Node) {
							switch key {
							case "retry":
								c.keys(value, sp+".retry", where, retryKeys, nil)
							case "with", "vars", "outputs":
								c.noteLines(sp+"."+key, value)
							}
						})
					}
				}
			})
		}
	})
}

// named returns where, the part of a workflow n is, with its name when it
// writes one, as job 0 "Users".
func named(where string, n ast.Node) string {
	for _, kv := range pairs(n) {
		if kv.Key != nil && kv.Key.GetToken() != nil && kv.Key.GetToken().Value == "name" && kv.Value != nil && kv.Value.GetToken() != nil {
			return fmt.Sprintf("%s %q", where, kv.Value.GetToken().Value)
		}
	}
	return where
}

// noteLines notes the lines of the keys of n, a value such as with, at any
// depth, under path, as with.headers.authorization, so that a finding in a
// value can be told on its line.
func (c *checker) noteLines(path string, n ast.Node) {
	for _, kv := range pairs(n) {
		if kv.Key == nil || kv.Key.GetToken() == nil || kv.Key.IsMergeKey() {
			continue
		}
		kp := path + "." + kv.Key.GetToken().Value
		c.lines[kp] = lineOf(kv.Key)
		c.noteLines(kp, kv.Value)
	}
	for k, item := range items(n) {
		ip := fmt.Sprintf("%s[%d]", path, k)
		c.lines[ip] = lineOf(item)
		c.noteLines(ip, item)
	}
}
