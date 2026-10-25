package schedule

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// newScheduler returns a scheduler of jobs, added in their order.
func newScheduler(t *testing.T, jobs ...Job) *Scheduler {
	t.Helper()
	s := New()
	for _, j := range jobs {
		if err := s.Add(j); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// canStart reports whether the scheduler would start id now.
func canStart(s *Scheduler, id string) bool {
	return slices.Contains(s.Runnable(), id)
}

func TestScheduler_Add(t *testing.T) {
	t.Run("a job without an ID", func(t *testing.T) {
		if err := New().Add(Job{}); err == nil {
			t.Error("Add() of a job without an ID succeeded")
		}
	})

	t.Run("a job with the ID of another", func(t *testing.T) {
		s := newScheduler(t, Job{ID: "a"})
		err := s.Add(Job{ID: "a"})
		if err == nil || err.Error() != "duplicate job ID: a" {
			t.Errorf("Add() error = %v, want the duplicate ID", err)
		}
	})

	t.Run("the needs are the scheduler's own", func(t *testing.T) {
		needs := []string{"a"}
		s := newScheduler(t, Job{ID: "a"}, Job{ID: "b", Needs: needs})
		needs[0] = "b"
		s.Finish("a", true)
		if !canStart(s, "b") {
			t.Error("b cannot start after a, as a change to the slice it was added with changed its needs")
		}
	})
}

func TestScheduler_SkipBlocked(t *testing.T) {
	t.Run("a failure skips the chain of jobs that need it", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "level1"},
			Job{ID: "level2", Needs: []string{"level1"}},
			Job{ID: "level3", Needs: []string{"level2"}},
		)
		s.Finish("level1", false)

		if got, want := s.SkipBlocked(), []string{"level2", "level3"}; !slices.Equal(got, want) {
			t.Errorf("SkipBlocked() = %v, want %v", got, want)
		}
		for _, id := range []string{"level2", "level3"} {
			if s.status[id] != skipped {
				t.Errorf("%s is %v, want skipped", id, s.status[id])
			}
		}
		if got := s.SkipBlocked(); len(got) != 0 {
			t.Errorf("SkipBlocked() again = %v, want none", got)
		}
		if got := s.Pending(); len(got) != 0 {
			t.Errorf("Pending() = %v, want none", got)
		}
	})

	t.Run("a chain written in reverse is skipped all the same", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "c", Needs: []string{"b"}},
			Job{ID: "b", Needs: []string{"a"}},
			Job{ID: "a"},
		)
		s.Finish("a", false)
		if got, want := s.SkipBlocked(), []string{"b", "c"}; !slices.Equal(got, want) {
			t.Errorf("SkipBlocked() = %v, want %v", got, want)
		}
	})

	t.Run("a job that does not need the failed one is not skipped", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "job1"},
			Job{ID: "job2", Needs: []string{"job1"}},
			Job{ID: "job3"},
		)
		s.Finish("job1", false)

		if got := s.SkipBlocked(); !slices.Equal(got, []string{"job2"}) {
			t.Errorf("SkipBlocked() = %v, want job2 alone", got)
		}
		if s.status["job3"] != pending {
			t.Errorf("job3 is %v, want pending", s.status["job3"])
		}
		if !canStart(s, "job3") {
			t.Error("job3 cannot start")
		}
	})

	t.Run("a job is not skipped for one that is still running or succeeded", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "a"},
			Job{ID: "b"},
			Job{ID: "c", Needs: []string{"a", "b"}},
		)
		s.Start("a")
		s.Finish("b", true)
		if got := s.SkipBlocked(); len(got) != 0 {
			t.Errorf("SkipBlocked() = %v, want none", got)
		}
	})

	t.Run("a job that is no longer pending is left alone", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "a"},
			Job{ID: "b", Needs: []string{"a"}},
		)
		s.Finish("a", false)
		s.Start("b")
		if got := s.SkipBlocked(); len(got) != 0 {
			t.Errorf("SkipBlocked() = %v, want none", got)
		}
	})
}

