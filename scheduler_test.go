package probe

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJobScheduler_MarkJobsWithFailedDependencies(t *testing.T) {
	t.Run("multi-level dependency failures are properly marked", func(t *testing.T) {
		// This test verifies that jobs with JobFailed dependencies are also marked as failed
		// Previously, only JobCompleted with !results was checked, causing deadlock
		scheduler := NewJobScheduler()

		// Level 1 job that will fail
		job1 := &Job{
			Name:  "level1",
			ID:    "level1",
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job1); err != nil {
			t.Fatalf("Failed to add job1: %v", err)
		}

		// Level 2 job that depends on level1
		job2 := &Job{
			Name:  "level2",
			ID:    "level2",
			Needs: []string{"level1"},
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job2); err != nil {
			t.Fatalf("Failed to add job2: %v", err)
		}

		// Level 3 job that depends on level2
		job3 := &Job{
			Name:  "level3",
			ID:    "level3",
			Needs: []string{"level2"},
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job3); err != nil {
			t.Fatalf("Failed to add job3: %v", err)
		}

		// Simulate level1 completing with failure
		scheduler.SetJobStatus("level1", JobCompleted, false)

		// Call MarkJobsWithFailedDependencies
		// With the fix, this should mark both level2 and level3 as failed in a single pass
		// because MarkJobsWithFailedDependencies now checks for both JobFailed and JobCompleted+!results
		skipped := scheduler.MarkJobsWithFailedDependencies()

		// Both level2 and level3 should be skipped
		// Note: The order may vary due to map iteration, so we check the length and contents
		if len(skipped) != 2 {
			t.Errorf("Expected 2 jobs to be skipped (level2 and level3), got %d: %v", len(skipped), skipped)
		}

		skippedMap := make(map[string]bool)
		for _, id := range skipped {
			skippedMap[id] = true
		}
		if !skippedMap["level2"] || !skippedMap["level3"] {
			t.Errorf("Expected level2 and level3 to be skipped, got: %v", skipped)
		}

		// Verify both are marked as JobFailed
		if scheduler.status["level2"] != JobFailed {
			t.Errorf("Expected level2 status to be JobFailed, got: %v", scheduler.status["level2"])
		}
		if scheduler.status["level3"] != JobFailed {
			t.Errorf("Expected level3 status to be JobFailed, got: %v", scheduler.status["level3"])
		}

		// Second call should return no skipped jobs (all are already failed)
		skipped2 := scheduler.MarkJobsWithFailedDependencies()
		if len(skipped2) != 0 {
			t.Errorf("Expected no more jobs to be skipped, got: %v", skipped2)
		}

		// Verify AllJobsCompleted returns true
		if !scheduler.AllJobsCompleted() {
			t.Error("Expected AllJobsCompleted to return true after all jobs are in terminal state")
		}
	})

	t.Run("independent jobs are not affected by failed dependencies", func(t *testing.T) {
		scheduler := NewJobScheduler()

		// Job that will fail
		job1 := &Job{
			Name:  "job1",
			ID:    "job1",
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job1); err != nil {
			t.Fatalf("Failed to add job1: %v", err)
		}

		// Job that depends on job1
		job2 := &Job{
			Name:  "job2",
			ID:    "job2",
			Needs: []string{"job1"},
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job2); err != nil {
			t.Fatalf("Failed to add job2: %v", err)
		}

		// Independent job
		job3 := &Job{
			Name:  "job3",
			ID:    "job3",
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job3); err != nil {
			t.Fatalf("Failed to add job3: %v", err)
		}

		// Simulate job1 completing with failure
		scheduler.SetJobStatus("job1", JobCompleted, false)

		// Mark failed dependencies
		skipped := scheduler.MarkJobsWithFailedDependencies()

		// Only job2 should be skipped, not job3
		if len(skipped) != 1 || skipped[0] != "job2" {
			t.Errorf("Expected only job2 to be skipped, got: %v", skipped)
		}

		// job3 should still be pending
		if scheduler.status["job3"] != JobPending {
			t.Errorf("Expected job3 to remain JobPending, got: %v", scheduler.status["job3"])
		}

		// job3 should be runnable
		if !scheduler.CanRunJob("job3") {
			t.Error("Expected job3 to be runnable")
		}
	})
}

func TestJobScheduler_CanRunJob_UnknownIDReturnsFalse(t *testing.T) {
	// CanRunJob is exported, so callers may pass arbitrary IDs. Looking up
	// a missing ID in js.jobs returns nil; without a guard, ranging over
	// the nil *Job's Needs slice panics. The function must instead return
	// false for unknown jobs.
	scheduler := NewJobScheduler()

	known := &Job{ID: "known", Name: "known"}
	if err := scheduler.AddJob(known); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CanRunJob panicked on unknown jobID: %v", r)
		}
	}()

	if got := scheduler.CanRunJob("does-not-exist"); got {
		t.Errorf("CanRunJob(unknown) = true, want false")
	}
}

