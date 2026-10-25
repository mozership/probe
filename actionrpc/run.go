package actionrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Run starts the action named name from the running probe binary, calls it
// once with the given parameters, and stops it. The action's own log records
// reach log at the levels the action gave them, and go-plugin's records of
// starting, calling and stopping the process at trace level; log decides
// what of them is shown.
func Run(name string, with map[string]any, log hclog.Logger) (map[string]any, error) {
	result, _, err := RunStep(name, Call{With: with}, log)
	return result, err
}

// TermGrace is how long an action whose call is given up on is given to exit
// on SIGTERM before it is killed.
const TermGrace = 2 * time.Second

// ExitWait is how long, at most, a step call that is given up on takes to
// return once its context is done: TermGrace for the action to exit on
// SIGTERM or be killed, and a second more for awaitExit to see it gone.
// That relies on command setting WaitDelay to TermGrace, so the process is
// already dead when Kill runs and its own graceful wait does not apply.
const ExitWait = TermGrace + time.Second

// RunStep is Run for a step: the action is told about the step and given the
// state it left in the job, and the state it leaves is returned, which is nil
// when it keeps none.
func RunStep(name string, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	return RunStepContext(context.Background(), name, call, log)
}

// RunStepContext is RunStep that gives up on the action once ctx is done:
// the action is sent SIGTERM, and killed if it has not exited TermGrace
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
// is killed if it has not exited TermGrace later.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = TermGrace
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
	p := &process{cmd: exec.CommandContext(ctx, path)}
	cl, action, err := p.start(ctx, secure, log)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer cl.Kill()
	if err := action.Cleanup(ctx, call); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return p.failed(cl, err)
	}
	return nil
}

func run(ctx context.Context, cmd *exec.Cmd, secure *plugin.SecureConfig, call Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	p := &process{cmd: cmd}
	cl, action, err := p.start(ctx, secure, log)
	if err != nil {
		if over(ctx) {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
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
			p.callFailed.Store(true)
			return nil, nil, ctx.Err()
		}
		return nil, nil, p.failed(cl, err)
	}

	return result, newState, nil
}

// process is the process that serves an action for one call.
type process struct {
	cmd *exec.Cmd
	// stderr keeps what the process writes to stderr apart from its log
	// records, which may tell why it exited or did not start.
	stderr stderrOutput
	// callFailed is set once the call failed, or the process did not start:
	// what go-plugin logs of stopping it then says nothing the error does
	// not.
	callFailed atomic.Bool
}

// start starts the process, and returns it along with the action to call.
// The caller stops the process with Kill. An error is a StartError, unless
// ctx is done.
func (p *process) start(ctx context.Context, secure *plugin.SecureConfig, log hclog.Logger) (*plugin.Client, *Client, error) {
	cl := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          PluginMap,
		Cmd:              p.cmd,
		SecureConfig:     secure,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		Logger:           newPluginLog(ctx, log, p.cmd.Path, &p.callFailed),
		Stderr:           &p.stderr,
		UnixSocketConfig: &plugin.UnixSocketConfig{
			TempDir: os.TempDir(),
		},
	})

	raw, err := func() (any, error) {
		protocol, err := cl.Client()
		if err != nil {
			return nil, err
		}
		return protocol.Dispense("actions")
	}()
	if err != nil {
		p.callFailed.Store(true)
		cl.Kill()
		// Kill waits for the process to exit, so all it wrote is kept.
		return nil, nil, &StartError{Output: p.stderr.reason(), Err: err}
	}
	return cl, raw.(*Client), nil
}

// failed returns err, the error of a call of the action cl serves, as an
// ExitError when the call failed for the process having exited, and as it is
// otherwise.
func (p *process) failed(cl *plugin.Client, err error) error {
	p.callFailed.Store(true)
	// A lost connection is Unavailable. An action may return the code too:
	// one that marks its errors says so, and of one that does not it is the
	// process having exited that tells.
	if status.Code(err) != codes.Unavailable || fromAction(err) {
		return err
	}
	deadline := time.Now().Add(exitWait)
	for !cl.Exited() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !cl.Exited() {
		return err
	}
	// go-plugin sets that the process exited once it has waited for it,
	// and so once its state is known, under the lock Exited takes.
	exitStatus := ""
	if p.cmd.ProcessState != nil {
		exitStatus = p.cmd.ProcessState.String()
	}
	return &ExitError{Status: exitStatus, Output: p.stderr.reason(), Err: err}
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
// the action's process exited, which one that crashed does at once. Only an
// action built against a version of this package from before its errors
// were marked as its own is waited for when it returns Unavailable itself.
const exitWait = 250 * time.Millisecond

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

// maxStderr is how much an action writes to stderr apart from its log
// records that is kept to tell why it exited.
const maxStderr = 64 << 10

// maxStderrLine is how much of a line an action writes to stderr is kept.
const maxStderrLine = 4 << 10

// maxReasonLines is how many lines of what an action wrote to stderr the
// error of one that exited tells.
const maxReasonLines = 10

// stderrOutput keeps what the process that serves an action writes to
// stderr apart from its log records, which are JSON, one to a line: what is
// left is what the Go runtime writes of a panic, or what the action wrote
// to the descriptor itself. go-plugin writes it here, whether or not its
// log shows it. What the action writes to os.Stderr is not among it, as
// go-plugin sends that over the connection, which is lost on exit.
type stderrOutput struct {
	mu   sync.Mutex
	line []byte
	kept []byte
}

func (o *stderrOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			o.keep()
			continue
		}
		// The rest of a line too long to keep is dropped.
		if len(o.line) < maxStderrLine {
			o.line = append(o.line, b)
		}
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
// SIGTERM by command does, by TermGrace or by being killed then. It gives up
// at ExitWait, a little later, for Kill to see to the process.
func awaitExit(cl *plugin.Client) {
	deadline := time.Now().Add(ExitWait)
	for !cl.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
