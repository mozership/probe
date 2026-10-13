package probe

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cleanupEnv is a directory with probe, a workflow to write, and the local
// action ./left, which leaves a file behind in each step and removes those
// of a run when it is told to clean up.
type cleanupEnv struct {
	dir   string
	probe string
	// state is where the action leaves its files and logs its cleanups.
	state string
}

func newCleanupEnv(t *testing.T, manifest string) *cleanupEnv {
	t.Helper()
	dir := t.TempDir()
	e := &cleanupEnv{dir: dir, probe: filepath.Join(dir, "probe"), state: filepath.Join(dir, "state")}
	for _, b := range [][]string{{e.probe, "./cmd/probe"}, {filepath.Join(dir, "left", "left"), "./testdata/cleanup-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	if err := os.Mkdir(e.state, 0o755); err != nil {
		t.Fatal(err)
	}
	e.write(t, "left/action.yml", manifest)
	return e
}

func (e *cleanupEnv) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *cleanupEnv) command(env ...string) *exec.Cmd {
	cmd := exec.Command(e.probe, filepath.Join(e.dir, "workflow.yml"))
	cmd.Env = append(append(os.Environ(), "CLEANUP_DIR="+e.state), env...)
	return cmd
}

// left returns the files the steps left behind that are still there, and
// cleanups the runs the action was told to clean up after, one for each
// call.
func (e *cleanupEnv) result(t *testing.T) (left, cleanups []string) {
	t.Helper()
	entries, err := os.ReadDir(e.state)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "cleanup.log" {
			left = append(left, entry.Name())
			continue
		}
		data, err := os.ReadFile(filepath.Join(e.state, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		cleanups = strings.Fields(string(data))
	}
	return left, cleanups
}

const cleaningManifest = "cleanup: true\nruns:\n  using: binary\n  path: left\n"

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected error: %v", err)
	}
	return exitErr.ExitCode()
}

// An action whose action.yml sets cleanup is called once after the run,
// however many steps and jobs used it and by whichever name, and what the
// steps left behind is there until then.
func TestEndToEndCleanup(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		workflow string
		env      []string
		wantCode int
		// wantLeft is how many files are left after the run, and
		// wantCleanups how many times the action was told to clean up.
		wantLeft     int
		wantCleanups int
		wantOut      string
	}{
		{
			name:     "once for the steps of every job",
			manifest: cleaningManifest,
			workflow: `name: cleanup
actions:
  left: ./left
jobs:
- name: first
  id: first
  steps:
  - uses: ./left
    with:
      name: a
    test: res.promised == true
  - uses: left
    with:
      name: b
    test: res.promised == true
- name: second
  needs: [first]
  steps:
  - uses: left
    with:
      name: c
  - name: what the steps left is still there
    uses: shell
    with:
      cmd: test "$(ls "$CLEANUP_DIR" | wc -l)" -eq 3
    test: res.code == 0
`,
			wantCode:     ExitOK,
			wantCleanups: 1,
		},
		{
			name:     "not for an action that does not ask",
			manifest: "runs:\n  using: binary\n  path: left\n",
			workflow: `name: cleanup
jobs:
- name: first
  steps:
  - uses: ./left
    with:
      name: a
    test: res.promised == false
`,
			wantCode: ExitOK,
			wantLeft: 1,
		},
		{
			name:     "after a step that failed",
			manifest: cleaningManifest,
			workflow: `name: cleanup
jobs:
- name: first
  steps:
  - uses: ./left
    with:
      name: a
    test: res.promised == false
  - uses: ./left
    with:
      name: b
`,
			wantCode:     ExitTestFailed,
			wantCleanups: 1,
		},
		{
			// The action goes on after its step timed out, and leaves its
			// file then.
			name:     "after a step that timed out",
			manifest: cleaningManifest,
			workflow: `name: cleanup
jobs:
- name: first
  steps:
  - uses: ./left
    timeout: 200ms
    with:
      name: a
      delay: 1s
`,
			wantCode:     ExitActionError,
			wantCleanups: 1,
		},
		{
			name:     "once its workflow is over for the steps of an embedded job",
			manifest: cleaningManifest,
			workflow: `name: cleanup
jobs:
- name: first
  steps:
  - uses: embedded
    with:
      path: ./job.yml
    test: res.code == 0
  - name: what the embedded job left is still there
    uses: shell
    with:
      cmd: test "$(ls "$CLEANUP_DIR" | wc -l)" -eq 2
    test: res.code == 0
  - uses: ./left
    with:
      name: c
`,
			wantCode:     ExitOK,
			wantCleanups: 1,
		},
		{
			name:     "a cleanup that fails is told and does not fail the run",
			manifest: cleaningManifest,
			workflow: `name: cleanup
jobs:
- name: first
  steps:
  - uses: ./left
    with:
      name: a
`,
			env:          []string{"CLEANUP_FAIL=1"},
			wantCode:     ExitOK,
			wantLeft:     1,
			wantCleanups: 1,
			wantOut:      "[WARN] the cleanup of",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCleanupEnv(t, tt.manifest)
			e.write(t, "workflow.yml", tt.workflow)
			e.write(t, "job.yml", `name: embedded
steps:
- uses: ./left
  with:
    name: a
- uses: ./left
  with:
    name: b
`)
			cmd := e.command(tt.env...)
			// The path of the embedded job is relative to where probe runs.
			cmd.Dir = e.dir
			out, err := cmd.CombinedOutput()
			if code := exitCode(t, err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("output does not contain %q\n%s", tt.wantOut, out)
			}
			left, cleanups := e.result(t)
			if len(left) != tt.wantLeft {
				t.Errorf("left %v, want %d files\n%s", left, tt.wantLeft, out)
			}
			if len(cleanups) != tt.wantCleanups {
				t.Errorf("cleaned up %d times, want %d\n%s", len(cleanups), tt.wantCleanups, out)
			}
		})
	}
}

// A run that a signal ends is cleaned up after before probe goes.
func TestEndToEndCleanupOnSignal(t *testing.T) {
	e := newCleanupEnv(t, cleaningManifest)
	e.write(t, "workflow.yml", `name: cleanup
jobs:
- name: first
  steps:
  - uses: ./left
    with:
      name: a
  - uses: shell
    with:
      cmd: sleep 5
`)
	cmd := e.command()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(30 * time.Second)
	for {
		if left, _ := e.result(t); len(left) == 1 {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("the step left nothing\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-exited:
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Errorf("probe ended with %v, want it ended by SIGTERM\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("probe did not end\n%s", out.String())
	}
	left, cleanups := e.result(t)
	if len(left) != 0 || len(cleanups) != 1 {
		t.Errorf("left %v after %d cleanups, want nothing left after one\n%s", left, len(cleanups), out.String())
	}
}
