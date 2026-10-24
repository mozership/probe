package probe

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/suggest"
)

// Severities of a Finding.
const (
	// SeverityError is a finding that stops the workflow from running as it
	// is written, or that makes a step read what is never there.
	SeverityError = "error"
	// SeverityWarning is a finding that the workflow may run with, but that
	// leaves it checking less than it seems to.
	SeverityWarning = "warning"
)

// Finding is one thing Check found in a workflow.
type Finding struct {
	Severity string
	File     string // the file it is in; empty when it cannot be told
	Line     int    // the line it is on; 0 when it cannot be told
	Where    string // the job and step it is in, such as job "Users", step 1 "Create"
	Message  string
}

// String is the finding on one line, as probe check prints it.
func (f Finding) String() string {
	var b strings.Builder
	if f.File != "" {
		b.WriteString(f.File)
		if f.Line > 0 {
			fmt.Fprintf(&b, ":%d", f.Line)
		}
		b.WriteString(": ")
	}
	b.WriteString(f.Severity)
	b.WriteString(": ")
	if f.Where != "" {
		b.WriteString(f.Where)
		b.WriteString(": ")
	}
	b.WriteString(f.Message)
	return b.String()
}

// CheckOptions tunes Check.
type CheckOptions struct {
	// Actions are the names of the actions a step may use, besides the
	// external ones it names by a path or a repository. When it is empty,
	// the names are not checked.
	Actions []string
	// Params are the keys each action takes in with, by its name. The with
	// of an action it does not name, which may take any key, is not
	// checked.
	Params map[string][]string
	// Contracts are the keys of with that ask each action to check a step
	// against a contract, which checks it as a test does, by its name: as
	// openapi asks the http action. A step with such a key and no test is
	// not warned of as unchecked.
	Contracts map[string][]string
	// Fields are the fields of the req and the res each action returns, by
	// its name, so that a test, an echo or an output that reads another is
	// reported. What a step of an action it does not name reads is not
	// checked.
	Fields map[string]actionrpc.Fields
	// Required are the keys of with each action cannot run without, by its
	// name, each as the keys that stand for one another, of which one is
	// enough: as get stands for method in the http action.
	Required map[string][][]string
	// Manifest reads the action.yml of an external action, a local one
	// relative to baseDir, so that its with is checked against the params
	// it declares, and what a step reads of its res against the fields of
	// res it declares. When it is nil, neither is checked, and nothing is
	// read from the network.
	Manifest func(uses, baseDir string) (*actionref.Manifest, error)
}

// Check reads the workflow at path, as a run reads it, and returns what is
// wrong with it or weak in it, without running anything: keys a workflow
// does not take, what a run would refuse to load, actions that do not
// exist, needs that cannot be met, expressions and templates that do not
// parse or that read a name that is not there to read, and outputs read
// before or without being published, as errors; and steps that nothing
// checks, tests that read nothing, and vars that nothing defines, as
// warnings.
//
// The error it returns is for a workflow that cannot be read at all, such
// as a file that does not exist.
func Check(path string, opts CheckOptions) ([]Finding, error) {
	p := New(path, false)
	// What a run warns of is told as a finding here.
	p.Config.Warn = nil
	files, err := p.yamlFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no YAML file", path)
	}
	data, contents, err := p.readYamlFiles(files)
	if err != nil {
		return nil, err
	}

	c := &checker{opts: opts, lines: map[string]int{}}
	// The params of external actions are added as they are read, so the
	// caller's map is left as it was.
	c.opts.Params = maps.Clone(opts.Params)
	if c.opts.Params == nil {
		c.opts.Params = map[string][]string{}
	}
	c.opts.Fields = maps.Clone(opts.Fields)
	if c.opts.Fields == nil {
		c.opts.Fields = map[string]actionrpc.Fields{}
	}
	c.mapFiles(files, contents)
	for _, r := range redefinedJobs(files, contents) {
		c.findings = append(c.findings, Finding{
			Severity: SeverityWarning,
			File:     r.File,
			Line:     r.Line,
			Message:  r.message(),
		})
	}

	// A key may be written more than once, as a run reads the workflow,
	// so that the files given can override one another.
	file, err := parser.ParseBytes([]byte(data), 0, parser.AllowDuplicateMapKey())
	if err != nil {
		c.add(SeverityError, 0, "", "YAML: %s", firstLine(err.Error()))
		return c.sorted(), nil
	}
	for _, doc := range file.Docs {
		c.walkWorkflow(doc.Body)
	}

	// What is wrong with the names of external actions is told on the line
	// of each, with whatever else is wrong, rather than ending the check.
	var aliases []aliasProblem
	var problems []problem
	p.Config.Actions = opts.Actions
	p.aliasProblems = &aliases
	p.problems = &problems
	if err := p.Load(); err != nil {
		c.add(SeverityError, 0, "", "%s", firstLine(err.Error()))
		return c.sorted(), nil
	}
	c.wf = &p.workflow
	c.checkAliases(aliases)
	c.checkActions()
	c.checkEmbedded()
	c.checkWith()
	c.checkRequired()
	c.checkNeeds()
	c.checkProblems(problems)
	c.checkOutputs()
	c.checkNames()
	c.checkResults()
	c.checkTests()
	c.checkRepeatTests()
	return c.sorted(), nil
}

