package probe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

// TestEndToEndLocalExternalAction runs a workflow whose step uses an action
// served by an executable of its own, found through the action.yml of a
// directory next to the workflow.
func TestEndToEndLocalExternalAction(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	actionDir := filepath.Join(dir, "greet")
	bin := filepath.Join(actionDir, "greet")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {bin, "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	platform := fmt.Sprintf("%s_%s", goEnv(t, "GOOS"), goEnv(t, "GOARCH"))

	workflow := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`name: external
jobs:
- name: greet
  steps:
  - name: greet probe
    uses: ./greet
    with:
      name: probe
    test: res.greeting == "hello probe"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		manifest string
		wantCode int
		wantOut  string
	}{
		{
			name:     "path",
			manifest: "runs:\n  using: binary\n  path: greet\n",
			wantCode: ExitOK,
		},
		{
			name:     "path with checksum",
			manifest: fmt.Sprintf("runs:\n  using: binary\n  path: greet\n  checksums:\n    %s: %s\n", platform, sum),
			wantCode: ExitOK,
		},
		{
			name:     "checksum mismatch",
			manifest: fmt.Sprintf("runs:\n  using: binary\n  path: greet\n  checksums:\n    %s: %s\n", platform, strings.Repeat("f", 64)),
			wantCode: ExitConfigError,
			wantOut:  "has SHA-256",
		},
		{
			name:     "unsupported using",
			manifest: "runs:\n  using: go\n  path: greet\n",
			wantCode: ExitConfigError,
			wantOut:  "runs.using must be",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte(tt.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			// Run from the repository root, not the workflow's directory, so
			// that ./greet has to be taken relative to the workflow file.
			out, err := exec.Command(probeBin, workflow).CombinedOutput()
			code := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error: %v", err)
				}
				code = exitErr.ExitCode()
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("output does not contain %q\n%s", tt.wantOut, out)
			}
		})
	}
}

// A verbose run shows the records an action logs at debug level, and one
// with -vv those at trace level as well, along with how the runner started,
// called and stopped the action's process. Each reads as the rest of the
// verbose output does, without timestamps or values printed as Go maps.
func TestEndToEndVerboseLevelsOfAnExternalAction(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	bin := filepath.Join(dir, "greet", "greet")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {bin, "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "greet", "action.yml"), []byte("runs:\n  using: binary\n  path: greet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`name: external
jobs:
- name: greet
  steps:
  - name: greet probe
    uses: ./greet
    with:
      name: probe
    test: res.greeting == "hello probe"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		var o, e strings.Builder
		cmd := exec.Command(probeBin, append(args, workflow)...)
		cmd.Stdout, cmd.Stderr = &o, &e
		if err := cmd.Run(); err != nil {
			t.Fatalf("probe %v: %v\n%s%s", args, err, o.String(), e.String())
		}
		return e.String()
	}

	debug := "[DEBUG] ./greet: greeting\n[DEBUG]   name: probe\n[DEBUG]   with:\n[DEBUG]     name: probe\n"
	internals := []string{"[TRACE] ./greet: starting plugin", "[TRACE] ./greet: plugin address", "[TRACE] ./greet: ActionsServer.Run called", "[TRACE] ./greet: greeted", "[TRACE] ./greet: plugin exited"}
	tests := []struct {
		args          []string
		debug, traces bool
	}{
		{nil, false, false},
		{[]string{"-v"}, true, false},
		{[]string{"-vv"}, true, true},
	}
	for _, tt := range tests {
		stderr := run(tt.args...)
		if got := strings.Contains(stderr, debug); got != tt.debug {
			t.Errorf("probe %v: has the action's debug record = %v, want %v:\n%s", tt.args, got, tt.debug, stderr)
		}
		for _, want := range internals {
			if got := strings.Contains(stderr, want); got != tt.traces {
				t.Errorf("probe %v: has %q = %v, want %v:\n%s", tt.args, want, got, tt.traces, stderr)
			}
		}
		for _, noise := range []string{"timestamp", "map[", "actions."} {
			if strings.Contains(stderr, noise) {
				t.Errorf("probe %v: the output has %q:\n%s", tt.args, noise, stderr)
			}
		}
	}
}

