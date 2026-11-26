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
	if want := []string{"workflow idle is never started: it has neither schedule nor loop, and no for_each runs it"}; !reflect.DeepEqual(warnings, want) {
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
		{"a schedule that never comes", "workflows:\n  w:\n    files: [w.yml]\n    schedule: '0 0 31 2 *'\n", "matches no time"},
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

func TestLoad_GraceOfNone(t *testing.T) {
	c, _, err := Load(writeServeFile(t, "grace: 0s\nworkflows:\n  w:\n    files: [w.yml]\n    loop: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Grace.Duration != 0 {
		t.Errorf("grace = %s, want none", c.Grace)
	}
	if c.dir == "" {
		t.Error("the directory of the serve file is not kept")
	}
}

func TestLoad_ForEach(t *testing.T) {
	c, warnings, err := Load(writeServeFile(t, `workflows:
  intake:
    files: [receive.yml]
    loop: true
    for_each:
      items: messages
      workflow: verify
      on_success: ack
      on_failure: none
  verify:
    files: [verify.yml]
  ack:
    files: [ack.yml]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q", warnings)
	}
	f := c.Workflows["intake"].ForEach
	if f.Concurrency != 1 || f.OnSuccess != "ack" || f.OnFailure != "" {
		t.Errorf("for_each = %+v", f)
	}
}

func TestLoad_ForEachWarnsWithoutOnSuccess(t *testing.T) {
	c, warnings, err := Load(writeServeFile(t, "workflows:\n  a:\n    files: [a.yml]\n    loop: true\n    for_each: {items: x, workflow: b}\n  b:\n    files: [b.yml]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "workflow a: for_each: has no on_success") {
		t.Errorf("warnings = %q", warnings)
	}
	if c.Workflows["a"].ForEach.OnSuccess != "" {
		t.Error("on_success is not empty")
	}

	_, warnings, err = Load(writeServeFile(t, "workflows:\n  a:\n    files: [a.yml]\n    loop: true\n    for_each: {items: x, workflow: b, on_success: none}\n  b:\n    files: [b.yml]\n"))
	if err != nil || len(warnings) != 0 {
		t.Errorf("on_success: none = %q, %v", warnings, err)
	}
}

func TestLoad_ForEachRefuses(t *testing.T) {
	base := "  b:\n    files: [b.yml]\n"
	tests := []struct{ name, forEach, more, want string }{
		{"no items", "{workflow: b, on_success: none}", "", "items names no output"},
		{"no workflow", "{items: x, on_success: none}", "", "workflow names none"},
		{"an unknown workflow", "{items: x, workflow: c, on_success: none}", "", "workflow: no workflow is named c"},
		{"an unknown on_success", "{items: x, workflow: b, on_success: c}", "", "on_success: no workflow is named c"},
		{"an unknown on_failure", "{items: x, workflow: b, on_success: none, on_failure: c}", "", "on_failure: no workflow is named c"},
		{"a workflow of none", "{items: x, workflow: none, on_success: none}", "", "workflow: no workflow is named none"},
		{"negative concurrency", "{items: x, workflow: b, on_success: none, concurrency: -1}", "", "concurrency must be 1 or more"},
		{"a for_each of its own", "{items: x, workflow: c, on_success: none}", "  c:\n    files: [c.yml]\n    for_each: {items: y, workflow: b, on_success: none}\n", "workflow: c has a for_each of its own"},
		{"an unknown key", "{items: x, workflow: b, on_succes: none}", "", "on_succes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "workflows:\n  a:\n    files: [a.yml]\n    loop: true\n    for_each: " + tt.forEach + "\n" + base + tt.more
			_, _, err := Load(writeServeFile(t, content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load = %v, want an error saying %q", err, tt.want)
			}
		})
	}
	_, _, err := Load(writeServeFile(t, "workflows:\n  none:\n    files: [a.yml]\n    loop: true\n"))
	if err == nil || !strings.Contains(err.Error(), "the name none is what on_success says to do nothing with") {
		t.Errorf("Load of a workflow named none = %v", err)
	}
}

func TestLoad_State(t *testing.T) {
	c, _, err := Load(writeServeFile(t, "workflows:\n  w:\n    files: [w.yml]\n    schedule: '* * * * *'\n    state: [cursor, seen]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Workflows["w"].State, []string{"cursor", "seen"}) {
		t.Errorf("state = %q", c.Workflows["w"].State)
	}
	tests := []struct{ name, content, want string }{
		{"without a schedule or a loop", "workflows:\n  w:\n    files: [w.yml]\n    state: [cursor]\n", "has a state, which is kept between the runs of a schedule or a loop, without either"},
		{"a name twice", "workflows:\n  w:\n    files: [w.yml]\n    loop: true\n    state: [cursor, cursor]\n", "state names cursor twice"},
		{"no name", "workflows:\n  w:\n    files: [w.yml]\n    loop: true\n    state: [' ']\n", "state names an output with no name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := Load(writeServeFile(t, tt.content)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}
