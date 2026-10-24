package actionrpc

import (
	"context"
	"path/filepath"
	"sync/atomic"

	"github.com/hashicorp/go-hclog"
)

// pluginLog is the log go-plugin is given for the process that serves an
// action. go-plugin logs through it what it does itself, as it starts the
// process, connects to it and stops it, and, through the logger it names
// after the process, the records the action logs. Its own are at trace
// level, whatever level go-plugin gives them, as they tell how the action is
// run rather than what it did: a verbose run then shows the action's records
// without them, and one that lets trace records through shows them too, for
// debugging an action. Its warnings and errors keep their levels, apart from
// those that say nothing the step's error does not.
//
// The action's records keep the levels the action gave them, as
// actionRecords passes them on.
type pluginLog struct {
	hclog.Logger
	// process is the name go-plugin gives the logger of the action's
	// records, that of the executable it starts; empty for a logger that
	// has none, such as the one go-plugin is given in the action's process.
	process string
	// ctx is done once the action is to be stopped. That it exited by the
	// signal it was sent then is what the runner meant to happen.
	ctx context.Context
	// callFailed is set once the call failed; nil is a call that has not.
	callFailed *atomic.Bool
}

// newPluginLog returns the log go-plugin is given for the process path, which
// is stopped once ctx is done, of a call that sets callFailed once it fails.
// log is where the records go.
func newPluginLog(ctx context.Context, log hclog.Logger, path string, callFailed *atomic.Bool) pluginLog {
	return pluginLog{Logger: log, process: filepath.Base(path), ctx: ctx, callFailed: callFailed}
}

// Log logs a record of go-plugin's own at the level pluginLog gives it.
func (l pluginLog) Log(level hclog.Level, msg string, args ...any) {
	switch level {
	case hclog.Warn:
		l.Warn(msg, args...)
	case hclog.Error:
		l.Error(msg, args...)
	default:
		l.Trace(msg, args...)
	}
}

func (l pluginLog) Debug(msg string, args ...any) { l.Trace(msg, args...) }

func (l pluginLog) Info(msg string, args ...any) { l.Trace(msg, args...) }

// IsDebug and IsInfo report whether a record at those levels is logged, as
// one at trace level, which it is logged as.
func (l pluginLog) IsDebug() bool { return l.IsTrace() }

func (l pluginLog) IsInfo() bool { return l.IsTrace() }

// Error logs go-plugin's record of a process that exited with an error at
// trace level when the process was stopped, as it then exited by the signal
// it was sent. A person would see it otherwise whether or not the step's
// failure says why.
func (l pluginLog) Error(msg string, args ...any) {
	if msg == "plugin process exited" && l.ctx != nil && l.ctx.Err() != nil {
		l.Trace(msg, args...)
		return
	}
	l.Logger.Error(msg, args...)
}

// Warn logs go-plugin's record of a process it could not shut down
// gracefully, and so killed, at trace level once the call failed: it makes
// one as well of a process that had exited already, as one that crashed or
// did not start has, and the error tells what became of the action. One
// that hangs when it is stopped after a call that succeeded is still told.
func (l pluginLog) Warn(msg string, args ...any) {
	if msg == "plugin failed to exit gracefully" && l.callFailed != nil && l.callFailed.Load() {
		l.Trace(msg, args...)
		return
	}
	l.Logger.Warn(msg, args...)
}

// Named returns the logger go-plugin logs the action's records through when
// name is that of the process, and one of go-plugin's own otherwise, such
// as the one it names stdio.
func (l pluginLog) Named(name string) hclog.Logger {
	if l.process != "" && name == l.process {
		// The runner's log is named after the action already.
		return actionRecords{Logger: l.Logger}
	}
	named := l
	named.Logger = l.Logger.Named(name)
	named.process = ""
	return named
}

// ResetNamed is Named for go-plugin's own records.
func (l pluginLog) ResetNamed(name string) hclog.Logger {
	named := l
	named.Logger = l.Logger.ResetNamed(name)
	named.process = ""
	return named
}

// With is With for go-plugin's own records.
func (l pluginLog) With(args ...any) hclog.Logger {
	with := l
	with.Logger = l.Logger.With(args...)
	return with
}

// actionRecords passes on the records an action logs, which go-plugin reads
// from its stderr, at the levels the action gave them, leaving out the
// timestamp go-plugin adds to each, which would only say again when the
// record came.
type actionRecords struct {
	hclog.Logger
}

func (l actionRecords) Log(level hclog.Level, msg string, args ...any) {
	l.Logger.Log(level, msg, withoutTimestamp(args)...)
}

func (l actionRecords) Trace(msg string, args ...any) {
	l.Logger.Trace(msg, withoutTimestamp(args)...)
}

func (l actionRecords) Debug(msg string, args ...any) {
	l.Logger.Debug(msg, withoutTimestamp(args)...)
}

func (l actionRecords) Info(msg string, args ...any) {
	l.Logger.Info(msg, withoutTimestamp(args)...)
}

func (l actionRecords) Warn(msg string, args ...any) {
	l.Logger.Warn(msg, withoutTimestamp(args)...)
}

func (l actionRecords) Error(msg string, args ...any) {
	l.Logger.Error(msg, withoutTimestamp(args)...)
}

// withoutTimestamp returns the fields of a record go-plugin read from an
// action without the timestamp it adds as their last; the fields of one
// that has none, such as a line that is not a record, as they are.
func withoutTimestamp(args []any) []any {
	if n := len(args); n >= 2 && n%2 == 0 && args[n-2] == "timestamp" {
		return args[:n-2]
	}
	return args
}
