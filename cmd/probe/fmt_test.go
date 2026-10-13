package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe"
)

func TestCmd_fmt(t *testing.T) {
	const (
		unformatted = "name: x\njobs:\n    - name: A   #first\n      steps:\n        - uses: hello\n\n        - uses: hello\n    - name: B\n      steps: []\n"
		formatted   = "name: x\n\njobs:\n- name: A # first\n  steps:\n  - uses: hello\n  - uses: hello\n\n- name: B\n  steps: []\n"
		broken      = "name: x\njobs: [\n"
	)
	// Each test has files of its own, since fmt rewrites them.
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		for name, content := range map[string]string{
			"a.yml":           unformatted,
			"ok.yml":          formatted,
			"sub/b.yaml":      unformatted,
			"sub/notes.txt":   unformatted,
			".hidden/c.yml":   unformatted,
			"broken/bad.yml":  broken,
			"broken/good.yml": unformatted,
		} {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	read := func(t *testing.T, path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	tests := []struct {
		name       string
		args       []string // relative to the directory of the test
		expectCode int
		expectOut  []string // the files printed, relative as well
		errContain string
		// expect is the content of files after the run.
		expect map[string]string
	}{
		{
			name:      "a file that is not formatted",
			args:      []string{"fmt", "a.yml"},
			expectOut: []string{"a.yml"},
			expect:    map[string]string{"a.yml": formatted},
		},
		{
			name:   "a file that is formatted",
			args:   []string{"fmt", "ok.yml"},
			expect: map[string]string{"ok.yml": formatted},
		},
		{
			name:      "several files",
			args:      []string{"fmt", "a.yml", "ok.yml", "sub/b.yaml"},
			expectOut: []string{"a.yml", "sub/b.yaml"},
			expect:    map[string]string{"a.yml": formatted, "sub/b.yaml": formatted},
		},
		{
			name:      "a directory",
			args:      []string{"fmt", "sub"},
			expectOut: []string{"sub/b.yaml"},
			expect:    map[string]string{"sub/b.yaml": formatted, "sub/notes.txt": unformatted},
		},
		{
			name:       "a directory and those under it, but the hidden",
			args:       []string{"fmt", "."},
			expectCode: probe.ExitConfigError,
			expectOut:  []string{"a.yml", "broken/good.yml", "sub/b.yaml"},
			errContain: "broken/bad.yml",
			expect:     map[string]string{"a.yml": formatted, "sub/b.yaml": formatted, ".hidden/c.yml": unformatted, "broken/good.yml": formatted, "broken/bad.yml": broken},
		},
		{
			name:       "a file that is not YAML",
			args:       []string{"fmt", "broken/bad.yml", "broken/good.yml"},
			expectCode: probe.ExitConfigError,
			expectOut:  []string{"broken/good.yml"},
			errContain: "broken/bad.yml: ",
			expect:     map[string]string{"broken/bad.yml": broken, "broken/good.yml": formatted},
		},
		{
			name:       "check a file that is not formatted",
			args:       []string{"fmt", "--check", "a.yml", "ok.yml"},
			expectCode: probe.ExitTestFailed,
			expectOut:  []string{"a.yml"},
			expect:     map[string]string{"a.yml": unformatted},
		},
		{
			name:   "check a file that is formatted",
			args:   []string{"fmt", "ok.yml", "--check"},
			expect: map[string]string{"ok.yml": formatted},
		},
		{
			name:       "check a file that is not YAML",
			args:       []string{"fmt", "--check", "broken"},
			expectCode: probe.ExitConfigError,
			expectOut:  []string{"broken/good.yml"},
			errContain: "broken/bad.yml: ",
			expect:     map[string]string{"broken/good.yml": unformatted},
		},
		{
			name:       "no file",
			args:       []string{"fmt"},
			expectCode: probe.ExitConfigError,
			errContain: "Usage: probe fmt [--check] <workflow-file>...",
		},
		{
			name:       "a file that does not exist",
			args:       []string{"fmt", "a.yml", "none.yml"},
			expectCode: probe.ExitConfigError,
			errContain: "none.yml: no such file or directory",
			expect:     map[string]string{"a.yml": unformatted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t)
			args := []string{"probe"}
			for _, a := range tt.args {
				if !strings.HasPrefix(a, "-") && a != "fmt" {
					a = filepath.Join(dir, a)
				}
				args = append(args, a)
			}
			c := newBufferCmd()
			if code := c.start(args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d; stdout: %s; stderr: %s", tt.args, code, tt.expectCode, c.outWriter, c.errWriter)
			}
			var want strings.Builder
			for _, f := range tt.expectOut {
				want.WriteString(filepath.Join(dir, f) + "\n")
			}
			if out := fmt.Sprintf("%s", c.outWriter); out != want.String() {
				t.Errorf("output = %q, want %q", out, want.String())
			}
			errOut := fmt.Sprintf("%s", c.errWriter)
			if !strings.Contains(errOut, tt.errContain) || (tt.errContain == "" && errOut != "") {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
			for name, content := range tt.expect {
				if got := read(t, filepath.Join(dir, name)); got != content {
					t.Errorf("%s = %q, want %q", name, got, content)
				}
			}
		})
	}

	t.Run("the mode of a file is kept", func(t *testing.T) {
		dir := setup(t)
		path := filepath.Join(dir, "a.yml")
		if c := newBufferCmd(); c.start([]string{"probe", "fmt", path}) != 0 {
			t.Fatalf("stderr: %s", c.errWriter)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Errorf("mode = %o, want 640", info.Mode().Perm())
		}
	})
}

func TestCmd_fmt_stdin(t *testing.T) {
	const (
		unformatted = "a:\n    - b:   1 #c\n"
		formatted   = "a:\n- b: 1 # c\n"
	)
	tests := []struct {
		name       string
		args       []string
		input      string
		expectCode int
		expectOut  string
		errContain string
	}{
		{"what is not formatted", []string{"probe", "fmt", "-"}, unformatted, 0, formatted, ""},
		{"what is formatted", []string{"probe", "fmt", "-"}, formatted, 0, formatted, ""},
		{"what is not YAML", []string{"probe", "fmt", "-"}, "a: [\n", probe.ExitConfigError, "", "[ERROR] "},
		{"check what is not formatted", []string{"probe", "fmt", "--check", "-"}, unformatted, probe.ExitTestFailed, "", ""},
		{"check what is formatted", []string{"probe", "fmt", "-", "--check"}, formatted, 0, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			c.inReader = strings.NewReader(tt.input)
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d; stderr: %s", tt.args, code, tt.expectCode, c.errWriter)
			}
			if out := fmt.Sprintf("%s", c.outWriter); out != tt.expectOut {
				t.Errorf("output = %q, want %q", out, tt.expectOut)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}
}

func TestCmd_fmt_checkWithoutFmt(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"a run", []string{"probe", "--check", "workflow.yml"}},
		{"the check subcommand", []string{"probe", "check", "--check", "workflow.yml"}},
		{"the gen subcommand", []string{"probe", "gen", "spec.yml", "--check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != probe.ExitConfigError {
				t.Errorf("start = %d, want %d", code, probe.ExitConfigError)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, "--check can only be used with the fmt subcommand") {
				t.Errorf("unexpected error output: %s", errOut)
			}
		})
	}
}
