package probe

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
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
	// Manifest reads the action.yml of an external action, a local one
	// relative to baseDir, so that its with is checked against the params
	// it declares. When it is nil, the with of an external action is not
	// checked, and nothing is read from the network.
	Manifest func(uses, baseDir string) (*actionref.Manifest, error)
}

// Check reads the workflow at path, as a run reads it, and returns what is
// wrong with it or weak in it, without running anything: keys a workflow
// does not take, what a run would refuse to load, actions that do not
// exist, needs that cannot be met, expressions and templates that do not
// parse, and outputs read before or without being published, as errors;
// and steps that nothing checks, or whose test reads nothing, as warnings.
//
// The error it returns is for a workflow that cannot be read at all, such
// as a file that does not exist.
func Check(path string, opts CheckOptions) ([]Finding, error) {
	p := New(path, false)
	files, err := p.yamlFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no YAML file", path)
	}
	data, err := p.readYamlFiles(files)
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
	if err := c.mapFiles(files); err != nil {
		return nil, err
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
	p.Config.Actions = opts.Actions
	p.aliasProblems = &aliases
	if err := p.Load(); err != nil {
		c.add(SeverityError, 0, "", "%s", firstLine(err.Error()))
		return c.sorted(), nil
	}
	c.wf = &p.workflow
	c.checkAliases(aliases)
	c.checkActions()
	c.checkEmbedded()
	c.checkWith()
	c.checkNeeds()
	c.checkSteps()
	c.checkExpressions()
	c.checkOutputs()
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
// file. readYamlFiles adds a newline after a file that does not end in one,
// unless it is the last.
func (c *checker) mapFiles(files []string) error {
	line := 1
	for k, f := range files {
		c.files = append(c.files, fileStart{name: f, line: line})
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		line += bytes.Count(data, []byte("\n"))
		if k < len(files)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			line++
		}
	}
	return nil
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
			if s := suggest(key, allowed); s != "" {
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

// suggest returns the key of allowed closest to key, when one is close
// enough to be what was meant.
func suggest(key string, allowed []string) string {
	best, bestDist := "", 3
	for _, a := range allowed {
		if d := distance(key, a); d < bestDist {
			best, bestDist = a, d
		}
	}
	return best
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func (c *checker) jobWhere(i int) string {
	return fmt.Sprintf("job %d %q", i, c.wf.Jobs[i].Name)
}

func (c *checker) stepWhere(i, j int) string {
	st := c.wf.Jobs[i].Steps[j]
	if st.Name == "" {
		return fmt.Sprintf("%s, step %d", c.jobWhere(i), j)
	}
	return fmt.Sprintf("%s, step %d %q", c.jobWhere(i), j, st.Name)
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
			if s := suggest(st.Uses, c.opts.Actions); s != "" {
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
				if s := suggest(uses, append(slices.Sorted(maps.Keys(c.wf.Actions)), c.opts.Actions...)); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				}
				c.add(SeverityError, line, c.stepWhere(i, j), "%s", msg)
			})
		}
	}
}

// readParams reads the params the external action uses declares in its
// action.yml, once for each action, for checkWith to check its with
// against. An action.yml that cannot be read leaves its with unchecked,
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
			if s := suggest(action, c.opts.Actions); s != "" {
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
		if s := suggest(key, params); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		}
		c.add(SeverityError, c.line(path+"."+key), where, "%s", msg)
	}
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
			if s := suggest(need, slices.Sorted(maps.Keys(ids))); s != "" {
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

func (c *checker) checkSteps() {
	for i := range c.wf.Jobs {
		if err := c.wf.Jobs[i].validateSteps(); err != nil {
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].steps", i)), c.jobWhere(i), "%s", firstLine(err.Error()))
		}
	}
}

// expression checks that an expression parses.
func (c *checker) expression(path, where, field, input string) {
	if strings.TrimSpace(input) == "" {
		return
	}
	if err := expr.Parse(input); err != nil {
		c.add(SeverityError, c.line(path), where, "%s: %s", field, firstLine(err.Error()))
	}
}

