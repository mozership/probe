package probe

import (
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/mask"
)

func TestStep_runTest_FalseClause(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name        string
		test        string
		res         map[string]any
		wantMessage string
		wantOutput  string
		notInOutput string
	}{
		{
			name:        "the first false condition and what it read",
			test:        "res.code == 200 &&\nres.body.name == \"alice\"",
			res:         map[string]any{"code": 200, "body": map[string]any{"name": "bob"}},
			wantMessage: `test evaluated to false: res.body.name == "alice" (res.body.name = "bob")`,
			wantOutput:  "       failed: res.body.name == \"alice\"\n         res.body.name = \"bob\"\n       request: ",
		},
		{
			name:        "an action that keeps its response out of the report",
			test:        "res.code == 200",
			res:         map[string]any{"code": 500, "dump": false},
			wantMessage: "test evaluated to false: res.code == 200",
			wantOutput:  "       failed: res.code == 200\n",
			notInOutput: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := NewPrinter(false)
			printer.SetMasker(mask.New(nil, nil))
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}}
			st.ctx = StepContext{Req: map[string]any{}, Res: tt.res}

			if st.runTest(printer) {
				t.Fatal("runTest() passed, want it to fail")
			}
			if st.failure == nil || st.failure.Message != tt.wantMessage {
				t.Errorf("failure = %+v, want the message %q", st.failure, tt.wantMessage)
			}
			out := failureTextOf(st.failure, st.Test)
			if !strings.HasPrefix(out, tt.wantOutput) {
				t.Errorf("output = %q, want it to start with %q", out, tt.wantOutput)
			}
			if tt.notInOutput != "" && strings.Contains(out, tt.notInOutput) {
				t.Errorf("output = %q, want it without %q", out, tt.notInOutput)
			}
		})
	}
}

func TestEvaluateTest(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	env := StepContext{Res: map[string]any{"code": 500}}
	_, evalErr := (&expr.Expr{}).Eval("nosuch(1)", env)
	if evalErr == nil {
		t.Fatal("nosuch(1) evaluated, want an error to test with")
	}

	tests := []struct {
		name         string
		test         string
		hideOperands bool
		wantPassed   bool
		wantKind     string
		wantMessage  string
		wantDetail   string
	}{
		{name: "a test that holds", test: "res.code == 500", wantPassed: true},
		{name: "a test that cannot be evaluated", test: "nosuch(1)", wantKind: FailureTestError, wantMessage: evalErr.Error(), wantDetail: testErrorDetail(evalErr.Error())},
		{name: "a test that is not a boolean", test: "res.code", wantKind: FailureTestType, wantMessage: "test evaluated to 500 (int), not a boolean", wantDetail: typeMismatchDetail("res.code", 500)},
		{name: "a test that is false", test: "res.code == 200", wantKind: FailureAssertion, wantMessage: "test evaluated to false: res.code == 200 (res.code = 500)", wantDetail: "       failed: res.code == 200\n         res.code = 500\n"},
		{name: "a test that is false, its operands hidden", test: "res.code == 200", hideOperands: true, wantKind: FailureAssertion, wantMessage: "test evaluated to false: res.code == 200", wantDetail: "       failed: res.code == 200\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.SetMasker(mask.New(nil, nil))

			passed, failure := evaluateTest(&expr.Expr{}, tt.test, env, printer, tt.hideOperands)
			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}
			if tt.wantPassed {
				if failure != nil {
					t.Errorf("failure = %+v, want none", failure)
				}
				return
			}
			if failure == nil || failure.Kind != tt.wantKind || failure.Message != tt.wantMessage {
				t.Errorf("failure = %+v, want %s %q", failure, tt.wantKind, tt.wantMessage)
			}
			if failure != nil && (failure.Request != nil || failure.Response != nil) {
				t.Errorf("failure = %+v, want no exchange: the caller adds one", failure)
			}
			if detail := failureTextOf(failure, tt.test); detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
		})
	}
}

// A test that cannot be evaluated, or is not a boolean, fails the step
// without the exchange, which only a false test is told with.
func TestStep_runTest_ErrorAndType(t *testing.T) {
	tests := []struct {
		name     string
		test     string
		wantKind string
	}{
		{name: "an error", test: "nosuch(1)", wantKind: FailureTestError},
		{name: "not a boolean", test: "res.code", wantKind: FailureTestType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.SetMasker(mask.New(nil, nil))
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}, ctx: StepContext{Req: map[string]any{"url": "/"}, Res: map[string]any{"code": 500}}}

			if st.runTest(printer) {
				t.Fatal("runTest() passed, want it to fail")
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want %s", st.failure, tt.wantKind)
			}
			out := failureTextOf(st.failure, st.Test)
			if st.failure.Request != nil || strings.Contains(out, "request:") {
				t.Errorf("failure = %+v, output = %q, want no exchange", st.failure, out)
			}
		})
	}
}

func TestStep_runTestRecordsFailure(t *testing.T) {
	tests := []struct {
		name     string
		test     string
		wantKind string
		wantMsg  string
	}{
		{"passes", "res.code == 200", "", ""},
		{"false", "res.code == 201", FailureAssertion, "test evaluated to false: res.code == 201 (res.code = 200)"},
		{"not a boolean", "res.code", FailureTestType, "test evaluated to 200 (int), not a boolean"},
		{"cannot evaluate", "res.code ==", FailureTestError, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{
				Step: &Step{
					Test: tt.test,
				},
				expr: &expr.Expr{},
				ctx: StepContext{
					Req: map[string]any{"path": "/"},
					Res: map[string]any{"code": 200},
				},
				// A failure left over from an earlier evaluation must not leak.
				failure: &StepFailure{Kind: "stale"},
			}

			st.runTest(newBufferPrinter())

			if tt.wantKind == "" {
				if st.failure != nil {
					t.Errorf("failure = %+v, want nil", st.failure)
				}
				return
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want kind %q", st.failure, tt.wantKind)
			}
			if tt.wantMsg != "" && st.failure.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", st.failure.Message, tt.wantMsg)
			}
			if tt.wantMsg == "" && st.failure.Message == "" {
				t.Error("message should explain the failure")
			}
		})
	}
}
