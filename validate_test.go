package probe

import (
	"strings"
	"testing"
)

func TestWorkflow_validateIDs(t *testing.T) {
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
			w := tt.workflow
			err := w.validateIDs()

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

// TestWorkflow_assignJobIDsOnceIDsAreValid checks the IDs of a workflow as a
// load does: validateIDs first, which refuses a duplicate, then assignJobIDs,
// which gives a job without an ID its default and leaves the IDs of the
// steps as written.
func TestWorkflow_assignJobIDsOnceIDsAreValid(t *testing.T) {
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
			w := tt.workflow

			// Run validation
			err := w.validateIDs()

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
			if err := w.assignJobIDs(); err != nil {
				t.Fatalf("assignJobIDs() error = %v", err)
			}

			// Check results
			for i, expectedJobID := range tt.expectedJobIDs {
				if w.Jobs[i].ID != expectedJobID {
					t.Errorf("job %d: expected ID %s, got %s", i, expectedJobID, w.Jobs[i].ID)
				}
			}

			for jobIdx, expectedStepIDs := range tt.expectedStepIDs {
				job := w.Jobs[jobIdx]
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

func TestWorkflow_validateIDs_ErrorMessages(t *testing.T) {
	t.Run("duplicate job ID error message", func(t *testing.T) {
		workflow := Workflow{
			Jobs: []Job{
				{ID: "job1", Name: "Job 1", Steps: []*Step{{Uses: "hello"}}},
				{ID: "job1", Name: "Job 2", Steps: []*Step{{Uses: "hello"}}},
			},
		}
		err := workflow.validateIDs()

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
		err := workflow.validateIDs()

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