func TestJobScheduler_NoDeadlockOnConcurrentReadAndWrite(t *testing.T) {
	// Regression test for the recursive RLock deadlock.
	//
	// GetRunnableJobs holds RLock, then calls CanRunJob which tries to
	// acquire RLock again. Per Go's sync.RWMutex contract, a blocked Lock
	// call excludes new readers from acquiring the lock to avoid writer
	// starvation; therefore, if any writer is waiting, the second RLock
	// inside the same goroutine deadlocks.
	//
	// To reproduce reliably, we spam GetRunnableJobs from many goroutines
	// while concurrently calling Lock-acquiring methods such as
	// SetJobStatus and IncrementRepeatCounter. With the bug present, the
	// scheduler hangs and the timeout below trips.
	scheduler := NewJobScheduler()
	const jobCount = 5
	for i := range jobCount {
		job := &Job{
			Name:  fmt.Sprintf("job-%d", i),
			ID:    fmt.Sprintf("id-%d", i),
			Steps: []*Step{},
		}
		if err := scheduler.AddJob(job); err != nil {
			t.Fatalf("AddJob: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		const iterations = 500
		for i := range iterations {
			wg.Add(3)
			go func() {
				defer wg.Done()
				_ = scheduler.GetRunnableJobs()
			}()
			go func(idx int) {
				defer wg.Done()
				scheduler.SetJobStatus(fmt.Sprintf("id-%d", idx%jobCount), JobRunning, false)
			}(i)
			go func(idx int) {
				defer wg.Done()
				scheduler.IncrementRepeatCounter(fmt.Sprintf("id-%d", idx%jobCount))
			}(i)
		}
		wg.Wait()
	}()

	select {
	case <-done:
		// reached without deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: scheduler operations did not finish within 5s (recursive RLock)")
	}
}

func TestJobScheduler_ValidateDependencies(t *testing.T) {
	t.Run("no circular dependency", func(t *testing.T) {
		scheduler := NewJobScheduler()

		// A -> B -> C (no cycle)
		jobA := &Job{ID: "A", Name: "A", Needs: []string{"B"}}
		jobB := &Job{ID: "B", Name: "B", Needs: []string{"C"}}
		jobC := &Job{ID: "C", Name: "C"}

		_ = scheduler.AddJob(jobA)
		_ = scheduler.AddJob(jobB)
		_ = scheduler.AddJob(jobC)

		err := scheduler.ValidateDependencies()
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("simple circular dependency A -> B -> A", func(t *testing.T) {
		scheduler := NewJobScheduler()

		jobA := &Job{ID: "A", Name: "A", Needs: []string{"B"}}
		jobB := &Job{ID: "B", Name: "B", Needs: []string{"A"}}

		_ = scheduler.AddJob(jobA)
		_ = scheduler.AddJob(jobB)

		err := scheduler.ValidateDependencies()
		if err == nil {
			t.Error("expected circular dependency error, got nil")
		}
		if !strings.Contains(err.Error(), "circular dependency") {
			t.Errorf("expected error message to contain 'circular dependency', got: %v", err)
		}
	})

	t.Run("three node circular dependency A -> B -> C -> A", func(t *testing.T) {
		scheduler := NewJobScheduler()

		jobA := &Job{ID: "A", Name: "A", Needs: []string{"B"}}
		jobB := &Job{ID: "B", Name: "B", Needs: []string{"C"}}
		jobC := &Job{ID: "C", Name: "C", Needs: []string{"A"}}

		_ = scheduler.AddJob(jobA)
		_ = scheduler.AddJob(jobB)
		_ = scheduler.AddJob(jobC)

		err := scheduler.ValidateDependencies()
		if err == nil {
			t.Error("expected circular dependency error, got nil")
		}
		if !strings.Contains(err.Error(), "circular dependency") {
			t.Errorf("expected error message to contain 'circular dependency', got: %v", err)
		}
	})

	t.Run("self reference A -> A", func(t *testing.T) {
		scheduler := NewJobScheduler()

		jobA := &Job{ID: "A", Name: "A", Needs: []string{"A"}}
		_ = scheduler.AddJob(jobA)

		err := scheduler.ValidateDependencies()
		if err == nil {
			t.Error("expected circular dependency error, got nil")
		}
		if !strings.Contains(err.Error(), "circular dependency") {
			t.Errorf("expected error message to contain 'circular dependency', got: %v", err)
		}
	})

	t.Run("non-existent dependency", func(t *testing.T) {
		scheduler := NewJobScheduler()

		jobA := &Job{ID: "A", Name: "A", Needs: []string{"nonexistent"}}
		_ = scheduler.AddJob(jobA)

		err := scheduler.ValidateDependencies()
		if err == nil {
			t.Error("expected non-existent dependency error, got nil")
		}
		if !strings.Contains(err.Error(), "non-existent") {
			t.Errorf("expected error message to contain 'non-existent', got: %v", err)
		}
	})

	t.Run("diamond dependency (no cycle)", func(t *testing.T) {
		scheduler := NewJobScheduler()

		//     A
		//    / \
		//   B   C
		//    \ /
		//     D
		jobA := &Job{ID: "A", Name: "A", Needs: []string{"B", "C"}}
		jobB := &Job{ID: "B", Name: "B", Needs: []string{"D"}}
		jobC := &Job{ID: "C", Name: "C", Needs: []string{"D"}}
		jobD := &Job{ID: "D", Name: "D"}

		_ = scheduler.AddJob(jobA)
		_ = scheduler.AddJob(jobB)
		_ = scheduler.AddJob(jobC)
		_ = scheduler.AddJob(jobD)

		err := scheduler.ValidateDependencies()
		if err != nil {
			t.Errorf("expected no error for diamond dependency, got: %v", err)
		}
	})
}

func TestJobScheduler_AlwaysJob(t *testing.T) {
	newScheduler := func(t *testing.T, jobs ...*Job) *JobScheduler {
		t.Helper()
		s := NewJobScheduler()
		for _, j := range jobs {
			if err := s.AddJob(j); err != nil {
				t.Fatal(err)
			}
		}
		return s
	}

	t.Run("waits for what it needs to be over", func(t *testing.T) {
		s := newScheduler(t,
			&Job{ID: "work"},
			&Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		if s.CanRunJob("cleanup") {
			t.Error("cleanup can run before work has started")
		}
		s.SetJobStatus("work", JobRunning, false)
		if s.CanRunJob("cleanup") {
			t.Error("cleanup can run while work is running")
		}
	})

	t.Run("runs after a job that succeeded", func(t *testing.T) {
		s := newScheduler(t,
			&Job{ID: "work"},
			&Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.IncrementRepeatCounter("work")
		s.SetJobStatus("work", JobCompleted, true)
		if !s.CanRunJob("cleanup") {
			t.Error("cleanup cannot run after work succeeded")
		}
	})

	t.Run("runs after a job that failed, where another job is skipped", func(t *testing.T) {
		s := newScheduler(t,
			&Job{ID: "work"},
			&Job{ID: "report", Needs: []string{"work"}},
			&Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.IncrementRepeatCounter("work")
		s.SetJobStatus("work", JobCompleted, false)

		if !s.CanRunJob("cleanup") {
			t.Error("cleanup cannot run after work failed")
		}
		if s.CanRunJob("report") {
			t.Error("report can run after work failed")
		}
		if skipped := s.MarkJobsWithFailedDependencies(); len(skipped) != 1 || skipped[0] != "report" {
			t.Errorf("skipped = %v, want report alone", skipped)
		}
		if !s.CanRunJob("cleanup") {
			t.Error("cleanup cannot run once the others are skipped")
		}
	})

	t.Run("runs after a job that was skipped for what it needs", func(t *testing.T) {
		s := newScheduler(t,
			&Job{ID: "setup"},
			&Job{ID: "work", Needs: []string{"setup"}},
			&Job{ID: "cleanup", Needs: []string{"setup", "work"}, Always: true},
			&Job{ID: "after", Needs: []string{"cleanup"}},
		)
		s.IncrementRepeatCounter("setup")
		s.SetJobStatus("setup", JobCompleted, false)
		if s.CanRunJob("cleanup") {
			t.Error("cleanup can run while work is still pending")
		}
		if skipped := s.MarkJobsWithFailedDependencies(); len(skipped) != 1 || skipped[0] != "work" {
			t.Errorf("skipped = %v, want work alone: cleanup always runs, and after waits for it", skipped)
		}
		if !s.CanRunJob("cleanup") {
			t.Error("cleanup cannot run once work is skipped")
		}

		// A job that needs the one that always runs is held to how that
		// one ends, as it is to any other.
		s.IncrementRepeatCounter("cleanup")
		s.SetJobStatus("cleanup", JobCompleted, true)
		if !s.CanRunJob("after") {
			t.Error("after cannot run although cleanup succeeded")
		}
	})

	t.Run("waits for every run of a repeated job", func(t *testing.T) {
		s := newScheduler(t,
			&Job{ID: "work", Repeat: &Repeat{Count: 2}},
			&Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.IncrementRepeatCounter("work")
		s.SetJobStatus("work", JobCompleted, false)
		if s.CanRunJob("cleanup") {
			t.Error("cleanup can run before the second run of work")
		}
		s.IncrementRepeatCounter("work")
		if !s.CanRunJob("cleanup") {
			t.Error("cleanup cannot run after both runs of work")
		}
	})
}
