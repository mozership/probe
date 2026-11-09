package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// includedSteps returns the names of the steps of each job, as A/a,A/b.
func includedSteps(jobs []Job) string {
	var names []string
	for _, j := range jobs {
		for _, st := range j.Steps {
			names = append(names, j.Name+"/"+st.Name)
		}
	}
	return strings.Join(names, ",")
}

// step writes a step named name, at the top of a file or of an entry.
func step(name string) string {
	return "- name: " + name + "\n  uses: http\n  with:\n    get: /\n  test: res.code == 200\n"
}

// indented indents each line of s by two spaces, as the steps of a job are.
func indented(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n  ") + "\n"
}

// loadDir loads the workflow of files, w.yml when none is given, in dir.
func loadDir(t *testing.T, dir string, files ...string) (*Probe, error) {
	t.Helper()
	if len(files) == 0 {
		files = []string{"w.yml"}
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, filepath.Join(dir, f))
	}
	p := &Probe{FilePath: strings.Join(paths, ","), Config: Config{Actions: builtinNames}}
	return p, p.Load()
}

func TestLoad_Include(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		// given are the files given, relative to the directory written;
		// w.yml when empty.
		given []string
		want  string
	}{
		{
			name: "steps in the place of the entry",
			files: map[string]string{
				"w.yml":        "name: w\njobs:\n- name: A\n  steps:\n" + indented(step("a")+"- include: steps/bc.yml\n"+step("d")),
				"steps/bc.yml": step("b") + step("c"),
			},
			want: "A/a,A/b,A/c,A/d",
		},
		{
			name: "a file included by more than one job",
			files: map[string]string{
				"w.yml":           "name: w\njobs:\n- name: A\n  steps:\n  - include: steps/login.yml\n- name: B\n  steps:\n  - include: steps/login.yml\n" + indented(step("b")),
				"steps/login.yml": step("login"),
			},
			want: "A/login,B/login,B/b",
		},
		{
			// The path is told from the file that writes jobs, not from
			// the first file given nor from where the run is.
			name: "a path from the file that writes jobs",
			files: map[string]string{
				"env/vars.yml":     "vars:\n  url: http://localhost\n",
				"main/w.yml":       "name: w\njobs:\n- name: A\n  steps:\n  - include: steps/a.yml\n",
				"main/steps/a.yml": step("a"),
				"env/steps/a.yml":  step("not this one"),
				"steps/a.yml":      step("nor this one"),
			},
			given: []string{"env/vars.yml", "main/w.yml"},
			want:  "A/a",
		},
		{
			name: "an anchor of the included file",
			files: map[string]string{
				"w.yml":       "name: w\njobs:\n- name: A\n  steps:\n  - include: steps/a.yml\n",
				"steps/a.yml": "- &get\n  name: a\n  uses: http\n  with:\n    get: /\n  test: res.code == 200\n- *get\n",
			},
			want: "A/a,A/a",
		},
		{
			name: "a path tagged as a string",
			files: map[string]string{
				"w.yml":       "name: w\njobs:\n- name: A\n  steps:\n  - include: !!str steps/a.yml\n",
				"steps/a.yml": step("a"),
			},
			want: "A/a",
		},
		{
			name: "jobs that are an alias of a list",
			files: map[string]string{
				"w.yml":       "name: w\nshared:\n  all: &all\n  - name: A\n    steps:\n    - include: steps/a.yml\njobs: *all\n",
				"steps/a.yml": step("a"),
			},
			want: "A/a",
		},
		{
			// A job that takes its steps from another by a merge key has
			// those of the other, included in.
			name: "steps of a job a merge key gives",
			files: map[string]string{
				"w.yml":       "name: w\nshared:\n  base: &base\n    steps:\n    - include: steps/a.yml\njobs:\n- <<: *base\n  name: A\n",
				"steps/a.yml": step("a"),
			},
			want: "A/a",
		},
		{
			// The steps an alias stands for are those the decoder reads:
			// those of the last anchor of its name, even one written after
			// it. Any other list would be decoded with its entries that
			// include left in it.
			name: "an anchor of the name written again after the alias",
			files: map[string]string{
				"w.yml":       "name: w\nshared:\n  steps: &s\n  - include: steps/a.yml\njobs:\n- name: B\n  steps: *s\n- name: C\n  steps: &s\n  - include: steps/c.yml\n",
				"steps/a.yml": step("a"),
				"steps/c.yml": step("c"),
			},
			want: "B/c,C/c",
		},
		{
			// The list is included in once, and both jobs read it.
			name: "steps anchored and read by two jobs",
			files: map[string]string{
				"w.yml":       "name: w\njobs:\n- name: A\n  steps: &steps\n  - include: steps/a.yml\n- name: B\n  steps: *steps\n",
				"steps/a.yml": step("a"),
			},
			want: "A/a,B/a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeTree(t, tt.files)
			p, err := loadDir(t, dir, tt.given...)
			if err != nil {
				t.Fatalf("Load() = %v", err)
			}
			if got := includedSteps(p.workflow.Jobs); got != tt.want {
				t.Errorf("steps = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestLoad_IncludeDefaults gives the included steps the defaults of the
// workflow and of their job, as those written in it.
func TestLoad_IncludeDefaults(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml":       "name: w\ndefaults:\n  http:\n    url: http://localhost\njobs:\n- name: A\n  defaults:\n    http:\n      timeout: 3s\n  steps:\n  - include: steps/a.yml\n",
		"steps/a.yml": step("a"),
	})
	p, err := loadDir(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	with := p.workflow.Jobs[0].Steps[0].With
	if with["url"] != "http://localhost" || with["timeout"] != "3s" {
		t.Errorf("with = %v, want the url of the workflow and the timeout of the job", with)
	}
}

func TestLoad_IncludeErrors(t *testing.T) {
	const job = "name: w\njobs:\n- name: A\n  steps:\n  - include: "
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "a file that does not exist",
			files: map[string]string{"w.yml": job + "steps/none.yml\n"},
			want:  "w.yml:5: steps/none.yml: no such file",
		},
		{
			name: "a directory",
			files: map[string]string{
				"w.yml":       job + "steps\n",
				"steps/a.yml": step("a"),
			},
			want: "w.yml:5: steps: is a directory; include takes a file",
		},
		{
			// A glob is a name like any other, as the order of the steps is
			// written.
			name: "a glob",
			files: map[string]string{
				"w.yml":       job + "steps/*.yml\n",
				"steps/a.yml": step("a"),
			},
			want: "steps/*.yml: no such file",
		},
		{
			name: "a file that is not YAML",
			files: map[string]string{
				"w.yml":       job + "steps/a.txt\n",
				"steps/a.txt": step("a"),
			},
			want: "not a YAML file",
		},
		{
			name: "other keys with include",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n    name: x\n",
				"steps/a.yml": step("a"),
			},
			want: `w.yml:5: include takes no other key, but "name" is written with it`,
		},
		{
			name:  "an include that is not a path",
			files: map[string]string{"w.yml": job + "[a.yml]\n"},
			want:  "w.yml:5: include takes the path of a file",
		},
		{
			name: "an entry of jobs that includes",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "name: A\nsteps:\n" + step("a"),
			},
			want: "w.yml:3: include is an entry of steps, not of jobs",
		},
		{
			name: "an included file that includes",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": step("a") + "- include: b.yml\n",
			},
			want: "a.yml:6: an included file cannot include",
		},
		{
			name: "a job",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": "name: A\nsteps:\n" + step("a"),
			},
			want: "a.yml: an included file holds a list of steps",
		},
		{
			name:  "the workflow",
			files: map[string]string{"w.yml": job + "w.yml\n"},
			want:  "w.yml: an included file holds a list of steps",
		},
		{
			name: "an empty file",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": "# nothing yet\n",
			},
			want: "a.yml: holds no step",
		},
		{
			name: "more than one document",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": step("a") + "---\n" + step("b"),
			},
			want: "a.yml:7: an included file holds a list of steps, not more than one document",
		},
		{
			name: "an included file that does not parse",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": "- name: a\n  with: [\n",
			},
			want: "a.yml:2: YAML: ",
		},
		{
			// The file and its line are told, not a line of the text
			// read, which means nothing to whoever reads it.
			name: "an included step that does not decode",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n",
				"steps/a.yml": "- name: a\n  uses: http\n  retry: 3\n",
			},
			want: "a.yml:3:10: ",
		},
		{
			name: "an alias to an anchor of the workflow",
			files: map[string]string{
				"w.yml":       "name: w\nvars:\n  step: &get\n    uses: http\njobs:\n- name: A\n  steps:\n  - include: steps/a.yml\n",
				"steps/a.yml": "- *get\n",
			},
			want: "a.yml:1: alias *get names no anchor of this file",
		},
		{
			// A step of the file has an id, which every step of the
			// workflow must have of its own.
			name: "a file with an id included twice",
			files: map[string]string{
				"w.yml":       job + "steps/a.yml\n  - include: steps/a.yml\n",
				"steps/a.yml": "- name: a\n  id: a\n  uses: http\n  with:\n    get: /\n  test: res.code == 200\n",
			},
			want: "duplicate step ID 'a'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeTree(t, tt.files)
			_, err := loadDir(t, dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() = %v, want an error with %q", err, tt.want)
			}
		})
	}
}

