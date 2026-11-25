package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mozership/probe"
	"github.com/mozership/probe/internal/serve"
)

const serveUsage = "Usage: probe serve <serve-file>\n"

// runServe keeps running the workflows of the serve file given, until probe
// is interrupted or terminated. A first signal stops it starting runs and
// waits for those going, up to the grace of the file; a second stops them
// at once.
func (c *Cmd) runServe() int {
	if len(c.SubCommandArgs) != 1 {
		c.usageError(serveUsage, "serve takes one serve file, not %d", len(c.SubCommandArgs))
		return probe.ExitConfigError
	}
	config, warnings, err := serve.Load(c.SubCommandArgs[0])
	if err != nil {
		c.errorf("%v", err)
		return probe.ExitConfigError
	}
	if c.mocking {
		return probe.ExitOK
	}
	exe, err := os.Executable()
	if err != nil {
		c.errorf("the probe to run the workflows with is not found: %v", err)
		return probe.ExitConfigError
	}

	log := slog.New(slog.NewJSONHandler(c.errWriter, nil))
	for _, w := range warnings {
		log.Warn(w)
	}
	s := serve.New(config, exe, log)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		<-signals
		stop()
		<-signals
		s.Abort()
	}()
	s.Serve(ctx)
	return probe.ExitOK
}
