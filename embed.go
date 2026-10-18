package probe

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
)

// embeddedAction is the action that runs a job file as a step, through
// Job.RunStandalone. Its job files are those of Probe, so they are read
// ahead of the run, for the graph and for what the run would refuse, as the
// action reads them with ReadJobFile.
const embeddedAction = "embedded"

// embeddedJobFile returns the path of the job file st runs, as its with
// gives it, when st is a step of the embedded action that gives one.
func embeddedJobFile(st *Step) (string, bool) {
	if st == nil || st.Uses != embeddedAction {
		return "", false
	}
	path, ok := st.With["path"].(string)
	return path, ok && path != ""
}

// ReadJobFile reads the job file at path, as the embedded action runs it: by
// names, the names the workflow that embeds it gives its external actions,
// and with the job's defaults applied to its steps. A nil names reads each
// uses as it is written.
func ReadJobFile(path string, names map[string]string) (*Job, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("embedded steps file does not exist: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded steps file: %w", err)
	}

	job := &Job{}
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Validator(validator.New()), yaml.AllowDuplicateMapKey())
	if err := dec.Decode(job); err != nil {
		return nil, fmt.Errorf("failed to decode YAML job: %w", err)
	}
	if len(job.Steps) == 0 {
		return nil, fmt.Errorf("no steps found in embedded file: %s", path)
	}

	// Before the defaults, which are keyed by what a step's uses says.
	if err := job.UseActions(names); err != nil {
		return nil, fmt.Errorf("embedded steps file %s: %w", path, err)
	}
	job.ApplyDefaults()
	return job, nil
}
