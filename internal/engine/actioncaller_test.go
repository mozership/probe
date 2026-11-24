package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/action"
	"github.com/mozership/probe/actionref"
)

// localAction writes a local action in a directory named name under dir,
// with manifest added to its action.yml, and returns its directory. Its
// executable is not one: the tests run it with a fake.
func localAction(t *testing.T, dir, name, manifest string) string {
	t.Helper()
	actionDir := filepath.Join(dir, name)
	if err := os.MkdirAll(actionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "bin"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	yml := "runs:\n  using: binary\n  path: bin\n" + manifest
	if err := os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	full, err := actionref.Full(actionDir, "")
	if err != nil {
		t.Fatal(err)
	}
	return full
}

// fakeRun records the calls it is given, and returns what ret makes of each.
type fakeRun struct {
	calls []action.Call
	ret   func(call action.Call) (map[string]any, map[string]any, error)
}

func (f *fakeRun) external(_ context.Context, _ *actionref.Executable, call action.Call, _ hclog.Logger) (map[string]any, map[string]any, error) {
	f.calls = append(f.calls, call)
	if f.ret == nil {
		return map[string]any{"status": 0}, nil, nil
	}
	return f.ret(call)
}

func newTestCaller(call action.Call, baseDir string, f *fakeRun, opts ...CallerOption) *ActionCaller {
	c := NewActionCaller(call, baseDir, append([]CallerOption{WithCallerLogOutput(io.Discard)}, opts...)...)
	c.runExternal = f.external
	return c
}

func TestActionCaller_ReadsNamesOfTheWorkflow(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "greet", "")
	f := &fakeRun{}
	step := action.Step{RunID: "run-1", JobID: "job"}
	c := newTestCaller(action.Call{Step: step, Actions: map[string]string{"greet": full}}, "", f)

	res, err := c.Call(context.Background(), "greet", map[string]any{"name": "probe"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Result["status"] != 0 {
		t.Errorf("result = %v", res.Result)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.calls))
	}
	got := f.calls[0]
	if got.With["name"] != "probe" {
		t.Errorf("with = %v", got.With)
	}
	if got.Step != step {
		t.Errorf("step = %+v, want the step of the calling action %+v", got.Step, step)
	}
	if got.Actions["greet"] != full {
		t.Errorf("actions = %v, want those of the calling action", got.Actions)
	}
}

func TestActionCaller_ResolvesALocalActionAgainstBaseDir(t *testing.T) {
	dir := t.TempDir()
	localAction(t, dir, "greet", "")
	f := &fakeRun{}
	c := newTestCaller(action.Call{}, dir, f)
	if _, err := c.Call(context.Background(), "./greet", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.calls))
	}
}

func TestActionCaller_EmptyUses(t *testing.T) {
	c := newTestCaller(action.Call{}, "", &fakeRun{})
	if _, err := c.Call(context.Background(), "", nil); err == nil {
		t.Fatal("an empty uses was called")
	}
}

func TestActionCaller_KeepsStateBetweenCalls(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "count", "")
	f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
		// The state is kept in the form it takes when it is sent, in
		// which a number may be a float64.
		var n int
		switch v := call.State["n"].(type) {
		case int:
			n = v
		case int64:
			n = int(v)
		case float64:
			n = int(v)
		}
		if call.With["fail"] == true {
			return nil, map[string]any{"n": 100}, errors.New("failed")
		}
		if call.With["keep"] == true {
			return map[string]any{"n": n}, nil, nil
		}
		return map[string]any{"n": n}, map[string]any{"n": n + 1}, nil
	}}
	c := newTestCaller(action.Call{Actions: map[string]string{"count": full}}, "", f)
	call := func(with map[string]any) any {
		t.Helper()
		res, err := c.Call(context.Background(), "count", with)
		if err != nil {
			t.Fatal(err)
		}
		return res.Result["n"]
	}
	if n := call(nil); n != 0 {
		t.Errorf("first n = %v, want 0", n)
	}
	if n := call(nil); n != 1 {
		t.Errorf("second n = %v, want 1", n)
	}
	// A nil state keeps the one there was.
	if n := call(map[string]any{"keep": true}); n != 2 {
		t.Errorf("third n = %v, want 2", n)
	}
	// The state of a call that failed is not kept.
	if _, err := c.Call(context.Background(), "count", map[string]any{"fail": true}); err == nil {
		t.Fatal("want the error of the action")
	}
	if n := call(nil); n != 2 {
		t.Errorf("n after a failed call = %v, want 2", n)
	}
	// The same action under another name keeps the same state.
	if _, err := c.Call(context.Background(), full, nil); err != nil {
		t.Fatal(err)
	}
	if n := call(nil); n != 4 {
		t.Errorf("n after a call by the full name = %v, want 4", n)
	}
}