type checker struct {
	opts     CheckOptions
	wf       *Workflow
	findings []Finding
	// lines are the lines of the keys of the workflow, by their path, such
	// as jobs[0].steps[1].test, and of each job and step, by jobs[0] and
	// jobs[0].steps[1].
	lines map[string]int
	// files are the files read, with the line of the text read that each
	// one starts on.
	files []fileStart
	// readManifests are the external actions whose action.yml was read.
	readManifests map[string]bool
}

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

func (c *checker) add(severity string, line int, where, format string, args ...any) {
	file, l := c.position(line)
	c.findings = append(c.findings, Finding{
		Severity: severity,
		File:     file,
		Line:     l,
		Where:    where,
		Message:  fmt.Sprintf(format, args...),
	})
}

// sorted returns the findings by file and line, keeping the order they were
// found in for those on one line.
func (c *checker) sorted() []Finding {
	out := slices.Clone(c.findings)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// line returns the line of path, or of the closest thing that holds it,
// such as the step for a key of the step that is not written.
func (c *checker) line(path string) int {
	for {
		if l, ok := c.lines[path]; ok {
			return l
		}
		i := strings.LastIndexAny(path, ".[")
		if i < 0 {
			return 0
		}
		path = path[:i]
	}
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
			if s := suggest.Closest(key, allowed); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
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
			jobWhere := named(fmt.Sprintf("job %d", i), job)
			c.keys(job, jp, jobWhere, jobKeys, func(key string, value ast.Node) {
				switch key {
				case "defaults":
					c.noteLines(jp+".defaults", value)
				case "repeat":
					c.keys(value, jp+".repeat", jobWhere, repeatKeys, nil)
				case "steps":
					for j, step := range items(value) {
						sp := fmt.Sprintf("%s.steps[%d]", jp, j)
						c.lines[sp] = lineOf(step)
						where := named(fmt.Sprintf("%s, step %d", jobWhere, j), step)
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func (c *checker) jobWhere(i int) string {
	return jobWhere(c.wf.Jobs, i)
}

func (c *checker) stepWhere(i, j int) string {
	return stepWhere(c.jobWhere(i), j, c.wf.Jobs[i].Steps[j])
}

// checkAliases tells what is wrong with the names the workflow's actions
// give, each on its line.
func (c *checker) checkAliases(problems []aliasProblem) {
	for _, p := range problems {
		if p.name != "" {
			c.add(SeverityError, c.line("actions."+p.name), "", "%s", firstLine(p.msg))
			continue
		}
		if p.job == workflowDefaults {
			c.add(SeverityError, c.line("defaults."+p.action), "", "%s", firstLine(p.msg))
			continue
		}
		c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].defaults.%s", p.job, p.action)), c.jobWhere(p.job), "%s", firstLine(p.msg))
	}
}

// defaultsPath returns the path of the defaults of action in the job at jp,
// or in the workflow when jp is empty, as the workflow writes them: by the
// action in full, or by a name its actions give it.
func (c *checker) defaultsPath(jp, action string) string {
	at := join(jp, "defaults") + "."
	path := at + action
	if _, ok := c.lines[path]; ok {
		return path
	}
	for _, name := range slices.Sorted(maps.Keys(c.wf.Actions)) {
		named := at + name
		if _, ok := c.lines[named]; ok && c.wf.Actions[name] == action {
			return named
		}
	}
	return path
}

func (c *checker) checkActions() {
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			path := fmt.Sprintf("jobs[%d].steps[%d].uses", i, j)
			if actionref.IsExternal(st.Uses) {
				if _, err := actionref.Parse(st.Uses); err != nil {
					c.add(SeverityError, c.line(path), c.stepWhere(i, j), "uses: %s", firstLine(err.Error()))
					continue
				}
				c.readParams(st.Uses, c.line(path), c.stepWhere(i, j))
				continue
			}
			if len(c.opts.Actions) == 0 || slices.Contains(c.opts.Actions, st.Uses) {
				continue
			}
			// A name still here is one that is wrong, which is told where
			// the workflow gives it.
			if _, named := c.wf.Actions[st.Uses]; named {
				continue
			}
			msg := fmt.Sprintf("unknown action %q", st.Uses)
			if s := suggest.Closest(st.Uses, c.opts.Actions); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(path), c.stepWhere(i, j), "%s", msg)
		}
	}
}

// checkEmbedded reads the job file of each step that embeds one, and those
// that job embeds in turn, and reports a uses in them that names no action:
// a job is read by the names of the workflow, so a name it uses must be one
// the workflow gives. A path that is a template is not known before the
// run, and a file that cannot be read is left to the run to report.
func (c *checker) checkEmbedded() {
	if len(c.opts.Actions) == 0 {
		return
	}
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			line := c.line(fmt.Sprintf("jobs[%d].steps[%d].with.path", i, j))
			eachEmbeddedStep(st, map[string]bool{}, func(file string, k int, es *Step) {
				uses := es.Uses
				// A name still here is one that is wrong, which is told
				// where the workflow gives it.
				if _, named := c.wf.Actions[uses]; named || slices.Contains(c.opts.Actions, uses) {
					return
				}
				if actionref.IsExternal(uses) {
					if _, err := actionref.Parse(uses); err != nil {
						c.add(SeverityError, line, c.stepWhere(i, j), "%s: step %d: uses: %s", file, k, firstLine(err.Error()))
					}
					return
				}
				msg := fmt.Sprintf("%s: step %d uses %q, which is not an action of Probe or a name under actions", file, k, uses)
				if s := suggest.Closest(uses, append(slices.Sorted(maps.Keys(c.wf.Actions)), c.opts.Actions...)); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				}
				c.add(SeverityError, line, c.stepWhere(i, j), "%s", msg)
			})
		}
	}
}

