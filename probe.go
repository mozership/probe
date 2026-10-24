package probe

import (
	"bytes"
	"fmt"
	"github.com/mozership/probe/actionrpc"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
	"github.com/mattn/go-isatty"
	"github.com/mozership/probe/dag"
	"github.com/mozership/probe/report"
)

type Probe struct {
	FilePath string
	workflow Workflow
	Config   Config
	// aliasProblems, when it is set, is given what is wrong with the names
	// the workflow's actions give, in place of Load failing on the first.
	aliasProblems *[]aliasProblem
	// problems, when it is set, is given what keeps the jobs from running
	// as they are written, in place of Load failing on them.
	problems *[]problem
}

type Config struct {
	Log io.Writer
	// Warn is where what is found as the workflow is loaded, and does not
	// keep it from running, is written; nil for nowhere.
	Warn    io.Writer
	Verbose bool
	// Trace, with Verbose, also shows how each action's process is
	// started, called and stopped, and the records an action logs at trace
	// level, for debugging an action.
	Trace  bool
	Timing bool
	// Output selects how the report is delivered while jobs run.
	Output OutputMode
	// Reports lists the report files written once the run has finished.
	Reports []report.Target
	// Baseline is the report of an earlier run, which the reports compare
	// the response times of the repeated steps to; nil for none.
	Baseline *report.Report
	// Guard is what the run allows its actions to do, such as only reading,
	// and only from some hosts. Each step's action is told it, and a step
	// whose action does not keep to it is refused unless it is allowed.
	Guard actionrpc.Guard
	// Env are environment variables read from files, by name, for the vars
	// of the workflow to read where the environment has none of the name.
	// They are given to the vars alone: Probe reads none of its own
	// settings from them, and the actions do not have them in their
	// environment.
	Env map[string]string
	// Vars are values for the vars of the workflow, by name, given by the
	// person running it. Each is taken as it is, in place of what the
	// workflow writes for the var, and the other vars read it.
	Vars map[string]string
	// Actions are the names of the actions of Probe, which a workflow
	// cannot give to an external action under actions. When it is empty,
	// the names a workflow gives are not checked against them.
	Actions []string
	// Jobs narrows the run to the jobs of these ids or names, with the jobs
	// they need and those that always run after them; all jobs when it is
	// empty.
	Jobs []string
}

func New(path string, v bool) *Probe {
	// Set TTY detection for embedded plugins
	if isatty.IsTerminal(os.Stdout.Fd()) {
		_ = os.Setenv("PROBE_TTY", "1")
	}

	return &Probe{
		FilePath: path,
		Config: Config{
			Log:     os.Stdout,
			Warn:    os.Stderr,
			Verbose: v,
			Timing:  false,
			Output:  OutputModeAuto,
		},
	}
}

func (p *Probe) Do() error {
	if err := p.Load(); err != nil {
		return err
	}

	return p.workflow.Start(p.Config)
}

func (p *Probe) ExitStatus() int {
	return p.workflow.exitStatus
}

// DagAscii returns the ASCII art representation of the workflow job dependencies with steps
func (p *Probe) DagAscii() (string, error) {
	if err := p.loadGraph(); err != nil {
		return "", err
	}
	return dag.ASCII{}.Render(p.workflow.Graph()), nil
}

// DagMermaid returns the Mermaid format representation of the workflow job dependencies
func (p *Probe) DagMermaid() (string, error) {
	if err := p.loadGraph(); err != nil {
		return "", err
	}
	return dag.Mermaid{}.Render(p.workflow.Graph()), nil
}

// loadGraph loads the workflow for dag to draw, with only the jobs
// Config.Jobs selects, as a run would run them. Needs in a cycle, which a
// run refuses, are refused, as there is no order to draw them in; a need
// that is no job is drawn as it is written.
func (p *Probe) loadGraph() error {
	if err := p.Load(); err != nil {
		return err
	}
	if err := p.workflow.checkCycle(); err != nil {
		return err
	}
	if len(p.Config.Jobs) == 0 {
		return nil
	}
	jobs, _, err := selectJobs(p.workflow.Jobs, p.Config.Jobs)
	if err != nil {
		return err
	}
	p.workflow.Jobs = jobs
	return nil
}

