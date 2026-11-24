package shell

import (
	"os"
	"testing"
	"time"
)

// waitGone reports whether the group led by pid is gone within a second.
func waitGone(pid int) bool {
	deadline := time.Now().Add(time.Second)
	for groupAlive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

func TestStopStarted(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		// The shell stays as the leader with sleep under it.
		{name: "running", cmd: "sleep 60 & wait"},
		// The shell exits and is reaped at once, leaving sleep in its group.
		{name: "leader gone", cmd: "sleep 60 & exit 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Req{Cmd: tt.cmd, Shell: "/bin/sh", Timeout: "5s", Background: true}
			result, err := req.Do()
			if err != nil {
				t.Fatalf("Do() error: %v", err)
			}
			cleanupBackground(t, result.Res.PID, result.Res.Log)
			if tt.name == "leader gone" {
				time.Sleep(200 * time.Millisecond)
			}

			StopStarted()

			if !waitGone(result.Res.PID) {
				t.Errorf("process group %d is still running", result.Res.PID)
			}
			if _, err := os.Stat(result.Res.Log); !os.IsNotExist(err) {
				t.Errorf("log %s was not removed: %v", result.Res.Log, err)
			}
		})
	}
}

func TestStopStartedForgetsStopped(t *testing.T) {
	// A stopped command is dropped, so a second stop has nothing to do.
	req := &Req{Cmd: "sleep 60", Shell: "/bin/sh", Timeout: "5s", Background: true}
	result, err := req.Do()
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	cleanupBackground(t, result.Res.PID, result.Res.Log)

	StopStarted()
	started.Lock()
	n := len(started.procs)
	started.Unlock()
	if n != 0 {
		t.Errorf("%d commands still recorded after StopStarted", n)
	}
}
