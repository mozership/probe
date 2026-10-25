package probe

import (
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/report"
)

// The runs of a job are counted only when it is repeated, warmed up or
// tested over its runs, and then the warmup runs are left out of the total.
// What the runs share is pointed to by the context each is given, and not
// copied with it.
func TestExecutor_setupContext(t *testing.T) {
	tests := []struct {
		name       string
		repeat     *Repeat
		wantCount  bool
		wantTotal  int
		wantWarmup int
	}{
		{name: "run once", repeat: nil},
		{name: "a count of one", repeat: &Repeat{Count: 1}},
		{name: "repeated", repeat: &Repeat{Count: 3}, wantCount: true, wantTotal: 3},
		{name: "warmed up", repeat: &Repeat{Count: 1, Warmup: 2}, wantCount: true, wantTotal: 1, wantWarmup: 2},
		{name: "tested over a run", repeat: &Repeat{Count: 1, Test: "true"}, wantCount: true, wantTotal: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "job", ID: "job", Repeat: tt.repeat}
			scheduler := NewJobScheduler()
			if err := scheduler.AddJob(job); err != nil {
				t.Fatal(err)
			}
			env := &runEnv{Printer: newBufferPrinter(), JobScheduler: scheduler}
			ctx := NewExecutor(job).setupContext(JobContext{runEnv: env})

			if ctx.runEnv != env {
				t.Error("the job should run with the workflow's environment, not a copy of it")
			}
			if got := ctx.repeating(); got != tt.wantCount {
				t.Fatalf("repeating() = %v, want %v", got, tt.wantCount)
			}
			if !tt.wantCount {
				return
			}
			if ctx.repeat.total != tt.wantTotal || ctx.repeat.warmup != tt.wantWarmup {
				t.Errorf("total, warmup = %d, %d, want %d, %d", ctx.repeat.total, ctx.repeat.warmup, tt.wantTotal, tt.wantWarmup)
			}
			run := ctx
			run.RepeatCurrent = 1
			run.SetFailed()
			if run.repeat != ctx.repeat {
				t.Error("a run should count in the job's counters, not a copy of them")
			}
			if ctx.Failed || ctx.RepeatCurrent != 0 {
				t.Error("a run should not change the context it was copied from")
			}
		})
	}
}

// runJob runs job as the workflow runs each of its jobs, its actions run by
// runner, and returns what Executor.Run reported and the result it left for
// the job.
func runJob(t *testing.T, job Job, runner ActionRunner) (bool, *JobResult) {
	t.Helper()
	w := &Workflow{Name: "executor", Jobs: []Job{job}, printer: newBufferPrinter()}
	scheduler, err := w.initJobScheduler()
	if err != nil {
		t.Fatal(err)
	}
	ctx := w.newJobContext(Config{ActionRunner: runner}, nil, scheduler)
	ctx.Outputs = NewOutputs()
	scheduler.StartJob(job.ID)
	passed := NewExecutor(&w.Jobs[0]).Run(ctx)
	return passed, ctx.Result.Jobs[job.ID]
}

