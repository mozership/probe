package workflowfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// write writes content to name in dir, and returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsYAML(t *testing.T) {
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"yml extension", "test.yml", true},
		{"yaml extension", "test.yaml", true},
		{"txt extension", "test.txt", false},
		{"no extension", "test", false},
		{"yml in middle", "test.yml.txt", false},
		{"yaml in middle", "test.yaml.txt", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsYAML(tt.file); got != tt.want {
				t.Errorf("IsYAML(%q) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}

func TestPaths(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yml", "name: a\n")
	b := write(t, dir, "b.yaml", "name: b\n")
	write(t, dir, "notes.txt", "x\n")
	// A directory, or a symlink to one, named as a YAML file is no workflow
	// file, and is left out as one that is not YAML is.
	if err := os.Mkdir(filepath.Join(dir, "sub.yml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "sub.yml"), filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want []string
	}{
		{"single file", a, []string{a}},
		{"directory", dir, []string{a, b}},
		{"glob", filepath.Join(dir, "*"), []string{a, b}},
		{"comma separated", a + " , " + b, []string{a, b}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Paths(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Paths(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestPaths_Refused refuses a path that names nothing, and a file named on
// its own that is not YAML.
func TestPaths_Refused(t *testing.T) {
	dir := t.TempDir()
	yml := write(t, dir, "w.yml", "name: w\n")
	txt := write(t, dir, "notes.txt", "name: w\n")

	tests := []struct {
		name string
		path string
		want string
	}{
		{"nonexistent file", filepath.Join(dir, "none.yml"), "none.yml: no such file or directory"},
		{"not YAML", yml + "," + txt, "notes.txt: not a YAML file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Paths(tt.path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Paths(%q) error = %v, want one containing %q", tt.path, err, tt.want)
			}
		})
	}
}

// TestRead reads the files as one text, with a newline after a file that
// does not end in one, unless it is the last, and the content of each as it
// is.
func TestRead(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yml", "name: a")
	b := write(t, dir, "b.yml", "vars:\n  x: 1\n")
	c := write(t, dir, "c.yml", "jobs: []")

	var read []string
	text, contents, err := Read([]string{a, b, c}, func(name string) ([]byte, error) {
		read = append(read, name)
		return os.ReadFile(name)
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "name: a\nvars:\n  x: 1\njobs: []"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	want := [][]byte{[]byte("name: a"), []byte("vars:\n  x: 1\n"), []byte("jobs: []")}
	if !reflect.DeepEqual(contents, want) {
		t.Errorf("contents = %q, want %q", contents, want)
	}
	if want := []string{a, b, c}; !reflect.DeepEqual(read, want) {
		t.Errorf("read = %v, want %v", read, want)
	}
}

func TestRead_Error(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.yml")
	if _, _, err := Read([]string{missing}, os.ReadFile); err == nil {
		t.Error("Read() of a missing file returned no error")
	}
}

func TestRedefinedJobs(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yml", "name: a\njobs:\n- name: A\n")
	b := write(t, dir, "b.yml", "vars:\n  jobs: 1\n# jobs:\n\"jobs\" :\r\n- name: B\n")
	vars := write(t, dir, "vars.yml", "vars:\n  who: x\n")
	c := write(t, dir, "c.yml", "jobs: []\n")

	read := func(paths ...string) ([]string, [][]byte) {
		_, data, err := Read(paths, os.ReadFile)
		if err != nil {
			t.Fatal(err)
		}
		return paths, data
	}

	got := RedefinedJobs(read(a, vars, b, vars, c))
	want := []Redefinition{
		{File: b, Line: 4, Prev: a, PrevLine: 2},
		{File: c, Line: 1, Prev: b, PrevLine: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RedefinedJobs() = %v, want %v", got, want)
	}

	if got := RedefinedJobs(read(a, vars)); len(got) != 0 {
		t.Errorf("RedefinedJobs() of one file with jobs = %v, want none", got)
	}
}

func TestRedefinition_String(t *testing.T) {
	r := Redefinition{File: "b.yml", Line: 4, Prev: "a.yml", PrevLine: 2}
	if got, want := r.String(), "b.yml:4: jobs replaces the jobs of a.yml:2, which do not run"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
