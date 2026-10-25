package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/report"
)

// runStepForTemplateError runs st in a job context of its own and returns
// how many times the action ran, the step's result and whether the job failed.
func runStepForTemplateError(t *testing.T, step *Step, vars map[string]any) (int, StepResult, bool) {
	t.Helper()

	calls := 0
	runner := &callCountingRunner{
		result:    map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}},
		callCount: &calls,
	}
	st := newStepRun(step, nil)

	rs := NewResult()
	rs.Jobs["j"] = &JobResult{JobName: "j", JobID: "j"}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{ActionRunner: runner},
			Printer: newBufferPrinter(),
			Result:  rs,
		},
		Vars:         vars,
		CurrentJobID: "j",
		Outputs:      NewOutputs(),
	}

	st.bindContext(*jCtx, nil)
	st.run(jCtx)

	results := rs.Jobs["j"].StepResults
	if len(results) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(results))
	}
	return calls, results[0], jCtx.Failed
}

func TestStep_TemplateErrorFailsStep(t *testing.T) {
	tests := []struct {
		name    string
		step    *Step
		vars    map[string]any
		message []string
	}{
		{
			name: "a value in with",
			step: &Step{
				Name: "login",
				Uses: "http",
				With: map[string]any{
					"url": "http://localhost",
					"headers": map[string]any{
						"x-request-id": "req-{{outputs.login.id}}",
					},
				},
			},
			message: []string{"with.headers.x-request-id: {{outputs.login.id}}: "},
		},
		{
			name: "a value in with that is a single template",
			step: &Step{
				Name: "port",
				Uses: "http",
				With: map[string]any{"items": []any{"{{vars.base.port}}"}},
			},
			vars:    map[string]any{"base": nil},
			message: []string{"with.items[0]: {{vars.base.port}}: "},
		},
		{
			name: "a step var",
			step: &Step{
				Name: "var",
				Uses: "http",
				Vars: map[string]any{"token": "{{outputs.login.token}}"},
			},
			message: []string{"vars.token: {{outputs.login.token}}: "},
		},
		{
			name: "a step var that is a map",
			step: &Step{
				Name: "map var",
				Uses: "http",
				Vars: map[string]any{"auth": map[string]any{"user": "{{1 +}}"}},
			},
			message: []string{"vars.auth.user: {{1 +}}: "},
		},
		{
			name: "the step name",
			step: &Step{
				Name: "check {{outputs.login.token}}",
				Uses: "http",
			},
			message: []string{"name: {{outputs.login.token}}: "},
		},
		{
			name: "the name and a var together",
			step: &Step{
				Name: "check {{outputs.a.b}}",
				Uses: "http",
				Vars: map[string]any{"x": "{{outputs.c.d}}"},
			},
			message: []string{"name: {{outputs.a.b}}: ", "vars.x: {{outputs.c.d}}: "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, result, failed := runStepForTemplateError(t, tt.step, tt.vars)
			if calls != 0 {
				t.Errorf("the action ran %d times, want 0", calls)
			}
			if !failed {
				t.Error("the job is not marked failed")
			}
			if result.Status != StatusError {
				t.Errorf("status = %v, want %v", result.Status, StatusError)
			}
			if result.Failure == nil || result.Failure.Kind != FailureTemplate {
				t.Fatalf("failure = %+v, want kind %q", result.Failure, FailureTemplate)
			}
			for _, m := range tt.message {
				if !strings.Contains(result.Failure.Message, m) {
					t.Errorf("failure message %q does not contain %q", result.Failure.Message, m)
				}
			}
		})
	}
}

func TestStep_TemplateErrorInSkippedStep(t *testing.T) {
	st := &Step{
		Name:   "skipped",
		Uses:   "http",
		SkipIf: "true",
		Vars:   map[string]any{"token": "{{outputs.login.token}}"},
	}
	calls, result, failed := runStepForTemplateError(t, st, nil)
	if calls != 0 || failed {
		t.Errorf("calls = %d, failed = %v, want a skipped step", calls, failed)
	}
	if result.Status != StatusSkipped {
		t.Errorf("status = %v, want %v", result.Status, StatusSkipped)
	}
}

func TestStep_TemplateWithUndefinedNameRuns(t *testing.T) {
	st := &Step{
		Name: "fallback {{vars.missing ?? 'x'}}",
		Uses: "http",
		With: map[string]any{"url": "{{vars.base ?? 'http://localhost'}}"},
		Test: "res.code == 200",
	}
	calls, result, failed := runStepForTemplateError(t, st, map[string]any{})
	if calls != 1 || failed {
		t.Errorf("calls = %d, failed = %v, want one successful run", calls, failed)
	}
	if result.Name != "fallback x" {
		t.Errorf("name = %q, want %q", result.Name, "fallback x")
	}
}

