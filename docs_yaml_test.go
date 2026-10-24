package probe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// DocsYAMLBlock matches a fenced yaml code block in a page, capturing the
// words after the language, as in ```yaml fragment, and the body. It is
// exported for TestDocsWorkflowsCheck, which is in package probe_test.
var DocsYAMLBlock = regexp.MustCompile("(?s)```ya?ml([^\n]*)\n(.*?)```")

// TestDocsYAMLBlocks parses every yaml code block in the docs, so that an
// example a reader copies is not one Probe cannot load: a plain value holding
// ": ", as an unquoted a ? b : c does, or one that starts with a quote and
// goes on after it, is a YAML error.
//
// A value written without quotes ends at " #", which YAML takes for a
// comment, so an expression such as all(res.body.items, #.active) loses its
// predicate and is left with an open parenthesis; such a value is reported.
//
// A block may repeat a key, as one that shows several ways to write a test
// does, and Probe itself loads a workflow that does. An alias whose anchor is
// defined in another file is an example of merging files, and is skipped.
func TestDocsYAMLBlocks(t *testing.T) {
	root := filepath.Join("docs", "content")
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
		for _, m := range DocsYAMLBlock.FindAllSubmatchIndex(data, -1) {
			count++
			line := strings.Count(string(data[:m[4]]), "\n") + 1
			var v any
			err := yaml.UnmarshalWithOptions(data[m[4]:m[5]], &v, yaml.AllowDuplicateMapKey())
			if err != nil && !strings.Contains(err.Error(), "alias") {
				t.Errorf("%s:%d: %s", path, line, strings.SplitN(err.Error(), "\n", 2)[0])
			}
			for _, s := range cutShort(v) {
				t.Errorf("%s:%d: a value cut short by a comment, quote it: %q", path, line, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("no yaml block found under %s", root)
	}
}

// cutShort returns the strings in v, at any depth, that open more
// parentheses than they close, as an expression does when YAML has cut it at
// " #".
func cutShort(v any) []string {
	var found []string
	switch v := v.(type) {
	case string:
		if strings.Count(v, "(") > strings.Count(v, ")") {
			found = append(found, v)
		}
	case map[string]any:
		for _, e := range v {
			found = append(found, cutShort(e)...)
		}
	case []any:
		for _, e := range v {
			found = append(found, cutShort(e)...)
		}
	}
	return found
}