// A job that fails reports so to its caller, which fails the workflow for
// it, in turn and with its runs overlapping alike. A failure that is only
// counted, in a warmup run or under a test over the runs, fails nothing.
func TestExecutor_Run(t *testing.T) {
	failing := newMockRunner()
	failing.SetError("hello", errors.New("boom"))
	passing := newMockRunner()
	passing.SetResult("hello", map[string]any{"status": 0})

	tests := []struct {
		name       string
		runner     ActionRunner
		repeat     *Repeat
		test       string
		wantPassed bool
	}{
		{name: "run once and passed", runner: passing, wantPassed: true},
		{name: "run once and failed", runner: failing},
		{name: "repeated and passed", runner: passing, repeat: &Repeat{Count: 3}, wantPassed: true},
		{name: "repeated and failed", runner: failing, repeat: &Repeat{Count: 3}},
		{name: "async and passed", runner: passing, repeat: &Repeat{Count: 3, Async: true}, wantPassed: true},
		{name: "async and failed", runner: failing, repeat: &Repeat{Count: 3, Async: true}},
		{
			name:       "failed only in the warmup run",
			runner:     &echoRunner{},
			repeat:     &Repeat{Count: 2, Warmup: 1},
			test:       `res.i != "0"`,
			wantPassed: true,
		},
		{name: "failed in every run warmed up", runner: &echoRunner{}, repeat: &Repeat{Count: 2, Warmup: 1}, test: "false"},
		{name: "failed under a test over the runs that held", runner: failing, repeat: &Repeat{Count: 2, Test: "true"}, wantPassed: true},
		{name: "passed under a test over the runs that did not hold", runner: passing, repeat: &Repeat{Count: 2, Test: "false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.repeat != nil {
				tt.repeat.Interval = Interval{Duration: time.Millisecond}
			}
			job := Job{
				Name:   "job",
				ID:     "job",
				Repeat: tt.repeat,
				Steps: []*Step{{
					Name: "step", Uses: "hello",
					With: map[string]any{"i": "{{repeat_index}}"},
					Test: tt.test,
				}},
			}
			passed, jr := runJob(t, job, tt.runner)
			if passed != tt.wantPassed {
				t.Errorf("Run() = %v, want %v", passed, tt.wantPassed)
			}
			if wantError := !tt.wantPassed; (jr.Status == StatusError) != wantError {
				t.Errorf("job status = %v, want an error %v", jr.Status, wantError)
			}
			if jr.EndTime.Before(jr.StartTime) {
				t.Errorf("end time %v is before the start time %v", jr.EndTime, jr.StartTime)
			}
		})
	}
}

// overlapRunner holds every call to an action until a second call is in
// one at the same time, or until wait is over, and records whether two
// calls ever were.
type overlapRunner struct {
	wait time.Duration

	mu         sync.Mutex
	running    int
	overlapped bool
	both       chan struct{}
}

func newOverlapRunner(wait time.Duration) *overlapRunner {
	return &overlapRunner{wait: wait, both: make(chan struct{})}
}

func (r *overlapRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.mu.Lock()
	r.running++
	if r.running > 1 && !r.overlapped {
		r.overlapped = true
		close(r.both)
	}
	r.mu.Unlock()

	select {
	case <-r.both:
	case <-time.After(r.wait):
	}

	r.mu.Lock()
	r.running--
	r.mu.Unlock()
	return map[string]any{"status": 0}, nil, nil
}

// The runs of an async job start an interval apart, without waiting for the
// run before to end, while those of any other job run one after another.
// An async run waits in its action for the next run to be in it as well,
// for as long as a stalled machine could take; a run in turn waits a short
// while, which no other run is in its action during.
func TestExecutor_Run_AsyncOverlaps(t *testing.T) {
	for _, async := range []bool{false, true} {
		name, wait := "in turn", 20*time.Millisecond
		if async {
			name, wait = "async", 30*time.Second
		}
		t.Run(name, func(t *testing.T) {
			runner := newOverlapRunner(wait)
			job := Job{
				Name:   "job",
				ID:     "job",
				Repeat: &Repeat{Count: 3, Interval: Interval{Duration: time.Millisecond}, Async: async},
				Steps:  []*Step{{Name: "pause", Uses: "hello"}},
			}
			if passed, _ := runJob(t, job, runner); !passed {
				t.Fatal("Run() = false, want true")
			}
			runner.mu.Lock()
			overlapped := runner.overlapped
			runner.mu.Unlock()
			if overlapped != async {
				t.Errorf("runs overlapped = %v, want %v", overlapped, async)
			}
		})
	}
}

