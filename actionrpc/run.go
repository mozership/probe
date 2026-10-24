package actionrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Run starts the action named name from the running probe binary, calls it
// once with the given parameters, and stops it. The action's own log records
// reach log, which decides what of them is shown.
func Run(name string, with map[string]any, log hclog.Logger) (map[string]any, error) {
	result, _, err := RunStep(name, Call{With: with}, log)
	return result, err
}

// StopGrace is how long an action whose call is given up on is given to exit
// on SIGTERM before it is killed.
const StopGrace = 2 * time.Second

// RunStep is Run for a step: the action is told about the step and given the
// state it left in the job, and the state it leaves is returned, which is nil
// when it keeps none.
func RunStep(name string, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	return RunStepContext(context.Background(), name, call, log)
}

// RunStepContext is RunStep that gives up on the action once ctx is done:
// the action is sent SIGTERM, and killed if it has not exited StopGrace
// later, and ctx's error is returned.
func RunStepContext(ctx context.Context, name string, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	return run(ctx, command(ctx, os.Args[0], BuiltinCmd, name), nil, call, log)
}

// RunExecutable starts the action served by the executable at path, calls it
// once with the given parameters, and stops it, as Run does for a built-in
// one. When sum is not empty, the executable is started only if its SHA-256
// digest is sum.
func RunExecutable(path string, sum []byte, with map[string]any, log hclog.Logger) (map[string]any, error) {
	result, _, err := RunExecutableStep(path, sum, Call{With: with}, log)
	return result, err
}

// RunExecutableStep is RunExecutable for a step, as RunStep is Run for one.
func RunExecutableStep(path string, sum []byte, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	return RunExecutableStepContext(context.Background(), path, sum, call, log)
}

// RunExecutableStepContext is RunExecutableStep that gives up on the action
// once ctx is done, as RunStepContext does.
func RunExecutableStepContext(ctx context.Context, path string, sum []byte, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	var secure *plugin.SecureConfig
	if len(sum) > 0 {
		secure = &plugin.SecureConfig{Checksum: sum, Hash: sha256.New()}
	}
	return run(ctx, command(ctx, path), secure, call, log)
}

// command returns the command that serves an action, which is stopped once
// ctx is done. It is sent SIGTERM rather than killed, so that it can stop
// what it started, such as the background commands of the shell action, and
// is killed if it has not exited StopGrace later.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = StopGrace
	return cmd
}

