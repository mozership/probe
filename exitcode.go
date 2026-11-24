package probe

import "github.com/mozership/probe/internal/outcome"

// Exit codes of a probe run. They tell apart what a caller such as CI or cron
// has to do about a failure: a failed test points at the system under test,
// a configuration error at the workflow or the command line, and an action
// error at the environment, such as a target that cannot be reached.
const (
	// ExitOK means every job succeeded.
	ExitOK = 0
	// ExitTestFailed means a test did not hold: it evaluated to false, could
	// not be evaluated, or did not evaluate to a boolean; or a request or a
	// response broke the contract an action checked it against.
	ExitTestFailed = 1
	// ExitConfigError means the workflow or the command line was wrong, a
	// step asked for what the guard of the run does not allow, or a report
	// file could not be written, so the run could not do what it was asked
	// to.
	ExitConfigError = 2
	// ExitActionError means an action returned an error, such as a refused
	// connection or a timeout, so a test could not be checked at all.
	ExitActionError = 3
)

// failureConfig is recorded when a job cannot start because its own
// definition is invalid, for example a malformed step ID.
const failureConfig = "config"

// runExitCode maps the failures recorded during a run to an exit code. When
// several kinds occurred, the one a caller has to act on first wins: a broken
// workflow before an unreachable target, and an unreachable target before a
// failed test, since the target being down usually explains the tests.
func runExitCode(rs *outcome.Run, failed bool) int {
	if !failed {
		return ExitOK
	}

	switch {
	case rs.HasFailure(failureConfig), rs.HasFailure(outcome.FailureRefused):
		return ExitConfigError
	case rs.HasFailure(outcome.FailureAction):
		return ExitActionError
	default:
		return ExitTestFailed
	}
}