func TestScheduler_Runnable(t *testing.T) {
	t.Run("jobs with no needs start in the order they were added", func(t *testing.T) {
		s := newScheduler(t, Job{ID: "c"}, Job{ID: "a"}, Job{ID: "b"})
		if got, want := s.Runnable(), []string{"c", "a", "b"}; !slices.Equal(got, want) {
			t.Errorf("Runnable() = %v, want %v", got, want)
		}
	})

	t.Run("a job waits for each of its needs to succeed", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "a"},
			Job{ID: "b"},
			Job{ID: "c", Needs: []string{"a", "b"}},
		)
		s.Start("a")
		s.Start("b")
		if got := s.Runnable(); len(got) != 0 {
			t.Errorf("Runnable() = %v while a and b run, want none", got)
		}
		s.Finish("a", true)
		if canStart(s, "c") {
			t.Error("c can start while b runs")
		}
		s.Finish("b", true)
		if got := s.Runnable(); !slices.Equal(got, []string{"c"}) {
			t.Errorf("Runnable() = %v, want c", got)
		}
		s.Start("c")
		if got := s.Runnable(); len(got) != 0 {
			t.Errorf("Runnable() = %v once c started, want none", got)
		}
	})

	t.Run("a job does not start after a need that failed", func(t *testing.T) {
		s := newScheduler(t, Job{ID: "a"}, Job{ID: "b", Needs: []string{"a"}})
		s.Finish("a", false)
		if canStart(s, "b") {
			t.Error("b can start after a failed")
		}
	})
}

func TestScheduler_Pending(t *testing.T) {
	s := newScheduler(t, Job{ID: "c"}, Job{ID: "a"}, Job{ID: "b"}, Job{ID: "d"})
	s.Start("b")
	s.Finish("d", true)
	if got, want := s.Pending(), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("Pending() = %v, want %v", got, want)
	}
}