// readParams reads the params the external action uses declares in its
// action.yml, once for each action, for checkWith to check its with
// against, and the fields of res it declares, for checkResults. An action.yml that cannot be read leaves its with unchecked,
// with a warning, as the run reads it again and fails then if it still
// cannot.
func (c *checker) readParams(uses string, line int, where string) {
	if c.opts.Manifest == nil {
		return
	}
	if c.readManifests == nil {
		c.readManifests = map[string]bool{}
	}
	if c.readManifests[uses] {
		return
	}
	c.readManifests[uses] = true
	m, err := c.opts.Manifest(uses, c.wf.basePath)
	if err != nil {
		c.add(SeverityWarning, line, where, "uses: the keys of with are not checked, as action.yml could not be read: %s", firstLine(err.Error()))
		return
	}
	if m.Params != nil {
		c.opts.Params[uses] = m.Params
	}
	if m.Res != nil {
		c.opts.Fields[uses] = actionrpc.Fields{Res: m.Res}
	}
}

// checkWith checks the keys of each step's with, and of the defaults of
// each job and of the workflow, against those the action takes. A key of a
// step's with that came from defaults is checked there, on the line it is
// written on, and a key that holds a template, which names a key only when
// it runs, is not checked.
func (c *checker) checkWith() {
	c.checkDefaults(c.wf.Defaults, "", "")
	shared, _ := c.wf.Defaults.(map[string]any)
	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		c.checkDefaults(job.Defaults, jp, c.jobWhere(i))
		defaults, _ := job.Defaults.(map[string]any)
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d].with", jp, j)
			// A key the step's with lacks comes from the defaults of its
			// job or of the workflow, where it is checked. A key the step
			// writes, by itself or by a YAML alias or merge key, is checked
			// here.
			inherited, _ := defaults[st.Uses].(map[string]any)
			fromWorkflow, _ := shared[st.Uses].(map[string]any)
			own := func(key string) bool {
				if _, written := c.lines[sp+"."+key]; written {
					return true
				}
				_, fromJob := inherited[key]
				_, fromShared := fromWorkflow[key]
				return !fromJob && !fromShared
			}
			c.withKeys(st.Uses, st.With, sp, c.stepWhere(i, j), "with", own)
		}
	}
}

// checkDefaults checks v, the defaults of the job at jp, or of the workflow
// when jp is empty: that each names an action, and that its keys are ones
// the action takes.
func (c *checker) checkDefaults(v any, jp, where string) {
	defaults, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, action := range slices.Sorted(maps.Keys(defaults)) {
		path := c.defaultsPath(jp, action)
		if _, named := c.wf.Actions[action]; named && !slices.Contains(c.opts.Actions, action) {
			continue
		}
		if len(c.opts.Actions) > 0 && !slices.Contains(c.opts.Actions, action) && !actionref.IsExternal(action) {
			msg := fmt.Sprintf("defaults: no action is named %q, so its defaults apply to no step", action)
			if s := suggest.Closest(action, c.opts.Actions); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(path), where, "%s", msg)
			continue
		}
		if actionref.IsExternal(action) {
			if _, err := actionref.Parse(action); err == nil {
				c.readParams(action, c.line(path), where)
			}
		}
		with, _ := defaults[action].(map[string]any)
		field := path
		if jp != "" {
			field = strings.TrimPrefix(path, jp+".")
		}
		c.withKeys(action, with, path, where, field, func(string) bool { return true })
	}
}

// withKeys checks the keys of with, at path, against those action takes,
// for each key own says belongs there.
func (c *checker) withKeys(action string, with map[string]any, path, where, field string, own func(string) bool) {
	params, ok := c.opts.Params[action]
	if !ok {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(with)) {
		// A key that holds a template names its key only when it runs. One
		// that holds an opener left unclosed is text, which is sent as it is.
		if slices.Contains(params, key) || len(expr.TemplateExprs(key)) > 0 || !own(key) {
			continue
		}
		msg := fmt.Sprintf("%s: unknown key %q for the %s action", field, key, action)
		if s := suggest.Closest(key, params); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		}
		c.add(SeverityError, c.line(path+"."+key), where, "%s", msg)
	}
}

// checkRequired reports a step whose with, the defaults merged into it,
// lacks a key its action cannot run without, as the action refuses to run
// it. A with with a key that holds a template, which names its key only
// when it runs, is not checked.
func (c *checker) checkRequired() {
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			groups := c.opts.Required[st.Uses]
			if len(groups) == 0 || slices.ContainsFunc(slices.Collect(maps.Keys(st.With)), isTemplate) {
				continue
			}
			for _, group := range groups {
				if slices.ContainsFunc(group, func(key string) bool { _, ok := st.With[key]; return ok }) {
					continue
				}
				path := fmt.Sprintf("jobs[%d].steps[%d].with", i, j)
				c.add(SeverityError, c.line(path), c.stepWhere(i, j), "with: the %s action cannot run without %s, which neither the step nor its defaults give", st.Uses, orNames(group))
			}
		}
	}
}