// A repeated step's result is made from its counter over the runs: whether
// it passed by which runs it passed, failed or was skipped in, and its
// failure by the first run's with how many runs failed.
func TestRepeatStepResult(t *testing.T) {
	first := &StepFailure{Kind: FailureAssertion, Message: "test evaluated to false"}
	tests := []struct {
		name        string
		counter     StepRepeatCounter
		readByTest  bool
		wantStatus  StatusType
		wantHasTest bool
		wantMessage string
	}{
		{name: "passed in every run", counter: StepRepeatCounter{SuccessCount: 3, Test: "true"}, wantStatus: StatusSuccess, wantHasTest: true},
		{name: "passed in the runs it ran in", counter: StepRepeatCounter{SuccessCount: 1, SkippedCount: 2}, wantStatus: StatusSuccess},
		{name: "ran in no run", counter: StepRepeatCounter{SkippedCount: 3}, wantStatus: StatusSkipped},
		{name: "counted in no run", counter: StepRepeatCounter{}, wantStatus: StatusSuccess},
		{
			name:        "failed in every run",
			counter:     StepRepeatCounter{FailureCount: 3, Failure: first},
			wantStatus:  StatusError,
			wantMessage: "3 of 3 iterations failed; the first: test evaluated to false",
		},
		{
			name:        "failed in the runs it ran in",
			counter:     StepRepeatCounter{FailureCount: 1, SkippedCount: 2, Failure: first},
			wantStatus:  StatusError,
			wantMessage: "1 of 3 iterations failed; the first: test evaluated to false",
		},
		{
			name:        "failed in some runs",
			counter:     StepRepeatCounter{SuccessCount: 2, FailureCount: 1, SkippedCount: 1, Failure: first},
			wantStatus:  StatusWarning,
			wantMessage: "1 of 4 iterations failed; the first: test evaluated to false",
		},
		{name: "checked by a contract", counter: StepRepeatCounter{SuccessCount: 1, Checked: true}, wantStatus: StatusSuccess, wantHasTest: true},
		{name: "read by the test over the runs", counter: StepRepeatCounter{SuccessCount: 1}, readByTest: true, wantStatus: StatusSuccess, wantHasTest: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.counter.ID, tt.counter.Name = "s", "Step"
			got := repeatStepResult(2, tt.counter, tt.readByTest)

			if got.Index != 2 || got.ID != "s" || got.Name != "Step" {
				t.Errorf("index, id, name = %d, %q, %q, want 2, %q, %q", got.Index, got.ID, got.Name, "s", "Step")
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", got.Status, tt.wantStatus)
			}
			if got.HasTest != tt.wantHasTest {
				t.Errorf("HasTest = %v, want %v", got.HasTest, tt.wantHasTest)
			}
			if got.RepeatCounter == nil || got.RepeatCounter.SuccessCount != tt.counter.SuccessCount || got.RepeatCounter.FailureCount != tt.counter.FailureCount {
				t.Errorf("RepeatCounter = %+v, want the counter %+v", got.RepeatCounter, tt.counter)
			}
			if tt.wantMessage == "" {
				if got.Failure != nil {
					t.Errorf("Failure = %+v, want none", got.Failure)
				}
				return
			}
			if got.Failure == nil || got.Failure.Message != tt.wantMessage || got.Failure.Kind != first.Kind {
				t.Fatalf("Failure = %+v, want %q of kind %q", got.Failure, tt.wantMessage, first.Kind)
			}
			if first.Message != "test evaluated to false" {
				t.Errorf("the counter's failure became %q, want it left as it was", first.Message)
			}
		})
	}
}

// The counters are read in the order of their keys, which is the order the
// steps ran in, with what the runs share filled in. A counter given its
// total by the step keeps it. The snapshot's fields are its own, and what
// they point to is the stored counter's.
func TestJobRepeat_snapshotCounters(t *testing.T) {
	failure := &StepFailure{Kind: FailureAssertion, Message: "test evaluated to false"}
	contract := &report.Contract{Spec: "openapi.yml"}
	rep := newJobRepeat(5, 2)
	rep.tested = true
	rep.counters[7] = StepRepeatCounter{Name: "third"}
	rep.counters[0] = StepRepeatCounter{
		Name: "first", RepeatTotal: 4,
		Failure: failure, Contract: contract,
		RTs: []float64{0.1}, EchoOutputs: []string{"echo"},
	}
	rep.counters[3] = StepRepeatCounter{Name: "second"}

	got := rep.snapshotCounters()
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
		if c.Warmup != 2 || !c.Tested {
			t.Errorf("%s: warmup, tested = %d, %v, want 2, true", c.Name, c.Warmup, c.Tested)
		}
	}
	if strings.Join(names, ",") != "first,second,third" {
		t.Fatalf("order = %v, want first, second, third", names)
	}
	if got[0].RepeatTotal != 4 || got[1].RepeatTotal != 3 || got[2].RepeatTotal != 3 {
		t.Errorf("totals = %d, %d, %d, want 4, 3, 3", got[0].RepeatTotal, got[1].RepeatTotal, got[2].RepeatTotal)
	}
	got[0].Name = "changed"
	if rep.counters[0].Name != "first" {
		t.Error("setting a field of the snapshot changed the stored counter")
	}
	if got[0].Failure != failure || got[0].Contract != contract {
		t.Error("the snapshot should point to the stored counter's failure and contract")
	}
	got[0].RTs[0] = 9
	got[0].EchoOutputs[0] = "changed"
	if rep.counters[0].RTs[0] != 9 || rep.counters[0].EchoOutputs[0] != "changed" {
		t.Error("the snapshot should share the stored counter's RTs and echo outputs")
	}
}

