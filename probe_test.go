package probe

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestIsYamlFile(t *testing.T) {
	p := &Probe{}
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
			got := p.isYamlFile(tt.filename)
			if got != tt.expected {
				t.Errorf("isYamlFile(%s) = %v, want %v", tt.filename, got, tt.expected)
			}
		})
	}
}

func TestYamlFiles(t *testing.T) {
	p := &Probe{}
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
			p.FilePath = tt.filePath
			files, err := p.yamlFiles()
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

	p := &Probe{FilePath: yml + "," + txt}
	if _, err := p.yamlFiles(); err == nil || !strings.Contains(err.Error(), "notes.txt: not a YAML file") {
		t.Errorf("yamlFiles() error = %v, want one naming notes.txt", err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "*")} {
		p := &Probe{FilePath: path}
		files, err := p.yamlFiles()
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
		_, data, err := (&Probe{}).readYamlFiles(paths)
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
	p := &Probe{}

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
			_, _, err := p.readYamlFiles(tt.paths)
			if (err != nil) != tt.wantErr {
				t.Errorf("readYamlFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeDefaults(t *testing.T) {

	tests := []struct {
		name     string
		data     map[string]any
		defaults map[string]any
		expected map[string]any
	}{
		{
			name: "simple defaults",
			data: map[string]any{
				"existing": "value",
			},
			defaults: map[string]any{
				"new_key": "default_value",
			},
			expected: map[string]any{
				"existing": "value",
				"new_key":  "default_value",
			},
		},
		{
			name: "nested defaults",
			data: map[string]any{
				"nested": map[string]any{
					"existing": "value",
				},
			},
			defaults: map[string]any{
				"nested": map[string]any{
					"new_key": "default_value",
				},
			},
			expected: map[string]any{
				"nested": map[string]any{
					"existing": "value",
					"new_key":  "default_value",
				},
			},
		},
		{
			name: "overwrite protection",
			data: map[string]any{
				"key": "original",
			},
			defaults: map[string]any{
				"key": "default",
			},
			expected: map[string]any{
				"key": "original",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeDefaults(tt.data, tt.defaults)
			if !reflect.DeepEqual(tt.data, tt.expected) {
				t.Errorf("mergeDefaults() = %v, want %v", tt.data, tt.expected)
			}
		})
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

func TestYamlFilesWithMultiplePaths(t *testing.T) {
	p := &Probe{}

	// Test with comma-separated paths
	p.FilePath = "./testdata/workflow.yml,./testdata/marshaled-workflow.yml"
	files, err := p.yamlFiles()
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

// Validation tests
func TestProbe_validateIDs(t *testing.T) {
	tests := []struct {
		name        string
		workflow    Workflow
		expectError bool
		errorType   string
	}{
		{
			name: "no duplicate IDs",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
							{ID: "step2", Name: "Step 2", Uses: "hello"},
						},
					},
					{
						ID:   "job2",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "step3", Name: "Step 3", Uses: "hello"},
							{ID: "step4", Name: "Step 4", Uses: "hello"},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate job IDs",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
						},
					},
					{
						ID:   "job1", // duplicate job ID
						Name: "Job 2",
						Steps: []*Step{
							{ID: "step2", Name: "Step 2", Uses: "hello"},
						},
					},
				},
			},
			expectError: true,
			errorType:   "duplicate_job_id",
		},
		{
			name: "duplicate step IDs across jobs",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
						},
					},
					{
						ID:   "job2",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "step1", Name: "Step 2", Uses: "hello"}, // duplicate step ID
						},
					},
				},
			},
			expectError: true,
			errorType:   "duplicate_step_id",
		},
		{
			name: "duplicate step IDs within same job",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
							{ID: "step1", Name: "Step 2", Uses: "hello"}, // duplicate step ID
						},
					},
				},
			},
			expectError: true,
			errorType:   "duplicate_step_id",
		},
		{
			name: "empty IDs are allowed for validation",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "", Name: "Step 1", Uses: "hello"},
							{ID: "", Name: "Step 2", Uses: "hello"},
						},
					},
					{
						ID:   "",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "", Name: "Step 3", Uses: "hello"},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "mixed empty and non-empty IDs",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
							{ID: "", Name: "Step 2", Uses: "hello"}, // empty ID is OK
						},
					},
					{
						ID:   "", // empty job ID is OK
						Name: "Job 2",
						Steps: []*Step{
							{ID: "step2", Name: "Step 3", Uses: "hello"},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Probe{workflow: tt.workflow}
			err := p.validateIDs()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}

				if probeErr, ok := err.(*ProbeError); ok {
					if probeErr.Operation != tt.errorType {
						t.Errorf("expected error operation %s, got %s", tt.errorType, probeErr.Operation)
					}
				} else {
					t.Errorf("expected ProbeError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestProbe_assignJobIDs(t *testing.T) {
	tests := []struct {
		name            string
		workflow        Workflow
		expectedJobIDs  []string
		expectedStepIDs [][]string // stepIDs per job
	}{
		{
			name: "initialize empty job IDs, and leave the steps without",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "", Name: "Step 1", Uses: "hello"},
							{ID: "", Name: "Step 2", Uses: "hello"},
						},
					},
					{
						ID:   "",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "", Name: "Step 3", Uses: "hello"},
						},
					},
				},
			},
			expectedJobIDs: []string{"job_0", "job_1"},
			expectedStepIDs: [][]string{
				{"", ""},
				{""},
			},
		},
		{
			name: "preserve existing IDs",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "custom_job",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "custom_step", Name: "Step 1", Uses: "hello"},
							{ID: "", Name: "Step 2", Uses: "hello"},
						},
					},
					{
						ID:   "",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "", Name: "Step 3", Uses: "hello"},
							{ID: "another_custom", Name: "Step 4", Uses: "hello"},
						},
					},
				},
			},
			expectedJobIDs: []string{"custom_job", "job_1"},
			expectedStepIDs: [][]string{
				{"custom_step", ""},
				{"", "another_custom"},
			},
		},
		{
			name: "no changes needed",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "job1",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
							{ID: "step2", Name: "Step 2", Uses: "hello"},
						},
					},
				},
			},
			expectedJobIDs: []string{"job1"},
			expectedStepIDs: [][]string{
				{"step1", "step2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Probe{workflow: tt.workflow}
			if err := p.workflow.assignJobIDs(); err != nil {
				t.Fatalf("assignJobIDs() error = %v", err)
			}

			// Check job IDs
			for i, expectedJobID := range tt.expectedJobIDs {
				if i >= len(p.workflow.Jobs) {
					t.Errorf("expected %d jobs, got %d", len(tt.expectedJobIDs), len(p.workflow.Jobs))
					continue
				}
				if p.workflow.Jobs[i].ID != expectedJobID {
					t.Errorf("job %d: expected ID %s, got %s", i, expectedJobID, p.workflow.Jobs[i].ID)
				}
			}

			// Check step IDs
			for jobIdx, expectedStepIDs := range tt.expectedStepIDs {
				if jobIdx >= len(p.workflow.Jobs) {
					continue
				}
				job := p.workflow.Jobs[jobIdx]
				for stepIdx, expectedStepID := range expectedStepIDs {
					if stepIdx >= len(job.Steps) {
						t.Errorf("job %d: expected %d steps, got %d", jobIdx, len(expectedStepIDs), len(job.Steps))
						continue
					}
					if job.Steps[stepIdx].ID != expectedStepID {
						t.Errorf("job %d, step %d: expected ID %s, got %s",
							jobIdx, stepIdx, expectedStepID, job.Steps[stepIdx].ID)
					}
				}
			}
		})
	}
}