func TestWorkflow_TemplateErrorExitCode(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{
		"req": map[string]any{},
		"res": map[string]any{"code": 200},
	})
	w := &Workflow{
		Name: "exit",
		Jobs: []Job{{Name: "a", Steps: []*Step{{
			Name: "s",
			Uses: "ok",
			With: map[string]any{"url": "{{outputs.missing.url}}"},
		}}}},
		printer: newBufferPrinter(),
	}
	if err := w.Start(Config{ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if w.exitStatus != ExitTestFailed {
		t.Errorf("exit status = %d, want %d", w.exitStatus, ExitTestFailed)
	}
}

// templateErrorOutput runs w with a JSON report and returns everything it
// printed and wrote, and the report.
func templateErrorOutput(t *testing.T, w *Workflow, runner ActionRunner) (string, report.Report) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{Reports: []report.Target{{Format: report.JSON, Path: path}}, ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	out := w.printer.outWriter.(*bytes.Buffer).String() +
		w.printer.errWriter.(*bytes.Buffer).String() +
		string(data)
	return out, r
}

func TestWorkflow_TemplateErrorHidesCredentials(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}})
	w := &Workflow{
		Name: "credentials",
		Vars: map[string]any{"pw": "learned-pw"},
		Jobs: []Job{{
			Name: "job",
			Steps: []*Step{
				{
					Name: "a literal in a credential's template",
					Uses: "ok",
					With: map[string]any{
						"password": "{{ 'literal-pw' + outputs.login.token }}",
						"headers":  map[string]any{"cookie": []any{"{{ 'cookie-pw' + outputs.login.token }}"}},
					},
				},
				{
					Name: "a credential quoted by the error of another value",
					Uses: "ok",
					With: map[string]any{
						"password": "{{vars.pw}}",
						"port":     "{{parse_int(vars.pw)}}",
					},
				},
			},
		}},
		printer: newBufferPrinter(),
	}

	out, r := templateErrorOutput(t, w, runner)
	for _, leak := range []string{"literal-pw", "cookie-pw", "learned-pw"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked into the output:\n%s", leak, out)
		}
	}

	steps := r.Jobs[0].Steps
	for _, want := range []string{"with.password: the template could not be evaluated", "with.headers.cookie[0]: the template could not be evaluated"} {
		if !strings.Contains(steps[0].Failure.Message, want) {
			t.Errorf("failure message %q does not contain %q", steps[0].Failure.Message, want)
		}
	}
	if steps[1].Failure == nil || !strings.Contains(steps[1].Failure.Message, "with.port: {{parse_int(vars.pw)}}: ") {
		t.Errorf("the error of a value that is not a credential should be kept, got %+v", steps[1].Failure)
	}
}

// The password of a URL is a credential, so the error of a template in its
// user info is hidden, while that of a template elsewhere in a URL, which
// most are, is kept.
func TestWorkflow_TemplateErrorHidesURLPassword(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}})
	step := func(name string, with map[string]any) *Step {
		return &Step{Name: name, Uses: "ok", With: with}
	}
	w := &Workflow{
		Name: "credentials",
		Vars: map[string]any{"pw": "learned-pw"},
		Jobs: []Job{{
			Name: "job",
			Steps: []*Step{
				step("a literal in the password", map[string]any{"url": "redis://app:{{ 'url-pw' + outputs.missing.x }}@cache:6379/0"}),
				step("with a slash and an at in the template", map[string]any{"url": "redis://app:{{ 'slash/pw@' + outputs.missing.x }}@cache/{{vars.pw}}"}),
				step("in a list", map[string]any{"targets": []any{map[string]any{"URL": "redis://{{ 'user-pw' + outputs.missing.x }}@cache"}}}),
				step("the password quoted by a conversion", map[string]any{"url": "redis://app:{{parse_int(vars.pw)}}@cache"}),
				step("a template in the path", map[string]any{"url": "https://api.example.com/{{ outputs.missing.x }}"}),
				step("after a password as it is", map[string]any{"url": "redis://app:plain@cache/{{ outputs.missing.x }}"}),
				step("an at in the path", map[string]any{"url": "https://example.com/{{ outputs.missing.x }}@v1"}),
			},
		}},
		printer: newBufferPrinter(),
	}

	out, r := templateErrorOutput(t, w, runner)
	for _, leak := range []string{"url-pw", "slash/pw", "user-pw", "learned-pw"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked into the output:\n%s", leak, out)
		}
	}
	steps := r.Jobs[0].Steps
	hidden := []string{"with.url", "with.url", "with.targets[0].URL", "with.url"}
	for i, path := range hidden {
		want := path + ": the template could not be evaluated"
		if steps[i].Failure == nil || !strings.Contains(steps[i].Failure.Message, want) {
			t.Errorf("steps[%d]: failure %+v does not contain %q", i, steps[i].Failure, want)
		}
	}
	for i := len(hidden); i < len(steps); i++ {
		if steps[i].Failure == nil || !strings.Contains(steps[i].Failure.Message, "with.url: {{ outputs.missing.x }}: ") {
			t.Errorf("steps[%d]: the error of a URL without a credential should be kept, got %+v", i, steps[i].Failure)
		}
	}
}

