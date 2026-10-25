package probe

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestLoad(t *testing.T) {
	p := &Probe{
		FilePath: "./testdata/workflow.yml",
		Config: Config{
			Verbose: true,
			Timing:  false,
		},
	}
	err := p.Load()
	if err != nil {
		t.Errorf("probe load error %s", err)
	}
	expects, err := os.ReadFile("./testdata/marshaled-workflow.yml")
	if err != nil {
		t.Errorf("file read error %s", err)
	}
	got, _ := yaml.Marshal(p.workflow)
	if string(got) != string(expects) {
		t.Errorf("\nExpected:\n%s\nGot:\n%s", expects, got)
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		verbose  bool
		expected *Probe
	}{
		{
			name:    "create new probe with verbose true",
			path:    "./test.yml",
			verbose: true,
			expected: &Probe{
				FilePath: "./test.yml",
				Config: Config{
					Verbose: true,
					Timing:  false,
				},
			},
		},
		{
			name:    "create new probe with verbose false",
			path:    "./another.yml",
			verbose: false,
			expected: &Probe{
				FilePath: "./another.yml",
				Config: Config{
					Verbose: false,
					Timing:  false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.path, tt.verbose)
			if got.FilePath != tt.expected.FilePath {
				t.Errorf("FilePath = %v, want %v", got.FilePath, tt.expected.FilePath)
			}
			if got.Config.Verbose != tt.expected.Config.Verbose {
				t.Errorf("Config.Verbose = %v, want %v", got.Config.Verbose, tt.expected.Config.Verbose)
			}
			if got.Config.Timing != tt.expected.Config.Timing {
				t.Errorf("Config.Timing = %v, want %v", got.Config.Timing, tt.expected.Config.Timing)
			}
		})
	}
}

// TestNew_LeavesEnvironment tests that New sets no environment variable, as
// PROBE_TTY, which the CLI sets for the actions it runs: a constructor of
// the library has no business changing the environment of the process.
func TestNew_LeavesEnvironment(t *testing.T) {
	t.Setenv("PROBE_TTY", "")
	if err := os.Unsetenv("PROBE_TTY"); err != nil {
		t.Fatal(err)
	}
	before := os.Environ()

	_ = New("./test.yml", false)

	if after := os.Environ(); !reflect.DeepEqual(after, before) {
		t.Errorf("New() changed the environment from %v to %v", before, after)
	}
}

func TestExitStatus(t *testing.T) {
	p := &Probe{}

	// Initialize workflow
	p.workflow = Workflow{exitStatus: 0}

	if got := p.ExitStatus(); got != 0 {
		t.Errorf("ExitStatus() = %v, want %v", got, 0)
	}

	// Set exit status to 1
	p.workflow.exitStatus = 1

	if got := p.ExitStatus(); got != 1 {
		t.Errorf("ExitStatus() = %v, want %v", got, 1)
	}
}

func TestLoadWithInvalidYaml(t *testing.T) {
	p := &Probe{
		FilePath: "./nonexistent.yml",
		Config: Config{
			Verbose: false,
			Timing:  false,
		},
	}

	err := p.Load()
	if err == nil {
		t.Error("Load() should return error for invalid file path, but got nil")
	}
}

func TestDoWithInvalidPath(t *testing.T) {
	p := &Probe{
		FilePath: "./nonexistent.yml",
		Config: Config{
			Verbose: false,
			Timing:  false,
		},
	}

	err := p.Do()
	if err == nil {
		t.Error("Do() should return error for invalid file path, but got nil")
	}
}

func TestProbe_LoadSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.yml")
	yml := "name: s\nsecrets:\n- API_TOKEN\n- DB_PASSWORD\njobs:\n- name: j\n  steps:\n  - name: s\n    uses: hello\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	p := New(path, false)
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"API_TOKEN", "DB_PASSWORD"}; !reflect.DeepEqual(p.workflow.Secrets, want) {
		t.Errorf("Secrets = %v, want %v", p.workflow.Secrets, want)
	}
}
