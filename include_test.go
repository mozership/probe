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

func jobNames(jobs []Job) []string {
	var names []string
	for _, j := range jobs {
		names = append(names, j.Name)
	}
	return names
}

// okStep is the steps of a job of a list, and okSteps those of a job that
// is a file of its own.
const (
	okStep  = "  steps:\n  - uses: http\n    with:\n      get: /\n    test: res.code == 200\n"
	okSteps = "steps:\n- uses: http\n  with:\n    get: /\n  test: res.code == 200\n"
)

func TestLoad_Include(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		// path is the file or files given, relative to the directory
		// written; w.yml when empty.
		path string
		want []string
	}{
		{
			name: "a job in the place of the entry",
			files: map[string]string{
				"w.yml":       "name: w\njobs:\n- name: A\n" + okStep + "- include: jobs/b.yml\n- name: C\n" + okStep,
				"jobs/b.yml":  "name: B\n" + okSteps,
				"jobs/no.yml": "name: Not included\n" + okSteps,
			},
			want: []string{"A", "B", "C"},
		},
		{
			name: "a list of jobs",
			files: map[string]string{
				"w.yml":       "name: w\njobs:\n- include: jobs/ab.yml\n",
				"jobs/ab.yml": "- name: A\n" + okStep + "- name: B\n" + okStep,
			},
			want: []string{"A", "B"},
		},
		{
			name: "the files of a glob by their names",
			files: map[string]string{
				"w.yml":         "name: w\njobs:\n- include: jobs/*\n",
				"jobs/2-b.yml":  "name: B\n" + okSteps,
				"jobs/1-a.yaml": "name: A\n" + okSteps,
				"jobs/notes.md": "not a job",
			},
			want: []string{"A", "B"},
		},
		{
			name: "the YAML files of a directory",
			files: map[string]string{
				"w.yml":          "name: w\njobs:\n- include: jobs\n",
				"jobs/b.yml":     "name: B\n" + okSteps,
				"jobs/a.yml":     "name: A\n" + okSteps,
				"jobs/notes.txt": "not a job",
				"jobs/sub/c.yml": "name: Not included\n" + okSteps,
			},
			want: []string{"A", "B"},
		},
		{
			// The path is told from the file that writes jobs, not from
			// the first file given nor from where the run is.
			name: "a path from the file that writes jobs",
			files: map[string]string{
				"env/vars.yml":    "vars:\n  url: http://localhost\n",
				"main/w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"main/jobs/a.yml": "name: A\n" + okSteps,
			},
			path: "env/vars.yml,main/w.yml",
			want: []string{"A"},
		},
		{
			name: "jobs that need one another across files",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n- include: jobs/b.yml\n",
				"jobs/a.yml": "name: A\nid: a\n" + okSteps,
				"jobs/b.yml": "name: B\nneeds: [a]\n" + okSteps,
			},
			want: []string{"A", "B"},
		},
		{
			name: "an anchor of the included file",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "- name: A\n  steps:\n  - &get\n    uses: http\n    with:\n      get: /\n    test: res.code == 200\n- name: B\n  steps:\n  - *get\n",
			},
			want: []string{"A", "B"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeTree(t, tt.files)
			path := tt.path
			if path == "" {
				path = "w.yml"
			}
			var paths []string
			for p := range strings.SplitSeq(path, ",") {
				paths = append(paths, filepath.Join(dir, p))
			}
			p := &Probe{FilePath: strings.Join(paths, ","), Config: Config{Actions: builtinNames}}
			if err := p.Load(); err != nil {
				t.Fatalf("Load() = %v", err)
			}
			got := jobNames(p.workflow.Jobs)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("jobs = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoad_IncludeDefaults gives the jobs of an included file the defaults
// of the workflow, as those written in it.
func TestLoad_IncludeDefaults(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml":      "name: w\ndefaults:\n  http:\n    url: http://localhost\njobs:\n- include: jobs/a.yml\n",
		"jobs/a.yml": "name: A\n" + okSteps,
	})
	p := &Probe{FilePath: filepath.Join(dir, "w.yml"), Config: Config{Actions: builtinNames}}
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	if got := p.workflow.Jobs[0].Steps[0].With["url"]; got != "http://localhost" {
		t.Errorf("url = %v, want the default of the workflow", got)
	}
}

