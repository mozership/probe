package probe

import (
	"testing"
	"time"
)

// Test StatusType constants have expected values
func TestStatusTypeConstants(t *testing.T) {
	// Test that status constants are assigned in expected order
	if StatusSuccess != 0 {
		t.Errorf("StatusSuccess should be 0, got %d", StatusSuccess)
	}
	if StatusError != 1 {
		t.Errorf("StatusError should be 1, got %d", StatusError)
	}
	if StatusWarning != 2 {
		t.Errorf("StatusWarning should be 2, got %d", StatusWarning)
	}
	if StatusSkipped != 3 {
		t.Errorf("StatusSkipped should be 3, got %d", StatusSkipped)
	}

	// Test that all constants are different
	statuses := []StatusType{StatusSuccess, StatusError, StatusWarning, StatusSkipped}
	for i, status1 := range statuses {
		for j, status2 := range statuses {
			if i != j && status1 == status2 {
				t.Errorf("StatusType constants should be unique, but %d and %d are both %d", i, j, status1)
			}
		}
	}
}

func TestResult_AddStepResult(t *testing.T) {
	wb := NewResult()
	jobID := "test-job"

	// Add a job buffer first
	wb.Jobs[jobID] = &JobResult{
		JobID:       jobID,
		JobName:     "Test Job",
		StartTime:   time.Now(),
		StepResults: []StepResult{},
	}

	// Create test step results
	stepResult1 := StepResult{
		Index:  0,
		Name:   "Step 1",
		Status: StatusSuccess,
	}

	stepResult2 := StepResult{
		Index:  1,
		Name:   "Step 2",
		Status: StatusError,
		RepeatCounter: &StepRepeatCounter{
			SuccessCount: 3,
			FailureCount: 1,
		},
	}

	// Add step results
	wb.AddStepResult(jobID, stepResult1)
	wb.AddStepResult(jobID, stepResult2)

	// Verify step results were added
	jobResult, exists := wb.Jobs[jobID]
	if !exists {
		t.Fatal("Job buffer should exist")
	}
	if len(jobResult.StepResults) != 2 {
		t.Errorf("Expected 2 step results, got %d", len(jobResult.StepResults))
	}

	if jobResult.StepResults[0].Name != "Step 1" {
		t.Errorf("Expected first step name 'Step 1', got '%s'", jobResult.StepResults[0].Name)
	}

	if jobResult.StepResults[1].Name != "Step 2" {
		t.Errorf("Expected second step name 'Step 2', got '%s'", jobResult.StepResults[1].Name)
	}

	if jobResult.StepResults[1].RepeatCounter == nil {
		t.Error("Expected RepeatCounter to be set for second step")
	} else if jobResult.StepResults[1].RepeatCounter.SuccessCount != 3 {
		t.Errorf("Expected RepeatCounter.SuccessCount = 3, got %d", jobResult.StepResults[1].RepeatCounter.SuccessCount)
	}
}

func TestResult_AddStepResult_NonExistentJob(t *testing.T) {
	wb := NewResult()

	stepResult := StepResult{
		Index:  0,
		Name:   "Step 1",
		Status: StatusSuccess,
	}

	// This should not panic even if job doesn't exist
	wb.AddStepResult("non-existent-job", stepResult)

	// Verify no job buffer was created
	if _, exists := wb.Jobs["non-existent-job"]; exists {
		t.Error("Job buffer should not be created for non-existent job")
	}
}

func TestResult_AddStepResult_Concurrent(t *testing.T) {
	wb := NewResult()
	jobID := "test-job"

	// Add a job buffer first
	wb.Jobs[jobID] = &JobResult{
		JobID:       jobID,
		JobName:     "Test Job",
		StartTime:   time.Now(),
		StepResults: []StepResult{},
	}

	// Test concurrent add and get operations
	done := make(chan bool, 2)

	// Goroutine 1: Add step results
	go func() {
		for i := range 10 {
			stepResult := StepResult{
				Index:  i,
				Name:   "Step " + string(rune('0'+i)),
				Status: StatusSuccess,
			}
			wb.AddStepResult(jobID, stepResult)
		}
		done <- true
	}()

	// Goroutine 2: Read job buffer.
	// AddStepResult writes StepResults under JobResult.mutex, so readers
	// must take the same lock — see printer.go where reports do exactly
	// this. Reading without the lock races with the writer goroutine.
	go func() {
		for range 5 {
			jobResult := wb.Jobs[jobID]
			if jobResult != nil {
				jobResult.mutex.Lock()
				_ = len(jobResult.StepResults)
				jobResult.mutex.Unlock()
			}
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify final state
	jobResult, exists := wb.Jobs[jobID]
	if !exists {
		t.Fatal("Job buffer should exist after concurrent operations")
	}
	jobResult.mutex.Lock()
	stepCount := len(jobResult.StepResults)
	jobResult.mutex.Unlock()
	if stepCount != 10 {
		t.Errorf("Expected 10 step results after concurrent operations, got %d", stepCount)
	}
}