// A local action in an embedded job is found next to the job file, as one in
// a workflow is found next to the workflow file.
func TestEndToEndLocalExternalActionInEmbeddedJob(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	jobsDir := filepath.Join(dir, "jobs")
	bin := filepath.Join(jobsDir, "greet", "greet")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {bin, "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	files := map[string]string{
		filepath.Join(jobsDir, "greet", "action.yml"): "runs:\n  using: binary\n  path: greet\n",
		filepath.Join(jobsDir, "greet.yml"): `name: greet
steps:
- name: greet probe
  id: greet
  uses: ./greet
  with:
    name: probe
  test: res.greeting == "hello probe"
  outputs:
    greeting: res.greeting
`,
		filepath.Join(dir, "workflow.yml"): fmt.Sprintf(`name: embedded external
jobs:
- name: embed
  steps:
  - uses: embedded
    with:
      path: %s
    test: res.code == 0 && res.outputs.greeting == "hello probe"
`, filepath.Join(jobsDir, "greet.yml")),
	}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := exec.Command(probeBin, filepath.Join(dir, "workflow.yml")).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

// The digest an executable was resolved with is checked again when it is
// started, so one replaced in between is not run.
func TestEndToEndExternalActionChecksumAtStart(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "greet")
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/external-action").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	log := hclog.NewNullLogger()

	res, _, err := actionrpc.RunExecutable(context.Background(), bin, sum[:], actionrpc.Call{With: map[string]any{"name": "probe"}}, log)
	if err != nil {
		t.Fatalf("RunExecutable() error = %v", err)
	}
	if got := res["res"].(map[string]any)["greeting"]; got != "hello probe" {
		t.Errorf("greeting = %v, want hello probe", got)
	}

	other := sha256.Sum256([]byte("something else"))
	if _, _, err := actionrpc.RunExecutable(context.Background(), bin, other[:], actionrpc.Call{}, log); err == nil || !strings.Contains(err.Error(), "checksums did not match") {
		t.Errorf("RunExecutable() with a wrong digest error = %v, want a checksum mismatch", err)
	}
}

func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// A step whose action fails tells why once, in the step's result, as the
// action said it: without the action's own records of the failure, unless
// the run is verbose, and without the wording of gRPC or of where the error
// was caught.
func TestEndToEndFailingActionTellsWhyOnce(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	bin := filepath.Join(dir, "failing", "failing")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {bin, "./testdata/failing-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "failing", "action.yml"), []byte("runs:\n  using: binary\n  path: failing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A port nothing listens on, for the http action to be refused at.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	workflow := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(workflow, fmt.Appendf(nil, `name: failing
jobs:
- name: http
  steps:
  - name: refused
    uses: http
    with:
      url: http://%s
      get: /x
    test: res.code == 200
- name: error
  steps:
  - name: error
    uses: ./failing
    with:
      mode: error
- name: panic
  steps:
  - name: panic
    uses: ./failing
    with:
      mode: panic
`, addr), 0o644); err != nil {
		t.Fatal(err)
	}
	refused := fmt.Sprintf(`Get "http://%s/x": dial tcp %s: connect: connection refused`, addr, addr)

	run := func(args ...string) (stdout, stderr string) {
		var o, e strings.Builder
		cmd := exec.Command(probeBin, append(args, workflow)...)
		cmd.Stdout, cmd.Stderr = &o, &e
		err := cmd.Run()
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != ExitActionError {
			t.Fatalf("probe %v: %v, want exit status %d\n%s%s", args, err, ExitActionError, o.String(), e.String())
		}
		return o.String(), e.String()
	}

	stdout, stderr := run("--output", "stream")
	all := stdout + stderr
	for _, msg := range []string{refused, "the widget broke", "the action exited before it returned (exit status 2): panic: the widget exploded"} {
		if n := strings.Count(all, msg); n != 1 {
			t.Errorf("the output tells %q %d times, want once:\n%s", msg, n, all)
		}
		if !strings.Contains(stdout, msg) {
			t.Errorf("the report does not tell %q:\n%s", msg, stdout)
		}
	}
	for _, noise := range []string{"[ERROR]", "rpc error", "step_execute", "caused by", "Error:", "goroutine"} {
		if strings.Contains(all, noise) {
			t.Errorf("the output has %q:\n%s", noise, all)
		}
	}
	// A warning tells what the step's result does not.
	if !strings.Contains(stderr, "the widget is old") {
		t.Errorf("the action's warning is not shown:\n%s", stderr)
	}

	// A verbose run shows the action's own records as well, and, for the
	// refused request, which returned none, what the step gave the action.
	_, stderr = run("-v", "--output", "stream")
	given := fmt.Sprintf("[DEBUG] Request (as the step gave it; the action returned none):\n[DEBUG]   get: /x\n[DEBUG]   url: http://%s\n", addr)
	for _, want := range []string{"[ERROR]", "widget failed", "panic: the widget exploded", given} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the verbose output does not have %q:\n%s", want, stderr)
		}
	}
}
