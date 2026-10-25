package actionrpc

import (
	"sync"

	"github.com/mozership/probe/internal/pb"
)

// Violation is what a contract does not allow in a request or a response.
type Violation struct {
	// In is where it is: request or response.
	In string
	// Field is the field, such as $.id; empty when it is not in one.
	Field string
	// Reason is the kind of violation; empty when there is none to tell.
	Reason  string
	Message string
}

// ContractMatch is what a contract matched a step to, which a report of the
// coverage of the contract counts.
type ContractMatch struct {
	// Spec is the contract, such as the path of an OpenAPI document.
	Spec string
	// Operation is the operation, such as GET /users/{id}.
	Operation string
	// Response is the response the operation declares for the step, such as
	// 200; empty when it declares none.
	Response string
}

// ContractCheck is the check of a step against a contract, such as an
// OpenAPI document, which the runner takes as the step's test: the step
// fails when the contract does not allow its request or response, and is
// checked even without a test of its own.
type ContractCheck struct {
	// Matched is what the contract matched the step to; nil when it has
	// nothing that matches it.
	Matched *ContractMatch
	// Violations are what the contract does not allow in the request and
	// the response, empty when it allows all of it.
	Violations []Violation
}

// ContractCheckOf returns the check that violations and matched tell, in the
// shapes a step shows them in res.violations and res.contract: each
// violation a map of in, field, reason and message, and matched a map of
// spec, operation and response, or nil.
func ContractCheckOf(violations []any, matched map[string]any) ContractCheck {
	var check ContractCheck
	str := func(m map[string]any, k string) string {
		s, _ := m[k].(string)
		return s
	}
	if matched != nil {
		check.Matched = &ContractMatch{Spec: str(matched, "spec"), Operation: str(matched, "operation"), Response: str(matched, "response")}
	}
	check.Violations = make([]Violation, 0, len(violations))
	for _, item := range violations {
		m, _ := item.(map[string]any)
		check.Violations = append(check.Violations, Violation{In: str(m, "in"), Field: str(m, "field"), Reason: str(m, "reason"), Message: str(m, "message")})
	}
	return check
}

// Contract records the check an action made of a step against a contract,
// for the runner to take as the step's test. A nil Contract records
// nothing.
type Contract struct {
	mu    sync.Mutex
	check *ContractCheck
}

// Report records the check the action made, in place of one it reported
// before.
func (c *Contract) Report(check ContractCheck) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.check = &check
}

// Check returns the check the action reported, or nil when it checked the
// step against no contract.
func (c *Contract) Check() *ContractCheck {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.check
}

func contractToPB(check *ContractCheck) *pb.Contract {
	if check == nil {
		return nil
	}
	out := &pb.Contract{Violations: make([]*pb.Violation, len(check.Violations))}
	if m := check.Matched; m != nil {
		out.Matched = &pb.ContractMatch{Spec: m.Spec, Operation: m.Operation, Response: m.Response}
	}
	for i, v := range check.Violations {
		out.Violations[i] = &pb.Violation{In: v.In, Field: v.Field, Reason: v.Reason, Message: v.Message}
	}
	return out
}

func contractFromPB(c *pb.Contract) *ContractCheck {
	if c == nil {
		return nil
	}
	out := &ContractCheck{Violations: make([]Violation, len(c.GetViolations()))}
	if m := c.GetMatched(); m != nil {
		out.Matched = &ContractMatch{Spec: m.GetSpec(), Operation: m.GetOperation(), Response: m.GetResponse()}
	}
	for i, v := range c.GetViolations() {
		out.Violations[i] = Violation{In: v.GetIn(), Field: v.GetField(), Reason: v.GetReason(), Message: v.GetMessage()}
	}
	return out
}