func TestActionCaller_Guard(t *testing.T) {
	dir := t.TempDir()
	keeps := localAction(t, dir, "keeps", "guard: [read-only]\n")
	keepsNone := localAction(t, dir, "none", "")
	names := map[string]string{"keeps": keeps, "none": keepsNone}

	tests := []struct {
		name    string
		guard   action.Guard
		uses    string
		refused bool
	}{
		{name: "no guard", uses: "none"},
		{name: "keeps to it", guard: action.Guard{ReadOnly: true}, uses: "keeps"},
		{name: "does not keep to it", guard: action.Guard{ReadOnly: true}, uses: "none", refused: true},
		{name: "keeps to one kind of two", guard: action.Guard{ReadOnly: true, AllowHosts: []string{"example.com"}}, uses: "keeps", refused: true},
		{name: "allowed by its full name", guard: action.Guard{ReadOnly: true, AllowActions: []string{keepsNone}}, uses: "none"},
		{name: "allowed by the name called", guard: action.Guard{ReadOnly: true, AllowActions: []string{"none"}}, uses: "none"},
		{
			// What the action.yml declares replaces what the guard came
			// with, as for a step.
			name:    "declares less than the guard came with",
			guard:   action.Guard{ReadOnly: true, Keeps: map[string][]string{keepsNone: {action.KindReadOnly}}},
			uses:    "none",
			refused: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRun{}
			c := newTestCaller(action.Call{Guard: tt.guard, Actions: names}, "", f)
			_, err := c.Call(context.Background(), tt.uses, nil)
			var refused *action.Refused
			if got := errors.As(err, &refused); got != tt.refused {
				t.Fatalf("refused = %v, want %v (err %v)", got, tt.refused, err)
			}
			if tt.refused {
				if len(f.calls) != 0 {
					t.Error("a refused action was run")
				}
				if !strings.Contains(err.Error(), "--allow-action") {
					t.Errorf("error = %q, want it to tell how to let the action run", err)
				}
				return
			}
			if len(f.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(f.calls))
			}
			if got := f.calls[0].Guard; got.ReadOnly != tt.guard.ReadOnly || !slices.Equal(got.AllowHosts, tt.guard.AllowHosts) {
				t.Errorf("guard = %+v, want the guard of the run", got)
			}
		})
	}
}

func TestActionCaller_BuiltinUnderTheGuard(t *testing.T) {
	var ran []string
	run := func(_ context.Context, probe, name string, _ action.Call, _ hclog.Logger) (map[string]any, map[string]any, error) {
		ran = append(ran, probe+" "+name)
		return map[string]any{"status": 0}, nil, nil
	}
	guard := action.Guard{ReadOnly: true, Keeps: map[string][]string{"http": {action.KindReadOnly}}}
	c := newTestCaller(action.Call{Guard: guard}, "", &fakeRun{}, WithProbeExecutable("/bin/probe"))
	c.runBuiltin = run

	if _, err := c.Call(context.Background(), "http", nil); err != nil {
		t.Fatal(err)
	}
	var refused *action.Refused
	if _, err := c.Call(context.Background(), "shell", nil); !errors.As(err, &refused) {
		t.Fatalf("err = %v, want shell refused under --read-only", err)
	}
	if !slices.Equal(ran, []string{"/bin/probe http"}) {
		t.Errorf("ran = %v", ran)
	}
}

func TestActionCaller_BuiltinWithoutProbeExecutable(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	c := newTestCaller(action.Call{}, "", &fakeRun{})
	c.runBuiltin = func(context.Context, string, string, action.Call, hclog.Logger) (map[string]any, map[string]any, error) {
		t.Fatal("a built-in action was run without a probe to start it from")
		return nil, nil, nil
	}
	_, err := c.Call(context.Background(), "http", nil)
	if err == nil || !strings.Contains(err.Error(), ExecutableEnv) {
		t.Fatalf("err = %v, want it to name %s", err, ExecutableEnv)
	}
}

