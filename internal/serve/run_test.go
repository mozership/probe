package serve

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestArgs(t *testing.T) {
	w := &Workflow{
		Files:        []string{"/w/a.yml", "/w/b.yml"},
		EnvFiles:     []string{"/w/.env"},
		Jobs:         []string{"receive", "ack"},
		ReadOnly:     true,
		AllowHosts:   []string{"api.example.com", "*.internal"},
		AllowActions: []string{"shell"},
	}
	want := []string{
		"--output", "stream",
		"--report", "json=/r/report.json",
		"--vars-file", "-",
		"--read-only=true",
		"--allow-host=api.example.com,*.internal",
		"--allow-action=shell",
		"--env-file", "/w/.env",
		"--job", "receive", "--job", "ack",
		"--", "/w/a.yml", "/w/b.yml",
	}
	if got := args(w, "/r/report.json"); !reflect.DeepEqual(got, want) {
		t.Errorf("args =\n%q\nwant\n%q", got, want)
	}

	// The guard is given in full when the serve file loosens it, so that
	// the environment of serve does not tighten it.
	got := args(&Workflow{Files: []string{"/w.yml"}}, "/r.json")
	for _, a := range []string{"--read-only=false", "--allow-host=", "--allow-action="} {
		if !slices.Contains(got, a) {
			t.Errorf("args = %q, want %s", got, a)
		}
	}
}

func TestChildEnv(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "PROBE_REPORT=json", "PROBE_BASELINE=b.json", "PROBE_OUTPUT=spinner", "PROBE_READ_ONLY=1", "PROBE_MAX_ATTEMPTS=3", "API_URL=x"})
	want := []string{"PATH=/bin", "PROBE_READ_ONLY=1", "PROBE_MAX_ATTEMPTS=3", "API_URL=x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("childEnv = %q, want %q", got, want)
	}
}

func TestVarsJSON(t *testing.T) {
	got, err := varsJSON(map[string]any{"env": "prod", "n": 1}, map[string]any{"n": 2, "item": map[string]any{"id": "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"env":"prod","item":{"id":"m1"},"n":2}`; string(got) != want {
		t.Errorf("varsJSON = %s, want %s", got, want)
	}
	if got, _ := varsJSON(nil, nil); string(got) != "{}" {
		t.Errorf("varsJSON of none = %s, want {}", got)
	}
}

func TestRunner_Start(t *testing.T) {
	tests := []struct {
		exit int
		want string
	}{
		{0, StatusPassed},
		{1, StatusFailed},
		{3, StatusFailed},
		{2, StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			rn := &runner{exe: fakeProbeExe(t, 0, tt.exit), results: t.TempDir(), grace: time.Second}
			t.Setenv("PROBE_REPORT", "markdown")
			w := &Workflow{Files: []string{"/w.yml"}, Vars: map[string]any{"env": "prod"}}
			r := rn.start(context.Background(), "health", w, map[string]any{"cursor": "7"})
			if r.Status != tt.want || r.ExitStatus != tt.exit || r.Err != nil {
				t.Errorf("run = %+v, want %s with %d", r, tt.want, tt.exit)
			}
			if !strings.HasPrefix(r.Dir, rn.results+"/health/") || !isRunID(r.ID) {
				t.Errorf("dir = %s, id = %s", r.Dir, r.ID)
			}
			fr := readFakeReport(t, r)
			if want := map[string]string{"env": "prod", "cursor": "7"}; !reflect.DeepEqual(fr.Vars, want) {
				t.Errorf("vars = %v, want %v", fr.Vars, want)
			}
			if slices.Contains(fr.Env, "PROBE_REPORT=markdown") {
				t.Error("the run was given PROBE_REPORT")
			}
			out, err := os.ReadFile(r.OutputPath())
			if err != nil || string(out) != "fake probe ran\n" {
				t.Errorf("output = %q, %v", out, err)
			}
		})
	}
}

// A run that goes on past its context is sent SIGTERM, and told as timed out
// or stopped by why the context ended.
func TestRunner_StartEndsRun(t *testing.T) {
	rn := &runner{exe: fakeProbeExe(t, time.Minute, 0), results: t.TempDir(), grace: time.Second}
	w := &Workflow{Files: []string{"/w.yml"}}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r := rn.start(ctx, "slow", w, nil)
	if r.Status != StatusTimeout || r.ExitStatus != -1 || r.FinishedAt.Sub(r.StartedAt) > 10*time.Second {
		t.Errorf("run = %+v, want it timed out", r)
	}

	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	if r := rn.start(ctx, "slow", w, nil); r.Status != StatusStopped {
		t.Errorf("run = %+v, want it stopped", r)
	}
}

func TestRunner_StartWithoutProbe(t *testing.T) {
	rn := &runner{exe: "/nonexistent/probe", results: t.TempDir(), grace: time.Second}
	r := rn.start(context.Background(), "w", &Workflow{Files: []string{"/w.yml"}}, nil)
	if r.Status != StatusError || r.Err == nil || r.ExitStatus != -1 {
		t.Errorf("run = %+v, want an error", r)
	}
}
