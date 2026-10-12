package embedded

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
)

func TestNewReq(t *testing.T) {
	got := NewReq()

	expected := &Req{
		Path: "",
		Vars: map[string]any{},
	}

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("\nExpected:\n%#v\nGot:\n%#v", expected, got)
	}
}

func TestReqDo_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		req         *Req
		expectError bool
	}{
		{
			name: "missing path",
			req: &Req{
				Vars: map[string]any{"test": "value"},
			},
			expectError: true,
		},
		{
			name: "empty path",
			req: &Req{
				Path: "",
				Vars: map[string]any{"test": "value"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.req.Do()

			if tt.expectError {
				if err == nil {
					t.Errorf("Do() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Do() unexpected error: %v", err)
				return
			}
		})
	}
}

// writeJob writes a job file into a directory of its own and returns its path.
func writeJob(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "job.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadJob(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		job, err := loadJob(writeJob(t, `name: j
defaults:
  http:
    timeout: 5s
steps:
- uses: http
  with:
    url: http://example.com
- uses: http
`), nil)
		if err != nil {
			t.Fatalf("loadJob() error = %v", err)
		}
		for i, st := range job.Steps {
			if st.With["timeout"] != "5s" {
				t.Errorf("step %d: With = %v, want the default timeout", i, st.With)
			}
		}
	})

	// The names are those of the workflow that embeds the job.
	redis := "github.com/mozership/probe-redis@0123456789abcdef0123456789abcdef01234567"
	names := map[string]string{"redis": redis, "greet": "/work/greet"}

	t.Run("reads the job by the names it is given", func(t *testing.T) {
		job, err := loadJob(writeJob(t, `name: j
defaults:
  redis:
    url: redis://localhost
steps:
- uses: redis
  with:
    commands: [PING]
- uses: `+redis+`
- uses: greet
- uses: http
`), names)
		if err != nil {
			t.Fatalf("loadJob() error = %v", err)
		}
		for i, want := range []string{redis, redis, "/work/greet", "http"} {
			if job.Steps[i].Uses != want {
				t.Errorf("step %d: uses = %q, want %q", i, job.Steps[i].Uses, want)
			}
		}
		// The defaults written by the name apply to a step that names the
		// action in full as well.
		for i := range 2 {
			if job.Steps[i].With["url"] != "redis://localhost" {
				t.Errorf("step %d: With = %v, want the default url", i, job.Steps[i].With)
			}
		}
	})

	t.Run("a name it is not given stays as it is written", func(t *testing.T) {
		job, err := loadJob(writeJob(t, "name: j\nsteps:\n- uses: redis\n"), nil)
		if err != nil {
			t.Fatalf("loadJob() error = %v", err)
		}
		if job.Steps[0].Uses != "redis" {
			t.Errorf("uses = %q, want it left as redis", job.Steps[0].Uses)
		}
	})

	t.Run("defaults written by the name and in full", func(t *testing.T) {
		_, err := loadJob(writeJob(t, `name: j
defaults:
  redis: {url: a}
  `+redis+`: {url: b}
steps:
- uses: redis
`), names)
		if err == nil || !strings.Contains(err.Error(), "are the same action, whose defaults can be written once") {
			t.Fatalf("loadJob() error = %v, want the defaults refused", err)
		}
	})

	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{name: "missing file", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "none.yml") }, wantErr: "does not exist"},
		{name: "invalid YAML", path: func(t *testing.T) string { return writeJob(t, "steps: [") }, wantErr: "failed to decode YAML job"},
		{name: "no steps", path: func(t *testing.T) string { return writeJob(t, "name: j\nsteps: []\n") }, wantErr: "no steps found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadJob(tt.path(t), nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadJob() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// A skipped step runs no action, so these jobs run without starting the
// plugin processes that real actions need.
func TestReqDo(t *testing.T) {
	t.Run("a job that runs", func(t *testing.T) {
		var before, after bool
		r := NewReq()
		r.Path = writeJob(t, `name: j
steps:
- name: skipped
  uses: hello
  skipif: "true"
`)
		r.cb = &Callback{
			before: func(string, map[string]any) { before = true },
			after:  func(*Result) { after = true },
		}
		res, err := r.Do()
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		if res.Res.Code != 0 || res.Status != 0 || res.Res.Error != "" {
			t.Errorf("Res = %+v, want a passing job", res.Res)
		}
		if !before || !after {
			t.Errorf("callbacks called: before %v, after %v", before, after)
		}
	})

	t.Run("a job that cannot run", func(t *testing.T) {
		r := NewReq()
		r.Path = writeJob(t, `name: j
steps:
- id: Not Valid
  uses: hello
`)
		res, err := r.Do()
		if err == nil || !strings.Contains(err.Error(), "step validation failed") {
			t.Fatalf("Do() error = %v, want a step validation error", err)
		}
		if res.Res.Code != 1 {
			t.Errorf("Res.Code = %d, want 1", res.Res.Code)
		}
	})

	// A local action is looked up next to the job file, not in the working
	// directory, so this one is reported missing from the job's directory.
	t.Run("a local action is relative to the job file", func(t *testing.T) {
		r := NewReq()
		r.Path = writeJob(t, `name: j
steps:
- uses: ./greet
`)
		_, err := r.Do()
		want := filepath.Join(filepath.Dir(r.Path), "greet")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Do() error = %v, want one naming %s", err, want)
		}
	})
}

func TestExecute(t *testing.T) {
	path := writeJob(t, `name: j
steps:
- uses: hello
  skipif: "true"
`)
	data := map[string]any{"path": path, "vars": map[string]any{"k": "v"}}
	got, err := Execute(data)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	res, _ := got["res"].(map[string]any)
	if res == nil || res["code"] != 0 {
		t.Errorf("Execute() res = %v, want code 0", got["res"])
	}
	if _, ok := data["path"]; !ok {
		t.Error("Execute() changed the caller's data")
	}
}

func TestWithBefore(t *testing.T) {
	called := false
	var capturedPath string
	var capturedVars map[string]any

	option := WithBefore(func(path string, vars map[string]any) {
		called = true
		capturedPath = path
		capturedVars = vars
	})

	cb := &Callback{}
	option(cb)

	if cb.before == nil {
		t.Error("WithBefore() did not set before callback")
		return
	}

	// Test the callback
	testVars := map[string]any{"key": "value"}
	cb.before("/test/path.yml", testVars)

	if !called {
		t.Error("before callback was not called")
	}
	if capturedPath != "/test/path.yml" {
		t.Errorf("Expected path '/test/path.yml', got '%s'", capturedPath)
	}
	if !reflect.DeepEqual(capturedVars, testVars) {
		t.Errorf("Expected vars %v, got %v", testVars, capturedVars)
	}
}

func TestWithAfter(t *testing.T) {
	called := false
	var capturedResult *Result

	option := WithAfter(func(result *Result) {
		called = true
		capturedResult = result
	})

	cb := &Callback{}
	option(cb)

	if cb.after == nil {
		t.Error("WithAfter() did not set after callback")
		return
	}

	// Test the callback
	testResult := &Result{
		Status: 1,
		Res: Res{
			Code:    1,
			Outputs: map[string]any{"output": "test"},
			Report:  "test report",
			Error:   "test error",
		},
		RT: time.Second,
	}
	cb.after(testResult)

	if !called {
		t.Error("after callback was not called")
	}
	if capturedResult != testResult {
		t.Error("after callback did not receive correct result")
	}
}

func TestExecuteTellsAnUnknownAction(t *testing.T) {
	// The skipped step would run nothing, but an action that does not exist
	// stops the job before its first step.
	path := writeJob(t, "name: inner\nsteps:\n- name: Skipped\n  uses: htp\n  skipif: \"true\"\n")

	_, err := Execute(map[string]any{"path": path}, WithBuiltinActions([]string{"hello", "http"}))
	if err == nil || !strings.Contains(err.Error(), `job "inner", step 0: unknown action "htp"`) || !strings.Contains(err.Error(), `did you mean "http"?`) {
		t.Fatalf("err = %v, want the unknown action told", err)
	}

	// Not told the actions of Probe, it leaves the step to fail when it runs.
	if _, err := Execute(map[string]any{"path": path}); err != nil {
		t.Fatalf("err = %v, want the job run", err)
	}
}

func TestExecuteUnderAGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yml")
	job := "name: inner\nsteps:\n- name: Write\n  uses: ssh\n  with:\n    host: prod.example.com\n    user: root\n    cmd: rm -rf /tmp/x\n  test: res.code == 0\n"
	if err := os.WriteFile(path, []byte(job), 0o600); err != nil {
		t.Fatal(err)
	}
	all := []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
	guard := actionrpc.Guard{ReadOnly: true, Keeps: map[string][]string{"embedded": all, "hello": all, "http": all, "db": all}}

	_, err := Execute(map[string]any{"path": path}, WithGuard(guard))
	if !actionrpc.IsRefused(err) {
		t.Fatalf("err = %v, want the step refused, so that the step embedding the job is", err)
	}
	if !strings.Contains(err.Error(), `step 0 "Write" of the embedded job`) {
		t.Errorf("err = %v, want it to name the step refused", err)
	}
}
