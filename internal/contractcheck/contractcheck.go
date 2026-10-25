// Package contractcheck holds what the actions that check a step against a
// contract, the http action's OpenAPI document and the grpc action's .proto
// files, share: reading the flags of the contract option, the shape of a
// violation, and handing the result to the runner and to res.
package contractcheck

import (
	"fmt"

	"github.com/mozership/probe/actionrpc"
)

// Flag returns the boolean key of the option o, named name as the step
// writes it, such as openapi, or def when it is missing.
func Flag(o map[string]any, name, key string, def bool) (bool, error) {
	v, ok := o[key]
	if !ok {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s.%s must be true or false", name, key)
	}
	return b, nil
}

// Violation is one thing a contract does not allow, found in the request
// or the response as in says, in the shape actionrpc.ContractCheckOf reads.
func Violation(in, message, reason, field string) map[string]any {
	v := map[string]any{"in": in, "message": message}
	if reason != "" {
		v["reason"] = reason
	}
	if field != "" {
		v["field"] = field
	}
	return v
}

// Report tells c, for the runner to take as the step's test, the violations
// a check found and what the contract matched the step to, and puts them in
// the res of ret, as violations and contract, for the step's own test to
// read. A nil violations means the step was checked against no contract,
// and nothing is reported.
func Report(c *actionrpc.Contract, ret map[string]any, violations []any, matched map[string]any) {
	if violations != nil {
		c.Report(actionrpc.ContractCheckOf(violations, matched))
	}
	res, ok := ret["res"].(map[string]any)
	if !ok {
		return
	}
	if violations != nil {
		res["violations"] = violations
	}
	if matched != nil {
		res["contract"] = matched
	}
}
