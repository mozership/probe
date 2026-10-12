package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mozership/probe/mapping"
)

type Req struct {
	Cmd        string            `map:"cmd" validate:"required"`
	Shell      string            `map:"shell"`
	Workdir    string            `map:"workdir"`
	Timeout    string            `map:"timeout"`
	Env        map[string]string `map:"env"`
	Background bool              `map:"background"`
	// Ready says when a background command is ready for the steps after
	// it: its key log names text the command writes once it is.
	Ready map[string]string `map:"ready"`
	cb    *Callback
}

type Res struct {
	Code   int    `map:"code"`
	Stdout string `map:"stdout"`
	Stderr string `map:"stderr"`
	PID    int    `map:"pid"`
	Log    string `map:"log"`
	// TimedOut is true when the command was stopped at the timeout. Code
	// is then -1, and Stdout and Stderr hold what it wrote until then.
	TimedOut bool `map:"timed_out"`
}

// outputGrace is how long a command's output is still read once the command
// has exited or been stopped at its timeout. A process the command left
// running can hold the output open long after that; it is not waited for.
var outputGrace = time.Second

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

type shellParams struct {
	cmd     string
	workdir string
	shell   string
	timeout time.Duration
	env     map[string]string
	// readyLog is the text a background command writes once it is ready,
	// or empty when the step does not wait for it.
	readyLog string
}

type Option func(*Callback)

type Callback struct {
	before func(cmd string, shell string, workdir string)
	after  func(result *Result)
}

func NewReq() *Req {
	return &Req{
		Shell:   "/bin/sh",
		Timeout: "30s",
		Env:     make(map[string]string),
	}
}

func parseParams(req *Req) (*shellParams, error) {
	params := &shellParams{
		cmd:     req.Cmd,
		workdir: req.Workdir,
		shell:   req.Shell,
		env:     req.Env,
	}

	// Validate required parameters
	if params.cmd == "" {
		return nil, fmt.Errorf("cmd parameter is required")
	}

	// Set default shell
	if params.shell == "" {
		params.shell = "/bin/sh"
	}

	// Validate shell path for security
	if err := validateShellPath(params.shell); err != nil {
		return nil, err
	}

	// Parse timeout
	timeoutStr := req.Timeout
	if timeoutStr == "" {
		timeoutStr = "30s"
	}
	timeout, err := parseTimeout(timeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid timeout format: %s", timeoutStr)
	}
	params.timeout = timeout

	// Validate working directory if provided
	if params.workdir != "" {
		if err := validateWorkdir(params.workdir); err != nil {
			return nil, err
		}
	}

	// The mapping leaves an empty map where ready is not given.
	if len(req.Ready) > 0 {
		if !req.Background {
			return nil, fmt.Errorf("ready is for a background command, and background is not true")
		}
		for k := range req.Ready {
			if k != "log" {
				return nil, fmt.Errorf("ready takes log, not %s", k)
			}
		}
		if req.Ready["log"] == "" {
			return nil, fmt.Errorf("ready.log is required: the text the command writes once it is ready")
		}
		params.readyLog = req.Ready["log"]
	}

	return params, nil
}

func validateShellPath(shell string) error {
	// Check if shell path is empty
	if shell == "" {
		return fmt.Errorf("shell path cannot be empty")
	}

	// Only allow common shell paths for security
	allowedShells := []string{
		"/bin/sh",
		"/bin/bash",
		"/bin/zsh",
		"/bin/dash",
		"/usr/bin/sh",
		"/usr/bin/bash",
		"/usr/bin/zsh",
		"/usr/bin/dash",
	}

	if slices.Contains(allowedShells, shell) {
		return nil
	}

	return fmt.Errorf("shell path not allowed: %s", shell)
}

func validateWorkdir(workdir string) error {
	// Convert relative path to absolute path
	absPath, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("failed to resolve workdir path: %s", err)
	}

	// Check if directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("workdir does not exist: %s", absPath)
	}

	return nil
}

func parseTimeout(timeoutStr string) (time.Duration, error) {
	// Check if it's a plain number (treat as seconds)
	if matched, _ := regexp.MatchString(`^\d+$`, timeoutStr); matched {
		if seconds, err := strconv.Atoi(timeoutStr); err == nil {
			return time.Duration(seconds) * time.Second, nil
		}
	}

	// Parse as duration string (e.g., "30s", "5m", "1h")
	return time.ParseDuration(timeoutStr)
}