func isTemplate(s string) bool {
	return len(expr.TemplateExprs(s)) > 0
}

// orNames lists names as a, b or c.
func orNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func (c *checker) checkNeeds() {
	ids := map[string]bool{}
	for _, job := range c.wf.Jobs {
		ids[job.ID] = true
	}
	unknown := false
	for i, job := range c.wf.Jobs {
		for _, need := range job.Needs {
			if ids[need] {
				continue
			}
			unknown = true
			msg := fmt.Sprintf("needs: no job has the id %q", need)
			if s := suggest.Closest(need, slices.Sorted(maps.Keys(ids))); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].needs", i)), c.jobWhere(i), "%s", msg)
		}
	}
	if unknown {
		return
	}

	s := NewJobScheduler()
	for i := range c.wf.Jobs {
		if err := s.AddJob(&c.wf.Jobs[i]); err != nil {
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d]", i)), c.jobWhere(i), "%s", firstLine(err.Error()))
			return
		}
	}
	if err := s.ValidateDependencies(); err != nil {
		c.add(SeverityError, 0, "", "%s", firstLine(err.Error()))
	}
}

// checkProblems tells what keeps the jobs from running as they are written,
// which a run is refused for as the workflow is loaded, and the templates of
// the workflow's vars that do not parse.
func (c *checker) checkProblems(problems []problem) {
	for _, p := range templateProblems("vars", "", "vars", c.wf.Vars) {
		c.addProblem(p)
	}
	for _, p := range problems {
		c.addProblem(p)
	}
}

// addProblem tells p by the first line of its error, which says where in
// the expression it is, and how the expression is likely meant to be
// written where it is written as another language would write it.
func (c *checker) addProblem(p problem) {
	msg := firstLine(p.err.Error())
	if h, ok := errors.AsType[*expr.HintError](p.err); ok {
		msg += "; " + strings.Join(h.Hints, "; ")
	}
	c.add(SeverityError, c.line(p.path), p.where, "%s", msg)
}

// publisher is a step that publishes outputs.
type publisher struct {
	job, step int
	id        string
	keys      []string
}

func (c *checker) checkOutputs() {
	// A step without an id publishes its outputs by their names alone, so it
	// is kept under none. Only the steps that publish outputs are kept under
	// an id.
	byID := map[string][]publisher{}
	byKey := map[string][]publisher{}
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			p := publisher{job: i, step: j, id: st.ID, keys: slices.Sorted(maps.Keys(st.Outputs))}
			if len(p.keys) > 0 && st.ID != "" {
				byID[st.ID] = append(byID[st.ID], p)
			}
			for _, k := range p.keys {
				byKey[k] = append(byKey[k], p)
			}
		}
	}
	needs := c.ancestors()
	// The names read by themselves, as outputs.token, rather than through
	// the id of the step that publishes them.
	flat := map[string]bool{}

	c.eachExpr(func(e exprAt) {
		chains := expr.Reads(e.input, "outputs")
		for _, chain := range chains {
			if len(byID[chain[0]]) == 0 {
				flat[chain[0]] = true
			}
		}
		// outputs.auth.token also reads outputs.auth, which is not reported
		// on its own.
		deeper := map[string]bool{}
		for _, chain := range chains {
			if len(chain) > 1 {
				deeper[chain[0]] = true
			}
		}
		seen := map[string]bool{}
		for _, chain := range chains {
			key := strings.Join(chain[:min(2, len(chain))], ".")
			if seen[key] || (len(chain) == 1 && deeper[chain[0]]) {
				continue
			}
			seen[key] = true
			msg, severity := c.outputRead(chain, byID, byKey, needs, e.job, e.step, e.late)
			if msg != "" {
				c.add(severity, c.line(e.path), e.where, "%s: outputs.%s %s", e.field, key, msg)
			}
		}
	})
	c.checkOutputNames(byID, byKey, flat, needs)
}

// checkOutputNames warns of an output name that more than one step
// publishes where a value is lost by it. outputs.<name> holds the value of
// the step that publishes it first, and each value is read through the id
// of its step, which draws no warning. What does is a step without an id
// publishing a name another step may publish first, as its value then
// cannot be read, and outputs.<name> read by itself while more than one step publishes
// it. An output named as the id of a step that publishes is warned of
// whoever reads it: outputs.<name> is that step's outputs or the value,
// whichever is published first.
func (c *checker) checkOutputNames(byID, byKey map[string][]publisher, flat map[string]bool, needs []map[int]bool) {
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		pubs := byKey[key]
		for n, p := range pubs {
			path := fmt.Sprintf("jobs[%d].steps[%d].outputs.%s", p.job, p.step, key)
			where := c.stepWhere(p.job, p.step)
			if owners := byID[key]; len(owners) > 0 && p.id != key {
				owner := owners[0]
				c.add(SeverityWarning, c.line(path), where, "outputs: %s is the id of %s too, so outputs.%s is that step's outputs or this value, whichever is published first; give this output another name", key, c.stepWhere(owner.job, owner.step), key)
				continue
			}
			if p.id == "" && len(pubs) > 1 {
				if other, ok := publishedBefore(p, pubs, needs); ok {
					c.add(SeverityWarning, c.line(path), where, "outputs: %s is also published by %s, so outputs.%s holds the value of whichever publishes it first, and this step has no id to read its own through; give it one", key, c.stepWhere(other.job, other.step), key)
				}
				continue
			}
			if n == 0 {
				continue
			}
			first := c.stepWhere(pubs[0].job, pubs[0].step)
			switch {
			case flat[key]:
				c.add(SeverityWarning, c.line(path), where, "outputs: %s is also published by %s, and outputs.%s, which holds the value of whichever publishes it first, is read; read outputs.%s.%s for this one", key, first, key, p.id, key)
			}
		}
	}
}

