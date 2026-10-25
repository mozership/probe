package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJobFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "job.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadJobFile(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		job, err := ReadJobFile(writeJobFile(t, `name: j
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
			t.Fatalf("ReadJobFile() error = %v", err)
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
		job, err := ReadJobFile(writeJobFile(t, `name: j
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
			t.Fatalf("ReadJobFile() error = %v", err)
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
		job, err := ReadJobFile(writeJobFile(t, "name: j\nsteps:\n- uses: redis\n"), nil)
		if err != nil {
			t.Fatalf("ReadJobFile() error = %v", err)
		}
		if job.Steps[0].Uses != "redis" {
			t.Errorf("uses = %q, want it left as redis", job.Steps[0].Uses)
		}
	})

	t.Run("defaults written by the name and in full", func(t *testing.T) {
		_, err := ReadJobFile(writeJobFile(t, `name: j
defaults:
  redis: {url: a}
  `+redis+`: {url: b}
steps:
- uses: redis
`), names)
		if err == nil || !strings.Contains(err.Error(), "are the same action, whose defaults can be written once") {
			t.Fatalf("ReadJobFile() error = %v, want the defaults refused", err)
		}
	})

	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{name: "missing file", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "none.yml") }, wantErr: "does not exist"},
		{name: "invalid YAML", path: func(t *testing.T) string { return writeJobFile(t, "steps: [") }, wantErr: "failed to decode YAML job"},
		{name: "no steps", path: func(t *testing.T) string { return writeJobFile(t, "name: j\nsteps: []\n") }, wantErr: "no steps found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadJobFile(tt.path(t), nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ReadJobFile() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEmbeddedJobFile(t *testing.T) {
	tests := []struct {
		name   string
		st     *Step
		want   string
		wantOK bool
	}{
		{name: "no step"},
		{name: "another action", st: &Step{Uses: "http", With: map[string]any{"path": "job.yml"}}},
		{name: "a job file", st: &Step{Uses: "embedded", With: map[string]any{"path": "job.yml"}}, want: "job.yml", wantOK: true},
		{name: "no path", st: &Step{Uses: "embedded"}},
		{name: "an empty path", st: &Step{Uses: "embedded", With: map[string]any{"path": ""}}},
		{name: "a path that is not a string", st: &Step{Uses: "embedded", With: map[string]any{"path": 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := embeddedJobFile(tt.st)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("embeddedJobFile() = %q, %v, want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
