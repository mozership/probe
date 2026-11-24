package engine

import (
	"fmt"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/mozership/probe/internal/guide"
	"github.com/mozership/probe/internal/workflowfile"
)

// WorkflowActions returns the names the workflow gives under actions, and
// the directory a local path among them is taken from.
func WorkflowActions(workflow string) (guide.Workflow, error) {
	files, err := workflowfile.Paths(workflow)
	if err != nil {
		return guide.Workflow{}, fmt.Errorf("workflow file %w", err)
	}
	if len(files) == 0 {
		return guide.Workflow{}, fmt.Errorf("%s: no YAML file", workflow)
	}
	text, contents, err := workflowfile.Read(files, ReadFile)
	if err != nil {
		return guide.Workflow{}, fmt.Errorf("workflow file %w", err)
	}
	src, err := NewSource(files, contents, text)
	if err != nil {
		return guide.Workflow{}, fmt.Errorf("%s: %w", workflow, err)
	}
	var w struct {
		Actions map[string]string `yaml:"actions"`
	}
	// The files override one another as a run reads them, a key written
	// again taking the place of the first.
	if err := src.decode(&w, yaml.AllowDuplicateMapKey()); err != nil {
		return guide.Workflow{}, fmt.Errorf("%s: %w", workflow, err)
	}
	abs, err := filepath.Abs(files[0])
	if err != nil {
		return guide.Workflow{}, err
	}
	return guide.Workflow{Uses: w.Actions, Dir: filepath.Dir(abs)}, nil
}