func TestExecutor_AppendRepeatStepResults(t *testing.T) {
	job := &Job{
		Name: "test-job",
		ID:   "test-job",
		Steps: []*Step{
			{
				Name: "test-step",
				Test: "status == 200",
			},
		},
	}
	executor := NewExecutor(job)

	// Create WorkflowBuffer with JobResult
	result := NewResult()
	jobResult := &JobResult{
		JobName: job.Name,
		JobID:   "test-job",
	}
	result.Jobs["test-job"] = jobResult

	// Create test context with step counters
	ctx := JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: NewPrinter(false),
			Result:  result,
		},
		repeat: &jobRepeat{
			counters: map[int]StepRepeatCounter{
				0: {
					SuccessCount: 3,
					FailureCount: 1,
					Name:         "test-step",
					LastResult:   true,
				},
			},
		},
	}

	// Call the method
	executor.appendRepeatStepResults(&ctx)

	// Check if step results were added to WorkflowBuffer
	jobResult, exists := result.Jobs["test-job"]
	if !exists {
		t.Fatal("Job buffer should exist after appendRepeatStepResults")
	}
	if len(jobResult.StepResults) == 0 {
		t.Error("appendRepeatStepResults should add StepResults to WorkflowBuffer")
	}

	// Should have created a StepResult with RepeatCounter
	if len(jobResult.StepResults) != 1 {
		t.Errorf("Expected 1 step result, got %d", len(jobResult.StepResults))
	}

	stepResult := jobResult.StepResults[0]
	if stepResult.RepeatCounter == nil {
		t.Error("StepResult should have RepeatCounter")
	}

	if stepResult.RepeatCounter.SuccessCount != 3 {
		t.Errorf("Expected SuccessCount=3, got %d", stepResult.RepeatCounter.SuccessCount)
	}

	if stepResult.RepeatCounter.FailureCount != 1 {
		t.Errorf("Expected FailureCount=1, got %d", stepResult.RepeatCounter.FailureCount)
	}
}

// TestExecutor_AsyncRepeat_HandleSkipNoDataRace covers the handleSkip
// path: when an async-repeat job evaluates skipif==true on every iteration,
// each goroutine reaches Job.handleSkip and writes JobResult.Status /
// JobResult.Success on the shared *JobResult. Without locking those
// writes, `go test -race` flags a data race between sibling iterations.
func TestExecutor_AsyncRepeat_HandleSkipNoDataRace(t *testing.T) {
	workflow := &Workflow{
		Name: "async-skip-race-test",
		Jobs: []Job{
			{
				Name:   "always-skip",
				ID:     "always-skip",
				SkipIf: "true",
				Steps:  []*Step{},
				Repeat: &Repeat{
					Count:    20,
					Interval: Interval{Duration: 1 * time.Millisecond},
					Async:    true,
				},
			},
		},
		printer: newBufferPrinter(),
	}

	config := Config{Verbose: false}
	if err := workflow.Start(config); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
}

