package probe

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsYamlFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
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
			got := isYamlFile(tt.filename)
			if got != tt.expected {
				t.Errorf("isYamlFile(%s) = %v, want %v", tt.filename, got, tt.expected)
			}
		})
	}
}

func TestYamlFiles(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		wantErr  bool
	}{
		{
			name:     "single file",
			filePath: "./testdata/workflow.yml",
			wantErr:  false,
		},
		{
			name:     "nonexistent file",
			filePath: "./nonexistent.yml",
			wantErr:  true,
		},
		{
			name:     "directory",
			filePath: "./testdata",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := yamlFiles(tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("yamlFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(files) == 0 {
				t.Errorf("yamlFiles() returned empty files slice")
			}
		})
	}
}

// TestYamlFiles_NotYAML refuses a file named on its own that is not YAML,
// and leaves out one that a directory or a glob finds.
func TestYamlFiles_NotYAML(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"w.yml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("name: w\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	yml, txt := filepath.Join(dir, "w.yml"), filepath.Join(dir, "notes.txt")

	if _, err := yamlFiles(yml + "," + txt); err == nil || !strings.Contains(err.Error(), "notes.txt: not a YAML file") {
		t.Errorf("yamlFiles() error = %v, want one naming notes.txt", err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "*")} {
		files, err := yamlFiles(path)
		if err != nil {
			t.Fatalf("yamlFiles(%s) unexpected error: %v", path, err)
		}
		if len(files) != 1 || files[0] != yml {
			t.Errorf("yamlFiles(%s) = %v, want [%s]", path, files, yml)
		}
	}
}

func TestRedefinedJobs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := write("a.yml", "name: a\njobs:\n- name: A\n")
	b := write("b.yml", "vars:\n  jobs: 1\n# jobs:\n\"jobs\" :\r\n- name: B\n")
	vars := write("vars.yml", "vars:\n  who: x\n")
	c := write("c.yml", "jobs: []\n")

	read := func(paths ...string) ([]string, [][]byte) {
		_, data, err := readYamlFiles(paths)
		if err != nil {
			t.Fatal(err)
		}
		return paths, data
	}

	got := redefinedJobs(read(a, vars, b, vars, c))
	want := []jobsRedefinition{
		{File: b, Line: 4, Prev: a, PrevLine: 2},
		{File: c, Line: 1, Prev: b, PrevLine: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("redefinedJobs() = %v, want %v", got, want)
	}

	if got := redefinedJobs(read(a, vars)); len(got) != 0 {
		t.Errorf("redefinedJobs() of one file with jobs = %v, want none", got)
	}
}

func TestReadYamlFiles(t *testing.T) {
	tests := []struct {
		name    string
		paths   []string
		wantErr bool
	}{
		{
			name:    "existing file",
			paths:   []string{"./testdata/workflow.yml"},
			wantErr: false,
		},
		{
			name:    "nonexistent file",
			paths:   []string{"nonexistent.yml"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := readYamlFiles(tt.paths)
			if (err != nil) != tt.wantErr {
				t.Errorf("readYamlFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestReadYamlFiles_Joined reads the files as one text, with a newline
// after a file that does not end in one, unless it is the last, and the
// content of each as it is.
func TestReadYamlFiles_Joined(t *testing.T) {
	dir := t.TempDir()
	a := writeWorkflow(t, dir, "a.yml", "name: a")
	b := writeWorkflow(t, dir, "b.yml", "vars:\n  x: 1\n")
	c := writeWorkflow(t, dir, "c.yml", "jobs: []")
	text, contents, err := readYamlFiles([]string{a, b, c})
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
}

func TestYamlFilesWithMultiplePaths(t *testing.T) {
	// Test with comma-separated paths
	files, err := yamlFiles("./testdata/workflow.yml,./testdata/marshaled-workflow.yml")
	if err != nil {
		t.Errorf("yamlFiles() with multiple paths error = %v", err)
		return
	}

	if len(files) != 2 {
		t.Errorf("yamlFiles() with multiple paths returned %d files, want 2", len(files))
	}

	// Check that both files are included
	foundWorkflow := false
	foundMarshaled := false
	for _, file := range files {
		if strings.Contains(file, "workflow.yml") {
			foundWorkflow = true
		}
		if strings.Contains(file, "marshaled-workflow.yml") {
			foundMarshaled = true
		}
	}

	if !foundWorkflow || !foundMarshaled {
		t.Errorf("yamlFiles() did not find both expected files: %v", files)
	}
}
