// Package check finds what keeps a workflow from running, or from checking
// what it seems to, without running it: probe check.
package check

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/mozership/probe/internal/actionref"
	"github.com/mozership/probe/internal/engine"
	"github.com/mozership/probe/internal/workflowfile"
	"github.com/mozership/probe/internal/yamlnode"
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

// CheckOptions tunes Check. The probe command gives it what it is to know of
// the built-in actions.
type CheckOptions struct {
	// Actions are the names of the actions a step may use, besides the
	// external ones it names by a path or a repository. When it is empty,
	// the names are not checked.
	Actions []string
	// Replaceable are those of Actions a workflow can give to an external
	// action under actions, as they are being replaced by one.
	Replaceable []string
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
	// NeedsNoCheck are the actions whose steps pass without a test, as
	// Config.NeedsNoCheck says. A step of one that has no test is not warned
	// of as unchecked.
	NeedsNoCheck []string
	// Required are the keys of with each action cannot run without, by its
	// name, each as the keys that stand for one another, of which one is
	// enough: as get stands for method in the http action.
	Required map[string][][]string
	// Choices are the values that the keys of with which take only some
	// take, by the action's name and then the key's, so that a value
	// written that is not one of them is reported, as op: remove is for the
	// file action. A value that holds a template, which is known only when
	// it runs, and an empty one, which leaves the key to its default, are
	// not checked.
	Choices map[string]map[string]Choices
	// Manifest reads the action.yml of an external action, a local one
	// relative to baseDir, so that its with is checked against the params
	// it declares, and what a step reads of its res against the fields of
	// res it declares. When it is nil, neither is checked, and nothing is
	// read from the network.
	Manifest func(uses, baseDir string) (*actionref.Manifest, error)
}

// Choices are the values a key of with takes, as CheckOptions.Choices
// gives them.
type Choices struct {
	// Values are the values the key takes.
	Values []string
	// IgnoreCase says that the key takes a value in any case.
	IgnoreCase bool
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
	// What a run warns of as it loads is told as a finding here, as load
	// writes no warning.
	files, err := workflowfile.Paths(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no YAML file", path)
	}
	data, contents, err := workflowfile.Read(files, engine.ReadFile)
	if err != nil {
		return nil, err
	}

	c := newChecker(opts)
	c.files = yamlnode.MapFiles(files, contents)
	for _, r := range workflowfile.RedefinedJobs(files, contents) {
		c.findings = append(c.findings, Finding{
			Severity: SeverityWarning,
			File:     r.File,
			Line:     r.Line,
			Message:  r.Message(),
		})
	}

	// A key may be written more than once, as a run reads the workflow,
	// so that the files given can override one another.
	src, err := engine.NewSource(files, contents, data)
	if err != nil {
		c.add(SeverityError, 0, "", "YAML: %s", engine.FirstLine(err.Error()))
		return c.sorted(), nil
	}
	// The files the steps include are read past those given, and what kept
	// an entry from including its file is told on its line. The entry is
	// left out, so that the rest is checked.
	c.files = src.Files
	for _, pr := range src.Problems {
		c.add(SeverityError, pr.Line, "", "%s", pr.Msg)
	}
	for _, doc := range src.File.Docs {
		c.walkWorkflow(doc.Body)
	}

	// What is wrong with the names of external actions is told on the line
	// of each, with whatever else is wrong, rather than ending the check.
	// The workflow is loaded from the source walked above, so that the
	// lines mapped to the files are those of the source loaded.
	wf, aliases, problems, err := engine.LoadSource(path, opts.Actions, opts.Replaceable, src)
	if err != nil {
		c.add(SeverityError, 0, "", "%s", engine.FirstLine(err.Error()))
		return c.sorted(), nil
	}
	c.wf = wf
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
	// opts are the options Check was given, which the checker only reads.
	opts CheckOptions
	// params and resFields are the params and the fields of res of each
	// action, by its name: those of opts, with those of each external
	// action added as its action.yml is read.
	params    map[string][]string
	resFields map[string][]string
	// contracts are the keys of with that ask each action to check a step
	// against a contract: those of opts, and those the action.yml of an
	// external action declares.
	contracts map[string][]string
	wf        *engine.Workflow
	findings  []Finding
	// lines are the lines of the keys of the workflow, by their path, such
	// as jobs[0].steps[1].test, and of each job and step, by jobs[0] and
	// jobs[0].steps[1].
	lines map[string]int
	// files are the files read, given and included, with the line of the
	// source that each one starts on.
	files yamlnode.Files
	// choices are the values that the keys of with which take only some
	// take, by the action's name and then the key's: those of opts, and
	// those the action.yml of an external action declares.
	choices map[string]map[string]Choices
	// readManifests are the external actions whose action.yml was read.
	readManifests map[string]bool
	// reported are the findings addOnce added, by their path and message.
	reported map[string]bool
}

// newChecker returns a checker for opts, with its own copy of what it adds
// to as it reads, so that the maps of opts are left as they were.
func newChecker(opts CheckOptions) *checker {
	c := &checker{
		opts:      opts,
		params:    maps.Clone(opts.Params),
		resFields: maps.Clone(opts.ResFields),
		contracts: maps.Clone(opts.Contracts),
		choices:   maps.Clone(opts.Choices),
		lines:     map[string]int{},
		reported:  map[string]bool{},
	}
	if c.params == nil {
		c.params = map[string][]string{}
	}
	if c.resFields == nil {
		c.resFields = map[string][]string{}
	}
	if c.contracts == nil {
		c.contracts = map[string][]string{}
	}
	if c.choices == nil {
		c.choices = map[string]map[string]Choices{}
	}
	return c
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

// addOnce adds a finding at the line of path, unless one with the same
// message was added there by addOnce already. A chain an expression reads
// is read with each of its prefixes, and a template may be written more
// than once in a value, so the same finding comes from several of them.
func (c *checker) addOnce(severity, path, where, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	key := path + "\x00" + msg
	if c.reported[key] {
		return
	}
	c.reported[key] = true
	c.add(severity, c.line(path), where, "%s", msg)
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

// jobWhere names the job at index i of the workflow checked.
func (c *checker) jobWhere(i int) string {
	return engine.JobWhere(c.wf.Jobs, i)
}

// stepWhere names the step at index j of the job at index i of the
// workflow checked.
func (c *checker) stepWhere(i, j int) string {
	return engine.StepWhere(c.jobWhere(i), j, c.wf.Jobs[i].Steps[j])
}
