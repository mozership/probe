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
	// read with, as a base file is with each test file.
	baseMarker = "base"
	// variantMarker marks blocks of a group that stand in for one another,
	// as the file of each environment does: one of them is read at a time.
	//
	// A group is checked once for each variant with each block marked
	// neither, together with the base blocks, in the order of the page; a
	// group without variants, once for each block marked neither; and a
	// group whose blocks are all base, once as a whole.
	variantMarker = "variant"
)

// docsBlock is a yaml block of a page, written to a file of its own.
type docsBlock struct {
	page    string
	line    int // the line of the page the block starts on
	file    string
	base    bool
	variant bool
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
	opts := probe.CheckOptions{Actions: actions.Names(), Params: actions.AllParams(), Contracts: actions.Contracts(), Fields: actions.AllFields(), Required: actions.AllRequired()}

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
			line := strings.Count(string(data[:m[4]]), "\n") + 1
			group, err := blockMarkers(meta)
			if err != nil {
				t.Errorf("%s:%d: %v", path, line-1, err)
				continue
			}
			if slices.Contains(meta, fragmentMarker) {
				continue
			}
			b := docsBlock{
				page:    path,
				line:    line,
				file:    filepath.Join(dir, fmt.Sprintf("%s-%d.yml", strings.ReplaceAll(path, string(filepath.Separator), "_"), n)),
				base:    slices.Contains(meta, baseMarker),
				variant: slices.Contains(meta, variantMarker),
			}
			if err := os.WriteFile(b.file, body, 0o600); err != nil {
				return err
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

// blockMarkers returns the group a block is in, from the words after its
// language, or an error for a word that is not a marker or for markers that
// do not go together, so that a misspelt one does not leave the block
// unchecked or checked alone.
func blockMarkers(meta []string) (string, error) {
	group, base, variant, fragment := "", false, false, false
	for _, w := range meta {
		switch {
		case w == fragmentMarker:
			fragment = true
		case w == baseMarker:
			base = true
		case w == variantMarker:
			variant = true
		case strings.HasPrefix(w, mergeMarker):
			if group != "" {
				return "", fmt.Errorf("more than one %s in %q", mergeMarker, strings.Join(meta, " "))
			}
			group = strings.TrimPrefix(w, mergeMarker)
			if group == "" {
				return "", fmt.Errorf("%s names no group in %q", mergeMarker, strings.Join(meta, " "))
			}
		default:
			return "", fmt.Errorf("unknown word %q after the language; the markers are %s, %s<group>, %s and %s", w, fragmentMarker, mergeMarker, baseMarker, variantMarker)
		}
	}
	switch {
	case (base || variant) && group == "":
		return "", fmt.Errorf("%s without %s<group>", strings.Join(meta, " "), mergeMarker)
	case base && variant:
		return "", fmt.Errorf("%s with %s: a block is read with every set or with some", baseMarker, variantMarker)
	case fragment && group != "":
		return "", fmt.Errorf("%s with %s%s: a block is either skipped or merged", fragmentMarker, mergeMarker, group)
	}
	return group, nil
}

// mergeSets returns the sets of blocks a group is checked as, as
// variantMarker tells.
func mergeSets(group []docsBlock) [][]docsBlock {
	var variants, others []int
	for i, b := range group {
		switch {
		case b.variant:
			variants = append(variants, i)
		case !b.base:
			others = append(others, i)
		}
	}
	if len(variants) == 0 && len(others) == 0 {
		return [][]docsBlock{group}
	}
	// -1 stands for no block of the kind, when the group has none.
	if len(variants) == 0 {
		variants = []int{-1}
	}
	if len(others) == 0 {
		others = []int{-1}
	}
	var sets [][]docsBlock
	for _, v := range variants {
		for _, o := range others {
			var set []docsBlock
			for i, b := range group {
				if b.base || i == v || i == o {
					set = append(set, b)
				}
			}
			sets = append(sets, set)
		}
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

func TestBlockMarkers(t *testing.T) {
	tests := []struct {
		meta  string
		group string
		ok    bool
	}{
		{"", "", true},
		{"fragment", "", true},
		{"merge=suite", "suite", true},
		{"merge=suite base", "suite", true},
		{"base merge=suite", "suite", true},
		{"fragmnet", "", false},
		{"merge=suite bsae", "", false},
		{"merge=", "", false},
		{"base", "", false},
		{"fragment merge=suite", "", false},
		{"merge=a merge=b", "", false},
		{"merge=suite variant", "suite", true},
		{"variant", "", false},
		{"merge=suite base variant", "", false},
	}
	for _, tt := range tests {
		group, err := blockMarkers(strings.Fields(tt.meta))
		if (err == nil) != tt.ok || group != tt.group {
			t.Errorf("blockMarkers(%q) = %q, %v; want %q, ok %v", tt.meta, group, err, tt.group, tt.ok)
		}
	}
}

func TestMergeSets(t *testing.T) {
	block := func(name string, base, variant bool) docsBlock {
		return docsBlock{file: name, base: base, variant: variant}
	}
	names := func(sets [][]docsBlock) []string {
		var out []string
		for _, set := range sets {
			var files []string
			for _, b := range set {
				files = append(files, b.file)
			}
			out = append(out, strings.Join(files, ","))
		}
		return out
	}
	tests := []struct {
		name  string
		group []docsBlock
		want  []string
	}{
		{"bases only", []docsBlock{block("a", true, false), block("b", true, false)}, []string{"a,b"}},
		{"bases and others", []docsBlock{block("base", true, false), block("x", false, false), block("y", false, false)}, []string{"base,x", "base,y"}},
		{"variants and others", []docsBlock{block("base", true, false), block("dev", false, true), block("prod", false, true), block("x", false, false), block("y", false, false)},
			[]string{"base,dev,x", "base,dev,y", "base,prod,x", "base,prod,y"}},
		{"variants only", []docsBlock{block("base", true, false), block("dev", false, true), block("prod", false, true)}, []string{"base,dev", "base,prod"}},
	}
	for _, tt := range tests {
		if got := names(mergeSets(tt.group)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: sets = %v, want %v", tt.name, got, tt.want)
		}
	}
}
