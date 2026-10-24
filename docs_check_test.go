package probe_test

import (
	"fmt"
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

// topName and topJobs match the keys at the top level of a block that make
// it a workflow: jobs, and the name a block that shows jobs alone is given to
// be checked. topOn matches the key that makes it a GitHub Actions workflow
// instead, as a page that runs Probe in CI shows.
var (
	topName = regexp.MustCompile(`(?m)^name:`)
	topJobs = regexp.MustCompile(`(?m)^jobs:`)
	topOn   = regexp.MustCompile(`(?m)^on:`)
)

// The words after the language of a block, as in ```yaml fragment, that
// tell the test how to check it. The site shows none of them.
const (
	// fragmentMarker marks a block that is a piece of a workflow, such as
	// one cut down to the part a page is about, and is not checked.
	fragmentMarker = "fragment"
	// mergeMarker, as merge=pipeline, marks a block that is one file of a
	// workflow read from several, as probe a.yml,b.yml reads it. The blocks
	// of a group are checked together, in the order of the page.
	mergeMarker = "merge="
	// baseMarker marks a block of a group that every other block of it is
	// read with, as a base file is with each test file. A group is checked
	// once for each block not marked so, together with those that are; a
	// group whose blocks are all marked so is checked once.
	baseMarker = "base"
)

// docsBlock is a yaml block of a page, written to a file of its own.
type docsBlock struct {
	page string
	line int // the line of the page the block starts on
	file string
	base bool
}

// TestDocsWorkflowsCheck runs what probe check runs on every workflow in the
// docs, and fails on any error or warning, so that an example a reader copies
// runs and reads only what it defines.
//
// A workflow is a yaml block with jobs at its top level and no on. A block
// that has no name, as one that shows jobs alone does, is checked with one.
// A block marked as a fragment is skipped, and one marked with merge= is
// checked with the other blocks of its group, as the files they show are run
// together.
//
// The with of an external action is not checked, as nothing is read from the
// network.
func TestDocsWorkflowsCheck(t *testing.T) {
	root := filepath.Join("docs", "content")
	dir := t.TempDir()
	name := filepath.Join(dir, "name.yml")
	if err := os.WriteFile(name, []byte("name: docs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
		groups := map[string][]docsBlock{}
		var order []string
		for n, m := range probe.DocsYAMLBlock.FindAllSubmatchIndex(data, -1) {
			meta := strings.Fields(string(data[m[2]:m[3]]))
			body := data[m[4]:m[5]]
			if slices.Contains(meta, fragmentMarker) {
				continue
			}
			b := docsBlock{
				page: path,
				line: strings.Count(string(data[:m[4]]), "\n") + 1,
				file: filepath.Join(dir, fmt.Sprintf("%s-%d.yml", strings.ReplaceAll(path, string(filepath.Separator), "_"), n)),
				base: slices.Contains(meta, baseMarker),
			}
			if err := os.WriteFile(b.file, body, 0o600); err != nil {
				return err
			}
			group := ""
			for _, w := range meta {
				if g, ok := strings.CutPrefix(w, mergeMarker); ok {
					group = g
				}
			}
			if group != "" {
				if _, ok := groups[group]; !ok {
					order = append(order, group)
				}
				groups[group] = append(groups[group], b)
				continue
			}
			if !topJobs.Match(body) || topOn.Match(body) {
				continue
			}
			count++
			checkDocs(t, opts, name, []docsBlock{b})
		}
		for _, g := range order {
			for _, blocks := range mergeSets(groups[g]) {
				count++
				checkDocs(t, opts, name, blocks)
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

// mergeSets returns the sets of blocks a group is checked as: each block not
// marked base with those that are, in the order of the page, or the whole
// group when every block is marked base.
func mergeSets(group []docsBlock) [][]docsBlock {
	var sets [][]docsBlock
	for i, b := range group {
		if b.base {
			continue
		}
		var set []docsBlock
		for j, o := range group {
			if o.base || j == i {
				set = append(set, o)
			}
		}
		sets = append(sets, set)
	}
	if len(sets) == 0 {
		sets = append(sets, group)
	}
	return sets
}

// checkDocs checks blocks read together as one workflow, with name read
// first when none of them has a name, and reports each finding on the line
// of the page it is on.
func checkDocs(t *testing.T, opts probe.CheckOptions, name string, blocks []docsBlock) {
	t.Helper()
	var files []string
	named := false
	for _, b := range blocks {
		files = append(files, b.file)
		data, err := os.ReadFile(b.file)
		if err != nil {
			t.Fatal(err)
		}
		named = named || topName.Match(data)
	}
	if !named {
		files = append([]string{name}, files...)
	}
	at := fmt.Sprintf("%s:%d", blocks[0].page, blocks[0].line)
	findings, err := probe.Check(strings.Join(files, ","), opts)
	if err != nil {
		t.Errorf("%s: %v", at, err)
		return
	}
	for _, f := range findings {
		where := at
		for _, b := range blocks {
			if f.File == b.file {
				where = fmt.Sprintf("%s:%d", b.page, b.line+max(f.Line, 1)-1)
			}
		}
		f.File = ""
		t.Errorf("%s: %s", where, f)
	}
}