func TestLoad_IncludeErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "a file that does not exist",
			files: map[string]string{"w.yml": "name: w\njobs:\n- include: jobs/none.yml\n"},
			want:  "w.yml:3: jobs/none.yml: no such file or directory",
		},
		{
			name:  "a glob that matches nothing",
			files: map[string]string{"w.yml": "name: w\njobs:\n- include: jobs/*.yml\n"},
			want:  "no YAML file matches",
		},
		{
			name: "a directory with no YAML file",
			files: map[string]string{
				"w.yml":         "name: w\njobs:\n- include: jobs\n",
				"jobs/notes.md": "x",
			},
			want: "no YAML file in the directory",
		},
		{
			name: "a file that is not YAML",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.txt\n",
				"jobs/a.txt": "name: A\n",
			},
			want: "not a YAML file",
		},
		{
			name: "other keys with include",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n  needs: [x]\n",
				"jobs/a.yml": "name: A\n" + okSteps,
			},
			want: `w.yml:3: include takes no other key, but "needs" is written with it`,
		},
		{
			name:  "an include that is not a path",
			files: map[string]string{"w.yml": "name: w\njobs:\n- include: [a.yml]\n"},
			want:  "w.yml:3: include takes the path of a file, a directory or a glob",
		},
		{
			name: "an included file that includes",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "- name: A\n" + okStep + "- include: b.yml\n",
			},
			want: "a.yml:7: an included file cannot include",
		},
		{
			name: "a file included twice",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n- include: jobs/*.yml\n",
				"jobs/a.yml": "name: A\n" + okSteps,
			},
			want: "a.yml: included already, at ",
		},
		{
			name: "the workflow included",
			files: map[string]string{
				"w.yml": "name: w\njobs:\n- include: '*.yml'\n",
			},
			want: "w.yml: a file of the workflow cannot be included",
		},
		{
			name: "a workflow included",
			files: map[string]string{
				"w.yml":     "name: w\njobs:\n- include: other.yml\n",
				"other.yml": "name: other\njobs:\n- name: A\n" + okStep,
			},
			want: "other.yml: is a workflow; an included file holds a job or a list of jobs",
		},
		{
			name: "an empty file",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "# nothing yet\n",
			},
			want: "a.yml: holds no job",
		},
		{
			name: "more than one document",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "name: A\n" + okSteps + "---\nname: B\n" + okSteps,
			},
			want: "a.yml:8: an included file holds a job or a list of jobs, not more than one document",
		},
		{
			name: "an included file that does not parse",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "name: A\nsteps: [\n",
			},
			want: "a.yml:2: YAML: ",
		},
		{
			// The file and its line are told, not a line of the text
			// read, which means nothing to whoever reads it.
			name: "an included job that does not decode",
			files: map[string]string{
				"w.yml":      "name: w\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "name: A\nsteps:\n- uses: http\n  retry: 3\n",
			},
			want: "a.yml:4:10: ",
		},
		{
			name: "an alias to an anchor of the workflow",
			files: map[string]string{
				"w.yml":      "name: w\nvars:\n  step: &get\n    uses: http\njobs:\n- include: jobs/a.yml\n",
				"jobs/a.yml": "name: A\nsteps:\n- *get\n",
			},
			want: "a.yml:3:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeTree(t, tt.files)
			p := &Probe{FilePath: filepath.Join(dir, "w.yml"), Config: Config{Actions: builtinNames}}
			err := p.Load()
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
		"a.yml":      "name: w\njobs:\n- include: jobs/none.yml\n",
		"b.yml":      "jobs:\n- include: jobs/b.yml\n",
		"jobs/b.yml": "name: B\n" + okSteps,
	})
	p := &Probe{FilePath: filepath.Join(dir, "a.yml") + "," + filepath.Join(dir, "b.yml"), Config: Config{Actions: builtinNames}}
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	if got := jobNames(p.workflow.Jobs); len(got) != 1 || got[0] != "B" {
		t.Errorf("jobs = %v, want B", got)
	}
}

// TestCheck_Include tells what is found in an included file by the file and
// its line, and an entry that cannot include its files on its line, with
// the rest checked.
func TestCheck_Include(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"w.yml": "name: w\njobs:\n- include: jobs/a.yml\n- include: jobs/none.yml\n- name: C\n" + okStep,
		"jobs/a.yml": `- name: A
  id: a
  steps:
  - uses: htp
    test: res.code == 200
- name: B
  needs: [z]
  steps:
  - uses: http
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
		message string
	}
	// The jobs are counted as they run, an entry that includes files
	// not among them.
	for _, f := range findings {
		if strings.Contains(f.Message, "tset") && !strings.HasPrefix(f.Where, `job 1 "B"`) {
			t.Errorf("where = %q, want job 1 \"B\"", f.Where)
		}
	}
	wants := []at{
		{"a.yml", 4, `unknown action "htp"`},
		{"a.yml", 7, `"z"`},
		{"a.yml", 12, `unknown key "tset"`},
		{"w.yml", 4, "no such file or directory"},
	}
	for _, w := range wants {
		found := false
		for _, f := range findings {
			if filepath.Base(f.File) == w.file && f.Line == w.line && strings.Contains(f.Message, w.message) {
				found = true
			}
		}
		if !found {
			var b strings.Builder
			for _, f := range findings {
				b.WriteString("\n  " + f.String())
			}
			t.Errorf("no finding at %s:%d saying %q in:%s", w.file, w.line, w.message, b.String())
		}
	}
}