// publishedBefore returns another step of pubs that may publish the name
// before p does, and whether there is one. A step sure to come after p, as
// a later step of its job or a step of a job that needs p's, does not.
func publishedBefore(p publisher, pubs []publisher, needs []map[int]bool) (publisher, bool) {
	for _, o := range pubs {
		switch {
		case o.job == p.job && o.step == p.step:
		case o.job == p.job && o.step > p.step:
		case o.job != p.job && needs[o.job][p.job]:
		default:
			return o, true
		}
	}
	return publisher{}, false
}

// exprAt is an expression of a job or a step, or one of the templates of
// it, and where it is.
type exprAt struct {
	path, where, field string
	// job and step are the indexes of the job and the step it is in; step
	// is -1 for a field of the job read before its steps.
	job, step int
	// late is whether it is read once the step has published its outputs,
	// as test and echo are.
	late  bool
	input string
}

// eachExpr calls f with each expression of the jobs and their steps, and
// each template, in the order they are read.
func (c *checker) eachExpr(f func(exprAt)) {
	templates := func(e exprAt, v any) {
		forStrings(e.path, v, func(path, s string) {
			for _, input := range expr.TemplateExprs(s) {
				e.path, e.input = path, input
				f(e)
			}
		})
	}
	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		jobField := func(key string) exprAt {
			return exprAt{path: jp + "." + key, where: c.jobWhere(i), field: key, job: i, step: -1}
		}
		templates(jobField("name"), job.Name)
		e := jobField("skipif")
		e.input = job.SkipIf
		f(e)
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d]", jp, j)
			field := func(key string, late bool) exprAt {
				return exprAt{path: sp + "." + key, where: c.stepWhere(i, j), field: key, job: i, step: j, late: late}
			}
			expression := func(key string, late bool, input string) {
				e := field(key, late)
				e.input = input
				f(e)
			}
			templates(field("name", false), st.Name)
			templates(field("with", false), st.With)
			templates(field("vars", false), st.Vars)
			templates(field("iteration", false), st.Iteration.Expr)
			templates(field("wait", false), st.Wait)
			expression("skipif", false, st.SkipIf)
			for _, k := range slices.Sorted(maps.Keys(st.Outputs)) {
				expression("outputs."+k, false, st.Outputs[k])
			}
			expression("test", true, st.Test)
			templates(field("echo", true), st.Echo)
		}
	}
}

// The names an expression of a step, or the skipif of a job, reads from,
// and those the tests of a repeated job read from, from the fields of what
// they are evaluated against.
var (
	stepNames   = exprKeys(reflect.TypeFor[StepContext]())
	repeatNames = exprKeys(reflect.TypeFor[repeatTestEnv]())
)

func exprKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		if name := f.Tag.Get("expr"); name != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkNames checks the names that the expressions and templates read. A
// name that what they are evaluated against does not have, such as steps in
// the test of a step, is an error: there is nothing to read it from, so the
// expression fails: the step fails, or its skipif, wait or output is left
// out. A var not defined where it is read, by the workflow or by the step,
// whose own vars read only those of the workflow, is a warning, as it reads
// nil unless --var gives it, which check cannot know. A call of a method
// expressions do not have, such as res.body.contains("x"), is an error too,
// with how it is written in expressions, as it fails whatever it is called
// on.
//
// Of the templates of a job's name, which are evaluated against the job and
// so can read more than they are meant to, and of the workflow's vars, which
// read the environment, only the vars they read and the methods they call
// are checked.
func (c *checker) checkNames() {
	workflowVars := varNames(c.wf.Vars)
	seen := map[string]bool{}
	// later are the vars the step defines that are not defined yet where
	// input is read.
	read := func(path, where, field, input string, names, vars, later []string) {
		add := func(severity, format string, args ...any) {
			key := path + "\x00" + fmt.Sprintf(format, args...)
			if !seen[key] {
				seen[key] = true
				c.add(severity, c.line(path), where, "%s: "+format, append([]any{field}, args...)...)
			}
		}
		for _, name := range expr.Names(input) {
			if names == nil || slices.Contains(names, name) {
				continue
			}
			add(SeverityError, "%s is not defined; it can read %s%s", name, listNames(names), nameHint(name, names))
		}
		for _, h := range expr.MethodCallHints(input) {
			add(SeverityError, "%s", h)
		}
		if vars == nil {
			return
		}
		for _, chain := range expr.Reads(input, "vars") {
			if slices.Contains(vars, chain[0]) {
				continue
			}
			msg := fmt.Sprintf("vars.%s is not defined by the workflow, so it is nil unless --var gives it", chain[0])
			if slices.Contains(later, chain[0]) {
				msg = fmt.Sprintf("vars.%s is not defined yet: a step's vars read only those of the workflow, so it is nil unless --var gives it", chain[0])
			} else if s := suggest.Closest(chain[0], vars); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			add(SeverityWarning, "%s", msg)
		}
	}

	forStrings("vars", c.wf.Vars, func(path, s string) {
		for _, input := range expr.TemplateExprs(s) {
			read(path, "", "vars", input, nil, workflowVars, nil)
		}
	})
	c.eachExpr(func(e exprAt) {
		names, vars := stepNames, workflowVars
		var later []string
		if e.step < 0 {
			if e.field == "name" {
				names = nil
			}
		} else {
			vars, later = stepVars(workflowVars, c.wf.Jobs[e.job].Steps[e.step], e.field)
		}
		read(e.path, e.where, e.field, e.input, names, vars, later)
	})
	for i, job := range c.wf.Jobs {
		if job.Repeat == nil {
			continue
		}
		for _, t := range []struct{ key, test string }{
			{"test", job.Repeat.Test},
			{"baseline_test", job.Repeat.BaselineTest},
		} {
			read(fmt.Sprintf("jobs[%d].repeat.%s", i, t.key), c.jobWhere(i), "repeat."+t.key, t.test, repeatNames, workflowVars, nil)
		}
	}
}

