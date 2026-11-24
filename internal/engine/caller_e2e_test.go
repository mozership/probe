package engine

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newCallerEnv is the directory of newCleanupEnv, with the local actions
// ./caller, which calls another with sdk.Caller, and ./greet.
func newCallerEnv(t *testing.T) *cleanupEnv {
	t.Helper()
	e := newCleanupEnv(t, cleaningManifest)
	for _, b := range [][]string{{filepath.Join(e.dir, "caller", "caller"), "github.com/mozership/probe/testdata/caller-action"}, {filepath.Join(e.dir, "greet", "greet"), "github.com/mozership/probe/testdata/external-action"}} {
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
func TestEndToEndCaller(t *testing.T) {
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
    test: res.called.greeting == "hello probe"
  - name: an action of Probe
    uses: ./caller
    with:
      uses: hello
      with:
        name: probe
    test: res.called.name == "probe"
  - name: an action that cleans up
    uses: ./caller
    with:
      uses: left
      times: 2
      with:
        name: a
    test: res.calls == 2 && res.called.promised == true
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
func TestEndToEndCallerUnderGuard(t *testing.T) {
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

// The credentials of what an action called is given are hidden in its
// records, although the step of the calling action was not given them.
func TestEndToEndCallerHidesCredentials(t *testing.T) {
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
        password: nested-password-4242
    test: res.called.greeting == "hello probe"
`)
	cmd := exec.Command(e.probe, "-vv", filepath.Join(e.dir, "workflow.yml"))
	out, err := cmd.CombinedOutput()
	if code := exitCode(t, err); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\n%s", code, ExitOK, out)
	}
	if !strings.Contains(string(out), "greeting") {
		t.Fatalf("the records of the action called are not shown:\n%s", out)
	}
	if strings.Contains(string(out), "nested-password-4242") {
		t.Errorf("the password given to the action called is shown:\n%s", out)
	}
}
