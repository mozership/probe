package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheck_Include tells what is found in an included file by the file and
// its line, and an entry that cannot include its file on its line, with the
// rest checked.
func TestCheck_Include(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml": `name: w
jobs:
- name: A
  steps:
  - include: steps/a.yml
  - include: steps/none.yml
  - name: c
    uses: http
    with:
      get: /
    tset: res.code == 200
`,
		"steps/a.yml": `- name: a
  uses: htp
  test: res.code == 200
- name: b
  uses: http
  with:
    get: /
  tset: res.code == 200
`,
	})
	findings, err := Check(filepath.Join(dir, "w.yml"), checkActions)
	if err != nil {
		t.Fatal(err)
	}
	type at struct {
		file    string
		line    int
		where   string
		message string
	}
	// The steps are counted as they run, an entry that includes a file
	// not among them.
	wants := []at{
		{"a.yml", 2, `job 0 "A", step 0 "a"`, `unknown action "htp"`},
		{"a.yml", 8, `job 0 "A", step 1 "b"`, `unknown key "tset"`},
		{"w.yml", 6, "", "no such file"},
		{"w.yml", 11, `job 0 "A", step 2 "c"`, `unknown key "tset"`},
	}
	for _, w := range wants {
		found := false
		for _, f := range findings {
			if filepath.Base(f.File) == w.file && f.Line == w.line && strings.HasPrefix(f.Where, w.where) && strings.Contains(f.Message, w.message) {
				found = true
			}
		}
		if !found {
			var b strings.Builder
			for _, f := range findings {
				b.WriteString("\n  " + f.String() + " [" + f.Where + "]")
			}
			t.Errorf("no finding at %s:%d in %s saying %q in:%s", w.file, w.line, w.where, w.message, b.String())
		}
	}
}

// TestCheck_IncludeAlias checks the steps of a list an alias names, those
// included in it with them.
func TestCheck_IncludeAlias(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml":       "name: w\nshared:\n  steps: &steps\n  - include: steps/a.yml\njobs:\n- name: A\n  steps: *steps\n",
		"steps/a.yml": "- name: a\n  uses: http\n  with:\n    get: /\n  tset: res.code == 200\n",
	})
	findings, err := Check(filepath.Join(dir, "w.yml"), checkActions)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if filepath.Base(f.File) == "a.yml" && f.Line == 5 && strings.Contains(f.Message, `unknown key "tset"`) {
			return
		}
	}
	t.Errorf("no finding of tset at a.yml:5 in %v", findings)
}

// TestCheck_IncludeMerge checks the steps a merge key gives a job, those
// included in them with them.
func TestCheck_IncludeMerge(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml":       "name: w\nshared:\n  base: &base\n    steps:\n    - include: steps/a.yml\njobs:\n- <<: *base\n  name: A\n",
		"steps/a.yml": "- name: a\n  uses: http\n  with:\n    get: /\n  tset: res.code == 200\n",
	})
	findings, err := Check(filepath.Join(dir, "w.yml"), checkActions)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if filepath.Base(f.File) == "a.yml" && f.Line == 5 && strings.Contains(f.Message, `unknown key "tset"`) {
			return
		}
	}
	t.Errorf("no finding of tset at a.yml:5 in %v", findings)
}

// writeTree writes each of files, by its path relative to dir, making the
// directories it is in, and returns dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
