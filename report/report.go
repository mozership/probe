// Package report renders the result of a workflow run as files for machines
// and for people: JSON, JUnit XML, Markdown and the GitHub Actions job summary.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/safefile"
)

// Format names a report file format.
type Format string

const (
	JSON     Format = "json"
	JUnit    Format = "junit"
	Markdown Format = "markdown"
	// GitHubSummary appends the Markdown page to the GitHub Actions job
	// summary, the file GITHUB_STEP_SUMMARY names.
	GitHubSummary Format = "github-summary"
)

// writer writes a report to the file at path.
type writer interface {
	write(r *Report, path string) error
}

// format is how a report format is written, and where to when no path is
// given.
type format struct {
	defaultPath string
	writer      writer
}

var formats = map[Format]format{
	JSON:     {"probe-report.json", fileWriter{JSON, (*Report).WriteJSON}},
	JUnit:    {"probe-junit.xml", fileWriter{JUnit, (*Report).WriteJUnit}},
	Markdown: {"probe-report.md", fileWriter{Markdown, (*Report).WriteMarkdown}},
	// The path is resolved when the report is written, from
	// GITHUB_STEP_SUMMARY.
	GitHubSummary: {"", stepSummaryWriter{}},
}

// fileWriter writes a format that is a file of its own, replacing whatever
// was at the path.
type fileWriter struct {
	format Format
	render func(r *Report, w io.Writer) error
}

func (fw fileWriter) write(r *Report, path string) error {
	// safefile.Replace keeps a symlink in the current directory, at the file or
	// any directory on the way, from redirecting the report elsewhere.
	err := safefile.Replace(path, func(w io.Writer) error { return fw.render(r, w) })
	if err != nil {
		return fmt.Errorf("failed to create %s report: %w", fw.format, err)
	}
	return nil
}

// Target is one report file to write after a run.
type Target struct {
	Format Format
	Path   string
}

// ParseTargets parses a comma separated list of format[=path] entries,
// such as "junit=out/junit.xml,json". A format without a path is written to
// its default file name in the current directory.
func ParseTargets(s string) ([]Target, error) {
	var targets []Target
	seen := make(map[Format]bool)

	for entry := range strings.SplitSeq(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		name, path, _ := strings.Cut(entry, "=")
		format := Format(strings.ToLower(strings.TrimSpace(name)))
		path = strings.TrimSpace(path)

		f, ok := formats[format]
		if !ok {
			return nil, fmt.Errorf("unknown report format: %s (expected json, junit, markdown or github-summary)", name)
		}
		if seen[format] {
			return nil, fmt.Errorf("report format given more than once: %s", format)
		}
		seen[format] = true

		if path == "" {
			path = f.defaultPath
		}
		targets = append(targets, Target{Format: format, Path: path})
	}

	return targets, nil
}

// Report statuses for the workflow, jobs and steps.
const (
	Passed   = "passed"
	Failed   = "failed"
	Skipped  = "skipped"
	Untested = "untested" // A step that ran but has no test expression
)

// Failure kinds, the reasons a step fails.
const (
	FailureAssertion = "assertion"  // The test expression evaluated to false
	FailureTestError = "test_error" // The test expression could not be evaluated
	FailureTestType  = "test_type"  // The test expression did not evaluate to a boolean
	FailureAction    = "action"     // The action itself returned an error
	FailureTemplate  = "template"   // A template in the step's with, vars or name could not be evaluated
	// The response broke the contract the action checked it against, such
	// as an OpenAPI document.
	FailureContractResponse = "contract_response"
	// The request broke the contract the action checked it against: the
	// workflow sent what the contract does not allow.
	FailureContractRequest = "contract_request"
	// The guard of the run, such as --read-only, refused what the step was
	// to do.
	FailureRefused = "refused"
)

// Report is the result of a workflow run in a form meant for machines: the
// JSON file is this structure as is, and the other formats are rendered from it.
type Report struct {
	Name string `json:"name"`
	// RunID names the run, as actions are told it, such as in the trace
	// header of the http action.
	RunID       string    `json:"run_id,omitempty"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMs  int64     `json:"duration_ms"`
	Summary     Summary   `json:"summary"`
	Jobs        []Job     `json:"jobs"`
}

// Summary counts jobs and steps by status.
type Summary struct {
	Jobs  Count `json:"jobs"`
	Steps Count `json:"steps"`
}

// Count is a tally by status.
type Count struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`
	Untested int `json:"untested,omitempty"`
}

// Job is one job of a Report.
type Job struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Steps      []Step    `json:"steps"`
}

