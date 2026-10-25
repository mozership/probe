package probe

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/parser"
	"github.com/mozership/probe/actionref"
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
	// ReqFields and ResFields are the top-level fields of the req and the
	// res each action returns, by its name, so that a test, an echo or an
	// output that reads another is reported. What a step reads of the req
	// or the res of an action they do not name is not checked.
	ReqFields map[string][]string
	ResFields map[string][]string
	// ReqTypes and ResTypes are the kinds of the fields of the req and the
	// res each action returns, by its name and then the field's: number,
	// string, bool, map or list. An expression that does with a field what
	// its kind does not allow is reported, as res.code == "200 OK" is, which
	// is never true of a number. A field of another kind or none, a field
	// they leave out, and a field of an action they do not name, may hold
	// anything, as may every field of an external action.
	ReqTypes map[string]map[string]string
	ResTypes map[string]map[string]string
	// WithFields are the actions whose req and res hold the keys of the
	// step's with too, besides the fields they are named with, as an action
	// that returns what it is given does.
	WithFields []string
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
// parse, that read a name that is not there to read, or that do with a
// value what its kind does not allow, and outputs read before or without
// being published, as errors; and steps that nothing checks, tests that
// read nothing, and vars that nothing defines, as warnings.
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
	c.opts.ResFields = maps.Clone(opts.ResFields)
	if c.opts.ResFields == nil {
		c.opts.ResFields = map[string][]string{}
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
	c.reportAliasProblems(aliases)
	c.checkActions()
	c.checkEmbedded()
	c.checkWith()
	c.checkRequired()
	c.checkNeeds()
	c.checkVarsTemplates()
	c.reportJobProblems(problems)
	c.checkOutputs()
	c.checkNames()
	c.checkResults()
	c.checkTypes()
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// listNames lists names as a, b and c.
func listNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
