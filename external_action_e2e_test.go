package probe

import (
	"crypto/sha256"
	"fmt"
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

	res, err := actionrpc.RunExecutable(bin, sum[:], map[string]any{"name": "probe"}, log)
	if err != nil {
		t.Fatalf("RunExecutable() error = %v", err)
	}
	if got := res["res"].(map[string]any)["greeting"]; got != "hello probe" {
		t.Errorf("greeting = %v, want hello probe", got)
	}

	other := sha256.Sum256([]byte("something else"))
	if _, err := actionrpc.RunExecutable(bin, other[:], nil, log); err == nil || !strings.Contains(err.Error(), "checksums did not match") {
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
