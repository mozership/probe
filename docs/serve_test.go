package docs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/serve"
)

// topWorkflows matches the key that makes a yaml block a serve file.
var topWorkflows = regexp.MustCompile(`(?m)^workflows:`)

// TestDocsServeFiles loads every serve file in the docs as probe serve
// loads it, so that an example a reader copies is one serve takes. The
// workflow files it names need not be there, as serve reads them only as
// it runs them.
func TestDocsServeFiles(t *testing.T) {
	dir := t.TempDir()
	count := 0
	err := filepath.WalkDir("content", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (!strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx")) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range docsYAMLBlock.FindAllSubmatchIndex(data, -1) {
			block := data[m[4]:m[5]]
			if strings.Contains(string(data[m[2]:m[3]]), fragmentMarker) || !topWorkflows.Match(block) {
				continue
			}
			count++
			line := strings.Count(string(data[:m[4]]), "\n") + 1
			file := filepath.Join(dir, "serve.yml")
			if err := os.WriteFile(file, block, 0o600); err != nil {
				return err
			}
			if _, warnings, err := serve.Load(file); err != nil {
				t.Errorf("%s:%d: %v", path, line, err)
			} else {
				for _, w := range warnings {
					t.Errorf("%s:%d: %s", path, line, w)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no serve file found in the docs")
	}
}