func (p *Probe) Load() error {
	files, err := p.yamlFiles()
	if err != nil {
		return NewFileError("load_yaml_files", "failed to locate YAML files", err).
			WithContext("workflow_path", p.FilePath)
	}
	y, data, err := p.readYamlFiles(files)
	if err != nil {
		return NewFileError("read_yaml_files", "failed to read YAML files", err).
			WithContext("files", files)
	}
	if p.Config.Warn != nil {
		for _, r := range redefinedJobs(files, data) {
			_, _ = fmt.Fprintln(p.Config.Warn, colorWarning().Sprintf("[WARN] %s", r))
		}
	}

	v := validator.New()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(y)), yaml.Validator(v), yaml.AllowDuplicateMapKey())
	if err = dec.Decode(&p.workflow); err != nil {
		return NewConfigurationError("decode_yaml", "failed to decode YAML workflow", err).
			WithContext("workflow_path", p.FilePath)
	}

	// Set base path for resolving relative paths in embedded actions
	if len(files) > 0 {
		absPath, err := filepath.Abs(files[0])
		if err == nil {
			p.workflow.basePath = filepath.Dir(absPath)
		}
	}

	// The names the workflow gives to external actions are replaced before
	// anything reads a uses, the defaults included.
	problems := p.workflow.checkAliases(p.Config.Actions)
	p.workflow.named = p.workflow.namedActions(problems)
	problems = append(problems, p.workflow.resolveAliases(problems)...)
	if p.aliasProblems != nil {
		*p.aliasProblems = problems
	} else if len(problems) > 0 {
		return NewConfigurationError("resolve_alias", "failed to resolve the names of external actions", problems[0]).
			WithContext("workflow_path", p.FilePath)
	}

	p.setDefaultsToSteps()

	// Additional validation and ID initialization
	if err := p.validateIDs(); err != nil {
		return err
	}

	// Validate repeat and retry limits
	if err := p.validateRepeatLimits(); err != nil {
		return err
	}

	if err := p.initializeEmptyIDs(); err != nil {
		return err
	}

	// What keeps a job from running is found now, so that the run is
	// refused before any job starts rather than when the job is reached.
	found := p.workflow.problems()
	if p.problems != nil {
		*p.problems = found
	} else if len(found) > 0 {
		return problemsError(found)
	}
	return nil
}

// readYamlFiles returns the files at paths as one text, and the content of
// each, in their order.
func (p *Probe) readYamlFiles(paths []string) (string, [][]byte, error) {
	var y strings.Builder
	contents := make([][]byte, 0, len(paths))

	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		contents = append(contents, data)
		y.Write(data)

		// Add newline between files to prevent concatenation issues
		if i < len(paths)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			y.WriteByte('\n')
		}
	}

	return y.String(), contents, nil
}

// jobsRedefinition is a file that writes jobs at the top after an earlier
// file did. The files are read as one text, so its jobs replace the earlier
// ones, which do not run.
type jobsRedefinition struct {
	File, Prev     string
	Line, PrevLine int
}

func (r jobsRedefinition) String() string {
	return fmt.Sprintf("%s:%d: %s", r.File, r.Line, r.message())
}

func (r jobsRedefinition) message() string {
	return fmt.Sprintf("jobs replaces the jobs of %s:%d, which do not run", r.Prev, r.PrevLine)
}

// topJobsKey is jobs as a key of the top mapping, which no other line
// starts with: what a key at the top holds is indented.
var topJobsKey = regexp.MustCompile(`^(?:jobs|"jobs"|'jobs')[ \t]*:(?:[ \t]|$)`)

