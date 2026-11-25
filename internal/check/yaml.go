package check

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/mozership/probe/internal/engine"
	"github.com/mozership/probe/internal/suggest"
	"github.com/mozership/probe/internal/yamlnode"
)

// position tells line, a line of the source, as a file and a line of it.
func (c *checker) position(line int) (string, int) {
	return c.files.At(line)
}

// Keys each part of a workflow takes, from the fields Probe decodes them into.
var (
	workflowKeys = yamlKeys(reflect.TypeFor[engine.Workflow]())
	jobKeys      = yamlKeys(reflect.TypeFor[engine.Job]())
	stepKeys     = yamlKeys(reflect.TypeFor[engine.Step]())
	retryKeys    = yamlKeys(reflect.TypeFor[engine.StepRetry]())
	repeatKeys   = yamlKeys(reflect.TypeFor[engine.Repeat]())
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

// keys checks the keys of the mapping n, at path, against those the part of
// a workflow it is takes, and calls each with each key it takes.
func (c *checker) keys(n ast.Node, path, where string, allowed []string, each func(key string, value ast.Node)) {
	for _, kv := range yamlnode.Pairs(n) {
		if kv.Key == nil || kv.Key.GetToken() == nil {
			continue
		}
		key := kv.Key.GetToken().Value
		if kv.Key.IsMergeKey() {
			continue
		}
		line := yamlnode.LineOf(kv.Key)
		c.lines[join(path, key)] = line
		if !slices.Contains(allowed, key) {
			if yamlnode.HoldsAnchor(kv.Value) {
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
		if key == "vars" || key == "actions" || key == "defaults" || key == "outputs" {
			c.noteLines(key, value)
		}
		if key != "jobs" {
			return
		}
		i := 0
		// A list an alias names is checked as the jobs, as it is run.
		for _, job := range yamlnode.Items(engine.Resolve(body, value)) {
			// An entry of jobs that includes a file is told of as the
			// files are read, and left out of the jobs that run. One is
			// left only in jobs a later file replaces, which are not read,
			// as is an entry of their steps.
			if engine.IsInclude(job) {
				continue
			}
			jp := fmt.Sprintf("jobs[%d]", i)
			c.lines[jp] = yamlnode.LineOf(job)
			jobLabel := named(fmt.Sprintf("job %d", i), job)
			i++
			walkSteps := func(value ast.Node) {
				j := 0
				for _, step := range yamlnode.Items(engine.Resolve(body, value)) {
					if engine.IsInclude(step) {
						continue
					}
					sp := fmt.Sprintf("%s.steps[%d]", jp, j)
					c.lines[sp] = yamlnode.LineOf(step)
					where := named(fmt.Sprintf("%s, step %d", jobLabel, j), step)
					j++
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
			c.keys(job, jp, jobLabel, jobKeys, func(key string, value ast.Node) {
				switch key {
				case "defaults":
					c.noteLines(jp+".defaults", value)
				case "repeat":
					c.keys(value, jp+".repeat", jobLabel, repeatKeys, nil)
				case "steps":
					walkSteps(value)
				}
			})
			// The steps a merge key gives a job are checked as its own,
			// as they are run.
			if engine.LastKey(job, "steps") == nil {
				if kv := engine.StepsOf(body, job); kv != nil {
					walkSteps(kv.Value)
				}
			}
		}
	})
}

// named returns where, the part of a workflow n is, with its name when it
// writes one, as job 0 "Users".
func named(where string, n ast.Node) string {
	for _, kv := range yamlnode.Pairs(n) {
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
	for _, kv := range yamlnode.Pairs(n) {
		if kv.Key == nil || kv.Key.GetToken() == nil || kv.Key.IsMergeKey() {
			continue
		}
		kp := path + "." + kv.Key.GetToken().Value
		c.lines[kp] = yamlnode.LineOf(kv.Key)
		c.noteLines(kp, kv.Value)
	}
	for k, item := range yamlnode.Items(n) {
		ip := fmt.Sprintf("%s[%d]", path, k)
		c.lines[ip] = yamlnode.LineOf(item)
		c.noteLines(ip, item)
	}
}
