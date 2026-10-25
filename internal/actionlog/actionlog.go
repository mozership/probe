// Package actionlog shows the records of an action's process, those the
// action logs and those go-plugin logs of running it, as the verbose output
// of a step is laid out, with the secrets of the run hidden.
package actionlog

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/dump"
	"github.com/mozership/probe/mask"
)

// Logger is the log the records of an action's process are shown
// through: those the action logs, and those go-plugin logs of starting,
// calling and stopping the process, which come at trace level. Each is
// written as the verbose output of a step is, its level first and then the
// action it is of, with its fields under it as YAML-like lines, as the
// request and the response are:
//
//	[DEBUG] ./my-action: retrying the request
//	[DEBUG]   attempt: 2
//	[DEBUG]   headers:
//	[DEBUG]     Accept: */*
//
// Secrets are hidden in what it writes.
type Logger struct {
	// name is that of the action, as the step's uses writes it, and of the
	// logger go-plugin names in it after it, as name.stdio.
	name  string
	level hclog.Level
	// verbose shows the records at error level, which the step's error
	// would otherwise say again.
	verbose bool
	masker  *mask.Masker
	out     io.Writer
	// mu is shared by the loggers named in this one, which go-plugin logs
	// through at once, so that their lines are not mixed.
	mu      *sync.Mutex
	implied []any
}

var _ hclog.Logger = (*Logger)(nil)

// New returns the log of the action name, which writes the records
// at level and above to out, hiding the secrets masker knows. Records at
// error level are written only when verbose is set.
func New(name string, out io.Writer, masker *mask.Masker, level hclog.Level, verbose bool) *Logger {
	return &Logger{name: name, level: level, verbose: verbose, masker: masker, out: out, mu: &sync.Mutex{}}
}

// Level returns the level an action's records are shown at: warn,
// debug in a verbose run, and trace in one that also shows how the runner
// runs the action's process.
func Level(verbose, trace bool) hclog.Level {
	switch {
	case verbose && trace:
		return hclog.Trace
	case verbose:
		return hclog.Debug
	default:
		return hclog.Warn
	}
}

// shows reports whether a record at level is written.
func (l *Logger) shows(level hclog.Level) bool {
	if l.level == hclog.Off || level < l.level {
		return false
	}
	// An action that fails says why in the error it returns, which the
	// step's result shows, and the records would only say it again, as
	// would go-plugin's of a process that exited with an error or of the
	// panic it wrote: the error tells those too. Warnings are kept, as they
	// tell what the result does not.
	return l.verbose || level < hclog.Error
}

func (l *Logger) Log(level hclog.Level, msg string, args ...any) {
	if !l.shows(level) {
		return
	}
	prefix := "[" + strings.ToUpper(level.String()) + "]"
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %s\n", prefix, l.name, msg)
	fields := l.masker.Map(fieldMap(append(append([]any{}, l.implied...), args...)))
	for _, line := range dump.Lines(fields, dump.Options{}) {
		fmt.Fprintf(&b, "%s   %s\n", prefix, line)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.out, l.masker.String(b.String()))
}

// fieldMap returns the fields of a record, given as keys and values in
// turn, as a map. A value without a key is kept under hclog.MissingKey, as
// hclog keeps it.
func fieldMap(args []any) map[string]any {
	if len(args)%2 != 0 {
		args = append(args[:len(args)-1:len(args)-1], hclog.MissingKey, args[len(args)-1])
	}
	fields := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		fields[fmt.Sprint(args[i])] = fieldValue(args[i+1])
	}
	return fields
}

// fieldValue returns what a field's value is laid out as: an error as its
// message, and a value hclog formats as it formats it. A format that does
// not start with a string, which hclog would panic on, is written as its
// values are.
func fieldValue(v any) any {
	switch val := v.(type) {
	case error:
		return val.Error()
	case hclog.Format:
		if len(val) == 0 {
			return ""
		}
		format, ok := val[0].(string)
		if !ok {
			return fmt.Sprint(val...)
		}
		return fmt.Sprintf(format, val[1:]...)
	case fmt.Stringer:
		return val.String()
	}
	return v
}

func (l *Logger) Trace(msg string, args ...any) { l.Log(hclog.Trace, msg, args...) }

func (l *Logger) Debug(msg string, args ...any) { l.Log(hclog.Debug, msg, args...) }

func (l *Logger) Info(msg string, args ...any) { l.Log(hclog.Info, msg, args...) }

func (l *Logger) Warn(msg string, args ...any) { l.Log(hclog.Warn, msg, args...) }

func (l *Logger) Error(msg string, args ...any) { l.Log(hclog.Error, msg, args...) }

func (l *Logger) IsTrace() bool { return l.shows(hclog.Trace) }

func (l *Logger) IsDebug() bool { return l.shows(hclog.Debug) }

func (l *Logger) IsInfo() bool { return l.shows(hclog.Info) }

func (l *Logger) IsWarn() bool { return l.shows(hclog.Warn) }

func (l *Logger) IsError() bool { return l.shows(hclog.Error) }

func (l *Logger) ImpliedArgs() []any { return l.implied }

func (l *Logger) With(args ...any) hclog.Logger {
	with := *l
	with.implied = append(append([]any{}, l.implied...), args...)
	return &with
}

func (l *Logger) Name() string { return l.name }

func (l *Logger) Named(name string) hclog.Logger {
	named := *l
	named.name = l.name + "." + name
	return &named
}

func (l *Logger) ResetNamed(name string) hclog.Logger {
	named := *l
	named.name = name
	return &named
}

func (l *Logger) SetLevel(level hclog.Level) { l.level = level }

func (l *Logger) GetLevel() hclog.Level { return l.level }

func (l *Logger) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return log.New(l.StandardWriter(opts), "", 0)
}

// StandardWriter returns a writer that logs each line written to it, at
// the level opts forces, or at info level.
func (l *Logger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	level := hclog.Info
	if opts != nil && opts.ForceLevel != hclog.NoLevel {
		level = opts.ForceLevel
	}
	return lineWriter(func(line string) { l.Log(level, line) })
}

// lineWriter calls itself with each line written to it, without its
// newline or a carriage return before it. Each write is taken as whole
// lines, as log.Logger makes one write of each record, and a line of any
// length is kept.
type lineWriter func(line string)

func (w lineWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(p), "\n"), "\n") {
		w(strings.TrimSuffix(line, "\r"))
	}
	return len(p), nil
}