// Step is one step of a Job.
type Step struct {
	Index      int      `json:"index"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Test       string   `json:"test,omitempty"`
	DurationMs int64    `json:"duration_ms"`
	Retry      *Retry   `json:"retry,omitempty"`
	Repeat     *Repeat  `json:"repeat,omitempty"`
	Echo       string   `json:"echo,omitempty"`
	Failure    *Failure `json:"failure,omitempty"`
	// Contract is what the action matched the response to in the contract
	// it checked it against, such as an operation of an OpenAPI document.
	Contract *Contract `json:"contract,omitempty"`
}

// Contract is what a response was matched to in a contract, which a report
// of the coverage of the contract counts.
type Contract struct {
	Spec      string `json:"spec"`               // The contract, such as the path of an OpenAPI document
	Operation string `json:"operation"`          // Such as GET /users/{id}
	Response  string `json:"response,omitempty"` // Such as 200, 2XX or default; empty when none is declared
}

// Retry records how many attempts a retried step took.
type Retry struct {
	Attempts int `json:"attempts"`
	Max      int `json:"max"`
}

// Repeat records the outcome of every iteration of a repeated job.
type Repeat struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failure int `json:"failure"`
	// Skipped is the iterations in which the step was not run: after a
	// step that failed in a job with fail_fast, or for an iteration that
	// came to no entry.
	Skipped int `json:"skipped,omitempty"`
}

// Failure says why a step failed.
type Failure struct {
	Kind     string         `json:"kind"`
	Message  string         `json:"message"`
	Request  map[string]any `json:"request,omitempty"`
	Response map[string]any `json:"response,omitempty"`
	// Violations are what broke the contract, when Kind is a contract's.
	Violations []Violation `json:"violations,omitempty"`
}

// Violation is one thing a contract does not allow.
type Violation struct {
	In      string `json:"in"`              // Where it was found: request or response
	Field   string `json:"field,omitempty"` // The field, such as /items/0/id
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
}

// String is the violation on one line: the field, what is wrong with it and
// why.
func (v Violation) String() string {
	s := v.Message
	if v.Reason != "" {
		s += ": " + v.Reason
	}
	if v.Field != "" {
		s = v.Field + ": " + s
	}
	return v.In + ": " + s
}

// New returns a report of a run with no jobs yet; AddJob adds them.
func New(name, description string, startedAt, finishedAt time.Time) *Report {
	return &Report{
		Name:        name,
		Description: description,
		Status:      Passed,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		DurationMs:  finishedAt.Sub(startedAt).Milliseconds(),
		Jobs:        []Job{},
	}
}

// AddJob appends a job, counts it and its steps in the summary, and fails
// the report when the job failed.
func (r *Report) AddJob(job Job) {
	r.Jobs = append(r.Jobs, job)
	r.Summary.Jobs.add(job.Status)
	for _, st := range job.Steps {
		r.Summary.Steps.add(st.Status)
	}
	if job.Status == Failed {
		r.Status = Failed
	}
}

func (c *Count) add(status string) {
	c.Total++
	switch status {
	case Passed:
		c.Passed++
	case Failed:
		c.Failed++
	case Skipped:
		c.Skipped++
	case Untested:
		c.Untested++
	}
}

// Mask hides secrets in every text the report carries, and credential
// headers in failed steps' requests and responses. It masks the data before
// it is rendered, because JSON and XML escape quotes, backslashes and
// ampersands, and a secret containing one would no longer match in the
// rendered file.
func (r *Report) Mask(m *mask.Masker) {
	r.Name = m.String(r.Name)
	r.Description = m.String(r.Description)
	for i := range r.Jobs {
		job := &r.Jobs[i]
		job.Name = m.String(job.Name)
		for j := range job.Steps {
			st := &job.Steps[j]
			st.Name = m.String(st.Name)
			st.Test = m.String(st.Test)
			st.Echo = m.String(st.Echo)
			if f := st.Failure; f != nil {
				f.Message = m.String(f.Message)
				f.Request = m.Map(f.Request)
				f.Response = m.Map(f.Response)
				for k := range f.Violations {
					v := &f.Violations[k]
					v.Field = m.String(v.Field)
					v.Reason = m.String(v.Reason)
					v.Message = m.String(v.Message)
				}
			}
		}
	}
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	// Responses are full of HTML and comparisons; the report is not embedded
	// in a page, so keep <, > and & readable.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Write renders the report in the target's format and writes it to the
// target's path, creating parent directories as needed.
func (r *Report) Write(t Target) error {
	f, ok := formats[t.Format]
	if !ok {
		return fmt.Errorf("unknown report format: %s", t.Format)
	}
	return f.writer.write(r, t.Path)
}

// failureDetail renders a failure as plain text for formats that embed it in
// a text block: the test, the message, then the request and response.
func failureDetail(st Step) string {
	var b strings.Builder
	if st.Test != "" {
		fmt.Fprintf(&b, "test: %s\n", st.Test)
	}
	if st.Repeat != nil {
		fmt.Fprintf(&b, "repeat: %d/%d succeeded\n", st.Repeat.Success, st.Repeat.Total)
	}
	if st.Failure == nil {
		return b.String()
	}
	fmt.Fprintf(&b, "%s: %s\n", st.Failure.Kind, st.Failure.Message)
	for _, v := range st.Failure.Violations {
		fmt.Fprintf(&b, "- %s\n", v)
	}
	if st.Failure.Request != nil {
		fmt.Fprintf(&b, "\nrequest:\n%s\n", indentJSON(st.Failure.Request))
	}
	if st.Failure.Response != nil {
		fmt.Fprintf(&b, "\nresponse:\n%s\n", indentJSON(st.Failure.Response))
	}
	return b.String()
}

func indentJSON(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimRight(b.String(), "\n")
}

func msToSec(ms int64) float64 {
	return float64(ms) / 1000
}