func TestProbe_validateAndInitializeIDs_Integration(t *testing.T) {
	tests := []struct {
		name                string
		workflow            Workflow
		expectValidationErr bool
		expectedJobIDs      []string
		expectedStepIDs     [][]string
	}{
		{
			name: "successful validation and initialization",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "", Name: "Step 1", Uses: "hello"},
							{ID: "custom_step", Name: "Step 2", Uses: "hello"},
						},
					},
					{
						ID:   "custom_job",
						Name: "Job 2",
						Steps: []*Step{
							{ID: "", Name: "Step 3", Uses: "hello"},
						},
					},
				},
			},
			expectValidationErr: false,
			expectedJobIDs:      []string{"job_0", "custom_job"},
			expectedStepIDs: [][]string{
				{"", "custom_step"},
				{""},
			},
		},
		{
			name: "validation fails before initialization",
			workflow: Workflow{
				Jobs: []Job{
					{
						ID:   "duplicate",
						Name: "Job 1",
						Steps: []*Step{
							{ID: "step1", Name: "Step 1", Uses: "hello"},
						},
					},
					{
						ID:   "duplicate", // duplicate job ID
						Name: "Job 2",
						Steps: []*Step{
							{ID: "step2", Name: "Step 2", Uses: "hello"},
						},
					},
				},
			},
			expectValidationErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Probe{workflow: tt.workflow}

			// Run validation
			err := p.validateIDs()

			if tt.expectValidationErr {
				if err == nil {
					t.Errorf("expected validation error but got none")
				}
				return // Don't test initialization if validation fails
			}

			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
				return
			}

			// Run initialization
			if err := p.workflow.assignJobIDs(); err != nil {
				t.Fatalf("assignJobIDs() error = %v", err)
			}

			// Check results
			for i, expectedJobID := range tt.expectedJobIDs {
				if p.workflow.Jobs[i].ID != expectedJobID {
					t.Errorf("job %d: expected ID %s, got %s", i, expectedJobID, p.workflow.Jobs[i].ID)
				}
			}

			for jobIdx, expectedStepIDs := range tt.expectedStepIDs {
				job := p.workflow.Jobs[jobIdx]
				for stepIdx, expectedStepID := range expectedStepIDs {
					if job.Steps[stepIdx].ID != expectedStepID {
						t.Errorf("job %d, step %d: expected ID %s, got %s",
							jobIdx, stepIdx, expectedStepID, job.Steps[stepIdx].ID)
					}
				}
			}
		})
	}
}

