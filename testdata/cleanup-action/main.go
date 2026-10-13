// Command cleanup-action serves an action that leaves something behind in a
// run and removes it once the run is over, the way an action that starts a
// server does. What a step leaves is a file named after the run in the
// directory the environment variable CLEANUP_DIR names, and each cleanup is
// written to cleanup.log there.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

type action struct{}

func (a action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep leaves the file <run ID>.<name> behind, after waiting for as long
// as delay says. res.promised tells whether the runner said it cleans up,
// and the actions that ran names are passed on to it as ones to clean up, as
// by an action that ran steps of its own.
func (action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	if delay, ok := call.With["delay"].(string); ok {
		d, err := time.ParseDuration(delay)
		if err != nil {
			return nil, nil, err
		}
		time.Sleep(d)
	}
	if ran, ok := call.With["ran"].([]any); ok {
		for _, uses := range ran {
			call.Cleanups.Add(fmt.Sprint(uses))
		}
	}
	left := filepath.Join(os.Getenv("CLEANUP_DIR"), fmt.Sprintf("%s.%v", call.Step.RunID, call.With["name"]))
	if err := os.WriteFile(left, nil, 0o644); err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"req":    call.With,
		"res":    map[string]any{"left": left, "promised": call.Cleanups != nil},
		"status": 0,
	}, nil, nil
}

// Cleanup removes the files the steps of the run left, and fails when the
// environment variable CLEANUP_FAIL is set.
func (action) Cleanup(call actionrpc.CleanupCall) error {
	dir := os.Getenv("CLEANUP_DIR")
	log, err := os.OpenFile(filepath.Join(dir, "cleanup.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(log, call.RunID); err != nil {
		return err
	}
	if err := log.Close(); err != nil {
		return err
	}
	if os.Getenv("CLEANUP_FAIL") != "" {
		return errors.New("cannot clean up")
	}
	left, err := filepath.Glob(filepath.Join(dir, call.RunID+".*"))
	if err != nil {
		return err
	}
	for _, name := range left {
		if err := os.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	actionrpc.Serve(func(hclog.Logger) actionrpc.Action { return action{} })
}
