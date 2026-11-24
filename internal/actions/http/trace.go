package http

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mozership/probe/actionrpc"
)

// DefaultTraceHeader is the header trace_header: true sends.
const DefaultTraceHeader = "X-Probe-Trace"

// takeTrace removes trace_header from m and returns the name of the header
// to send the trace in: DefaultTraceHeader for true, the name given for a
// string, and none for false or none given. It is not named trace, which is
// the shorthand of the TRACE method.
func takeTrace(m map[string]any) (string, error) {
	v, ok := m["trace_header"]
	if !ok {
		return "", nil
	}
	delete(m, "trace_header")
	switch t := v.(type) {
	case bool:
		if t {
			return DefaultTraceHeader, nil
		}
		return "", nil
	case string:
		if !validHeaderName(t) {
			return "", fmt.Errorf("trace_header must be true, false or a header name, not %q", t)
		}
		return t, nil
	default:
		return "", errors.New("trace_header must be true, false or a header name")
	}
}

// validHeaderName reports whether s can name a header: a token as RFC 9110
// section 5.6.2 defines it.
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

// traceValue says where a request comes from: the run, the job, the step,
// the run of a repeated job and the attempt of a retried step. Each value is
// escaped as in a query, so that one holding a space, a semicolon or a letter
// outside ASCII cannot break the header.
func traceValue(s actionrpc.Step) string {
	// Probe tells a step without an id as step_ and where it is written; a
	// probe from before it did sends none for one in a job run by the
	// embedded action, which is named the same way here.
	step := s.ID
	if step == "" {
		step = "step_" + strconv.Itoa(s.Index)
	}
	return fmt.Sprintf("run=%s; job=%s; step=%s; repeat=%d; attempt=%d",
		url.QueryEscape(s.RunID), url.QueryEscape(s.JobID), url.QueryEscape(step), s.Repeat, s.Attempt)
}
