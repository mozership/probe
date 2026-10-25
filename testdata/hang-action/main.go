// Command hang-action serves an action that never returns, for tests of how
// a runner gives up on one. It writes its process ID to the file with.pidfile
// names, and on SIGTERM writes the file next to it with ".term" added before
// it exits. Its cleanup never returns either, and takes the run ID as the
// file to write its process ID to.
package main

import (
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

var (
	mu      sync.Mutex
	pidfile string
)

type action struct{}

func (action) Run(with map[string]any) (map[string]any, error) {
	path, _ := with["pidfile"].(string)
	if err := hang(path); err != nil {
		return nil, err
	}
	return nil, nil
}

func (action) Cleanup(call actionrpc.CleanupCall) error {
	return hang(call.RunID)
}

// hang writes the process ID to the file at path and never returns, unless
// it cannot write it.
func hang(path string) error {
	mu.Lock()
	pidfile = path
	mu.Unlock()
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	select {}
}

func main() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		mu.Lock()
		path := pidfile
		mu.Unlock()
		if path != "" {
			_ = os.WriteFile(path+".term", nil, 0o644)
		}
		os.Exit(1)
	}()

	actionrpc.Serve(func(hclog.Logger) actionrpc.Action { return action{} })
}
