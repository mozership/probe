package probe

import (
	"bytes"
	"fmt"
	"github.com/linyows/probe/actionrpc"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
	"github.com/linyows/probe/dag"
	"github.com/linyows/probe/report"
	"github.com/mattn/go-isatty"
)

type Probe struct {
	FilePath string
	workflow Workflow
	Config   Config
	// aliasProblems, when it is set, is given what is wrong with the names
	// the workflow's actions give, in place of Load failing on the first.
	aliasProblems *[]aliasProblem
}

type Config struct {
	Log     io.Writer
	Verbose bool
	Timing  bool
	// Output selects how the report is delivered while jobs run.
	Output OutputMode
	// Reports lists the report files written once the run has finished.
	Reports []report.Target
	// Guard is what the run allows its actions to do, such as only reading,
	// and only from some hosts. Each step's action is told it, and a step
	// whose action does not keep to it is refused unless it is allowed.
	Guard actionrpc.Guard
	// Vars are values for the vars of the workflow, by name, given by the
	// person running it. Each is taken as it is, in place of what the
	// workflow writes for the var, and the other vars read it.
	Vars map[string]string
	// Actions are the names of the actions of Probe, which a workflow
	// cannot give to an external action under actions. When it is empty,
	// the names a workflow gives are not checked against them.
	Actions []string
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
	if err := p.Load(); err != nil {
		return "", err
	}
	return dag.ASCII{}.Render(p.workflow.Graph()), nil
}

// DagMermaid returns the Mermaid format representation of the workflow job dependencies
func (p *Probe) DagMermaid() (string, error) {
	if err := p.Load(); err != nil {
		return "", err
	}
	return dag.Mermaid{}.Render(p.workflow.Graph()), nil
}

func (p *Probe) Load() error {
	files, err := p.yamlFiles()
	if err != nil {
		return NewFileError("load_yaml_files", "failed to locate YAML files", err).
			WithContext("workflow_path", p.FilePath)
	}
	y, err := p.readYamlFiles(files)
	if err != nil {
		return NewFileError("read_yaml_files", "failed to read YAML files", err).
			WithContext("files", files)
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

	p.initializeEmptyIDs()

	return nil
}

func (p *Probe) readYamlFiles(paths []string) (string, error) {
	var y strings.Builder

	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		y.Write(data)

		// Add newline between files to prevent concatenation issues
		if i < len(paths)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			y.WriteByte('\n')
		}
	}

	return y.String(), nil
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

// initializeEmptyIDs initializes empty job IDs and step IDs with index numbers
func (p *Probe) initializeEmptyIDs() {
	for jobIdx := range p.workflow.Jobs {
		job := &p.workflow.Jobs[jobIdx]

		// Initialize empty job ID with index number
		if job.ID == "" {
			job.ID = fmt.Sprintf("job_%d", jobIdx)
		}

		// Initialize empty step IDs with index numbers
		for stepIdx, step := range job.Steps {
			if step.ID == "" {
				step.ID = fmt.Sprintf("step_%d", stepIdx)
			}
		}
	}
}

// validateRepeatLimits validates repeat count and max_attempts against configured limits
func (p *Probe) validateRepeatLimits() error {
	for jobIdx, job := range p.workflow.Jobs {
		// Validate repeat configuration
		if job.Repeat != nil {
			if err := job.Repeat.Validate(); err != nil {
				return NewConfigurationError("invalid_repeat",
					fmt.Sprintf("job %d (%s): %v", jobIdx, job.Name, err), nil)
			}
		}

		// Validate retry configuration for each step
		for stepIdx, step := range job.Steps {
			if step.Retry != nil {
				if err := step.Retry.Validate(); err != nil {
					return NewConfigurationError("invalid_retry",
						fmt.Sprintf("job %d (%s), step %d: %v", jobIdx, job.Name, stepIdx, err), nil)
				}
			}
		}
	}
	return nil
}