func TestProbe_validateIDs_ErrorMessages(t *testing.T) {
	t.Run("duplicate job ID error message", func(t *testing.T) {
		workflow := Workflow{
			Jobs: []Job{
				{ID: "job1", Name: "Job 1", Steps: []*Step{{Uses: "hello"}}},
				{ID: "job1", Name: "Job 2", Steps: []*Step{{Uses: "hello"}}},
			},
		}
		p := &Probe{workflow: workflow}
		err := p.validateIDs()

		if err == nil {
			t.Fatal("expected error but got none")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate job ID 'job1'") {
			t.Errorf("error message should contain duplicate job ID info, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "job 1") && !strings.Contains(errMsg, "job 0") {
			t.Errorf("error message should contain job indices, got: %s", errMsg)
		}
	})

	t.Run("duplicate step ID error message", func(t *testing.T) {
		workflow := Workflow{
			Jobs: []Job{
				{
					ID: "job1", Name: "Job 1",
					Steps: []*Step{
						{ID: "step1", Uses: "hello"},
						{ID: "step1", Uses: "hello"},
					},
				},
			},
		}
		p := &Probe{workflow: workflow}
		err := p.validateIDs()

		if err == nil {
			t.Fatal("expected error but got none")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate step ID 'step1'") {
			t.Errorf("error message should contain duplicate step ID info, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "job[0].step[1]") {
			t.Errorf("error message should contain step location info, got: %s", errMsg)
		}
	})
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
