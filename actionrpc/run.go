package actionrpc

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
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
	cl, action, err := start(exec.CommandContext(ctx, path), secure, log)
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
	cl, action, err := start(cmd, secure, stoppedLogger{Logger: log, ctx: ctx})
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
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}

	return result, newState, nil
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
type stoppedLogger struct {
	hclog.Logger
	ctx context.Context
}

func (l stoppedLogger) Error(msg string, args ...any) {
	// The message is go-plugin's, for a process that exited with an error.
	if msg == "plugin process exited" && l.ctx.Err() != nil {
		l.Debug(msg, args...)
		return
	}
	l.Logger.Error(msg, args...)
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
func start(cmd *exec.Cmd, secure *plugin.SecureConfig, log hclog.Logger) (*plugin.Client, *Client, error) {
	cl := plugin.NewClient(&plugin.ClientConfig{
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
