package oas

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozership/probe/report"
	"github.com/pb33f/libopenapi"
)

// Coverage is how much of an OpenAPI document the steps of a run checked:
// each operation the document declares, and each response the operation
// declares, with the steps whose response was matched to it.
type Coverage struct {
	Spec       string
	Operations []OperationCoverage
	// OperationsOnly is set for a contract that declares no responses, such
	// as a .proto file, whose methods do not say which statuses they end
	// with, so that its coverage is told by operations alone.
	OperationsOnly bool
}

// OperationCoverage is one operation of a document, such as GET /users/{id}.
type OperationCoverage struct {
	Operation string
	Steps     int // the steps whose response was matched to the operation
	Responses []ResponseCoverage
}

// ResponseCoverage is one response an operation declares, such as 200, 2XX
// or default.
type ResponseCoverage struct {
	Response string
	Steps    int // the steps whose response was matched to it
}

// ReadReport reads a report written by --report json.
func ReadReport(path string) (*report.Report, error) {
	return report.Read(path)
}

// NewCoverage counts the steps of r whose response was matched to each
// operation and response of the OpenAPI document at spec. A step counts for
// the document when it names the same path, as the step gave it.
func NewCoverage(spec string, r *report.Report) (*Coverage, error) {
	data, err := os.ReadFile(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenAPI spec: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenAPI model: %w", err)
	}

	var declared []Declared
	if model.Model.Paths != nil && model.Model.Paths.PathItems != nil {
		for path, item := range model.Model.Paths.PathItems.FromOldest() {
			for method, op := range item.GetOperations().FromOldest() {
				d := Declared{Operation: strings.ToUpper(method) + " " + path}
				if op.Responses != nil {
					if op.Responses.Codes != nil {
						for code := range op.Responses.Codes.KeysFromOldest() {
							d.Responses = append(d.Responses, code)
						}
					}
					if op.Responses.Default != nil {
						d.Responses = append(d.Responses, "default")
					}
				}
				declared = append(declared, d)
			}
		}
	}
	return CountCoverage(spec, declared, r)
}

// Declared is an operation a contract declares, with the responses it
// declares for it, in the order it declares them.
type Declared struct {
	Operation string
	Responses []string
}

// CountCoverage counts the steps of r whose response was matched to each
// operation and response declared in the contract spec names, such as an
// OpenAPI document or a .proto file. A step counts for the contract when it
// names the same path, as the step gave it. It is an error when no step of
// r was checked against spec.
func CountCoverage(spec string, declared []Declared, r *report.Report) (*Coverage, error) {
	type key struct{ operation, response string }
	steps := map[key]int{}
	checked := map[string]bool{}
	for _, job := range r.Jobs {
		for _, st := range job.Steps {
			c := st.Contract
			if c == nil {
				continue
			}
			checked[c.Spec] = true
			if filepath.Clean(c.Spec) != filepath.Clean(spec) {
				continue
			}
			steps[key{c.Operation, ""}]++
			if c.Response != "" {
				steps[key{c.Operation, c.Response}]++
			}
		}
	}
	if len(checked) == 0 {
		return nil, errors.New("no step of the report was checked against a contract: give openapi to the http steps or proto to the grpc steps, and write the report with --report json")
	}
	if !matchesAny(spec, checked) {
		return nil, fmt.Errorf("no step of the report was checked against %s; they were checked against %s", spec, strings.Join(sortedNames(checked), ", "))
	}

	cov := &Coverage{Spec: spec}
	for _, d := range declared {
		oc := OperationCoverage{Operation: d.Operation, Steps: steps[key{d.Operation, ""}]}
		for _, res := range d.Responses {
			oc.Responses = append(oc.Responses, ResponseCoverage{Response: res, Steps: steps[key{d.Operation, res}]})
		}
		cov.Operations = append(cov.Operations, oc)
	}
	return cov, nil
}

func matchesAny(spec string, names map[string]bool) bool {
	for name := range names {
		if filepath.Clean(name) == filepath.Clean(spec) {
			return true
		}
	}
	return false
}

func sortedNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Counts returns how many operations and responses the document declares,
// and how many of each at least one step checked.
func (c *Coverage) Counts() (operations, checkedOperations, responses, checkedResponses int) {
	for _, op := range c.Operations {
		operations++
		if op.Steps > 0 {
			checkedOperations++
		}
		for _, res := range op.Responses {
			responses++
			if res.Steps > 0 {
				checkedResponses++
			}
		}
	}
	return
}

// Write writes the coverage as text: each operation with its responses,
// marked with ✓ when a step checked it and - when none did, then the totals.
func (c *Coverage) Write(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Coverage of %s\n\n", c.Spec)
	for _, op := range c.Operations {
		fmt.Fprintf(&b, "%s %s%s\n", mark(op.Steps), op.Operation, stepCount(op.Steps))
		for _, res := range op.Responses {
			fmt.Fprintf(&b, "    %s %s%s\n", mark(res.Steps), res.Response, stepCount(res.Steps))
		}
	}
	ops, checkedOps, ress, checkedRess := c.Counts()
	fmt.Fprintf(&b, "\nOperations: %s\n", ratio(checkedOps, ops))
	if !c.OperationsOnly {
		fmt.Fprintf(&b, "Responses:  %s\n", ratio(checkedRess, ress))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func mark(steps int) string {
	if steps > 0 {
		return "✓"
	}
	return "-"
}

func stepCount(steps int) string {
	switch steps {
	case 0:
		return ""
	case 1:
		return " (1 step)"
	default:
		return fmt.Sprintf(" (%d steps)", steps)
	}
}

func ratio(n, total int) string {
	if total == 0 {
		return "none declared"
	}
	return fmt.Sprintf("%d of %d checked (%.1f%%)", n, total, float64(n)*100/float64(total))
}