// The fields of a step that are evaluated once its action has returned, and
// can read what it returned under res and req: test, echo and outputs. The
// rest are evaluated before it runs.
func readsResult(field string) bool {
	return field == "test" || field == "echo" || strings.HasPrefix(field, "outputs.")
}

// checkResults checks what the expressions and templates of each step read
// under res and req. Those evaluated before the step runs, such as its
// skipif, read them empty, which is an error. Those evaluated after, its
// test, echo and outputs, may read only the fields the step's action
// returns, as it declares them: a field it never returns reads nil, which
// is an error for a built-in action, and a warning for an external one,
// whose action.yml says what it returns without Probe knowing it. Only the
// top level is checked, as what is under a field, such as a body, is data.
func (c *checker) checkResults() {
	seen := map[string]bool{}
	add := func(severity string, e exprAt, format string, args ...any) {
		key := e.path + "\x00" + fmt.Sprintf(format, args...)
		if !seen[key] {
			seen[key] = true
			c.add(severity, c.line(e.path), e.where, "%s: "+format, append([]any{e.field}, args...)...)
		}
	}
	c.eachExpr(func(e exprAt) {
		if e.step < 0 {
			return
		}
		st := c.wf.Jobs[e.job].Steps[e.step]
		if !readsResult(e.field) {
			for _, name := range expr.Names(e.input) {
				if name == "res" || name == "req" {
					add(SeverityError, e, "%s is empty here, as the step has not run yet when its %s is evaluated; %s is read by the step's test, echo and outputs", name, e.field, name)
				}
			}
			return
		}
		fields, ok := c.resultFields(st)
		if !ok {
			return
		}
		severity := SeverityError
		if actionref.IsExternal(st.Uses) {
			severity = SeverityWarning
		}
		for _, name := range []string{"req", "res"} {
			declared, checked := fields[name]
			if !checked {
				continue
			}
			for _, chain := range expr.Reads(e.input, name) {
				if slices.Contains(declared, chain[0]) {
					continue
				}
				msg := fmt.Sprintf("%s.%s is not a field the %s action returns", name, chain[0], st.Uses)
				if s := suggest.Closest(chain[0], declared); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				} else {
					msg += fmt.Sprintf("; its %s has %s", name, listNames(declared))
				}
				add(severity, e, "%s", msg)
			}
		}
	})
}

// resultFields returns the fields of req and of res that the action of st
// returns, by req and res, leaving out either one its action does not
// declare, and false when what st reads of neither is checked: for an
// action that declares nothing, and for one that returns the keys of with,
// for a step whose with has a key that holds a template, which names its
// key only when it runs.
func (c *checker) resultFields(st *Step) (map[string][]string, bool) {
	f, ok := c.opts.Fields[st.Uses]
	if !ok || (f.With && slices.ContainsFunc(slices.Collect(maps.Keys(st.With)), isTemplate)) {
		return nil, false
	}
	req, res := f.Of(st.With)
	out := map[string][]string{"res": addedResFields(res)}
	// An external action declares only the fields of its res.
	if !actionref.IsExternal(st.Uses) {
		out["req"] = req
	}
	return out, true
}

// nameHint returns what name, which is not among names, was likely meant
// as, to follow what names it, or an empty string.
func nameHint(name string, names []string) string {
	step := slices.Equal(names, stepNames)
	switch {
	case expr.NameFor(name) != "":
		return fmt.Sprintf("; did you mean %s?", expr.NameFor(name))
	case suggest.Closest(name, names) != "":
		return fmt.Sprintf("; did you mean %q?", suggest.Closest(name, names))
	case step && name == "steps":
		return "; what an earlier step publishes is read under outputs"
	case step && name == strings.ToUpper(name) && strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "":
		lower := strings.ToLower(name)
		return fmt.Sprintf("; the environment is read only by the workflow's vars, as %s: \"{{%s}}\", which a step reads as vars.%s", lower, name, lower)
	}
	return ""
}

