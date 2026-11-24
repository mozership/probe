// Package probe runs and checks Probe workflows from Go, and calls actions
// the way a workflow's steps call them.
package probe

import (
	"io"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/engine"
)

type (
	Probe              = engine.Probe
	Config             = engine.Config
	OutputMode         = engine.OutputMode
	ActionRunner       = engine.ActionRunner
	RunOptions         = engine.RunOptions
	PluginActionRunner = engine.PluginActionRunner
	ActionCaller       = engine.ActionCaller
	CallerOption       = engine.CallerOption
	CallResult         = engine.CallResult
	CheckOptions       = engine.CheckOptions
	Choices            = engine.Choices
	Finding            = engine.Finding
)

const (
	OutputModeAuto    = engine.OutputModeAuto
	OutputModeSpinner = engine.OutputModeSpinner
	OutputModeStream  = engine.OutputModeStream

	ExitOK          = engine.ExitOK
	ExitTestFailed  = engine.ExitTestFailed
	ExitConfigError = engine.ExitConfigError
	ExitActionError = engine.ExitActionError

	SeverityError   = engine.SeverityError
	SeverityWarning = engine.SeverityWarning

	ExecutableEnv = engine.ExecutableEnv
)

// New returns a Probe that runs the workflow at path, printing step details
// when verbose is set.
func New(path string, verbose bool) *Probe {
	return engine.New(path, verbose)
}

// ParseOutputMode reads an output mode as --output takes it.
func ParseOutputMode(s string) (OutputMode, error) {
	return engine.ParseOutputMode(s)
}

// Check reads the workflow at path, a comma-separated list of files, and
// returns what keeps it from running or checking what it seems to.
func Check(path string, opts CheckOptions) ([]Finding, error) {
	return engine.Check(path, opts)
}

// NewActionCaller returns a caller of actions that a step runs with call,
// with local actions relative to baseDir.
func NewActionCaller(call actionrpc.Call, baseDir string, opts ...CallerOption) *ActionCaller {
	return engine.NewActionCaller(call, baseDir, opts...)
}

// WithCallerLogOutput writes the log records of the actions called to w.
func WithCallerLogOutput(w io.Writer) CallerOption {
	return engine.WithCallerLogOutput(w)
}
