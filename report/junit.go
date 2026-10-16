package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// The JUnit XML schema has no single standard. This follows the shape that
// GitHub Actions reporters, GitLab and Jenkins all read: a testsuite per job and
// a testcase per step. A failed test is a <failure>; an action that errored or
// a test that could not be evaluated is an <error>.

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string          `xml:"name,attr"`
	ID        string          `xml:"id,attr,omitempty"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      string          `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr,omitempty"`
	Cases     []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitProblem `xml:"failure,omitempty"`
	Error     *junitProblem `xml:"error,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut *junitText    `xml:"system-out,omitempty"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",cdata"`
}

// junitText keeps multi-line text readable: chardata would escape every
// newline and quote, while CDATA leaves them as they are.
type junitText struct {
	Text string `xml:",cdata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// WriteJUnit writes the report as JUnit XML.
func (r *Report) WriteJUnit(w io.Writer) error {
	doc := junitTestSuites{
		Name: r.Name,
		Time: junitTime(r.DurationMs),
	}

	for _, job := range r.Jobs {
		suite := junitTestSuite{
			Name: job.Name,
			ID:   job.ID,
			Time: junitTime(job.DurationMs),
		}
		if !job.StartedAt.IsZero() {
			suite.Timestamp = job.StartedAt.Format(time.RFC3339)
		}

		failedStep := false
		for _, st := range job.Steps {
			tc := junitTestCase{
				Name:      fmt.Sprintf("%d. %s", st.Index, st.Name),
				ClassName: job.Name,
				Time:      junitTime(st.DurationMs),
			}
			if st.Echo != "" {
				tc.SystemOut = &junitText{Text: st.Echo}
			}
			switch st.Status {
			case Failed:
				failedStep = true
				tc.Failure, tc.Error = junitFailure(st)
			case Skipped:
				tc.Skipped = &junitSkipped{}
			}
			suite.add(tc)
		}

		// The tests of a repeated job, over its runs and against the run
		// compared to, are testcases of their own, which decide whether the
		// job passed.
		for _, t := range []struct {
			name string
			rt   *RepeatTest
		}{{"repeat test", job.RepeatTest}, {"baseline test", job.BaselineTest}} {
			if t.rt == nil {
				continue
			}
			tc := junitTestCase{
				Name:      t.name,
				ClassName: job.Name,
				Time:      junitTime(0),
			}
			switch t.rt.Status {
			case Failed:
				failedStep = true
				tc.Failure, tc.Error = junitRepeatTestFailure(t.rt)
			case Skipped:
				tc.Skipped = &junitSkipped{Message: t.rt.Reason}
			}
			suite.add(tc)
		}

		// A job can be skipped before any step runs, or fail without a failed
		// step. Give either a testcase of its own, or the job would read as an
		// empty, passing suite.
		switch {
		case job.Status == Skipped && len(job.Steps) == 0:
			suite.add(junitTestCase{
				Name:      job.Name,
				ClassName: job.Name,
				Time:      junitTime(0),
				Skipped:   &junitSkipped{Message: "job skipped"},
			})
		case job.Status == Failed && !failedStep:
			suite.add(junitTestCase{
				Name:      job.Name,
				ClassName: job.Name,
				Time:      junitTime(job.DurationMs),
				Error:     &junitProblem{Message: "job failed without a failed step", Type: "job"},
			})
		}

		doc.Tests += suite.Tests
		doc.Failures += suite.Failures
		doc.Errors += suite.Errors
		doc.Skipped += suite.Skipped
		doc.Suites = append(doc.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func (s *junitTestSuite) add(tc junitTestCase) {
	s.Tests++
	switch {
	case tc.Failure != nil:
		s.Failures++
	case tc.Error != nil:
		s.Errors++
	case tc.Skipped != nil:
		s.Skipped++
	}
	s.Cases = append(s.Cases, tc)
}

// junitFailure maps a failed step to a <failure> when its test was false or
// its response broke its contract, and to an <error> otherwise.
func junitFailure(st Step) (failure, errElem *junitProblem) {
	p := &junitProblem{Body: failureDetail(st)}
	if st.Failure == nil {
		p.Message = "step failed"
		if st.Repeat != nil {
			p.Message = fmt.Sprintf("%d of %d iterations failed", st.Repeat.Failure, st.Repeat.Total)
		}
		p.Type = FailureAssertion
		return p, nil
	}

	p.Message = st.Failure.Message
	p.Type = st.Failure.Kind
	if st.Failure.Kind == FailureAssertion || st.Failure.Kind == FailureContractResponse {
		return p, nil
	}
	return nil, p
}

// junitRepeatTestFailure maps a failed test of a repeated job to a <failure>
// when it was false, and to an <error> when it could not be checked.
func junitRepeatTestFailure(rt *RepeatTest) (failure, errElem *junitProblem) {
	p := &junitProblem{Message: "test failed", Type: FailureAssertion, Body: rt.Test}
	if f := rt.Failure; f != nil {
		p.Message = f.Message
		p.Type = f.Kind
		p.Body = rt.Test + "\n" + f.Message
	}
	if p.Type == FailureAssertion {
		return p, nil
	}
	return nil, p
}

func junitTime(ms int64) string {
	return fmt.Sprintf("%.3f", msToSec(ms))
}