func TestScheduler_NoDeadlockOnConcurrentReadAndWrite(t *testing.T) {
	// Regression test for the recursive RLock deadlock.
	//
	// Runnable held RLock, then called a method which tried to acquire
	// RLock again. Per Go's sync.RWMutex contract, a blocked Lock call
	// excludes new readers from acquiring the lock to avoid writer
	// starvation; therefore, if any writer is waiting, the second RLock
	// inside the same goroutine deadlocks.
	//
	// To reproduce reliably, we spam Runnable from many goroutines while
	// concurrently calling Lock-acquiring methods such as Start and
	// Finish. With the bug present, the scheduler hangs and the timeout
	// below trips.
	s := New()
	const jobCount = 5
	for i := range jobCount {
		job := Job{ID: fmt.Sprintf("id-%d", i)}
		if i > 0 {
			job.Needs = []string{fmt.Sprintf("id-%d", i-1)}
		}
		if err := s.Add(job); err != nil {
			t.Fatalf("Add: %v", err)
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
				_ = s.Runnable()
			}()
			go func(idx int) {
				defer wg.Done()
				s.Start(fmt.Sprintf("id-%d", idx%jobCount))
			}(i)
			go func(idx int) {
				defer wg.Done()
				s.Finish(fmt.Sprintf("id-%d", idx%jobCount), idx%2 == 0)
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

func TestScheduler_Validate(t *testing.T) {
	tests := []struct {
		name    string
		jobs    []Job
		wantErr string
	}{
		{
			name: "no circular dependency",
			jobs: []Job{{ID: "A", Needs: []string{"B"}}, {ID: "B", Needs: []string{"C"}}, {ID: "C"}},
		},
		{
			name:    "simple circular dependency A -> B -> A",
			jobs:    []Job{{ID: "A", Needs: []string{"B"}}, {ID: "B", Needs: []string{"A"}}},
			wantErr: "circular dependency detected: A -> B",
		},
		{
			name:    "three node circular dependency A -> B -> C -> A",
			jobs:    []Job{{ID: "A", Needs: []string{"B"}}, {ID: "B", Needs: []string{"C"}}, {ID: "C", Needs: []string{"A"}}},
			wantErr: "circular dependency detected: A -> B -> C",
		},
		{
			name:    "self reference A -> A",
			jobs:    []Job{{ID: "A", Needs: []string{"A"}}},
			wantErr: "circular dependency detected: A",
		},
		{
			name:    "non-existent dependency",
			jobs:    []Job{{ID: "A", Needs: []string{"nonexistent"}}},
			wantErr: "job 'A' depends on non-existent job 'nonexistent'",
		},
		{
			name: "the first non-existent dependency in the order the jobs were added",
			jobs: []Job{
				{ID: "z", Needs: []string{"missing1"}},
				{ID: "a", Needs: []string{"missing2"}},
			},
			wantErr: "job 'z' depends on non-existent job 'missing1'",
		},
		{
			name: "diamond dependency (no cycle)",
			//     A
			//    / \
			//   B   C
			//    \ /
			//     D
			jobs: []Job{
				{ID: "A", Needs: []string{"B", "C"}},
				{ID: "B", Needs: []string{"D"}},
				{ID: "C", Needs: []string{"D"}},
				{ID: "D"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newScheduler(t, tt.jobs...).Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want none", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// A cycle is told from its first job in the order the jobs were added, not
// in the order of the IDs, and a need that is no job is not refused.
func TestScheduler_CheckCycle(t *testing.T) {
	s := newScheduler(t,
		Job{ID: "zeta", Needs: []string{"alpha"}},
		Job{ID: "alpha", Needs: []string{"zeta"}},
	)
	const want = "circular dependency detected: zeta -> alpha"
	if err := s.CheckCycle(); err == nil || err.Error() != want {
		t.Errorf("CheckCycle() error = %v, want %q", err, want)
	}

	s = newScheduler(t, Job{ID: "a", Needs: []string{"missing"}})
	if err := s.CheckCycle(); err != nil {
		t.Errorf("CheckCycle() of a need that is no job = %v, want none", err)
	}
}

func TestScheduler_AlwaysJob(t *testing.T) {
	t.Run("waits for what it needs to be over", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		if canStart(s, "cleanup") {
			t.Error("cleanup can run before work has started")
		}
		s.Start("work")
		if canStart(s, "cleanup") {
			t.Error("cleanup can run while work is running")
		}
	})

	t.Run("runs after a job that succeeded", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.Finish("work", true)
		if !canStart(s, "cleanup") {
			t.Error("cleanup cannot run after work succeeded")
		}
	})

	t.Run("runs after a job that failed, where another job is skipped", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "report", Needs: []string{"work"}},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.Finish("work", false)

		if !canStart(s, "cleanup") {
			t.Error("cleanup cannot run after work failed")
		}
		if canStart(s, "report") {
			t.Error("report can run after work failed")
		}
		if skipped := s.SkipBlocked(); !slices.Equal(skipped, []string{"report"}) {
			t.Errorf("skipped = %v, want report alone", skipped)
		}
		if !canStart(s, "cleanup") {
			t.Error("cleanup cannot run once the others are skipped")
		}
	})

	t.Run("runs after a job that was skipped for what it needs", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "setup"},
			Job{ID: "work", Needs: []string{"setup"}},
			Job{ID: "cleanup", Needs: []string{"setup", "work"}, Always: true},
			Job{ID: "after", Needs: []string{"cleanup"}},
		)
		s.Finish("setup", false)
		if canStart(s, "cleanup") {
			t.Error("cleanup can run while work is still pending")
		}
		if skipped := s.SkipBlocked(); !slices.Equal(skipped, []string{"work"}) {
			t.Errorf("skipped = %v, want work alone: cleanup always runs, and after waits for it", skipped)
		}
		if !canStart(s, "cleanup") {
			t.Error("cleanup cannot run once work is skipped")
		}

		// A job that needs the one that always runs is held to how that
		// one ends, as it is to any other.
		s.Finish("cleanup", true)
		if !canStart(s, "after") {
			t.Error("after cannot run although cleanup succeeded")
		}
	})

	t.Run("a job that needs it is skipped when it fails", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
			Job{ID: "after", Needs: []string{"cleanup"}},
		)
		s.Finish("work", true)
		s.Finish("cleanup", false)
		if skipped := s.SkipBlocked(); !slices.Equal(skipped, []string{"after"}) {
			t.Errorf("skipped = %v, want after", skipped)
		}
	})

	t.Run("a job that always runs and needs one that always runs waits for it", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
			Job{ID: "notify", Needs: []string{"cleanup"}, Always: true},
		)
		s.Finish("work", false)
		if canStart(s, "notify") {
			t.Error("notify can run before cleanup")
		}
		s.Start("cleanup")
		if canStart(s, "notify") {
			t.Error("notify can run while cleanup runs")
		}
		s.Finish("cleanup", false)
		if !canStart(s, "notify") {
			t.Error("notify cannot run after cleanup failed")
		}
	})

	t.Run("is never skipped", func(t *testing.T) {
		s := newScheduler(t,
			Job{ID: "work"},
			Job{ID: "cleanup", Needs: []string{"work"}, Always: true},
		)
		s.Finish("work", false)
		if skipped := s.SkipBlocked(); len(skipped) != 0 {
			t.Errorf("skipped = %v, want none", skipped)
		}
		if !canStart(s, "cleanup") {
			t.Error("cleanup cannot run")
		}
	})
}