// TestExecutor_AsyncRepeat_NoDataRace exercises the executeJobRepeatLoopAsync
// path end-to-end with a mocked ActionRunner so that, under `go test -race`,
// any concurrent mutation of shared *Step / *Job state is reported as a race.
//
// Before the fix, every goroutine spawned by the async repeat loop called
// e.job.run on the same *Job, which in turn mutated j.Name (expandJobName)
// and st.Expr / st.ctx / st.startedAt / st.err / st.retryAttempt on the same
// *Step instances.
func TestExecutor_AsyncRepeat_NoDataRace(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("hello", map[string]any{"status": 0})

	// The run is given the mock, so that st.executeAction takes the mock
	// path instead of spawning the real plugin process.
	step := &Step{
		Name: "tick",
		Uses: "hello",
	}

	workflow := &Workflow{
		Name: "async-race-test",
		Jobs: []Job{
			{
				Name:  "racer",
				ID:    "racer",
				Steps: []*Step{step},
				Repeat: &Repeat{
					Count:    20,
					Interval: Interval{Duration: 1 * time.Millisecond},
					Async:    true,
				},
			},
		},
		printer: newBufferPrinter(),
	}

	config := Config{Verbose: false, ActionRunner: runner}
	if err := workflow.Start(config); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
}

// echoRunner answers every action with its own parameters as res, as hello
// does, after sleeping for with.sleep when that is set. It records the
// values of with.got, so that a test can see what each run read.
type echoRunner struct {
	mu  sync.Mutex
	got []string
}

func (r *echoRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	with := call.With
	if s, ok := with["sleep"].(string); ok {
		if d, err := time.ParseDuration(s); err == nil {
			time.Sleep(d)
		}
	}
	if got, ok := with["got"].(string); ok {
		r.mu.Lock()
		r.got = append(r.got, got)
		r.mu.Unlock()
	}
	res := make(map[string]any, len(with))
	maps.Copy(res, with)
	return map[string]any{"req": with, "res": res, "status": 0}, nil, nil
}

// repeatOutputsWorkflow repeats a job that publishes a value naming its run,
// waits, and reads it back, followed by a job that reads it after. One step
// is skipped by the last run.
func repeatOutputsWorkflow(async bool) *Workflow {
	repeated := Job{
		Name: "repeated",
		ID:   "repeated",
		Repeat: &Repeat{
			Count:    4,
			Interval: Interval{Duration: 10 * time.Millisecond},
			Async:    async,
		},
		Steps: []*Step{
			{
				Name: "publish", ID: "pub", Uses: "hello",
				With:    map[string]any{"v": "run-{{repeat_index}}"},
				Outputs: map[string]string{"v": "res.v"},
			},
			{
				// The last run skips this one, so what the jobs after read is
				// what the run before left.
				Name: "early", ID: "early", Uses: "hello",
				SkipIf:  "repeat_index == 3",
				With:    map[string]any{"e": "early-{{repeat_index}}"},
				Outputs: map[string]string{"e": "res.e"},
			},
			{
				// Long enough for the other runs to publish meanwhile.
				Name: "pause", ID: "pause", Uses: "hello",
				With: map[string]any{"sleep": "100ms"},
			},
			{
				Name: "read own", ID: "own", Uses: "hello",
				With: map[string]any{"got": "{{outputs.pub.v}}", "want": "run-{{repeat_index}}"},
				Test: "res.got == res.want",
			},
		},
	}
	after := Job{
		Name:  "after",
		ID:    "after",
		Needs: []string{"repeated"},
		Steps: []*Step{
			{
				Name: "read last", ID: "last", Uses: "hello",
				With: map[string]any{"got": "{{outputs.pub.v}}", "early": "{{outputs.early.e}}"},
				Test: `res.got == "run-3" && res.early == "early-2"`,
			},
		},
	}
	return &Workflow{
		Name:    "repeat outputs",
		Jobs:    []Job{repeated, after},
		printer: newBufferPrinter(),
	}
}

func TestRepeatedJobOutputs(t *testing.T) {
	// A repeated job used to save no outputs at all, so its later steps
	// could not read its earlier ones, nor could the jobs after it. Each run
	// reads its own, also when the runs overlap, and the jobs after read
	// those of the last run.
	for _, async := range []bool{false, true} {
		name := "in turn"
		if async {
			name = "async"
		}
		t.Run(name, func(t *testing.T) {
			runner := &echoRunner{}
			w := repeatOutputsWorkflow(async)
			if err := w.Start(Config{ActionRunner: runner}); err != nil {
				t.Fatalf("Start() error: %v", err)
			}
			if w.exitStatus != 0 {
				runner.mu.Lock()
				got := strings.Join(runner.got, ", ")
				runner.mu.Unlock()
				t.Errorf("exit status = %d, want 0; values read: %s", w.exitStatus, got)
			}
		})
	}
}