func TestWorkflow_VarsTemplateErrorHidesSecrets(t *testing.T) {
	w := &Workflow{
		Name:    "secrets",
		Secrets: []string{"TEST_PASSWORD"},
		env:     map[string]string{"TEST_PASSWORD": "hunter2xyz"},
		Vars:    map[string]any{"n": "{{parse_int(TEST_PASSWORD)}}"},
		Jobs:    []Job{{Name: "job", Steps: []*Step{{Name: "s", Uses: "ok"}}}},
		printer: newBufferPrinter(),
	}
	err := w.Start(Config{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2xyz") {
		t.Errorf("the secret leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "<secret:TEST_PASSWORD>") {
		t.Errorf("expected the secret to be masked, got %v", err)
	}
	var fErr *expr.FieldError
	if !errors.As(err, &fErr) || fErr.Path != "vars.n" {
		t.Errorf("expected the FieldError to be found through the masking, got %v", err)
	}
}

// An iteration runs the same step again. A run whose template cannot be
// evaluated does not report what the run before it left, such as the time
// and the attempts of an action that it did not run itself.
func TestStep_TemplateErrorDoesNotReportTheLastRun(t *testing.T) {
	calls := 0
	job := &Job{ID: "j", Name: "j", Steps: []*Step{{
		Name:      "again {{vars.k.y}}",
		Uses:      "http",
		Retry:     &StepRetry{MaxAttempts: 3},
		Test:      "true",
		Iteration: Iteration{Entries: []map[string]any{{"k": map[string]any{"y": 1}}, {}}},
	}}}
	runner := &callCountingRunner{
		result:    map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}, "rt": "10ms"},
		callCount: &calls,
	}
	rs := NewResult()
	rs.Jobs["j"] = &JobResult{JobName: "j", JobID: "j"}
	ctx := JobContext{
		runEnv: &runEnv{
			Config:  Config{ActionRunner: runner},
			Printer: newBufferPrinter(),
			Result:  rs,
		},
		CurrentJobID: "j",
		Outputs:      NewOutputs(),
	}
	if _, err := job.run(ctx); err != nil {
		t.Fatal(err)
	}

	results := rs.Jobs["j"].StepResults
	if calls != 1 || len(results) != 2 {
		t.Fatalf("the action ran %d times with %d results, want once with 2", calls, len(results))
	}
	if results[0].RetryAttempt != 1 {
		t.Fatalf("the first run's retry attempt = %d, want 1", results[0].RetryAttempt)
	}
	if results[1].Elapsed != 0 {
		t.Errorf("elapsed = %v, want 0 for a step whose action did not run", results[1].Elapsed)
	}
	if results[1].RetryAttempt != 0 {
		t.Errorf("retry attempt = %d, want 0", results[1].RetryAttempt)
	}
}

func TestWorkflow_RepeatKeepsFailureKind(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "async"}[async], func(t *testing.T) {
			runner := newMockRunner()
			runner.SetResult("ok", map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}})
			runner.SetError("down", errors.New("connection refused"))
			w := &Workflow{
				Name: "repeat",
				Jobs: []Job{{
					Name:   "job",
					Repeat: &Repeat{Count: 2, Interval: Interval{Duration: time.Millisecond}, Async: async},
					Steps: []*Step{
						{Name: "template", Uses: "ok", With: map[string]any{"url": "{{outputs.a.b}}"}},
						{Name: "action", Uses: "down", Test: "res.code == 200"},
						{Name: "assertion", Uses: "ok", Test: "res.code == 500"},
					},
				}},
				printer: newBufferPrinter(),
			}
			_, r := templateErrorOutput(t, w, runner)

			want := []string{FailureTemplate, FailureAction, FailureAssertion}
			for i, st := range r.Jobs[0].Steps {
				if st.Failure == nil {
					t.Errorf("step %d: no failure recorded", i)
					continue
				}
				if st.Failure.Kind != want[i] {
					t.Errorf("step %d: kind = %q, want %q", i, st.Failure.Kind, want[i])
				}
				if !strings.HasPrefix(st.Failure.Message, "2 of 2 iterations failed; the first: ") {
					t.Errorf("step %d: message = %q", i, st.Failure.Message)
				}
			}
		})
	}
}

