package probe

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/report"
)

// contractOf is the check of an action that checked the step against a
// contract and found the violations given.
func contractOf(vs ...map[string]any) *actionrpc.ContractCheck {
	list := []any{}
	for _, v := range vs {
		list = append(list, v)
	}
	check := actionrpc.ContractCheckOf(list, nil)
	return &check
}

// The check the action reports of an attempt against a contract is the
// step's, and an attempt that fails leaves none of an earlier one.
func TestStep_executeSingleAction_TakesTheContractTheActionReports(t *testing.T) {
	callCount := 0
	broken := contractOf(map[string]any{"in": "response", "message": "body failed"})
	runner := &callCountingRunner{
		callCount: &callCount,
		result:    map[string]any{"res": map[string]any{"code": 200}},
		contractFunc: func(int) *actionrpc.ContractCheck {
			return broken
		},
	}
	st := &stepRun{Step: &Step{Uses: "http"}, expr: &expr.Expr{}}
	jCtx := &JobContext{runEnv: &runEnv{Printer: newBufferPrinter()}}

	if _, err := st.executeSingleAction(runner, map[string]any{}, jCtx, false); err != nil {
		t.Fatalf("executeSingleAction() error = %v", err)
	}
	if !reflect.DeepEqual(st.contract, broken) {
		t.Errorf("contract = %+v, want %+v", st.contract, broken)
	}

	runner.err = errors.New("connection refused")
	if _, err := st.executeSingleAction(runner, map[string]any{}, jCtx, false); err == nil {
		t.Fatal("executeSingleAction() error = nil, want the action's")
	}
	if st.contract != nil {
		t.Errorf("contract = %+v after a failed attempt, want none", st.contract)
	}
}

func TestStep_check_Contract(t *testing.T) {
	broken := map[string]any{"in": "response", "field": "$.id", "reason": "want integer", "message": "body failed"}

	tests := []struct {
		name        string
		test        string
		contract    *actionrpc.ContractCheck
		wantChecked bool
		wantOK      bool
		wantKind    string
	}{
		{name: "no contract and no test"},
		{name: "a contract kept, without a test", contract: contractOf(), wantChecked: true, wantOK: true},
		{name: "a contract kept, with a test that holds", test: "res.code == 200", contract: contractOf(), wantChecked: true, wantOK: true},
		{name: "a contract kept, with a test that fails", test: "res.code == 201", contract: contractOf(), wantChecked: true, wantKind: FailureAssertion},
		{name: "a contract broken, without a test", contract: contractOf(broken), wantChecked: true, wantKind: FailureContractResponse},
		{name: "a contract broken, with a test that holds", test: "res.code == 200", contract: contractOf(broken), wantChecked: true, wantKind: FailureContractResponse},
		{name: "a request that breaks the contract", test: "res.code == 200", contract: contractOf(map[string]any{"in": "request", "message": "body failed"}), wantChecked: true, wantKind: FailureContractRequest},
		// What res holds is the step's to read; the runner hears of a
		// contract only from the action.
		{name: "violations in res alone", test: "res.code == 200", wantChecked: true, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := map[string]any{"code": 200}
			if tt.contract == nil && tt.test != "" {
				res["violations"] = []any{broken}
			}
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}, ctx: StepContext{Res: res}, contract: tt.contract}
			if got := st.checked(); got != tt.wantChecked {
				t.Fatalf("checked() = %v, want %v", got, tt.wantChecked)
			}
			if !tt.wantChecked {
				return
			}
			ok := st.check(newBufferPrinter())
			if ok != tt.wantOK {
				t.Fatalf("check() = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK {
				if st.failure != nil {
					t.Errorf("failure = %+v, want nil", st.failure)
				}
				return
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want kind %q", st.failure, tt.wantKind)
			}
		})
	}
}

