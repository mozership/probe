// Command failing-action serves an action that fails in the way with.mode
// names, for tests of how a runner tells why an action failed:
//
//   - error returns an error, after logging it and a warning
//   - status returns a gRPC status of its own, NotFound
//   - unavailable returns a gRPC status of its own, Unavailable, which a
//     lost connection has as well
//   - panic panics
//   - exit writes why to stderr and exits with 3
//
// Its cleanup fails the same way for the run named "error" or "panic".
package main

import (
	"errors"
	"os"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testAction struct{ log hclog.Logger }

func (a testAction) Run(with map[string]any) (map[string]any, error) {
	switch with["mode"] {
	case "error":
		a.log.Warn("the widget is old")
		a.log.Error("widget failed", "error", "the widget broke")
		return nil, errors.New("the widget broke")
	case "status":
		return nil, status.Error(codes.NotFound, "no such widget")
	case "unavailable":
		return nil, status.Error(codes.Unavailable, "the widget is down")
	case "panic":
		panic("the widget exploded")
	case "exit":
		// go-plugin sends what is written to os.Stderr over the connection,
		// which is lost on exit; what is written to the descriptor itself
		// reaches the runner as a panic does.
		_, _ = syscall.Write(2, []byte("the widget is gone\n"))
		os.Exit(3)
	}
	return map[string]any{"req": with, "res": map[string]any{}, "status": 0}, nil
}

func (a testAction) Cleanup(call sdk.CleanupCall) error {
	switch call.RunID {
	case "error":
		return errors.New("the widget would not go")
	case "panic":
		panic("the widget exploded while cleaning up")
	}
	return nil
}

func main() {
	sdk.Serve(func(log hclog.Logger) sdk.Action { return testAction{log: log} })
}