// TestWorkflow_TemplateErrorHidesCredentialsUnderAKeyTemplate checks that a
// value under a key that becomes a credential header, such as one whose name
// comes from a var, has the details of its error hidden as one under the
// header written so does, while the error names the key as written.
func TestWorkflow_TemplateErrorHidesCredentialsUnderAKeyTemplate(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}})
	w := &Workflow{
		Name: "credentials",
		Vars: map[string]any{"header": "authorization"},
		Jobs: []Job{{
			Name: "job",
			Steps: []*Step{{
				Name: "a credential header named by a template",
				Uses: "ok",
				With: map[string]any{
					"headers": map[string]any{"{{ vars.header }}": "{{ 'Bearer literal-token' + outputs.login.token }}"},
				},
			}},
		}},
		printer: newBufferPrinter(),
	}

	out, r := templateErrorOutput(t, w, runner)
	if strings.Contains(out, "literal-token") {
		t.Errorf("the credential leaked into the output:\n%s", out)
	}
	want := "with.headers.{{ vars.header }}: the template could not be evaluated"
	if msg := r.Jobs[0].Steps[0].Failure.Message; !strings.Contains(msg, want) {
		t.Errorf("failure message %q does not contain %q", msg, want)
	}
}

// TestWorkflow_KeyCollisionUnderACredentialIsShown checks that a key that
// comes to a credential header taken is reported as such, rather than as a
// template that could not be evaluated: it was, and its error holds no
// credential.
func TestWorkflow_KeyCollisionUnderACredentialIsShown(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"req": map[string]any{}, "res": map[string]any{"code": 200}})
	w := &Workflow{
		Name: "collision",
		Vars: map[string]any{"header": "authorization"},
		Jobs: []Job{{
			Name: "job",
			Steps: []*Step{{
				Name: "two authorization headers",
				Uses: "ok",
				With: map[string]any{
					"headers": map[string]any{
						"authorization":     "Bearer literal-token",
						"{{ vars.header }}": "Bearer other-token",
					},
				},
			}},
		}},
		printer: newBufferPrinter(),
	}

	_, r := templateErrorOutput(t, w, runner)
	want := `with.headers.{{ vars.header }}: key {{ vars.header }} comes to the same key as "authorization"`
	if msg := r.Jobs[0].Steps[0].Failure.Message; !strings.Contains(msg, want) {
		t.Errorf("failure message %q does not contain %q", msg, want)
	}
}

// TestStep_ExpressionHints tells a test, or a template, written as another
// language would write it how expressions write it, after the error it
// fails with.
func TestStep_ExpressionHints(t *testing.T) {
	_, result, failed := runStepForTemplateError(t, &Step{
		Name: "test",
		Uses: "http",
		Test: `res.code.toString() == "200"`,
	}, nil)
	if !failed {
		t.Error("the job is not marked failed")
	}
	for _, want := range []string{"test error: ", "| res.code.toString() == \"200\"", "hint: expressions have no toString() method: write string(res.code)"} {
		if !strings.Contains(failureTextOf(result.Failure, result.Test), want) {
			t.Errorf("test output %q does not contain %q", failureTextOf(result.Failure, result.Test), want)
		}
	}

	_, result, _ = runStepForTemplateError(t, &Step{
		Name: "with",
		Uses: "http",
		With: map[string]any{"url": "http://localhost/{{ vars.name.toLowerCase() }}"},
	}, map[string]any{"name": "Alice"})
	if result.Failure == nil || !strings.Contains(result.Failure.Message, "with.url: {{ vars.name.toLowerCase() }}: ") ||
		!strings.Contains(result.Failure.Message, "hint: expressions have no toLowerCase() method: write lower(vars.name)") {
		t.Errorf("failure = %+v, want the template's error and the hint", result.Failure)
	}
}