// varNames returns the names of vars, sorted, or nil when a name holds a
// template, so that the names are known only as the workflow runs.
func varNames(vars map[string]any) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		if len(expr.TemplateExprs(name)) > 0 {
			return nil
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// stepVars returns the names of the vars that field of a step reads, as a
// run gives them: the step's vars are evaluated against those of the
// workflow alone; its iteration, when a template gives it, against those
// and the step's own; and the rest against those, and the keys of the entry
// of the iteration it runs with. vars is nil when the names are not known
// before the step runs, as the keys of the entries a template gives are
// not. later are the names the step defines that field does not read.
func stepVars(workflow []string, st *Step, field string) (vars, later []string) {
	own := varNames(st.Vars)
	if workflow == nil || (own == nil && len(st.Vars) > 0) {
		return nil, nil
	}
	for _, entry := range st.Iteration.Entries {
		for name := range entry {
			own = append(own, name)
		}
	}
	switch {
	case field == "vars":
		return workflow, own
	case field != "iteration" && st.Iteration.Expr != "":
		return nil, nil
	}
	names := append(slices.Clone(workflow), own...)
	slices.Sort(names)
	return slices.Compact(names), nil
}

// listNames lists names as a, b and c.
func listNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// forStrings calls f with each string in v, a string or a map or list of
// them at any depth, keys too, and the path it is at under path.
func forStrings(path string, v any, f func(path, s string)) {
	switch v := v.(type) {
	case string:
		f(path, v)
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			f(path+"."+k, k)
			forStrings(path+"."+k, v[k], f)
		}
	case []any:
		for i, e := range v {
			forStrings(fmt.Sprintf("%s[%d]", path, i), e, f)
		}
	}
}

// outputRead returns what is wrong with reading chain from outputs at job i
// and step j, and how bad it is; an empty message when nothing is.
func (c *checker) outputRead(chain []string, byID, byKey map[string][]publisher, needs []map[int]bool, i, j int, late bool) (string, string) {
	if pubs := byID[chain[0]]; len(pubs) > 0 {
		if len(chain) > 1 {
			var with []publisher
			var keys []string
			for _, p := range pubs {
				if slices.Contains(p.keys, chain[1]) {
					with = append(with, p)
				}
				keys = append(keys, p.keys...)
			}
			if len(with) == 0 {
				slices.Sort(keys)
				return fmt.Sprintf("is not published: step %q publishes %s", chain[0], strings.Join(slices.Compact(keys), ", ")), SeverityError
			}
			pubs = with
		}
		return c.ordered(pubs, needs, i, j, late)
	}

	pubs := byKey[chain[0]]
	if len(pubs) == 0 {
		msg := "is not published by any step"
		if s := suggest.Closest(chain[0], publishedNames(byID, byKey)); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		}
		return msg, SeverityError
	}
	return c.ordered(pubs, needs, i, j, late)
}

// ordered returns what is wrong with reading what pubs publish at job i and
// step j: nothing when one of them publishes it before, an error when each
// of them is a step that comes later, and a warning when one is in a job
// that job i does not need, so that it may or may not have run.
func (c *checker) ordered(pubs []publisher, needs []map[int]bool, i, j int, late bool) (string, string) {
	var unordered []publisher
	for _, p := range pubs {
		if p.job == i {
			if p.step < j || (p.step == j && late) {
				return "", ""
			}
			continue
		}
		if needs[i][p.job] {
			return "", ""
		}
		unordered = append(unordered, p)
	}
	switch len(unordered) {
	case 0:
		return "is read before its step publishes it", SeverityError
	case 1:
		return fmt.Sprintf("may not be published yet: job %q does not need job %q, whose step publishes it", c.wf.Jobs[i].Name, c.wf.Jobs[unordered[0].job].Name), SeverityWarning
	default:
		return fmt.Sprintf("may not be published yet: job %q does not need the jobs whose steps publish it", c.wf.Jobs[i].Name), SeverityWarning
	}
}

func publishedNames(byID, byKey map[string][]publisher) []string {
	names := slices.Collect(maps.Keys(byID))
	for k := range byKey {
		names = append(names, k)
	}
	sort.Strings(names)
	return slices.Compact(names)
}

// ancestors returns, for each job, the jobs it needs, directly or through
// the jobs they need.
func (c *checker) ancestors() []map[int]bool {
	index := map[string]int{}
	for i, job := range c.wf.Jobs {
		index[job.ID] = i
	}
	out := make([]map[int]bool, len(c.wf.Jobs))
	var visit func(i int, into map[int]bool)
	visit = func(i int, into map[int]bool) {
		for _, id := range c.wf.Jobs[i].Needs {
			k, ok := index[id]
			if !ok || into[k] {
				continue
			}
			into[k] = true
			visit(k, into)
		}
	}
	for i := range c.wf.Jobs {
		out[i] = map[int]bool{}
		visit(i, out[i])
	}
	return out
}