func TestStep_contractFailure(t *testing.T) {
	st := &stepRun{
		Step: &Step{},
		ctx: StepContext{
			Req: map[string]any{"url": "http://example.com/v1/users/2"},
			Res: map[string]any{"code": 200},
		},
		contract: contractOf(
			map[string]any{"in": "response", "reason": "missing property 'name'", "message": "body failed"},
			map[string]any{"in": "response", "field": "$.id", "reason": "got string, want integer", "message": "body failed"},
		),
	}

	f := st.contractFailure()
	if f == nil {
		t.Fatal("contractFailure() = nil, want a failure")
	}
	if f.Kind != FailureContractResponse {
		t.Errorf("Kind = %q, want %q", f.Kind, FailureContractResponse)
	}
	if f.Message != "the response breaks its contract in 2 places" {
		t.Errorf("Message = %q", f.Message)
	}
	want := []report.Violation{
		{In: "response", Reason: "missing property 'name'", Message: "body failed"},
		{In: "response", Field: "$.id", Reason: "got string, want integer", Message: "body failed"},
	}
	if !reflect.DeepEqual(f.Violations, want) {
		t.Errorf("Violations = %+v, want %+v", f.Violations, want)
	}
	if f.Request == nil || f.Response == nil {
		t.Error("the failure should carry the request and response, as a failed test does")
	}
}

func TestStep_createStepResult_Contract(t *testing.T) {
	broken := map[string]any{"in": "response", "message": "GET Path '/v1/teams' not found"}

	tests := []struct {
		name       string
		contract   *actionrpc.ContractCheck
		wantStatus StatusType
	}{
		{name: "a response that keeps to its contract passes without a test", contract: contractOf(), wantStatus: StatusSuccess},
		{name: "a response that breaks its contract fails", contract: contractOf(broken), wantStatus: StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 200}}, contract: tt.contract}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
					Result:  NewResult(),
				},
			}

			result := st.createStepResult("Contract", jCtx, st.verdict(jCtx))
			if result.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", result.Status, tt.wantStatus)
			}
			if !result.HasTest {
				t.Error("HasTest = false, want true: the contract checks the step")
			}
			if tt.wantStatus != StatusError {
				return
			}
			if result.Failure == nil || len(result.Failure.Violations) != 1 {
				t.Fatalf("Failure = %+v, want the violation", result.Failure)
			}
			if out := failureTextOf(result.Failure, result.Test); !strings.Contains(out, "GET Path '/v1/teams' not found") {
				t.Errorf("failure text = %q, want the violation", out)
			}
			if got := jCtx.Result.exitCode(true); got != ExitTestFailed {
				t.Errorf("exit code = %d, want %d", got, ExitTestFailed)
			}
		})
	}
}

func TestStep_executeActionWithRetry_ContractBroken(t *testing.T) {
	callCount := 0
	mock := &callCountingRunner{
		callCount: &callCount,
		resultFunc: func(count int) map[string]any {
			return map[string]any{"status": 0, "res": map[string]any{"code": 200}}
		},
		contractFunc: func(count int) *actionrpc.ContractCheck {
			if count <= 2 {
				return contractOf(map[string]any{"in": "response", "message": "body failed"})
			}
			return contractOf()
		},
	}
	step := &stepRun{
		Step: &Step{
			Uses: "http",
			Test: "res.code == 200",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 10 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The test holds every time; the attempts that break the contract are
	// retried as failed tests are.
	if callCount != 3 {
		t.Errorf("attempts = %d, want 3", callCount)
	}
}

func TestStep_handleRepeatExecution_Contract(t *testing.T) {
	step := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
		repeat: &jobRepeat{
			counters: make(map[int]StepRepeatCounter),
			total:    2,
		},
	}

	step.contract = contractOf()
	step.handleRepeatExecution(jCtx, "Contract", false)
	step.contract = contractOf(map[string]any{"in": "response", "message": "body failed"})
	step.handleRepeatExecution(jCtx, "Contract", false)

	counter := jCtx.repeat.counters[0]
	if counter.SuccessCount != 1 || counter.FailureCount != 1 {
		t.Errorf("counts = %d/%d, want 1 success and 1 failure", counter.SuccessCount, counter.FailureCount)
	}
	if !counter.Checked {
		t.Error("Checked = false, want true")
	}
	if counter.Failure == nil || counter.Failure.Kind != FailureContractResponse {
		t.Errorf("Failure = %+v, want a contract failure", counter.Failure)
	}
}

func TestStep_contractFailure_Kind(t *testing.T) {
	request := map[string]any{"in": "request", "message": "request body failed"}
	response := map[string]any{"in": "response", "message": "response body failed"}

	tests := []struct {
		name        string
		violations  []map[string]any
		wantKind    string
		wantMessage string
	}{
		{
			name:        "in the response",
			violations:  []map[string]any{response},
			wantKind:    FailureContractResponse,
			wantMessage: "the response breaks its contract in 1 place",
		},
		{
			name:        "in the request",
			violations:  []map[string]any{request, request},
			wantKind:    FailureContractRequest,
			wantMessage: "the request breaks its contract in 2 places",
		},
		{
			name:        "in both, where the request decides the kind",
			violations:  []map[string]any{request, response, response},
			wantKind:    FailureContractRequest,
			wantMessage: "the request breaks its contract in 1 place, and the response in 2 places",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, contract: contractOf(tt.violations...)}
			f := st.contractFailure()
			if f == nil {
				t.Fatal("contractFailure() = nil, want a failure")
			}
			if f.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", f.Kind, tt.wantKind)
			}
			if f.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", f.Message, tt.wantMessage)
			}
			if len(f.Violations) != len(tt.violations) {
				t.Errorf("Violations = %+v, want %d", f.Violations, len(tt.violations))
			}
		})
	}
}

