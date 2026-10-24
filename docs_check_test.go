package probe_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe"
	"github.com/mozership/probe/actions"
)

// topName and topJobs match the keys at the top level of a block that a
// workflow cannot load without, so that a block with both is a workflow
// rather than a piece of one. topOn matches the key that makes it a GitHub
// Actions workflow instead, as a page that runs Probe in CI shows.
var (
	topName = regexp.MustCompile(`(?m)^name:`)
	topJobs = regexp.MustCompile(`(?m)^jobs:`)
	topOn   = regexp.MustCompile(`(?m)^on:`)
)

// fragmentMarker is the word after the language of a block, as in
// ```yaml fragment, that says it is part of a workflow: a file that is merged
// with another, or a workflow cut down to the part a page is about. It is not
// shown on the page.
const fragmentMarker = "fragment"

// TestDocsWorkflowsCheck runs what probe check runs on every workflow in the
// docs, a yaml block with a name and jobs at its top level, and fails on any
// error or warning, so that an example a reader copies runs and reads only
// what it defines. A block without either key is a piece of a workflow, and
// one marked as a fragment is skipped; the pieces of an example that merges
// files are not merged to be checked.
//
// The with of an external action is not checked, as nothing is read from the
// network.
func TestDocsWorkflowsCheck(t *testing.T) {
	root := filepath.Join("docs", "content")
	dir := t.TempDir()
	opts := probe.CheckOptions{Actions: actions.Names(), Params: actions.AllParams(), Contracts: actions.Contracts()}
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range probe.DocsYAMLBlock.FindAllSubmatchIndex(data, -1) {
			meta := strings.Fields(string(data[m[2]:m[3]]))
			body := data[m[4]:m[5]]
			if slices.Contains(meta, fragmentMarker) || !topName.Match(body) || !topJobs.Match(body) || topOn.Match(body) {
				continue
			}
			count++
			line := strings.Count(string(data[:m[4]]), "\n") + 1
			file := filepath.Join(dir, "workflow.yml")
			if err := os.WriteFile(file, body, 0o600); err != nil {
				return err
			}
			findings, err := probe.Check(file, opts)
			if err != nil {
				t.Errorf("%s:%d: %v", path, line, err)
				continue
			}
			for _, f := range findings {
				f.File = ""
				if f.Line > 0 {
					f.Line += line - 1
				}
				t.Errorf("%s:%d: %s", path, max(f.Line, line), f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("no workflow found under %s", root)
	}
}