// templates checks that the templates in v, a string or a map or list of
// them at any depth, keys too, parse.
func (c *checker) templates(path, where, field string, v any) {
	switch v := v.(type) {
	case string:
		for _, e := range expr.TemplateExprs(v) {
			if err := expr.Parse(e); err != nil {
				c.add(SeverityError, c.line(path), where, "%s: template {{%s}}: %s", field, e, firstLine(err.Error()))
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			c.templates(path+"."+k, where, field+"."+k, k)
			c.templates(path+"."+k, where, field+"."+k, v[k])
		}
	case []any:
		for i, e := range v {
			c.templates(fmt.Sprintf("%s[%d]", path, i), where, fmt.Sprintf("%s[%d]", field, i), e)
		}
	}
}

func (c *checker) checkExpressions() {
	c.templates("vars", "", "vars", c.wf.Vars)
	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		c.templates(jp+".name", c.jobWhere(i), "name", job.Name)
		c.expression(jp+".skipif", c.jobWhere(i), "skipif", job.SkipIf)
		if job.Repeat != nil {
			c.expression(jp+".repeat.test", c.jobWhere(i), "repeat.test", job.Repeat.Test)
			c.expression(jp+".repeat.baseline_test", c.jobWhere(i), "repeat.baseline_test", job.Repeat.BaselineTest)
		}
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d]", jp, j)
			where := c.stepWhere(i, j)
			c.templates(sp+".name", where, "name", st.Name)
			c.templates(sp+".with", where, "with", st.With)
			c.templates(sp+".vars", where, "vars", st.Vars)
			c.templates(sp+".echo", where, "echo", st.Echo)
			c.templates(sp+".iteration", where, "iteration", st.Iteration.Expr)
			c.expression(sp+".test", where, "test", st.Test)
			c.expression(sp+".skipif", where, "skipif", st.SkipIf)
			for _, k := range slices.Sorted(maps.Keys(st.Outputs)) {
				c.expression(sp+".outputs."+k, where, "outputs."+k, st.Outputs[k])
			}
		}
	}
}

// publisher is a step that publishes outputs.
type publisher struct {
	job, step int
	id        string
	keys      []string
}

func (c *checker) checkOutputs() {
	// A step without an id is given one by its index, step_0 and so on, in
	// each job, so one id may name steps of several jobs. Only the steps
	// that publish outputs are kept under it, each of them.
	byID := map[string][]publisher{}
	byKey := map[string][]publisher{}
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			p := publisher{job: i, step: j, id: st.ID, keys: slices.Sorted(maps.Keys(st.Outputs))}
			if len(p.keys) > 0 {
				byID[st.ID] = append(byID[st.ID], p)
			}
			for _, k := range p.keys {
				byKey[k] = append(byKey[k], p)
			}
		}
	}
	needs := c.ancestors()

	// read checks the outputs input, an expression, reads at the point of
	// job i and step j, -1 for a field of the job read before its steps.
	// late is whether the field is read once the step has published its
	// outputs, as test and echo are.
	read := func(path, where, field string, i, j int, late bool, input string) {
		chains := expr.Reads(input, "outputs")
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
			msg, severity := c.outputRead(chain, byID, byKey, needs, i, j, late)
			if msg != "" {
				c.add(severity, c.line(path), where, "%s: outputs.%s %s", field, key, msg)
			}
		}
	}
	readTemplates := func(path, where, field string, i, j int, late bool, v any) {
		forStrings(path, v, func(path, s string) {
			for _, e := range expr.TemplateExprs(s) {
				read(path, where, field, i, j, late, e)
			}
		})
	}

	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		readTemplates(jp+".name", c.jobWhere(i), "name", i, -1, false, job.Name)
		read(jp+".skipif", c.jobWhere(i), "skipif", i, -1, false, job.SkipIf)
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d]", jp, j)
			where := c.stepWhere(i, j)
			readTemplates(sp+".name", where, "name", i, j, false, st.Name)
			readTemplates(sp+".with", where, "with", i, j, false, st.With)
			readTemplates(sp+".vars", where, "vars", i, j, false, st.Vars)
			readTemplates(sp+".iteration", where, "iteration", i, j, false, st.Iteration.Expr)
			read(sp+".skipif", where, "skipif", i, j, false, st.SkipIf)
			for _, k := range slices.Sorted(maps.Keys(st.Outputs)) {
				read(sp+".outputs."+k, where, "outputs."+k, i, j, false, st.Outputs[k])
			}
			read(sp+".test", where, "test", i, j, true, st.Test)
			readTemplates(sp+".echo", where, "echo", i, j, true, st.Echo)
		}
	}
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
		if s := suggest(chain[0], publishedNames(byID, byKey)); s != "" {
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
				id = fmt.Sprintf("step_%d", j)
			}
			switch {
			case strings.TrimSpace(st.Test) == "":
				if !checkedByContract(st.Uses, st.With) && !tested[id] {
					c.add(SeverityWarning, c.line(path), c.stepWhere(i, j), "nothing checks this step: it has no test")
				}
			case expr.Parse(st.Test) == nil && expr.ReadsNothing(st.Test):
				c.add(SeverityWarning, c.line(path+".test"), c.stepWhere(i, j), "test reads nothing from the step, so it gives the same result whatever the step does")
			}
		}
	}
}

// contracts are the keys of with that ask an action to check a step
// against a contract, by the action that does.
var contracts = map[string]string{"openapi": "http", "proto": "grpc"}

// checkedByContract reports whether with asks the action uses to check the
// step against a contract, as openapi asks the http action and proto the
// grpc action, which checks it as a test does. The same key given to
// another action asks it nothing of the kind.
func checkedByContract(uses string, with map[string]any) bool {
	for key, action := range contracts {
		if _, ok := with[key].(map[string]any); ok && uses == action {
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
				id = fmt.Sprintf("step_%d", j)
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
