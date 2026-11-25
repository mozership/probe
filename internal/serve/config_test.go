package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeServeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serve.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeServeFile(t, `workers: 2
results:
  dir: out
  keep: 720h
grace: 5s
workflows:
  health:
    files: [workflows/health.yml, /abs/base.yml]
    env_files: [.env]
    vars:
      env: prod
      retries: 3
    jobs: [check]
    schedule: "*/5 * * * *"
    read_only: true
    allow_hosts: [api.example.com]
    allow_actions: [shell]
    timeout: 2m
  intake:
    files: [workflows/intake.yml]
    loop: true
    interval: 10s
`)
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q", warnings)
	}
	dir := filepath.Dir(path)
	if c.Workers != 2 || c.Results.Dir != filepath.Join(dir, "out") || c.Results.Keep.Duration != 720*time.Hour || c.Grace.Duration != 5*time.Second {
		t.Errorf("config = %+v", c)
	}
	h := c.Workflows["health"]
	if want := []string{filepath.Join(dir, "workflows/health.yml"), "/abs/base.yml"}; !reflect.DeepEqual(h.Files, want) {
		t.Errorf("files = %q, want %q", h.Files, want)
	}
	if want := []string{filepath.Join(dir, ".env")}; !reflect.DeepEqual(h.EnvFiles, want) {
		t.Errorf("env_files = %q, want %q", h.EnvFiles, want)
	}
	if h.schedule == nil || h.Timeout.Duration != 2*time.Minute || !h.ReadOnly {
		t.Errorf("health = %+v", h)
	}
	if i := c.Workflows["intake"]; !i.Loop || i.Interval.Duration != 10*time.Second || i.schedule != nil {
		t.Errorf("intake = %+v", i)
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeServeFile(t, `workflows:
  w:
    files: [w.yml]
    loop: true
`)
	c, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Workers != defaultWorkers || c.Results.Dir != filepath.Join(filepath.Dir(path), defaultResultsDir) || c.Results.Keep.Duration != 0 || c.Grace.Duration != defaultGrace {
		t.Errorf("config = %+v", c)
	}
}

func TestLoad_WarnsOfWorkflowNeverStarted(t *testing.T) {
	_, warnings, err := Load(writeServeFile(t, "workflows:\n  idle:\n    files: [w.yml]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"workflow idle is never started: it has neither schedule nor loop"}; !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings = %q, want %q", warnings, want)
	}
}

func TestLoad_Refuses(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"no workflows", "workers: 1\n", "workflows names none"},
		{"an unknown key", "workflows:\n  w:\n    files: [w.yml]\n    shedule: '* * * * *'\n", "shedule"},
		{"an unknown top key", "worker: 1\nworkflows:\n  w:\n    files: [w.yml]\n", "worker"},
		{"no files", "workflows:\n  w:\n    loop: true\n", "workflow w: names no files"},
		{"nothing", "workflows:\n  w:\n", "workflow w: names no files"},
		{"a bad name", "workflows:\n  'a b':\n    files: [w.yml]\n", "workflow a b: the name is to be"},
		{"both", "workflows:\n  w:\n    files: [w.yml]\n    loop: true\n    schedule: '* * * * *'\n", "has both schedule and loop"},
		{"interval without loop", "workflows:\n  w:\n    files: [w.yml]\n    interval: 1s\n", "without loop"},
		{"a bad schedule", "workflows:\n  w:\n    files: [w.yml]\n    schedule: '61 * * * *'\n", "schedule: minute of"},
		{"negative workers", "workers: -1\nworkflows:\n  w:\n    files: [w.yml]\n", "workers must be 1 or more"},
		{"a bad duration", "workflows:\n  w:\n    files: [w.yml]\n    timeout: soon\n", "soon: use a duration"},
		{"a negative duration", "workflows:\n  w:\n    files: [w.yml]\n    timeout: -1s\n", "-1s: a duration cannot be below zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeServeFile(t, tt.content)
			_, _, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load = %v, want an error saying %q", err, tt.want)
			}
			if err != nil && !strings.HasPrefix(err.Error(), path+": ") {
				t.Errorf("Load = %v, want the file named", err)
			}
		})
	}
}