// TestLoad_IncludeReplacedJobs reads only the jobs that run: those a later
// file replaces include nothing.
func TestLoad_IncludeReplacedJobs(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.yml":       "name: w\njobs:\n- name: A\n  steps:\n  - include: steps/none.yml\n",
		"b.yml":       "jobs:\n- name: B\n  steps:\n  - include: steps/b.yml\n",
		"steps/b.yml": step("b"),
	})
	p, err := loadDir(t, dir, "a.yml", "b.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got := includedSteps(p.workflow.Jobs); got != "B/b" {
		t.Errorf("steps = %s, want B/b", got)
	}
}

// TestLoad_IncludeAnchors keeps the anchors of an included file to it: an
// alias of the workflow does not read one, and one of the same name does
// not change what an alias of the workflow reads.
func TestLoad_IncludeAnchors(t *testing.T) {
	t.Run("an alias of the workflow to an anchor of an included file", func(t *testing.T) {
		dir := writeTree(t, map[string]string{
			"w.yml":       "name: w\njobs:\n- name: A\n  steps:\n  - include: steps/a.yml\n  - *get\n",
			"steps/a.yml": "- &get\n  name: a\n  uses: http\n  with:\n    get: /\n  test: res.code == 200\n",
		})
		if _, err := loadDir(t, dir); err == nil {
			t.Errorf("Load() = nil, want an error for the alias *get, whose anchor is in another file")
		}
	})
	t.Run("an anchor of an included file with the name of one of the workflow", func(t *testing.T) {
		dir := writeTree(t, map[string]string{
			"w.yml":       "name: w\nvars:\n  path: &path /workflow\njobs:\n- name: A\n  steps:\n  - include: steps/a.yml\n  - name: b\n    uses: http\n    with:\n      get: *path\n    test: res.code == 200\n",
			"steps/a.yml": "- name: a\n  uses: http\n  with:\n    get: &path /included\n  test: res.code == 200\n",
		})
		p, err := loadDir(t, dir)
		if err != nil {
			t.Fatal(err)
		}
		steps := p.workflow.Jobs[0].Steps
		if got := steps[0].With["get"]; got != "/included" {
			t.Errorf("step a: get = %v, want /included", got)
		}
		if got := steps[1].With["get"]; got != "/workflow" {
			t.Errorf("step b: get = %v, want /workflow", got)
		}
	})
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