func (r *Req) Do() (*Result, error) {
	// Always create result with current request data, even if validation fails
	result := &Result{Req: *r}

	if r.Cmd == "" {
		return result, fmt.Errorf("Req.Cmd is required")
	}

	params, err := parseParams(r)
	if err != nil {
		return result, err
	}

	// callback before
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(params.cmd, params.shell, params.workdir)
	}

	// Create command with appropriate context
	ctx := context.Background()
	if !r.Background {
		// For synchronous execution, use timeout context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, params.timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, params.shell, "-c", params.cmd)

	// Set working directory
	if params.workdir != "" {
		absWorkdir, _ := filepath.Abs(params.workdir)
		cmd.Dir = absWorkdir
	}

	// Set environment variables
	cmd.Env = os.Environ()
	for key, value := range params.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// For background execution, configure SysProcAttr to create new session
	if r.Background {
		if runtime.GOOS != "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true, // Create new session (also creates new process group)
			}
		}
	}

	start := time.Now()

	// For background execution, setup log file and start process
	if r.Background {
		// Every run gets a file of its own: naming it after the command made
		// two runs of the same command write into one file. The workflow
		// removes it once the run is over.
		logFile, err := os.CreateTemp("", "probe-shell-action.*.log")
		if err != nil {
			return result, fmt.Errorf("failed to create log file: %w", err)
		}
		logPath := logFile.Name()

		// Redirect both stdout and stderr to the same log file
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		// Start the command and record it in one go, so that StopStarted,
		// which takes the same lock, never runs in between.
		started.Lock()
		if err := cmd.Start(); err != nil {
			started.Unlock()
			_ = logFile.Close()
			_ = os.Remove(logPath)
			return result, fmt.Errorf("failed to start command: %w", err)
		}
		proc := &startedProc{pid: cmd.Process.Pid, log: logPath}
		started.procs = append(started.procs, proc)
		started.Unlock()

		// Close the log file when the process exits, and tell a wait for
		// readiness that it did.
		exited := make(chan struct{})
		exitCode := 0
		go func() {
			_ = cmd.Wait()
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			_ = logFile.Close()
			started.Lock()
			proc.exited = true
			started.Unlock()
			close(exited)
		}()

		result.Res = Res{
			Code:   -1, // Indicate background process (not finished)
			Stdout: "",
			Stderr: "",
			PID:    cmd.Process.Pid,
			Log:    logPath,
		}
		result.Status = -1 // Indicate background execution

		if params.readyLog != "" {
			switch waitReady(logPath, params.readyLog, cmd.Process.Pid, exited, params.timeout) {
			case readyExited:
				// exitCode is written before exited is closed.
				result.Res.Code = exitCode
				result.Res.Stdout = readLog(logPath)
				result.Status = 1
			case readyTimedOut:
				stopGroups([]int{cmd.Process.Pid})
				result.Res.TimedOut = true
				result.Res.Stdout = readLog(logPath)
				result.Status = 1
			}
		}
		result.RT = time.Since(start)

		// callback after
		if r.cb != nil && r.cb.after != nil {
			r.cb.after(result)
		}

		return result, nil
	}

	// Capture stdout and stderr for synchronous execution. Buffers rather
	// than pipes let Wait stop reading after outputGrace: a process the
	// command started can keep the output open, and reading it to the end
	// would outlast the timeout.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = outputGrace
	// Whether the timeout stopped the command is recorded where it happens:
	// when the deadline passes after the command exited, while its output is
	// still being read, the kill finds nothing to stop.
	var killed atomic.Bool
	cmd.Cancel = func() error {
		err := cmd.Process.Kill()
		if err == nil {
			killed.Store(true)
		}
		return err
	}

	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("failed to start command: %w", err)
	}
	cmdErr := cmd.Wait()
	result.RT = time.Since(start)

	timedOut := killed.Load()
	exitCode := 0
	var exitError *exec.ExitError
	switch {
	case errors.As(cmdErr, &exitError):
		exitCode = exitError.ExitCode()
	case timedOut:
		exitCode = -1
	case cmd.ProcessState != nil:
		// The command exited, but something it left running still held its
		// output open, and was not waited for; or the deadline passed while
		// that output was being read. Either way the command's own status
		// stands.
		exitCode = cmd.ProcessState.ExitCode()
	case cmdErr != nil:
		return result, fmt.Errorf("command execution failed: %w", cmdErr)
	}

	// Determine status based on exit code (0 = success, 1 = failure)
	status := 1 // default to failure
	if exitCode == 0 {
		status = 0 // success
	}

	result.Res = Res{
		Code:     exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		PID:      cmd.Process.Pid,
		TimedOut: timedOut,
	}
	result.Status = status

	// callback after
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(result)
	}

	return result, nil
}

func Execute(data map[string]any, opts ...Option) (map[string]any, error) {
	// Keep the parameters' types: env arrives as a nested map, and turning it
	// into a string here is what used to drop it before it reached the command.
	m := mapping.EnvToStringValue(data)

	// ready is checked as it was given: the mapping leaves an empty map for
	// one that is missing and drops a value that is not a string, so
	// ready: {} or ready: {log: 123} would run the command without waiting.
	if v, ok := data["ready"]; ok {
		if err := checkReady(v); err != nil {
			return map[string]any{}, err
		}
	}

	r := NewReq()

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	// A parameter that cannot be read stops the step before the command
	// runs: going on would run it with that parameter at its zero value,
	// such as in the foreground for background: maybe.
	if err := mapping.MapToStructByTags(m, r); err != nil {
		return map[string]any{}, err
	}

	result, err := r.Do()
	if err != nil {
		// Even on error, try to return a structured result if we have one
		if result != nil {
			if mapResult, structErr := mapping.StructToMapByTags(result); structErr == nil {
				return mapResult, err
			}
		}
		return map[string]any{}, err
	}

	mapResult, err := mapping.StructToMapByTags(result)
	if err != nil {
		return map[string]any{}, err
	}

	// Return the result directly without flattening
	return mapResult, nil
}

// checkReady checks the ready parameter as a step gave it: a map whose only
// key is log, holding the text to wait for.
func checkReady(v any) error {
	ready, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("ready must be a map with log, the text the command writes once it is ready, not %T", v)
	}
	for k := range ready {
		if k != "log" {
			return fmt.Errorf("ready takes log, not %s", k)
		}
	}
	log, ok := ready["log"]
	if !ok {
		return fmt.Errorf("ready.log is required: the text the command writes once it is ready")
	}
	s, ok := log.(string)
	if !ok {
		return fmt.Errorf("ready.log must be a string, not %T: quote it, as log: \"8080\"", log)
	}
	if s == "" {
		return fmt.Errorf("ready.log is required: the text the command writes once it is ready")
	}
	return nil
}

func WithBefore(f func(cmd string, shell string, workdir string)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(result *Result)) Option {
	return func(c *Callback) {
		c.after = f
	}
}