// checkTests warns of steps that nothing checks, and of tests that read
// nothing, which pass or fail whatever the step does.
func (c *checker) checkTests() {
	for i, job := range c.wf.Jobs {
		// A step whose runs the test of a repeated job reads is checked by
		// that test.
		var tested map[string]bool
		if job.Repeat != nil {
			tested = repeatReads(job.Repeat.Test, job.Repeat.BaselineTest)
		}
		for j, st := range job.Steps {
			path := fmt.Sprintf("jobs[%d].steps[%d]", i, j)
			id := st.ID
			if id == "" {
				id = defaultStepID(j)
			}
			switch {
			case strings.TrimSpace(st.Test) == "":
				// The hello action does nothing that could go wrong, so
				// a step of it, which is there for its echo or to publish
				// what it is given, has nothing to check.
				if st.Uses != "hello" && !c.checkedByContract(st.Uses, st.With) && !tested[id] {
					c.add(SeverityWarning, c.line(path), c.stepWhere(i, j), "nothing checks this step: it has no test")
				}
			case expr.Parse(st.Test) == nil && expr.ReadsNothing(st.Test):
				c.add(SeverityWarning, c.line(path+".test"), c.stepWhere(i, j), "test reads nothing from the step, so it gives the same result whatever the step does")
			}
		}
	}
}

// checkedByContract reports whether with asks the action uses to check the
// step against a contract, as CheckOptions.Contracts says. The same key
// given to another action asks it nothing of the kind.
func (c *checker) checkedByContract(uses string, with map[string]any) bool {
	for _, key := range c.opts.Contracts[uses] {
		if _, ok := with[key].(map[string]any); ok {
			return true
		}
	}
	return false
}

// The fields of a step that the tests of a repeated job read under steps,
// those that the test against the run compared to reads under
// baseline.steps, and those of their rt.
var (
	repeatRunFields   = []string{"runs", "success", "failure", "skipped", "success_rate", "rt"}
	baselineRunFields = []string{"runs", "success", "success_rate", "rt"}
	repeatRTFields    = []string{"min", "mean", "p50", "p90", "p95", "p99", "max"}
)

// checkRepeatTests checks that the tests of a repeated job read steps of the
// job by their ids, and fields that they have; that only the test against
// the run compared to reads it, under baseline.steps; and that this test
// reads it.
func (c *checker) checkRepeatTests() {
	for i, job := range c.wf.Jobs {
		if job.Repeat == nil {
			continue
		}
		ids := make([]string, 0, len(job.Steps))
		for j, st := range job.Steps {
			id := st.ID
			if id == "" {
				id = defaultStepID(j)
			}
			ids = append(ids, id)
		}
		for _, t := range []struct{ key, test string }{
			{"test", job.Repeat.Test},
			{"baseline_test", job.Repeat.BaselineTest},
		} {
			if strings.TrimSpace(t.test) == "" || expr.Parse(t.test) != nil {
				continue
			}
			key := "repeat." + t.key
			line := c.line(fmt.Sprintf("jobs[%d].%s", i, key))
			// A chain is read with each of its prefixes, so the same finding
			// comes from several of them.
			seen := map[string]bool{}
			add := func(severity, msg string) {
				if msg != "" && !seen[msg] {
					seen[msg] = true
					c.add(severity, line, c.jobWhere(i), "%s: %s", key, msg)
				}
			}
			for _, chain := range expr.Reads(t.test, "steps") {
				add(SeverityError, repeatChainProblem("steps", chain, ids, repeatRunFields))
			}
			baseline := expr.Reads(t.test, "baseline")
			byKey := expr.ReadsByVariableKey(t.test, "baseline")
			// A test against the run compared to that reads it by a key
			// known only when it runs is refused as the workflow is loaded;
			// see Repeat.Validate.
			switch {
			case t.key == "test" && len(baseline)+len(byKey) > 0:
				add(SeverityError, "baseline is read only by repeat.baseline_test")
			case t.key == "test":
			case len(baseline) == 0 && len(byKey) == 0:
				add(SeverityWarning, "it reads nothing under baseline, so it can go in repeat.test")
			}
			for _, chain := range baseline {
				if t.key != "baseline_test" {
					break
				}
				if chain[0] != "steps" {
					add(SeverityError, fmt.Sprintf("baseline has no %s; it has steps", chain[0]))
					continue
				}
				if len(chain) > 1 {
					add(SeverityError, repeatChainProblem("baseline.steps", chain[1:], ids, baselineRunFields))
				}
			}
		}
	}
}

// repeatChainProblem returns what is wrong with chain, read by a test of a
// repeated job under prefix, given the ids of the steps of the job and the
// fields that a step has there; empty when nothing is.
func repeatChainProblem(prefix string, chain, ids, fields []string) string {
	switch {
	case !slices.Contains(ids, chain[0]):
		return fmt.Sprintf("%s.%s is not a step of the job; its steps are %s",
			prefix, chain[0], strings.Join(ids, ", "))
	case len(chain) > 1 && !slices.Contains(fields, chain[1]):
		return fmt.Sprintf("%s.%s has no %s; it has %s",
			prefix, chain[0], chain[1], strings.Join(fields, ", "))
	case len(chain) > 2 && chain[1] != "rt":
		return fmt.Sprintf("%s.%s.%s is a number, which has no %s",
			prefix, chain[0], chain[1], chain[2])
	case len(chain) > 2 && !slices.Contains(repeatRTFields, chain[2]):
		return fmt.Sprintf("%s.%s.rt has no %s; it has %s",
			prefix, chain[0], chain[2], strings.Join(repeatRTFields, ", "))
	case len(chain) > 3:
		return fmt.Sprintf("%s.%s.rt.%s is a number, which has no %s",
			prefix, chain[0], chain[2], chain[3])
	}
	return ""
}
