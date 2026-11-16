package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

// newCallerEnv is the directory of newCleanupEnv, with the local actions
// ./caller, which calls another with ActionCaller, and ./greet.
func newCallerEnv(t *testing.T) *cleanupEnv {
	t.Helper()
	e := newCleanupEnv(t, cleaningManifest)
	for _, b := range [][]string{{filepath.Join(e.dir, "caller", "caller"), "./testdata/caller-action"}, {filepath.Join(e.dir, "greet", "greet"), "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	e.write(t, "caller/action.yml", "runs:\n  using: binary\n  path: caller\n")
	e.write(t, "greet/action.yml", "runs:\n  using: binary\n  path: greet\n")
	return e
}

// An action calls an external action by the name the workflow gives it,
// and an action of Probe from the probe that runs it, which the CLI names
// in PROBE_EXECUTABLE. An action called that cleans up is called once the
// run is over, once however many times it was called.
func TestEndToEndActionCaller(t *testing.T) {
	e := newCallerEnv(t)
	e.write(t, "workflow.yml", `name: caller
actions:
  greet: ./greet
  left: ./left
jobs:
- name: call
  steps:
  - name: an external action by its name
    uses: ./caller
    with:
      uses: greet
      with:
        name: probe
    test: res.greeting == "hello probe"
  - name: an action of Probe
    uses: ./caller
    with:
      uses: hello
      with:
        name: probe
    test: res.name == "probe"
  - name: an action that cleans up
    uses: ./caller
    with:
      uses: left
      times: 2
      with:
        name: a
    test: res.promised == true
`)
	cmd := e.command()
	out, err := cmd.CombinedOutput()
	if code := exitCode(t, err); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\n%s", code, ExitOK, out)
	}
	left, cleanups := e.result(t)
	if len(left) != 0 {
		t.Errorf("left = %v, want what the action left removed by its cleanup\n%s", left, out)
	}
	if len(cleanups) != 1 {
		t.Errorf("cleanups = %v, want one\n%s", cleanups, out)
	}
}

// Under a guard, an action called is refused when it does not keep to it,
// although the calling action was allowed to run.
func TestEndToEndActionCallerUnderGuard(t *testing.T) {
	e := newCallerEnv(t)
	e.write(t, "workflow.yml", `name: caller
actions:
  greet: ./greet
jobs:
- name: call
  steps:
  - uses: ./caller
    with:
      uses: greet
      with:
        name: probe
`)
	cmd := exec.Command(e.probe, "--read-only", "--allow-action", "./caller", filepath.Join(e.dir, "workflow.yml"))
	out, err := cmd.CombinedOutput()
	if code := exitCode(t, err); code == ExitOK {
		t.Fatalf("exit code = 0, want the step refused\n%s", out)
	}
	if !strings.Contains(string(out), "does not keep to --read-only") {
		t.Errorf("output does not say why the action called was refused:\n%s", out)
	}
}

// An action called is stopped once the context of the call is done.
func TestActionCaller_StopsTheActionOnTimeout(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "hang", "hang"), "./testdata/hang-action").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "hang", "action.yml"), []byte("runs:\n  using: binary\n  path: hang\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(dir, "pid")
	c := NewActionCaller(actionrpc.Call{}, dir, WithCallerLog(hclog.NewNullLogger()))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Call(ctx, "./hang", map[string]any{"pidfile": pidfile})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline of the context", err)
	}
	if d := time.Since(start); d > time.Second+actionrpc.ExitWait {
		t.Errorf("the call returned after %s", d)
	}
	if _, err := os.Stat(pidfile + ".term"); err != nil {
		t.Errorf("the action was not sent SIGTERM: %v", err)
	}
}