func TestStep_contractMatched(t *testing.T) {
	tests := []struct {
		name     string
		contract *actionrpc.ContractCheck
		want     *report.Contract
	}{
		{name: "no contract"},
		{name: "a contract with nothing that matches", contract: contractOf()},
		{
			name:     "an operation and a response",
			contract: &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}},
			want:     &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"},
		},
		{
			name:     "an operation that declares no response for the status",
			contract: &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}"}},
			want:     &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, contract: tt.contract}
			if got := st.contractMatched(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("contractMatched() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStep_createStepResult_ContractMatched(t *testing.T) {
	contract := &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}}
	st := &stepRun{Step: &Step{}, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 200}}, contract: contract}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
	}

	result := st.createStepResult("Get a user", jCtx, StatusSuccess)
	want := &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}
	if !reflect.DeepEqual(result.Contract, want) {
		t.Errorf("Contract = %+v, want %+v", result.Contract, want)
	}
}

func TestStep_handleRepeatExecution_ContractMatched(t *testing.T) {
	step := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
		repeat: &jobRepeat{
			counters: make(map[int]StepRepeatCounter),
			total:    2,
		},
		CurrentJobID: "job-1",
	}
	jCtx.Result.Jobs["job-1"] = &JobResult{JobID: "job-1"}

	for _, response := range []string{"200", "2XX"} {
		step.contract = &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /items", Response: response}}
		step.handleRepeatExecution(jCtx, "Items", false)
	}

	// The first iteration's is kept, as its failure is.
	want := &report.Contract{Spec: "openapi.yml", Operation: "GET /items", Response: "200"}
	if got := jCtx.repeat.counters[0].Contract; !reflect.DeepEqual(got, want) {
		t.Errorf("Contract = %+v, want %+v", got, want)
	}

	// The step's result, made from the counter, carries it.
	e := &Executor{job: &Job{ID: "job-1", Steps: []*Step{step.Step}}}
	e.appendRepeatStepResults(jCtx)
	results := jCtx.Result.Jobs["job-1"].StepResults
	if len(results) != 1 || !reflect.DeepEqual(results[0].Contract, want) {
		t.Errorf("StepResults = %+v, want one carrying %+v", results, want)
	}
}

func TestStep_executeActionWithRetry_ContractOnly(t *testing.T) {
	broken := map[string]any{"in": "response", "message": "body failed"}

	tests := []struct {
		name         string
		brokenUntil  int // the attempts up to this one break the contract
		wantAttempts int
	}{
		{name: "retried until the contract holds", brokenUntil: 2, wantAttempts: 3},
		{name: "retried as many times as allowed", brokenUntil: 5, wantAttempts: 3},
		{name: "not retried when the contract holds", brokenUntil: 0, wantAttempts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			mock := &callCountingRunner{
				callCount: &callCount,
				resultFunc: func(count int) map[string]any {
					return map[string]any{"status": 0, "res": map[string]any{"code": 200}}
				},
				contractFunc: func(count int) *actionrpc.ContractCheck {
					if count <= tt.brokenUntil {
						return contractOf(broken)
					}
					return contractOf()
				},
			}
			// No test: the contract alone checks the step.
			step := &stepRun{
				Step: &Step{
					Uses:  "http",
					Retry: &StepRetry{MaxAttempts: 3, Interval: Interval{Duration: time.Millisecond}},
				},
				expr: &expr.Expr{},
			}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
			}

			if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", callCount, tt.wantAttempts)
			}
		})
	}
}
