// Package serve keeps running and runs workflows: on a schedule, or again
// and again, each run a process of probe of its own. It knows nothing of
// what a run does; it reads what the run reports.
package serve

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/mozership/probe/internal/cron"
	"github.com/mozership/probe/internal/duration"
)

// Config is what a serve file says.
type Config struct {
	// Workers is how many runs go at once, across every workflow.
	Workers int     `yaml:"workers"`
	Results Results `yaml:"results"`
	// Grace is how long the runs going when serve is stopped are waited
	// for before they are stopped too.
	Grace     Duration             `yaml:"grace"`
	Workflows map[string]*Workflow `yaml:"workflows"`

	// dir is the directory of the serve file, which the runs run in.
	dir string
}

// Results is where what the runs leave is kept.
type Results struct {
	Dir string `yaml:"dir"`
	// Keep is how long a run is kept after it ends; zero keeps every run.
	Keep Duration `yaml:"keep"`
}

// Workflow is a workflow serve runs, and how a run of it is started.
type Workflow struct {
	Files        []string       `yaml:"files"`
	Vars         map[string]any `yaml:"vars"`
	EnvFiles     []string       `yaml:"env_files"`
	Jobs         []string       `yaml:"jobs"`
	ReadOnly     bool           `yaml:"read_only"`
	AllowHosts   []string       `yaml:"allow_hosts"`
	AllowActions []string       `yaml:"allow_actions"`
	// Schedule is a cron expression of the times the workflow is run at.
	Schedule string `yaml:"schedule"`
	// Loop runs the workflow again once the run before it is over, after
	// Interval.
	Loop     bool     `yaml:"loop"`
	Interval Duration `yaml:"interval"`
	// Timeout ends a run that goes on longer; zero for none.
	Timeout Duration `yaml:"timeout"`

	schedule *cron.Schedule
}

// Duration is a duration as a workflow writes one, such as 30s or 1m30s.
type Duration struct {
	time.Duration
	// given is whether the file gives it, as 0s does.
	given bool
}

// UnmarshalYAML reads a duration as duration.Parse does.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	v, err := duration.Parse(raw)
	if err != nil {
		return fmt.Errorf("%v: %w", raw, err)
	}
	d.Duration = v
	d.given = true
	return nil
}

const (
	defaultWorkers    = 4
	defaultResultsDir = "runs"
	defaultGrace      = 30 * time.Second
)

var workflowName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Load reads the serve file at path. The paths it names are taken from the
// directory it is in. It returns what does not keep it from serving but is
// likely a mistake, as warnings.
func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var c Config
	if err := yaml.UnmarshalWithOptions(data, &c, yaml.DisallowUnknownField()); err != nil {
		return nil, nil, fmt.Errorf("%s: %s", path, yaml.FormatError(err, false, true))
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	warnings, err := c.prepare(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, warnings, nil
}

// prepare checks c, fills in what it leaves out, and makes the paths it
// names absolute, from dir.
func (c *Config) prepare(dir string) ([]string, error) {
	c.dir = dir
	if c.Workers == 0 {
		c.Workers = defaultWorkers
	}
	if c.Workers < 0 {
		return nil, fmt.Errorf("workers must be 1 or more, not %d", c.Workers)
	}
	if c.Results.Dir == "" {
		c.Results.Dir = defaultResultsDir
	}
	c.Results.Dir = from(dir, c.Results.Dir)
	if !c.Grace.given {
		c.Grace.Duration = defaultGrace
	}
	if len(c.Workflows) == 0 {
		return nil, errors.New("workflows names none")
	}

	var errs []error
	var warnings []string
	for _, name := range slices.Sorted(maps.Keys(c.Workflows)) {
		w := c.Workflows[name]
		if w == nil {
			errs = append(errs, fmt.Errorf("workflow %s: names no files", name))
			continue
		}
		if err := w.prepare(name, dir); err != nil {
			errs = append(errs, fmt.Errorf("workflow %s: %w", name, err))
			continue
		}
		if w.schedule == nil && !w.Loop {
			warnings = append(warnings, fmt.Sprintf("workflow %s is never started: it has neither schedule nor loop", name))
		}
	}
	return warnings, errors.Join(errs...)
}

func (w *Workflow) prepare(name, dir string) error {
	if !workflowName.MatchString(name) {
		return errors.New("the name is to be letters, digits, _ and -, starting with a letter or a digit")
	}
	if len(w.Files) == 0 {
		return errors.New("names no files")
	}
	for i, f := range w.Files {
		w.Files[i] = from(dir, f)
	}
	for i, f := range w.EnvFiles {
		w.EnvFiles[i] = from(dir, f)
	}
	if w.Schedule != "" {
		if w.Loop {
			return errors.New("has both schedule and loop; a workflow is started by one")
		}
		s, err := cron.Parse(w.Schedule)
		if err != nil {
			return fmt.Errorf("schedule: %w", err)
		}
		if _, ok := s.Next(time.Now()); !ok {
			return fmt.Errorf("schedule: %q matches no time in the years to come", w.Schedule)
		}
		w.schedule = s
	}
	if w.Interval.Duration > 0 && !w.Loop {
		return errors.New("has an interval, which is the wait between the runs of a loop, without loop")
	}
	return nil
}

func from(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