func TestActionCaller_ProbeExecutableFromTheEnvironment(t *testing.T) {
	t.Setenv(ExecutableEnv, "/usr/local/bin/probe")
	var from string
	c := newTestCaller(action.Call{}, "", &fakeRun{})
	c.runBuiltin = func(_ context.Context, probe, _ string, _ action.Call, _ hclog.Logger) (map[string]any, map[string]any, error) {
		from = probe
		return nil, nil, nil
	}
	if _, err := c.Call(context.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if from != "/usr/local/bin/probe" {
		t.Errorf("started from %q", from)
	}
}

func TestActionCaller_Cleanups(t *testing.T) {
	dir := t.TempDir()
	cleans := localAction(t, dir, "cleans", "cleanup: true\n")
	plain := localAction(t, dir, "plain", "")
	names := map[string]string{"cleans": cleans, "plain": plain}

	t.Run("owed once, also when the call fails", func(t *testing.T) {
		owed := &action.Cleanups{}
		f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
			if call.Cleanups == nil {
				t.Error("the action was not told the runner cleans up")
			}
			if call.With["fail"] == true {
				return nil, nil, errors.New("failed")
			}
			return nil, nil, nil
		}}
		c := newTestCaller(action.Call{Cleanups: owed, Actions: names}, "", f)
		for _, with := range []map[string]any{{"fail": true}, nil, nil} {
			_, _ = c.Call(context.Background(), "cleans", with)
		}
		if _, err := c.Call(context.Background(), "plain", nil); err != nil {
			t.Fatal(err)
		}
		if got := owed.List(); !slices.Equal(got, []string{cleans}) {
			t.Errorf("owed = %v, want [%s]", got, cleans)
		}
	})

	t.Run("those the action ran", func(t *testing.T) {
		owed := &action.Cleanups{}
		f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
			call.Cleanups.Add("github.com/o/r@" + strings.Repeat("a", 40))
			return nil, nil, errors.New("failed after it ran one")
		}}
		c := newTestCaller(action.Call{Cleanups: owed, Actions: names}, "", f)
		_, _ = c.Call(context.Background(), "plain", nil)
		if got := owed.List(); len(got) != 1 || !strings.HasPrefix(got[0], "github.com/o/r@") {
			t.Errorf("owed = %v", got)
		}
	})

	t.Run("a runner that does not clean up", func(t *testing.T) {
		f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
			if call.Cleanups != nil {
				t.Error("the action was told the runner cleans up")
			}
			return nil, nil, nil
		}}
		c := newTestCaller(action.Call{Actions: names}, "", f)
		if _, err := c.Call(context.Background(), "cleans", nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestActionCaller_PassesOnProcessesAndContract(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "serve", "")
	procs := &action.Processes{}
	f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
		call.Processes.Add(action.Process{PID: 4242})
		call.Contract.Report(action.ContractCheck{Matched: &action.ContractMatch{Spec: "openapi.yml"}})
		return map[string]any{"status": 0}, nil, nil
	}}
	c := newTestCaller(action.Call{Processes: procs, Actions: map[string]string{"serve": full}}, "", f)
	res, err := c.Call(context.Background(), "serve", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := procs.List(); len(got) != 1 || got[0].PID != 4242 {
		t.Errorf("processes = %+v, want the one the action left", got)
	}
	if res.Contract == nil || res.Contract.Matched == nil || res.Contract.Matched.Spec != "openapi.yml" {
		t.Errorf("contract = %+v", res.Contract)
	}
}

func TestActionCaller_MissingAction(t *testing.T) {
	c := newTestCaller(action.Call{}, t.TempDir(), &fakeRun{})
	_, err := c.Call(context.Background(), "./nowhere", nil)
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.Type != ErrorTypeConfiguration {
		t.Fatalf("err = %v, want a configuration error", err)
	}
}