// redefinedJobs returns each of files, whose contents are data, that writes
// jobs at the top after an earlier file did, with the one it replaces. Only
// jobs is told of: a file that replaces vars, say, is how a workflow is run
// in other environments.
func redefinedJobs(files []string, data [][]byte) []jobsRedefinition {
	var out []jobsRedefinition
	prev, prevLine := "", 0
	for k, f := range files {
		line := 0
		for i, l := range strings.Split(string(data[k]), "\n") {
			if topJobsKey.MatchString(strings.TrimSuffix(l, "\r")) {
				line = i + 1
			}
		}
		if line == 0 {
			continue
		}
		if prev != "" {
			out = append(out, jobsRedefinition{File: f, Line: line, Prev: prev, PrevLine: prevLine})
		}
		prev, prevLine = f, line
	}
	return out
}

// yamlFiles resolves paths and returns a list of YAML (.yml and .yaml) files
func (p *Probe) yamlFiles() ([]string, error) {
	var files []string
	paths := strings.SplitSeq(p.FilePath, ",")

	for path := range paths {
		path = strings.TrimSpace(path)

		// Check if it's a single file or a directory
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				// Read all YAML files from the directory
				rfiles, err := os.ReadDir(path)
				if err != nil {
					return nil, err
				}
				for _, rf := range rfiles {
					if !rf.IsDir() && p.isYamlFile(rf.Name()) {
						files = append(files, filepath.Join(path, rf.Name()))
					}
				}
			} else if p.isYamlFile(path) {
				// Single file path
				files = append(files, path)
			} else {
				// A file named on its own is not left out without a word,
				// as one a directory or a glob finds is.
				return nil, fmt.Errorf("%s: not a YAML file (.yml or .yaml)", path)
			}
		} else {
			// Handle wildcard pattern
			matches, err := filepath.Glob(path)
			if err != nil {
				return nil, err
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("%s: no such file or directory", path)
			}
			for _, match := range matches {
				if p.isYamlFile(match) {
					files = append(files, match)
				}
			}
		}
	}

	return files, nil
}

// isYAMLFile checks if the filename has a .yml or .yaml extension
func (p *Probe) isYamlFile(f string) bool {
	return strings.HasSuffix(f, ".yml") || strings.HasSuffix(f, ".yaml")
}

func (p *Probe) setDefaultsToSteps() {
	// The job's defaults come first, as what is set is kept: the
	// workflow's then set only what neither the step nor its job did.
	for i := range p.workflow.Jobs {
		p.workflow.Jobs[i].ApplyDefaults()
		p.workflow.Jobs[i].applyDefaults(p.workflow.Defaults)
	}
}

// validateIDs checks for duplicate job IDs and step IDs across the workflow
func (p *Probe) validateIDs() error {
	jobIDs := make(map[string]int)           // jobID -> jobIndex
	globalStepIDs := make(map[string]string) // stepID -> "job[index].step[index]"

	for jobIdx, job := range p.workflow.Jobs {
		// Check for duplicate job IDs
		if job.ID != "" {
			if existingIdx, exists := jobIDs[job.ID]; exists {
				return NewConfigurationError("duplicate_job_id",
					fmt.Sprintf("duplicate job ID '%s' found in job %d (already used in job %d)",
						job.ID, jobIdx, existingIdx), nil)
			}
			jobIDs[job.ID] = jobIdx
		}

		// Check for duplicate step IDs across all jobs
		for stepIdx, step := range job.Steps {
			if step.ID != "" {
				stepLocation := fmt.Sprintf("job[%d].step[%d]", jobIdx, stepIdx)
				if existingLocation, exists := globalStepIDs[step.ID]; exists {
					return NewConfigurationError("duplicate_step_id",
						fmt.Sprintf("duplicate step ID '%s' found in %s (already used in %s)",
							step.ID, stepLocation, existingLocation), nil)
				}
				globalStepIDs[step.ID] = stepLocation
			}
		}
	}

	return nil
}

// initializeEmptyIDs gives each job without an ID its default one, by where
// it is written. A step without an ID is left without one, so that what
// needs one, such as its outputs, is refused: it is read as defaultStepID
// where it is read by where it is written.
func (p *Probe) initializeEmptyIDs() error {
	return p.workflow.assignJobIDs()
}

// validateRepeatLimits validates repeat count and max_attempts against configured limits
func (p *Probe) validateRepeatLimits() error {
	return p.workflow.validateRepeatLimits()
}
