package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mozership/probe/internal/report"
)

// probe serve runs a workflow again and again, each run a probe of its own
// that writes its json report where the log of the run says, and stops on
// SIGTERM once the run going is over.
func TestServe_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds probe")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("hello.yml", `name: Hello
vars:
  who: nobody
jobs:
- name: Greet
  steps:
  - uses: hello
    with:
      name: "{{vars.who}}"
outputs:
  who: vars.who
`)
	write("serve.yml", `results:
  dir: runs
workflows:
  hello:
    files: [hello.yml]
    vars:
      who: serve
    loop: true
    interval: 100ms
`)

	var stderr syncBuffer
	cmd := exec.Command(bin, "serve", filepath.Join(dir, "serve.yml"))
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var runs []map[string]any
	for len(runs) < 2 {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("no two runs in the log:\n%s", stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
		runs = logged(t, stderr.String(), "run")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve ended with %v:\n%s", err, stderr.String())
	}
	if len(logged(t, stderr.String(), "stopped")) != 1 {
		t.Errorf("no stopped in the log:\n%s", stderr.String())
	}

	run := runs[0]
	if run["status"] != "passed" || run["exit_status"] != float64(0) {
		t.Fatalf("run = %v\n%s", run, stderr.String())
	}
	data, err := os.ReadFile(run["report"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Name != "Hello" || r.Status != report.Passed || r.Outputs["who"] != "serve" {
		t.Errorf("report = %+v", r)
	}
	if filepath.Dir(filepath.Dir(run["report"].(string))) != filepath.Join(dir, "runs", "hello") {
		t.Errorf("report at %s", run["report"])
	}
}

// logged returns the lines of the JSON log whose msg is msg.
func logged(t *testing.T, log, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewBufferString(log))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("a line of the log is not JSON: %s", sc.Text())
		}
		if line["msg"] == msg {
			out = append(out, line)
		}
	}
	return out
}

// syncBuffer is a buffer the process writes to while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