func TestActionCaller_SymlinkedAliasIsTheSameAction(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "real", "cleanup: true\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(full, link); err != nil {
		t.Fatal(err)
	}
	owed := &action.Cleanups{}
	f := &fakeRun{ret: func(call action.Call) (map[string]any, map[string]any, error) {
		n, _ := call.State["n"].(int64)
		return map[string]any{"n": n}, map[string]any{"n": n + 1}, nil
	}}
	// The workflow names a local action by an absolute path that may pass
	// through a link.
	c := newTestCaller(action.Call{Cleanups: owed, Actions: map[string]string{"linked": link}}, "", f)
	if _, err := c.Call(context.Background(), "linked", nil); err != nil {
		t.Fatal(err)
	}
	res, err := c.Call(context.Background(), full, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.Result["n"].(int64); n != 1 {
		t.Errorf("n = %v, want the state the call by the link left", res.Result["n"])
	}
	if got := owed.List(); !slices.Equal(got, []string{full}) {
		t.Errorf("owed = %v, want [%s] once", got, full)
	}
}

func TestActionCaller_DoneContext(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "greet", "")
	f := &fakeRun{}
	c := newTestCaller(action.Call{Actions: map[string]string{"greet": full}}, "", f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, "greet", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's", err)
	}
	if len(f.calls) != 0 {
		t.Error("an action was run on a done context")
	}
}

func TestWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	defer close(release)
	done := make(chan error, 1)
	go func() { done <- withContext(ctx, func() error { <-release; return nil }) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's before f returns", err)
	}
	if err := withContext(context.Background(), func() error { return io.EOF }); err != io.EOF {
		t.Errorf("err = %v, want f's", err)
	}
}

func TestActionCaller_HidesCredentialsInRecords(t *testing.T) {
	dir := t.TempDir()
	full := localAction(t, dir, "login", "")
	var out bytes.Buffer
	f := &fakeRun{}
	c := newTestCaller(action.Call{Actions: map[string]string{"login": full}}, "", f, WithCallerLogOutput(&out))
	c.runExternal = func(_ context.Context, _ *actionref.Executable, call action.Call, log hclog.Logger) (map[string]any, map[string]any, error) {
		log.Debug("logging in", "with", call.With)
		if call.With["password"] == nil {
			log.Debug("using the token", "token", "issued-token-0987654321")
		}
		return map[string]any{"res": map[string]any{"session_token": "issued-token-0987654321"}}, nil, nil
	}
	if _, err := c.Call(context.Background(), "login", map[string]any{"password": "given-password-1234"}); err != nil {
		t.Fatal(err)
	}
	// A credential the action returned is hidden from then on, as one a
	// step returns is.
	if _, err := c.Call(context.Background(), "login", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "logging in") {
		t.Fatalf("the records are not written: %s", out.String())
	}
	for _, secret := range []string{"given-password-1234", "issued-token-0987654321"} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("%s is shown in the records:\n%s", secret, out.String())
		}
	}
}

// An action called is stopped once the context of the call is done.
func TestActionCaller_StopsTheActionOnTimeout(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "hang", "hang"), "github.com/mozership/probe/testdata/hang-action").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "hang", "action.yml"), []byte("runs:\n  using: binary\n  path: hang\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(dir, "pid")
	c := NewActionCaller(action.Call{}, dir, WithCallerLogOutput(io.Discard))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Call(ctx, "./hang", map[string]any{"pidfile": pidfile})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline of the context", err)
	}
	if d := time.Since(start); d > time.Second+action.ExitWait {
		t.Errorf("the call returned after %s", d)
	}
	if _, err := os.Stat(pidfile + ".term"); err != nil {
		t.Errorf("the action was not sent SIGTERM: %v", err)
	}
}

// A fetch of an action.yml given up on keeps no other call waiting.
func TestActionCaller_FetchGivenUpOnKeepsNoCallWaiting(t *testing.T) {
	dir := t.TempDir()
	slow := localAction(t, dir, "slow", "")
	release := make(chan struct{})
	defer close(release)
	c := newTestCaller(action.Call{Actions: map[string]string{"slow": slow}}, "", &fakeRun{}, WithProbeExecutable("/bin/probe"))
	c.manifest = func(full string) (*actionref.Manifest, error) {
		<-release
		return actionref.ReadManifest(full, "")
	}
	c.runBuiltin = func(context.Context, string, string, action.Call, hclog.Logger) (map[string]any, map[string]any, error) {
		return map[string]any{"status": 0}, nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "slow", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline of the context", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "hello", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a call of another action waits for the fetch given up on")
	}
}