// CleanupExecutable starts the action served by the executable at path, as
// RunExecutable does, has it clean up after the run that call names, and
// stops it. The action is stopped where it is once ctx is done.
func CleanupExecutable(ctx context.Context, path string, sum []byte, call CleanupCall, log hclog.Logger) error {
	var secure *plugin.SecureConfig
	if len(sum) > 0 {
		secure = &plugin.SecureConfig{Checksum: sum, Hash: sha256.New()}
	}
	// The process ends with ctx as well, so that one that does not come up
	// is not waited for any longer than one that does not answer.
	cl, action, err := start(exec.CommandContext(ctx, path), secure, log, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer cl.Kill()
	return action.Cleanup(ctx, call)
}

func run(ctx context.Context, cmd *exec.Cmd, secure *plugin.SecureConfig, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	stderr := &actionStderr{}
	exit := &exitStatus{}
	cl, action, err := start(cmd, secure, stoppedLogger{Logger: log, ctx: ctx, exit: exit}, stderr)
	if err != nil {
		if over(ctx) {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &StartError{Output: stderr.reason(), Err: err}
	}
	defer func() {
		// Kill would have the action shut down without a signal, before the
		// SIGTERM it is sent once ctx is done has reached it.
		if over(ctx) {
			awaitExit(cl)
		}
		cl.Kill()
	}()

	result, newState, err := action.RunStepContext(ctx, call)
	if err != nil {
		if over(ctx) {
			return nil, nil, ctx.Err()
		}
		return nil, nil, exitError(cl, err, stderr, exit)
	}

	return result, newState, nil
}

// ExitError is the error of an action whose process exited before it
// returned, as one that panics does. What the call failed with tells only
// that the connection was lost, so the error tells how the process exited
// and what it wrote to stderr that is not one of its log records, such as
// the message of the panic.
type ExitError struct {
	// Status is how the process exited, such as "exit status 2", or empty
	// when it is not known.
	Status string
	// Output is the first lines the process wrote to stderr that are not
	// its log records, such as the message a panic starts with. It is empty
	// when it wrote none.
	Output string
	// Err is what the call failed with.
	Err error
}

func (e *ExitError) Error() string {
	msg := "the action exited before it returned"
	if e.Status != "" {
		msg += " (" + e.Status + ")"
	}
	if e.Output != "" {
		msg += ": " + e.Output
	}
	return msg
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

// exitWait is how long a call that lost its connection waits to see whether
// the action's process exited.
const exitWait = time.Second

// exitError returns err, the error of a call of the action cl serves, as an
// ExitError when the call failed for the process having exited, and as it is
// otherwise.
func exitError(cl *plugin.Client, err error, stderr *actionStderr, exit *exitStatus) error {
	// A lost connection is Unavailable. An action may return the code too:
	// one that marks its errors says so, and of one that does not it is the
	// process having exited that tells.
	if status.Code(err) != codes.Unavailable || fromAction(err) {
		return err
	}
	deadline := time.Now().Add(exitWait)
	for !cl.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !cl.Exited() {
		return err
	}
	return &ExitError{Status: exit.get(), Output: stderr.reason(), Err: err}
}

// StartError is the error of an action whose process could not be started
// or connected to.
type StartError struct {
	// Output is the first lines the process wrote to stderr that are not
	// its log records, which may say why it did not start. It is empty when
	// it wrote none.
	Output string
	// Err is what starting it failed with, as go-plugin tells it.
	Err error
}

func (e *StartError) Error() string {
	msg := "the action did not start"
	if e.Output != "" {
		msg += ": " + e.Output
	}
	return msg + "\n" + e.Err.Error()
}

func (e *StartError) Unwrap() error {
	return e.Err
}

// over reports whether ctx is done, waiting for it to be once its deadline
// has passed: gRPC gives up on a call by the deadline, and can do so before
// ctx's own timer has gone off, when ctx.Err is still nil.
func over(ctx context.Context) bool {
	if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
		<-ctx.Done()
	}
	return ctx.Err() != nil
}

// stoppedLogger is the log go-plugin is given for an action that is stopped
// once ctx is done. That the action exited by the signal it was sent then is
// what the runner meant to happen, so it is not logged as an error, which a
// person would see whether or not the step's failure says why.
//
// It also keeps in exit how the process exited, when it exited with an
// error, for the error of a call that the process did not return from.
type stoppedLogger struct {
	hclog.Logger
	ctx  context.Context
	exit *exitStatus
}

func (l stoppedLogger) Error(msg string, args ...any) {
	// The message is go-plugin's, for a process that exited with an error.
	if msg == "plugin process exited" {
		l.exit.set(args)
		if l.ctx.Err() != nil {
			l.Debug(msg, args...)
			return
		}
	}
	l.Logger.Error(msg, args...)
}

// Warn leaves out go-plugin's record of a process it could not shut down
// gracefully, and so killed, which it also makes of one that had already
// exited, as one that failed to start or crashed has: the step's error
// tells what became of the action.
func (l stoppedLogger) Warn(msg string, args ...any) {
	if msg == "plugin failed to exit gracefully" {
		l.Debug(msg, args...)
		return
	}
	l.Logger.Warn(msg, args...)
}

// exitStatus is how the process that served an action exited, as go-plugin
// logs it.
type exitStatus struct {
	mu     sync.Mutex
	status string
}

// set keeps the error among args, the arguments of go-plugin's record of
// the process having exited.
func (e *exitStatus) set(args []any) {
	if e == nil {
		return
	}
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == "error" {
			e.mu.Lock()
			e.status = fmt.Sprint(args[i+1])
			e.mu.Unlock()
			return
		}
	}
}

func (e *exitStatus) get() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// maxStderr is how much an action writes to stderr apart from its log
// records that is kept to tell why it exited.
const maxStderr = 64 << 10

// maxReasonLines is how many lines of what an action wrote to stderr the
// error of one that exited tells.
const maxReasonLines = 10

// actionStderr is what the process that serves an action writes to stderr
// apart from its log records, to tell why it exited. go-plugin hands it on
// in two parts: what reaches the process's stderr itself, as the Go
// runtime's account of a panic does, and what the action writes to
// os.Stderr, which go-plugin sends over the connection instead.
type actionStderr struct {
	fd     stderrOutput
	synced stderrOutput
}

// reason returns why the process exited as it tells on stderr, from the
// first of the two parts that says anything: the Go runtime's account of a
// panic says it best.
func (a *actionStderr) reason() string {
	if r := a.fd.reason(); r != "" {
		return r
	}
	return a.synced.reason()
}

// stderrOutput keeps what is written to it apart from log records, which
// are JSON, one to a line. go-plugin writes it here, whether or not its log
// shows it.
type stderrOutput struct {
	mu   sync.Mutex
	line []byte
	kept []byte
}

func (o *stderrOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, b := range p {
		if b != '\n' {
			o.line = append(o.line, b)
			continue
		}
		o.keep()
	}
	return len(p), nil
}

// keep keeps the line written so far, unless it is a log record or there is
// no room left for it. Callers hold o.mu.
func (o *stderrOutput) keep() {
	line := o.line
	o.line = o.line[:0]
	if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed) {
		return
	}
	if len(o.kept)+len(line)+1 > maxStderr {
		return
	}
	o.kept = append(o.kept, line...)
	o.kept = append(o.kept, '\n')
}

// reason returns the first paragraph of what was kept, as a Go panic starts
// with its message and then, after a blank line, tells where it happened;
// at most maxReasonLines lines of it.
func (o *stderrOutput) reason() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	text := string(o.kept) + string(o.line)
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if len(lines) == maxReasonLines {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// awaitExit waits for the process that serves an action to exit, as one sent
// SIGTERM by command does, by StopGrace or by being killed then. It gives up
// a little later, for Kill to see to the process.
func awaitExit(cl *plugin.Client) {
	deadline := time.Now().Add(StopGrace + time.Second)
	for !cl.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// start starts the process that serves an action, and returns it along with
// the action to call. The caller stops the process with Kill.
func start(cmd *exec.Cmd, secure *plugin.SecureConfig, log hclog.Logger, stderr *actionStderr) (*plugin.Client, *Client, error) {
	var fd, synced io.Writer
	if stderr != nil {
		fd, synced = &stderr.fd, &stderr.synced
	}
	cl := plugin.NewClient(&plugin.ClientConfig{
		Stderr:           fd,
		SyncStderr:       synced,
		HandshakeConfig:  Handshake,
		Plugins:          PluginMap,
		Cmd:              cmd,
		SecureConfig:     secure,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		Logger:           log,
		UnixSocketConfig: &plugin.UnixSocketConfig{
			TempDir: os.TempDir(),
		},
	})

	protocol, err := cl.Client()
	if err != nil {
		cl.Kill()
		return nil, nil, err
	}

	raw, err := protocol.Dispense("actions")
	if err != nil {
		cl.Kill()
		return nil, nil, err
	}
	return cl, raw.(*Client), nil
}
